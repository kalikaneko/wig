package sqlite

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// OpenDB opens a SQLite database and runs the database migrations.
func OpenDB(dburi string, migrations []Migration) (*sqlx.DB, error) {
	// Add sqlite3-specific parameters if none are already
	// specified in the connection URI.
	if !strings.Contains(dburi, "?") {
		dburi += "?cache=shared&_busy_timeout=10000&_journal=WAL"
	}

	db, err := sqlx.Open("sqlite3", dburi)
	if err != nil {
		return nil, err
	}

	// Limit the pool to a single connection.
	// https://github.com/mattn/go-sqlite3/issues/209
	db.SetMaxOpenConns(1)

	if err = migrate(db, migrations); err != nil {
		db.Close() // nolint
		return nil, err
	}

	return db, nil
}

func migrate(db *sqlx.DB, migrations []Migration) error {
	return WithTx(db, func(tx *sqlx.Tx) error {
		var idx int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&idx); err != nil {
			return fmt.Errorf("getting latest applied migration: %w", err)
		}

		if idx == len(migrations) {
			// Already fully migrated, nothing needed.
			return nil
		} else if idx > len(migrations) {
			return fmt.Errorf("database is at version %d, which is more recent than this binary understands", idx)
		}

		for i, f := range migrations[idx:] {
			if err := f(tx); err != nil {
				return fmt.Errorf("migration to version %d failed: %w", i+1, err)
			}
		}

		if n := len(migrations); n > 0 {
			// For some reason, ? substitution doesn't work in PRAGMA
			// statements, sqlite reports a parse error.
			if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", n)); err != nil {
				return fmt.Errorf("recording new DB version: %w", err)
			}
		}

		return nil
	})
}

// A Migration performs a mutation on the database schema.
type Migration func(*sqlx.Tx) error

// Statement for migrations, executes one or more SQL statements.
func Statement(idl ...string) func(*sqlx.Tx) error {
	return func(tx *sqlx.Tx) error {
		for _, stmt := range idl {
			if _, err := tx.Exec(stmt); err != nil {
				return err
			}
		}
		return nil
	}
}

// WithTx wraps a function in a SQL transaction.
func WithTx(db *sqlx.DB, f func(*sqlx.Tx) error) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback() // nolint: errcheck
		return err
	}
	return tx.Commit()
}

// ErrRollback is used to mark read-only transactions and automatically call Rollback() on them when returned from a WithTx caller.
var ErrRollback = errors.New("")
