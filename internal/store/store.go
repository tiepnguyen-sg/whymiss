package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sync"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (ADR-0007)
)

const (
	maxStoreConnections = 4
	busyTimeoutMS       = 5000
)

// Store is a handle to one whymiss SQLite database file.
//
// WriteObservation, WriteSample, and Prune serialize SQLite mutations. Readers
// may use Store concurrently. The guard is local because the composition root has
// several independent collectors and retention must never race their writes.
type Store struct {
	db      *sql.DB
	path    string
	writeMu sync.Mutex
}

// Open opens (creating if absent) the SQLite database at path, sets WAL
// journal mode and synchronous=NORMAL (ADR-0002: durability of the last
// few observations is worth less than not competing with the node for
// disk I/O, I-5), and applies any migrations not yet recorded in the
// database's own PRAGMA user_version.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", sqliteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(maxStoreConnections)
	db.SetMaxIdleConns(maxStoreConnections)
	if err := db.PingContext(ctx); err != nil {
		db.Close() //nolint:errcheck,gosec // already returning the connection error
		return nil, fmt.Errorf("open %s: connect: %w", path, err)
	}
	if err := enableIncrementalAutoVacuumForNewDatabase(ctx, db); err != nil {
		db.Close() //nolint:errcheck,gosec // already returning the configuration error
		return nil, fmt.Errorf("open %s: configure page reclamation: %w", path, err)
	}
	if err := enableWAL(ctx, db); err != nil {
		db.Close() //nolint:errcheck,gosec // already returning the configuration error
		return nil, fmt.Errorf("open %s: configure journal: %w", path, err)
	}

	var databasePath string
	if err := db.QueryRowContext(ctx, `SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&databasePath); err != nil {
		db.Close() //nolint:errcheck,gosec // returning the query error
		return nil, fmt.Errorf("resolve database path for %s: %w", path, err)
	}
	s := &Store{db: db, path: databasePath}
	if err := s.migrate(ctx); err != nil {
		db.Close() //nolint:errcheck,gosec // already returning the more relevant error below
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := s.validateSchema(ctx); err != nil {
		db.Close() //nolint:errcheck,gosec // already returning the validation error
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return s, nil
}

func enableWAL(ctx context.Context, db *sql.DB) error {
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		return fmt.Errorf("enable WAL: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("journal mode is %q, want wal", mode)
	}
	return nil
}

// enableIncrementalAutoVacuumForNewDatabase avoids a full-file VACUUM during
// normal retention. SQLite only permits enabling auto-vacuum without rebuilding
// the file before the first application table is created, so existing databases
// keep the legacy VACUUM fallback in Prune.
func enableIncrementalAutoVacuumForNewDatabase(ctx context.Context, db *sql.DB) error {
	var applicationObjects int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM sqlite_schema
WHERE name NOT LIKE 'sqlite_%' AND type IN ('table', 'index')`).Scan(&applicationObjects); err != nil {
		return fmt.Errorf("inspect schema: %w", err)
	}
	if applicationObjects != 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, "PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
		return fmt.Errorf("enable incremental auto-vacuum: %w", err)
	}
	var mode int
	if err := db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return fmt.Errorf("verify auto_vacuum: %w", err)
	}
	if mode != autoVacuumIncremental {
		return fmt.Errorf("auto_vacuum mode is %d, want incremental (%d)", mode, autoVacuumIncremental)
	}
	return nil
}

// sqliteDSN applies connection-local safety settings to every pooled SQLite
// connection. Executing PRAGMA once after sql.Open only configures whichever
// connection database/sql happened to choose for that statement.
func sqliteDSN(path string) string {
	u := &url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_auto_vacuum", "INCREMENTAL")
	q.Set("_busy_timeout", fmt.Sprintf("%d", busyTimeoutMS))
	q.Set("_defensive", "1")
	q.Set("_dqs", "0")
	q.Set("_foreign_keys", "1")
	q.Set("_journal_mode", "WAL")
	q.Add("_pragma", "trusted_schema(OFF)")
	q.Set("_synchronous", "NORMAL")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	return u.String()
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
	migrations := schemaMigrations()
	latest := migrations[len(migrations)-1].version
	if current > latest {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d)", current, latest)
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
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback migration %d: %w", m.version, rollbackErr))
			}
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
