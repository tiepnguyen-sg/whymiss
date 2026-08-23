package store

import (
	"context"
	"fmt"
)

// migration is one forward-only schema change, numbered starting at 1.
// SQLite's own PRAGMA user_version holds which migration a database has
// applied — no separate schema_version table needed for that (ADR-0002:
// "the schema version lives in the database").
type migration struct {
	version int
	sql     string
}

// schemaMigrations returns migrations in application order for one Open call, up
// to the highest version not yet recorded in PRAGMA user_version. Append
// only — an already-shipped migration's SQL text is immutable, since a
// database that already ran it must never see it change underneath it.
// Returning a fresh slice prevents package-level mutable schema state.
func schemaMigrations() []migration {
	return []migration{
		{
			version: 1,
			sql: `
CREATE TABLE observations (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	slot         INTEGER NOT NULL,
	kind         TEXT NOT NULL,
	at           TEXT NOT NULL,
	clock_offset_ns INTEGER NOT NULL,
	source       TEXT NOT NULL,
	attrs        TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_observations_slot ON observations(slot);
CREATE INDEX idx_observations_at ON observations(at);

CREATE TABLE samples (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	at        TEXT NOT NULL,
	component TEXT NOT NULL,
	name      TEXT NOT NULL,
	value     REAL NOT NULL,
	source    TEXT NOT NULL
);
CREATE INDEX idx_samples_at ON samples(at);
CREATE INDEX idx_samples_component_name ON samples(component, name);
`,
		},
		{
			// clock_offset_ns alone can't distinguish "measured zero" from
			// "never sampled" (I-9) — a stored row needs its own flag, not just
			// the in-memory struct.
			version: 2,
			sql:     `ALTER TABLE observations ADD COLUMN clock_measured INTEGER NOT NULL DEFAULT 0;`,
		},
		{
			// A measured bit and offset do not say when the measurement happened,
			// so they cannot prove it was fresh for the observation (I-9). Keep the
			// empty default for pre-v3 rows: legacy data remains honestly untrusted.
			version: 3,
			sql:     `ALTER TABLE observations ADD COLUMN clock_sample_at TEXT NOT NULL DEFAULT '';`,
		},
		{
			version: 4,
			sql: `ALTER TABLE samples ADD COLUMN clock_offset_ns INTEGER NOT NULL DEFAULT 0;
ALTER TABLE samples ADD COLUMN clock_measured INTEGER NOT NULL DEFAULT 0;
ALTER TABLE samples ADD COLUMN clock_sample_at TEXT NOT NULL DEFAULT '';`,
		},
	}
}

type expectedColumn struct {
	name     string
	typeName string
	notNull  bool
	primary  bool
}

func schemaTables() []struct {
	name    string
	columns []expectedColumn
} {
	return []struct {
		name    string
		columns []expectedColumn
	}{
		{name: "observations", columns: []expectedColumn{
			{name: "id", typeName: "INTEGER", primary: true},
			{name: "slot", typeName: "INTEGER", notNull: true},
			{name: "kind", typeName: "TEXT", notNull: true},
			{name: "at", typeName: "TEXT", notNull: true},
			{name: "clock_offset_ns", typeName: "INTEGER", notNull: true},
			{name: "source", typeName: "TEXT", notNull: true},
			{name: "attrs", typeName: "TEXT", notNull: true},
			{name: "clock_measured", typeName: "INTEGER", notNull: true},
			{name: "clock_sample_at", typeName: "TEXT", notNull: true},
		}},
		{name: "samples", columns: []expectedColumn{
			{name: "id", typeName: "INTEGER", primary: true},
			{name: "at", typeName: "TEXT", notNull: true},
			{name: "component", typeName: "TEXT", notNull: true},
			{name: "name", typeName: "TEXT", notNull: true},
			{name: "value", typeName: "REAL", notNull: true},
			{name: "source", typeName: "TEXT", notNull: true},
			{name: "clock_offset_ns", typeName: "INTEGER", notNull: true},
			{name: "clock_measured", typeName: "INTEGER", notNull: true},
			{name: "clock_sample_at", typeName: "TEXT", notNull: true},
		}},
	}
}

func schemaIndexes() []struct {
	name, table string
	columns     []string
} {
	return []struct {
		name, table string
		columns     []string
	}{
		{name: "idx_observations_slot", table: "observations", columns: []string{"slot"}},
		{name: "idx_observations_at", table: "observations", columns: []string{"at"}},
		{name: "idx_samples_at", table: "samples", columns: []string{"at"}},
		{name: "idx_samples_component_name", table: "samples", columns: []string{"component", "name"}},
	}
}

// validateSchema fails during startup instead of letting a manipulated or partial
// database silently drop every subsequent observation. Extra indexes are allowed;
// the application-owned tables and required indexes must match this version.
func (s *Store) validateSchema(ctx context.Context) error {
	for _, table := range schemaTables() {
		actual, err := s.inspectTableColumns(ctx, table.name)
		if err != nil {
			return err
		}
		if len(actual) != len(table.columns) {
			return fmt.Errorf("schema mismatch for table %s: got %d columns, want %d", table.name, len(actual), len(table.columns))
		}
		for i := range table.columns {
			if actual[i] != table.columns[i] {
				return fmt.Errorf("schema mismatch for table %s column %d: got %+v, want %+v", table.name, i, actual[i], table.columns[i])
			}
		}
	}
	for _, index := range schemaIndexes() {
		var table string
		if err := s.db.QueryRowContext(ctx, `SELECT tbl_name FROM sqlite_schema WHERE type = 'index' AND name = ?`, index.name).Scan(&table); err != nil {
			return fmt.Errorf("inspect index %s: %w", index.name, err)
		}
		if table != index.table {
			return fmt.Errorf("schema mismatch: index %s belongs to table %s, want %s", index.name, table, index.table)
		}
		columns, err := s.inspectIndexColumns(ctx, index.name)
		if err != nil {
			return err
		}
		if len(columns) != len(index.columns) {
			return fmt.Errorf("schema mismatch: index %s has columns %v, want %v", index.name, columns, index.columns)
		}
		for i := range columns {
			if columns[i] != index.columns[i] {
				return fmt.Errorf("schema mismatch: index %s has columns %v, want %v", index.name, columns, index.columns)
			}
		}
	}
	return nil
}

func (s *Store) inspectTableColumns(ctx context.Context, table string) (_ []expectedColumn, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, type, "notnull", pk FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return nil, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close table %s inspection: %w", table, closeErr)
		}
	}()
	var columns []expectedColumn
	for rows.Next() {
		var column expectedColumn
		var notNull, primary int
		if err := rows.Scan(&column.name, &column.typeName, &notNull, &primary); err != nil {
			return nil, fmt.Errorf("inspect table %s column: %w", table, err)
		}
		column.notNull = notNull != 0
		column.primary = primary != 0
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect table %s columns: %w", table, err)
	}
	return columns, nil
}

func (s *Store) inspectIndexColumns(ctx context.Context, index string) (_ []string, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
	if err != nil {
		return nil, fmt.Errorf("inspect index %s columns: %w", index, err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close index %s inspection: %w", index, closeErr)
		}
	}()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, fmt.Errorf("inspect index %s column: %w", index, err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect index %s columns: %w", index, err)
	}
	return columns, nil
}
