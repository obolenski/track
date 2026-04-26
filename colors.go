package main

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ColorScheme holds the adaptive colors based on terminal capabilities
type ColorScheme struct {
	Primary     lipgloss.Color
	Secondary   lipgloss.Color
	Accent      lipgloss.Color
	Success     lipgloss.Color
	Danger      lipgloss.Color
	Muted       lipgloss.Color
	DimMuted    lipgloss.Color
	SelectionBg lipgloss.Color
	SelectionFg lipgloss.Color
}

// NewColorScheme detects terminal capabilities and returns an appropriate color scheme
func NewColorScheme() ColorScheme {
	profile := termenv.ColorProfile()

	if profile >= termenv.TrueColor {
		return ColorScheme{
			Primary:     lipgloss.Color("212"),
			Secondary:   lipgloss.Color("86"),
			Accent:      lipgloss.Color("205"),
			Success:     lipgloss.Color("42"),
			Danger:      lipgloss.Color("196"),
			Muted:       lipgloss.Color("240"),
			DimMuted:    lipgloss.Color("250"),
			SelectionBg: lipgloss.Color("86"),
			SelectionFg: lipgloss.Color("0"),
		}
	}

	if profile >= termenv.ANSI256 {
		return ColorScheme{
			Primary:     lipgloss.Color("5"),
			Secondary:   lipgloss.Color("6"),
			Accent:      lipgloss.Color("5"),
			Success:     lipgloss.Color("2"),
			Danger:      lipgloss.Color("1"),
			Muted:       lipgloss.Color("7"),
			DimMuted:    lipgloss.Color("8"),
			SelectionBg: lipgloss.Color("6"),
			SelectionFg: lipgloss.Color("0"),
		}
	}

	return ColorScheme{
		Primary:     lipgloss.Color("5"),
		Secondary:   lipgloss.Color("6"),
		Accent:      lipgloss.Color("5"),
		Success:     lipgloss.Color("2"),
		Danger:      lipgloss.Color("1"),
		Muted:       lipgloss.Color("7"),
		DimMuted:    lipgloss.Color("8"),
		SelectionBg: lipgloss.Color("6"),
		SelectionFg: lipgloss.Color("0"),
	}
}
