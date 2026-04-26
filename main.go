package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"track/database"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mbndr/figlet4go"
	"github.com/spf13/cobra"
)

// figletRenderer is initialised once and reused for all banner renders.
var figletRenderer = figlet4go.NewAsciiRender()

func renderBanner(text string) string {
	s, err := figletRenderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(s, "\n")
}

// --- TUI ---------------------------------------------------------------

type mode int

const (
	modeNormal mode = iota
	modeAddingDate
	modeDeleting
	// Management screen modes
	modeManaging        // browsing the event list
	modeManageCreating  // typing a new event name
	modeManageRenaming  // typing a new name for selected event
	modeManageConfirmDelete // waiting for y/n confirmation
)

type model struct {
	db                *database.DB
	currentEventType  *database.EventType
	allEventTypes     []database.EventType
	lastOccurrence    *time.Time
	monthlyCounts     []database.MonthlyCount
	latestOccurrences []time.Time
	mode              mode
	input             textinput.Model
	message           string
	// management screen state
	managedIdx        int   // selected row in management list
	deleteOccCount    int   // occurrence count for the event pending deletion
	width             int
	height            int
	colors            ColorScheme
}

func newDateInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "YYYY-MM-DD"
	ti.CharLimit = 10
	ti.Width = 12
	ti.SetValue(time.Now().Format("2006-01-02"))
	ti.Focus()
	return ti
}

func newNameInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 64
	ti.Width = 32
	ti.Focus()
	return ti
}

func initialModel() (model, error) {
	db, err := database.Open()
	if err != nil {
		return model{}, err
	}

	eventType, err := db.GetDefaultEventType()
	if err != nil {
		return model{}, fmt.Errorf("no default event type set. Run: track create <name> && track default <name>")
	}

	allTypes, err := db.GetAllEventTypes()
	if err != nil {
		return model{}, err
	}

	lastOcc, err := db.GetLastOccurrence(eventType.ID)
	if err != nil {
		return model{}, err
	}

	counts, err := db.GetMonthlyCounts(eventType.ID)
	if err != nil {
		return model{}, err
	}

	latestOccs, err := db.GetLatestOccurrences(eventType.ID, 10)
	if err != nil {
		return model{}, err
	}

	return model{
		db:               db,
		currentEventType: eventType,
		allEventTypes:    allTypes,
		lastOccurrence:   lastOcc,
		monthlyCounts:    counts,
		latestOccurrences: latestOccs,
		mode:             modeNormal,
		input:            textinput.New(),
		colors:           NewColorScheme(),
	}, nil
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeNormal:
			return m.updateNormal(msg)
		case modeAddingDate:
			return m.updateAddingDate(msg)
		case modeDeleting:
			return m.updateDeleting(msg)
		case modeManaging:
			return m.updateManaging(msg)
		case modeManageCreating:
			return m.updateManageCreating(msg)
		case modeManageRenaming:
			return m.updateManageRenaming(msg)
		case modeManageConfirmDelete:
			return m.updateManageConfirmDelete(msg)
		}
	}

	return m, nil
}

// ---- Normal mode ----

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "a":
		m.mode = modeAddingDate
		m.input = newDateInput()
		m.message = ""
		return m, textinput.Blink

	case "t", "T":
		if err := m.addOccurrence(time.Now().Format("2006-01-02")); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
		} else {
			m.message = "+ Added occurrence for today"
			m.refreshData()
		}
		return m, nil

	case "m":
		m.mode = modeManaging
		m.message = ""
		// Pre-select current event in the list
		for i, et := range m.allEventTypes {
			if et.ID == m.currentEventType.ID {
				m.managedIdx = i
				break
			}
		}
		return m, nil

	case "d":
		m.mode = modeDeleting
		m.input = newDateInput()
		m.message = ""
		return m, textinput.Blink
	}

	return m, nil
}

// ---- Date-adding mode ----

