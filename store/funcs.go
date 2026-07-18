package store

import (
	"database/sql"
	"fmt"
)

// ProbeFTS5 checks whether the linked SQLite build has FTS5 compiled in.
func ProbeFTS5(db *sql.DB) error {
	var enabled int
	if err := db.QueryRow("SELECT sqlite_compileoption_used('ENABLE_FTS5')").Scan(&enabled); err != nil {
		return fmt.Errorf("sqlite: FTS5 probe: %w", err)
	}
	if enabled == 0 {
		return fmt.Errorf("sqlite: FTS5 is not compiled into the linked modernc.org/sqlite build; this is required for memory search")
	}
	return nil
}
