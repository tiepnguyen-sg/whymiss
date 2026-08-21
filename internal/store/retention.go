package store

import (
	"context"
	"fmt"
	"time"
)

// pruneBatchSize bounds how many rows Prune deletes per pass when trimming
// by byte count, so a single Prune call never holds a huge transaction
// open and competing with the node for disk I/O (I-5) in one long burst.
const pruneBatchSize = 1000

// Prune deletes the oldest rows in both tables until both maxAge and
// maxBytes are satisfied (ADR-0002: "deletes oldest-first until both the
// age limit and the byte limit are satisfied, then reclaims space"), then
// runs VACUUM to actually shrink the file on disk — SQLite does not
// release freed pages back to the filesystem on DELETE alone.
func (s *Store) Prune(ctx context.Context, maxAge time.Duration, maxBytes int64) error {
	cutoff := time.Now().UTC().Add(-maxAge).Format(timeLayout)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM observations WHERE at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune observations by age: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM samples WHERE at < ?`, cutoff); err != nil {
		return fmt.Errorf("prune samples by age: %w", err)
	}

	for {
		size, err := s.sizeBytes(ctx)
		if err != nil {
			return fmt.Errorf("measure database size: %w", err)
		}
		if size <= maxBytes {
			break
		}

		obsDeleted, err := s.deleteOldestBatch(ctx, "observations")
		if err != nil {
			return fmt.Errorf("prune observations by size: %w", err)
		}
		samplesDeleted, err := s.deleteOldestBatch(ctx, "samples")
		if err != nil {
			return fmt.Errorf("prune samples by size: %w", err)
		}
		if obsDeleted == 0 && samplesDeleted == 0 {
			// Nothing left to delete — the byte limit cannot be satisfied
			// by pruning alone (e.g. it is smaller than an empty
			// database's own overhead). Stop rather than loop forever.
			break
		}
	}

	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return nil
}

// deleteOldestBatch deletes up to pruneBatchSize of table's oldest rows (by
// its own rowid, which is monotonically increasing with insertion order —
// equivalent to oldest-by-at for a table nothing ever updates) and reports
// how many were actually removed.
func (s *Store) deleteOldestBatch(ctx context.Context, table string) (int64, error) {
	// table is one of the two fixed literal names this package's own
	// callers pass ("observations", "samples") — never operator- or
	// attacker-supplied — so building the statement directly is safe here.
	query := fmt.Sprintf( //nolint:gosec // G201: table is a fixed internal literal, not external input
		`DELETE FROM %s WHERE id IN (SELECT id FROM %s ORDER BY id ASC LIMIT ?)`, table, table)
	result, err := s.db.ExecContext(ctx, query, pruneBatchSize)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// sizeBytes reports the database file's current size via SQLite's own
// page accounting, which needs no filesystem access and so works
// identically whether path was a file or (in tests) an in-memory database.
func (s *Store) sizeBytes(ctx context.Context) (int64, error) {
	var pageCount, pageSize int64
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, fmt.Errorf("read page_count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, fmt.Errorf("read page_size: %w", err)
	}
	return pageCount * pageSize, nil
}
