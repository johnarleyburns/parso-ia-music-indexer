package db

import (
	"database/sql"
	"time"
)

type CursorState struct {
	Cursor       string
	ItemsIndexed int
}

func GetCursor(db *sql.DB) (*CursorState, error) {
	var state CursorState
	err := db.QueryRow(
		`SELECT last_cursor, items_indexed FROM cursor_state WHERE id = 1`,
	).Scan(&state.Cursor, &state.ItemsIndexed)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func SaveCursor(db *sql.DB, cursor string, itemsIndexed int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(
		`UPDATE cursor_state SET last_cursor = ?, items_indexed = ?, last_run_at = ? WHERE id = 1`,
		cursor, itemsIndexed, now,
	)
	return err
}