func (m model) updateAddingDate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.message = ""
		return m, nil

	case "enter":
		if err := m.addOccurrence(m.input.Value()); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
		} else {
			m.message = "+ Added occurrence"
			m.mode = modeNormal
			m.refreshData()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---- Occurrence-deletion mode ----

func (m model) updateDeleting(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.message = ""
		return m, nil

	case "enter":
		date, err := time.Parse("2006-01-02", m.input.Value())
		if err != nil {
			m.message = "Error: invalid date format"
		} else if err := m.db.DeleteOccurrence(m.currentEventType.ID, date); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
		} else {
			m.message = "- Deleted occurrence"
			m.mode = modeNormal
			m.refreshData()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---- Management screen ----

func (m model) updateManaging(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeNormal
		m.message = ""
		return m, nil

	case "up", "k":
		if m.managedIdx > 0 {
			m.managedIdx--
		}
		return m, nil

	case "down", "j":
		if m.managedIdx < len(m.allEventTypes)-1 {
			m.managedIdx++
		}
		return m, nil

	case "enter":
		// Select event as active
		if len(m.allEventTypes) > 0 {
			m.currentEventType = &m.allEventTypes[m.managedIdx]
			m.mode = modeNormal
			m.message = fmt.Sprintf("Switched to: %s", m.currentEventType.Name)
			m.refreshData()
		}
		return m, nil

	case "n":
		// Create new event
		m.mode = modeManageCreating
		m.input = newNameInput("e.g. headache")
		m.message = ""
		return m, textinput.Blink

	case "r":
		// Rename selected event
		if len(m.allEventTypes) == 0 {
			return m, nil
		}
		m.mode = modeManageRenaming
		m.input = newNameInput("new name")
		m.input.SetValue(m.allEventTypes[m.managedIdx].Name)
		m.input.CursorEnd()
		m.message = ""
		return m, textinput.Blink

	case "D":
		// Delete selected event (capital D to avoid accidents)
		if len(m.allEventTypes) == 0 {
			return m, nil
		}
		count, err := m.db.CountOccurrencesForEventType(m.allEventTypes[m.managedIdx].ID)
		if err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
			return m, nil
		}
		m.deleteOccCount = count
		m.mode = modeManageConfirmDelete
		return m, nil

	case "s":
		// Set as default
		if len(m.allEventTypes) == 0 {
			return m, nil
		}
		et := m.allEventTypes[m.managedIdx]
		if err := m.db.SetDefaultEventTypeByID(et.ID); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
		} else {
			m.message = fmt.Sprintf("Default set to: %s", et.Name)
			m.refreshData()
		}
		return m, nil
	}

	return m, nil
}

func (m model) updateManageCreating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeManaging
		m.message = ""
		return m, nil

	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.message = "Error: name cannot be empty"
			return m, nil
		}
		if err := m.db.CreateEventType(name); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
		} else {
			m.refreshData()
			// Select the newly created event in the list
			for i, et := range m.allEventTypes {
				if et.Name == name {
					m.managedIdx = i
					break
				}
			}
			m.message = fmt.Sprintf("Created: %s", name)
			m.mode = modeManaging
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateManageRenaming(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeManaging
		m.message = ""
		return m, nil

	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.message = "Error: name cannot be empty"
			return m, nil
		}
		et := m.allEventTypes[m.managedIdx]
		if err := m.db.RenameEventType(et.ID, name); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
		} else {
			// If the renamed event is the current one, update it
			if m.currentEventType.ID == et.ID {
				m.currentEventType.Name = name
			}
			m.refreshData()
			// Re-find the renamed event in the refreshed list
			for i, e := range m.allEventTypes {
				if e.ID == et.ID {
					m.managedIdx = i
					break
				}
			}
			m.message = fmt.Sprintf("Renamed to: %s", name)
			m.mode = modeManaging
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateManageConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n", "N":
		m.mode = modeManaging
		m.message = "Deletion cancelled"
		return m, nil

	case "y", "Y":
		et := m.allEventTypes[m.managedIdx]
		wasActive := m.currentEventType.ID == et.ID
		if err := m.db.DeleteEventType(et.ID); err != nil {
			m.message = fmt.Sprintf("Error: %v", err)
			m.mode = modeManaging
			return m, nil
		}
		m.refreshData()
		// Clamp index
		if m.managedIdx >= len(m.allEventTypes) {
			m.managedIdx = len(m.allEventTypes) - 1
		}
		if m.managedIdx < 0 {
			m.managedIdx = 0
		}
		// If we deleted the active event, switch to whatever is selected (or none)
		if wasActive {
			if len(m.allEventTypes) > 0 {
				m.currentEventType = &m.allEventTypes[m.managedIdx]
				m.refreshData()
				m.message = fmt.Sprintf("Deleted. Switched to: %s", m.currentEventType.Name)
			} else {
				// No events left — return to normal with no current event
				m.currentEventType = nil
				m.mode = modeNormal
				m.message = "All event types deleted"
				return m, nil
			}
		} else {
			m.message = fmt.Sprintf("Deleted: %s", et.Name)
		}
		m.mode = modeManaging
		return m, nil
	}

	return m, nil
}

