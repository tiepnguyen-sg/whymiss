package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (ADR-0007)
)

// Store is a handle to one whymiss SQLite database file.
//
// Exactly one goroutine should own the write path (ADR-0002); readers may
// use Store concurrently. Store does not itself enforce single-writer
// discipline — internal/app (the composition root) is where that
// invariant is wired, by construction, not by a runtime check here.
type Store struct {
	db *sql.DB
}

// Open opens (creating if absent) the SQLite database at path, sets WAL
// journal mode and synchronous=NORMAL (ADR-0002: durability of the last
// few observations is worth less than not competing with the node for
// disk I/O, I-5), and applies any migrations not yet recorded in the
// database's own PRAGMA user_version.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close() //nolint:errcheck,gosec // already returning the more relevant error below
			return nil, fmt.Errorf("open %s: %s: %w", path, pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close() //nolint:errcheck,gosec // already returning the more relevant error below
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies every migration whose version exceeds the database's
// current PRAGMA user_version, in order, each inside its own transaction
// (ADR-0002: "applied at startup inside a transaction").
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if err := s.applyMigration(ctx, m); err != nil {
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, m migration) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback() //nolint:errcheck,gosec // best-effort: the original error is what matters
		}
	}()

	if _, err = tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("run migration sql: %w", err)
	}
	// PRAGMA statements don't accept bound parameters; the version is this
	// package's own int, never operator- or attacker-supplied, so building
	// the statement directly is safe here.
	if _, err = tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil { //nolint:gosec // G202: m.version is an int from this package's own migrations slice
		return fmt.Errorf("set schema version: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
