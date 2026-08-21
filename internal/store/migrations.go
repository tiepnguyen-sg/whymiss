package store

// migration is one forward-only schema change, numbered starting at 1.
// SQLite's own PRAGMA user_version holds which migration a database has
// applied — no separate schema_version table needed for that (ADR-0002:
// "the schema version lives in the database").
type migration struct {
	version int
	sql     string
}

// migrations is applied in order, inside one transaction per Open call, up
// to the highest version not yet recorded in PRAGMA user_version. Append
// only — an already-shipped migration's SQL text is immutable, since a
// database that already ran it must never see it change underneath it.
var migrations = []migration{
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
}