// ---- Helpers ----

func (m *model) addOccurrence(dateStr string) error {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return fmt.Errorf("invalid date format (use YYYY-MM-DD)")
	}
	return m.db.AddOccurrence(m.currentEventType.ID, date)
}

func (m *model) refreshData() {
	allTypes, _ := m.db.GetAllEventTypes()
	m.allEventTypes = allTypes

	if m.currentEventType == nil {
		return
	}

	// Ensure currentEventType still exists after refresh
	found := false
	for _, et := range m.allEventTypes {
		if et.ID == m.currentEventType.ID {
			found = true
			break
		}
	}
	if !found {
		return
	}

	lastOcc, _ := m.db.GetLastOccurrence(m.currentEventType.ID)
	m.lastOccurrence = lastOcc

	counts, _ := m.db.GetMonthlyCounts(m.currentEventType.ID)
	m.monthlyCounts = counts

	latestOccs, _ := m.db.GetLatestOccurrences(m.currentEventType.ID, 10)
	m.latestOccurrences = latestOccs
}

// ---- View ----

func (m model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Management screen takes over the full view
	if m.mode == modeManaging ||
		m.mode == modeManageCreating ||
		m.mode == modeManageRenaming ||
		m.mode == modeManageConfirmDelete {
		return m.viewManagement()
	}

	var b strings.Builder

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.colors.Primary).
		MarginBottom(1)

	if m.currentEventType != nil {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Track: %s", m.currentEventType.Name)))
	} else {
		b.WriteString(headerStyle.Render("Track"))
	}
	b.WriteString("\n\n")

	bannerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(m.colors.Secondary).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.colors.Secondary).
		Padding(1, 2)

	var bannerContent string
	if m.currentEventType == nil || m.lastOccurrence == nil {
		bannerContent = renderBanner("NO DATA")
	} else {
		bannerContent = renderBanner(fmt.Sprintf("%d DAYS", daysSince(m.lastOccurrence)))
	}
	b.WriteString(bannerStyle.Render(bannerContent))
	b.WriteString("\n\n")

	if m.currentEventType != nil {
		b.WriteString(m.renderLatestEvents())
		b.WriteString("\n")
		b.WriteString(m.renderMonthlyTable())
	}

	b.WriteString("\n")

	switch m.mode {
	case modeAddingDate:
		inputStyle := lipgloss.NewStyle().Foreground(m.colors.Accent)
		b.WriteString(inputStyle.Render("Enter date (YYYY-MM-DD): "))
		b.WriteString(m.input.View())
		b.WriteString("\n")
	case modeDeleting:
		inputStyle := lipgloss.NewStyle().Foreground(m.colors.Danger)
		b.WriteString(inputStyle.Render("Delete occurrence on date (YYYY-MM-DD): "))
		b.WriteString(m.input.View())
		b.WriteString("\n")
	}

	if m.message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(m.colors.Success).Italic(true)
		b.WriteString("\n")
		b.WriteString(msgStyle.Render(m.message))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(m.colors.DimMuted)
	switch m.mode {
	case modeNormal:
		b.WriteString(helpStyle.Render("a: add occurrence  t: add today  d: delete occurrence  m: manage events  q: quit"))
	case modeAddingDate:
		b.WriteString(helpStyle.Render("enter: confirm  esc: cancel"))
	case modeDeleting:
		b.WriteString(helpStyle.Render("enter: confirm  esc: cancel"))
	}

	return b.String()
}

