package store

import (
	"context"
	"database/sql"
)

// Optimize keeps SQLite's query planner statistics current (v1.13.5): run
// at start and daily. Without them the planner guessed, e.g. read a drawer's
// seven documents by walking the whole space in date order. It analyzes
// only the tables that changed a lot since; fully, as a sampled analysis
// misjudged how many documents a space has (half a second at 50,000
// documents). Postgres gathers its own statistics (autovacuum).
func Optimize(ctx context.Context, db *sql.DB, postgres bool) error {
	if postgres {
		return nil
	}
	_, err := db.ExecContext(ctx, `PRAGMA optimize = 0x10002`)
	return err
}
