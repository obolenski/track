package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

type EventType struct {
	ID   int
	Name string
}

type Occurrence struct {
	ID          int
	EventTypeID int
	Date        time.Time
}

type MonthlyCount struct {
	Year  int
	Month int
	Count int
}

func getDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "track.db"
	}
	return filepath.Join(home, ".track.db")
}

func Open() (*DB, error) {
	db, err := sql.Open("sqlite3", getDBPath())
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	d := &DB{db}
	if err := d.createTables(); err != nil {
		return nil, err
	}

	return d, nil
}

func (db *DB) createTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS event_types (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS occurrences (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type_id INTEGER NOT NULL,
			date DATE NOT NULL,
			FOREIGN KEY (event_type_id) REFERENCES event_types(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_occurrences_date ON occurrences(date)`,
		`CREATE INDEX IF NOT EXISTS idx_occurrences_event_type ON occurrences(event_type_id)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT
		)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) CreateEventType(name string) error {
	_, err := db.Exec("INSERT INTO event_types (name) VALUES (?)", name)
	return err
}

func (db *DB) RenameEventType(id int, newName string) error {
	result, err := db.Exec("UPDATE event_types SET name = ? WHERE id = ?", newName, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("event type not found")
	}
	return nil
}

func (db *DB) DeleteEventType(id int) error {
	// Remove occurrences first (no ON DELETE CASCADE in SQLite by default)
	if _, err := db.Exec("DELETE FROM occurrences WHERE event_type_id = ?", id); err != nil {
		return err
	}
	// Remove from settings if it was the default
	if _, err := db.Exec("DELETE FROM settings WHERE key = 'default_event_type' AND CAST(value AS INTEGER) = ?", id); err != nil {
		return err
	}
	result, err := db.Exec("DELETE FROM event_types WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("event type not found")
	}
	return nil
}

func (db *DB) CountOccurrencesForEventType(id int) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM occurrences WHERE event_type_id = ?", id).Scan(&count)
	return count, err
}

func (db *DB) SetDefaultEventType(name string) error {
	var id int
	if err := db.QueryRow("SELECT id FROM event_types WHERE name = ?", name).Scan(&id); err != nil {
		return fmt.Errorf("event type not found: %s", name)
	}
	_, err := db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('default_event_type', ?)`, id)
	return err
}

func (db *DB) SetDefaultEventTypeByID(id int) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('default_event_type', ?)`, id)
	return err
}

func (db *DB) GetDefaultEventType() (*EventType, error) {
	var et EventType
	err := db.QueryRow(`
		SELECT e.id, e.name FROM event_types e
		JOIN settings s ON CAST(s.value AS INTEGER) = e.id
		WHERE s.key = 'default_event_type'
	`).Scan(&et.ID, &et.Name)
	if err != nil {
		return nil, err
	}
	return &et, nil
}

func (db *DB) GetEventTypeByName(name string) (*EventType, error) {
	var et EventType
	err := db.QueryRow("SELECT id, name FROM event_types WHERE name = ?", name).Scan(&et.ID, &et.Name)
	if err != nil {
		return nil, fmt.Errorf("event type not found: %s", name)
	}
	return &et, nil
}

func (db *DB) GetAllEventTypes() ([]EventType, error) {
	rows, err := db.Query("SELECT id, name FROM event_types ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var types []EventType
	for rows.Next() {
		var et EventType
		if err := rows.Scan(&et.ID, &et.Name); err != nil {
			return nil, err
		}
		types = append(types, et)
	}

	return types, nil
}

func (db *DB) AddOccurrence(eventTypeID int, date time.Time) error {
	dateStr := date.Format("2006-01-02")
	_, err := db.Exec("INSERT INTO occurrences (event_type_id, date) VALUES (?, date(?))", eventTypeID, dateStr)
	return err
}

func parseDate(s string) (time.Time, error) {
	for _, format := range []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(format, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date format: %s", s)
}

func (db *DB) GetLastOccurrence(eventTypeID int) (*time.Time, error) {
	var dateStr string
	err := db.QueryRow(`
		SELECT date FROM occurrences
		WHERE event_type_id = ?
		ORDER BY date DESC
		LIMIT 1
	`, eventTypeID).Scan(&dateStr)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	date, err := parseDate(dateStr)
	if err != nil {
		return nil, err
	}
	return &date, nil
}

func (db *DB) GetMonthlyCounts(eventTypeID int) ([]MonthlyCount, error) {
	rows, err := db.Query(`
		SELECT
			CAST(strftime('%Y', date(date)) AS INTEGER) as year,
			CAST(strftime('%m', date(date)) AS INTEGER) as month,
			COUNT(*) as count
		FROM occurrences
		WHERE event_type_id = ?
		GROUP BY year, month
		ORDER BY year DESC, month DESC
	`, eventTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var counts []MonthlyCount
	for rows.Next() {
		var mc MonthlyCount
		if err := rows.Scan(&mc.Year, &mc.Month, &mc.Count); err != nil {
			return nil, err
		}
		counts = append(counts, mc)
	}

	return counts, nil
}

func (db *DB) GetLatestOccurrences(eventTypeID int, limit int) ([]time.Time, error) {
	rows, err := db.Query(`
		SELECT date FROM occurrences
		WHERE event_type_id = ?
		ORDER BY date DESC
		LIMIT ?
	`, eventTypeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dates []time.Time
	for rows.Next() {
		var dateStr string
		if err := rows.Scan(&dateStr); err != nil {
			return nil, err
		}
		date, err := parseDate(dateStr)
		if err != nil {
			return nil, err
		}
		dates = append(dates, date)
	}

	return dates, nil
}

func (db *DB) DeleteOccurrence(eventTypeID int, date time.Time) error {
	dateStr := date.Format("2006-01-02")
	result, err := db.Exec(`
		DELETE FROM occurrences
		WHERE id IN (
			SELECT id FROM occurrences
			WHERE event_type_id = ? AND date(date) = date(?)
			ORDER BY id DESC
			LIMIT 1
		)
	`, eventTypeID, dateStr)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no occurrence found for that date")
	}

	return nil
}