func (m model) viewManagement() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(m.colors.Primary)
	b.WriteString(titleStyle.Render("Manage event types"))
	b.WriteString("\n\n")

	selectedStyle := lipgloss.NewStyle().
		Foreground(m.colors.SelectionFg).
		Background(m.colors.SelectionBg).
		Bold(true)
	normalStyle   := lipgloss.NewStyle().Foreground(m.colors.Muted)
	defaultMark   := lipgloss.NewStyle().Foreground(m.colors.Accent)
	dimStyle      := lipgloss.NewStyle().Foreground(m.colors.DimMuted)

	defaultType, _ := m.db.GetDefaultEventType()

	if len(m.allEventTypes) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(m.colors.DimMuted).Italic(true)
		b.WriteString(emptyStyle.Render("No event types yet. Press n to create one."))
		b.WriteString("\n")
	}

	for i, et := range m.allEventTypes {
		isDefault := defaultType != nil && et.ID == defaultType.ID
		isCurrent := m.currentEventType != nil && et.ID == m.currentEventType.ID

		suffix := ""
		if isDefault && isCurrent {
			suffix = "  " + defaultMark.Render("[default, active]")
		} else if isDefault {
			suffix = "  " + defaultMark.Render("[default]")
		} else if isCurrent {
			suffix = "  " + dimStyle.Render("[active]")
		}

		label := et.Name

		if i == m.managedIdx {
			b.WriteString(selectedStyle.Render("> "+label))
		} else {
			b.WriteString(normalStyle.Render("  "+label))
		}
		b.WriteString(suffix)
		b.WriteString("\n")
	}

	b.WriteString("\n")

	// Inline input or confirmation prompt
	switch m.mode {
	case modeManageCreating:
		inputStyle := lipgloss.NewStyle().Foreground(m.colors.Accent)
		b.WriteString(inputStyle.Render("New event name: "))
		b.WriteString(m.input.View())
		b.WriteString("\n")

	case modeManageRenaming:
		if len(m.allEventTypes) > 0 {
			inputStyle := lipgloss.NewStyle().Foreground(m.colors.Accent)
			b.WriteString(inputStyle.Render(fmt.Sprintf("Rename \"%s\" to: ", m.allEventTypes[m.managedIdx].Name)))
			b.WriteString(m.input.View())
			b.WriteString("\n")
		}

	case modeManageConfirmDelete:
		if len(m.allEventTypes) > 0 {
			et := m.allEventTypes[m.managedIdx]
			warnStyle := lipgloss.NewStyle().Foreground(m.colors.Danger)
			dimStyle2  := lipgloss.NewStyle().Foreground(m.colors.DimMuted)
			occStr := "no occurrences"
			if m.deleteOccCount == 1 {
				occStr = "1 occurrence"
			} else if m.deleteOccCount > 1 {
				occStr = fmt.Sprintf("%d occurrences", m.deleteOccCount)
			}
			b.WriteString(warnStyle.Render(fmt.Sprintf(
				"Delete \"%s\" and all its data (%s)? [y/n]",
				et.Name, occStr,
			)))
			b.WriteString(dimStyle2.Render("  This cannot be undone."))
			b.WriteString("\n")
		}
	}

	if m.message != "" {
		msgStyle := lipgloss.NewStyle().Foreground(m.colors.Success).Italic(true)
		b.WriteString("\n")
		b.WriteString(msgStyle.Render(m.message))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().Foreground(m.colors.DimMuted)
	switch m.mode {
	case modeManaging:
		b.WriteString(helpStyle.Render("enter: select  n: new  r: rename  s: set default  D: delete  esc: back"))
	case modeManageCreating, modeManageRenaming:
		b.WriteString(helpStyle.Render("enter: confirm  esc: cancel"))
	case modeManageConfirmDelete:
		b.WriteString(helpStyle.Render("y: confirm delete  n/esc: cancel"))
	}

	return b.String()
}

func daysSince(t *time.Time) int {
	if t == nil {
		return 0
	}
	return int(time.Since(*t).Hours() / 24)
}

func (m model) renderLatestEvents() string {
	if len(m.latestOccurrences) == 0 {
		return ""
	}

	headerStyle  := lipgloss.NewStyle().Bold(true).Foreground(m.colors.Primary)
	dateStyle    := lipgloss.NewStyle().Foreground(m.colors.Muted)
	daysAgoStyle := lipgloss.NewStyle().Foreground(m.colors.DimMuted).Italic(true)
	gapStyle     := lipgloss.NewStyle().Foreground(m.colors.DimMuted)

	var b strings.Builder
	b.WriteString(headerStyle.Render("Latest events:"))
	b.WriteString("\n")

	slice := m.latestOccurrences[:min(5, len(m.latestOccurrences))]
	now := time.Now()
	for i, occ := range slice {
		daysAgo := int(now.Sub(occ).Hours() / 24)
		var daysAgoStr string
		switch daysAgo {
		case 0:
			daysAgoStr = "today"
		case 1:
			daysAgoStr = "yesterday"
		default:
			daysAgoStr = fmt.Sprintf("%d days ago", daysAgo)
		}
		b.WriteString(dateStyle.Render(fmt.Sprintf("  * %s ", occ.Format("2006-01-02 (Mon)"))))
		b.WriteString(daysAgoStyle.Render(daysAgoStr))
		if i+1 < len(slice) {
			gap := int(occ.Sub(slice[i+1]).Hours() / 24)
			b.WriteString(gapStyle.Render(fmt.Sprintf("  (+%d)", gap)))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func groupByYear(counts []database.MonthlyCount) map[int][]database.MonthlyCount {
	groups := make(map[int][]database.MonthlyCount)
	for _, mc := range counts {
		groups[mc.Year] = append(groups[mc.Year], mc)
	}
	return groups
}

func sortedYearsDesc(yearGroups map[int][]database.MonthlyCount) []int {
	years := make([]int, 0, len(yearGroups))
	for year := range yearGroups {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years
}

func (m model) renderMonthlyTable() string {
	if len(m.monthlyCounts) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(m.colors.DimMuted).Italic(true)
		return emptyStyle.Render("No occurrences recorded yet.")
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(m.colors.Primary)

	colYear  := lipgloss.NewStyle().Foreground(m.colors.Primary).Bold(true).Width(8)
	colMonth := lipgloss.NewStyle().Foreground(m.colors.Primary).Bold(true).Width(12)
	colCount := lipgloss.NewStyle().Foreground(m.colors.Primary).Bold(true).Width(8)

	rowYear  := lipgloss.NewStyle().Foreground(m.colors.Muted).Width(8)
	rowMonth := lipgloss.NewStyle().Foreground(m.colors.Muted).Width(12)
	rowCount := lipgloss.NewStyle().Foreground(m.colors.Muted).Width(8)

	var b strings.Builder
	b.WriteString(headerStyle.Render("Monthly occurrences:"))
	b.WriteString("\n\n")

	b.WriteString(colYear.Render("Year"))
	b.WriteString(colMonth.Render("Month"))
	b.WriteString(colCount.Render("Count"))
	b.WriteString("\n\n")

	yearGroups := groupByYear(m.monthlyCounts)
	for _, year := range sortedYearsDesc(yearGroups) {
		for _, mc := range yearGroups[year] {
			b.WriteString(rowYear.Render(fmt.Sprintf("%d", mc.Year)))
			b.WriteString(rowMonth.Render(time.Month(mc.Month).String()))
			b.WriteString(rowCount.Render(fmt.Sprintf("%d", mc.Count)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func runTUI() error {
	m, err := initialModel()
	if err != nil {
		return err
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// --- CLI ---------------------------------------------------------------

func openDB() (*database.DB, error) {
	return database.Open()
}

func resolveEventType(db *database.DB, name string) (*database.EventType, error) {
	if name != "" {
		return db.GetEventTypeByName(name)
	}
	return db.GetDefaultEventType()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "track",
		Short: "Track occurrences of events over time",
		Long:  "Track is a TUI + CLI tool for recording and reviewing how often events occur.\nRun without arguments to open the interactive TUI.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
		SilenceUsage: true,
	}

	root.AddCommand(
		newCreateCmd(),
		newDefaultCmd(),
		newListCmd(),
		newAddCmd(),
		newStatsCmd(),
	)

	return root
}

func newCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new event type",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			name := strings.Join(args, " ")
			if err := db.CreateEventType(name); err != nil {
				return err
			}
			fmt.Printf("Created event type: %s\n", name)
			return nil
		},
	}
}

func newDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default <name>",
		Short: "Set the default event type",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			name := strings.Join(args, " ")
			if err := db.SetDefaultEventType(name); err != nil {
				return err
			}
			fmt.Printf("Set default event type: %s\n", name)
			return nil
		},
	}
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all event types",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			types, err := db.GetAllEventTypes()
			if err != nil {
				return err
			}
			defaultType, _ := db.GetDefaultEventType()

			fmt.Println("Event types:")
			for _, et := range types {
				marker := " "
				if defaultType != nil && et.ID == defaultType.ID {
					marker = "*"
				}
				fmt.Printf("%s %s\n", marker, et.Name)
			}
			return nil
		},
	}
}

func newAddCmd() *cobra.Command {
	var eventName string

	cmd := &cobra.Command{
		Use:   "add [date]",
		Short: "Add an occurrence (default: today)",
		Long: `Add an occurrence for the default event type, or a specific one with --event.
Date must be in YYYY-MM-DD format. Defaults to today if omitted.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			et, err := resolveEventType(db, eventName)
			if err != nil {
				return err
			}

			dateStr := time.Now().Format("2006-01-02")
			if len(args) == 1 {
				dateStr = args[0]
			}

			date, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return fmt.Errorf("invalid date %q (use YYYY-MM-DD)", dateStr)
			}

			if err := db.AddOccurrence(et.ID, date); err != nil {
				return err
			}
			fmt.Printf("Added occurrence for %s on %s\n", et.Name, dateStr)
			return nil
		},
	}

	cmd.Flags().StringVarP(&eventName, "event", "e", "", "Event type name (defaults to the default event type)")
	return cmd
}

func newStatsCmd() *cobra.Command {
	var eventName string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show statistics for an event type",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			et, err := resolveEventType(db, eventName)
			if err != nil {
				return err
			}

			lastOcc, err := db.GetLastOccurrence(et.ID)
			if err != nil {
				return err
			}

			counts, err := db.GetMonthlyCounts(et.ID)
			if err != nil {
				return err
			}

			fmt.Printf("Statistics for: %s\n\n", et.Name)
			if lastOcc == nil {
				fmt.Println("No occurrences recorded.")
				return nil
			}

			fmt.Printf("Days since last occurrence: %d\n", daysSince(lastOcc))
			fmt.Printf("Last occurrence: %s\n\n", lastOcc.Format("2006-01-02"))

			if len(counts) > 0 {
				fmt.Println("Monthly counts:")
				yearGroups := groupByYear(counts)
				for _, year := range sortedYearsDesc(yearGroups) {
					fmt.Printf("  %d:\n", year)
					for _, mc := range yearGroups[year] {
						fmt.Printf("    %s: %d\n", time.Month(mc.Month).String(), mc.Count)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&eventName, "event", "e", "", "Event type name (defaults to the default event type)")
	return cmd
}

// --- Entry point -------------------------------------------------------

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
