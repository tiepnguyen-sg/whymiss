package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
)

// pruneBatchSize bounds how many rows Prune deletes per pass when trimming
// by byte count, so a single Prune call never holds a huge transaction
// open and competing with the node for disk I/O (I-5) in one long burst.
const pruneBatchSize = 1000

const autoVacuumIncremental = 2

// Prune deletes the oldest rows in both tables until both maxAge and
// maxBytes are satisfied (ADR-0002: "deletes oldest-first until both the
// age limit and the byte limit are satisfied, then reclaims space"). New
// databases use incremental auto-vacuum; legacy pre-release databases use a
// full VACUUM compatibility path.
func (s *Store) Prune(ctx context.Context, maxAge time.Duration, maxBytes int64) error {
	if maxAge <= 0 {
		return fmt.Errorf("max age must be positive, got %s", maxAge)
	}
	if maxBytes <= 0 {
		return fmt.Errorf("max bytes must be positive, got %d", maxBytes)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_, err := s.deleteByAge(ctx, time.Now().UTC().Add(-maxAge))
	if err != nil {
		return err
	}
	if err := s.checkpointWAL(ctx); err != nil {
		return err
	}
	overhead, err := s.sidecarSizeBytes()
	if err != nil {
		return fmt.Errorf("measure database sidecars: %w", err)
	}
	liveBudget := maxBytes - overhead
	if liveBudget <= 0 {
		return fmt.Errorf("byte limit %d cannot fit SQLite sidecars using %d bytes", maxBytes, overhead)
	}

	for {
		size, err := s.sizeBytes(ctx)
		if err != nil {
			return fmt.Errorf("measure database size: %w", err)
		}
		if size <= liveBudget {
			break
		}

		batchDeleted, err := s.deleteOldestBatch(ctx, pruneBatchSize)
		if err != nil {
			return fmt.Errorf("prune oldest rows by size: %w", err)
		}
		if batchDeleted == 0 {
			return fmt.Errorf("byte limit %d cannot be satisfied: empty database requires %d live bytes plus %d sidecar bytes", maxBytes, size, overhead)
		}
		if err := s.checkpointWAL(ctx); err != nil {
			return err
		}
	}

	var freePages int64
	if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freePages); err != nil {
		return fmt.Errorf("read freelist_count: %w", err)
	}
	if freePages > 0 {
		if err := s.reclaimFreePages(ctx); err != nil {
			return err
		}
	}
	if err := s.checkpointWAL(ctx); err != nil {
		return err
	}
	size, err := s.physicalSizeBytes()
	if err != nil {
		return fmt.Errorf("verify physical database size: %w", err)
	}
	if size > maxBytes {
		return fmt.Errorf("byte limit %d not satisfied after reclaim: database files use %d bytes", maxBytes, size)
	}
	return nil
}

func (s *Store) deleteByAge(ctx context.Context, cutoff time.Time) (_ int64, err error) {
	var deleted int64
	for {
		batchDeleted, batchErr := s.deleteOldestBatchBefore(ctx, pruneBatchSize, cutoff)
		if batchErr != nil {
			return deleted, fmt.Errorf("prune rows by age: %w", batchErr)
		}
		if batchDeleted == 0 {
			return deleted, nil
		}
		deleted += batchDeleted
		if checkpointErr := s.checkpointWAL(ctx); checkpointErr != nil {
			return deleted, checkpointErr
		}
	}
}

// deleteOldestBatch deletes up to limit rows globally across observations and
// samples, ordered by event timestamp. IDs are only tie-breakers; insertion
// order is not chronology because collectors can deliver facts late.
func (s *Store) deleteOldestBatch(ctx context.Context, limit int) (_ int64, err error) {
	return s.deleteOldestMatchingBatch(ctx, limit, "", nil)
}

func (s *Store) deleteOldestBatchBefore(ctx context.Context, limit int, cutoff time.Time) (_ int64, err error) {
	const before = `WHERE at < ?`
	return s.deleteOldestMatchingBatch(ctx, limit, before, []any{cutoff.UTC().Format(timeLayout)})
}

func (s *Store) deleteOldestMatchingBatch(ctx context.Context, limit int, filter string, filterArgs []any) (_ int64, err error) {
	if limit <= 0 {
		return 0, fmt.Errorf("batch limit must be positive, got %d", limit)
	}
	if filter != "" && filter != "WHERE at < ?" {
		return 0, fmt.Errorf("unsupported prune filter %q", filter)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin batch: %w", err)
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback oldest batch: %w", rollbackErr))
			}
		}
	}()

	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE prune_batch (table_name TEXT NOT NULL, id INTEGER NOT NULL)`); err != nil {
		return 0, fmt.Errorf("prepare oldest batch: %w", err)
	}
	insertQuery := `
INSERT INTO prune_batch (table_name, id)
SELECT table_name, id
FROM (
	SELECT 'observations' AS table_name, id, at FROM observations
	UNION ALL
	SELECT 'samples' AS table_name, id, at FROM samples
)
ORDER BY at ASC, table_name ASC, id ASC
LIMIT ?`
	if filter != "" {
		insertQuery = `
INSERT INTO prune_batch (table_name, id)
SELECT table_name, id
FROM (
	SELECT 'observations' AS table_name, id, at FROM observations WHERE at < ?
	UNION ALL
	SELECT 'samples' AS table_name, id, at FROM samples WHERE at < ?
)
ORDER BY at ASC, table_name ASC, id ASC
LIMIT ?`
	}
	args := make([]any, 0, len(filterArgs)*2+1)
	args = append(args, filterArgs...)
	args = append(args, filterArgs...)
	args = append(args, limit)
	if _, err = tx.ExecContext(ctx, insertQuery, args...); err != nil {
		return 0, fmt.Errorf("select oldest batch: %w", err)
	}

	var deleted int64
	for _, target := range []struct {
		tableName string
		query     string
	}{
		{tableName: "observations", query: `DELETE FROM observations WHERE id IN (SELECT id FROM prune_batch WHERE table_name = 'observations')`},
		{tableName: "samples", query: `DELETE FROM samples WHERE id IN (SELECT id FROM prune_batch WHERE table_name = 'samples')`},
	} {
		result, execErr := tx.ExecContext(ctx, target.query)
		if execErr != nil {
			return 0, fmt.Errorf("delete %s batch: %w", target.tableName, execErr)
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return 0, fmt.Errorf("count deleted %s rows: %w", target.tableName, rowsErr)
		}
		deleted += rows
	}

	if _, err = tx.ExecContext(ctx, `DROP TABLE prune_batch`); err != nil {
		return 0, fmt.Errorf("drop oldest batch: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit oldest batch: %w", err)
	}
	return deleted, nil
}

// reclaimFreePages uses incremental auto-vacuum for databases created by this
// release. A full VACUUM remains a compatibility fallback for pre-release
// databases created before incremental page reclamation was enabled.
func (s *Store) reclaimFreePages(ctx context.Context) error {
	var mode int
	if err := s.db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return fmt.Errorf("read auto_vacuum: %w", err)
	}
	if mode == autoVacuumIncremental {
		if _, err := s.db.ExecContext(ctx, "PRAGMA incremental_vacuum"); err != nil {
			return fmt.Errorf("incremental vacuum: %w", err)
		}
		return nil
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum legacy database: %w", err)
	}
	return nil
}

// checkpointWAL makes the reclaimed byte budget visible on disk instead of
// leaving old frames in the -wal sidecar indefinitely.
func (s *Store) checkpointWAL(ctx context.Context) error {
	var busy, logFrames, checkpointedFrames int
	if err := s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointedFrames); err != nil {
		return fmt.Errorf("checkpoint WAL: %w", err)
	}
	if busy != 0 {
		return fmt.Errorf("checkpoint WAL: database remained busy (%d log frames, %d checkpointed)", logFrames, checkpointedFrames)
	}
	return nil
}

// sizeBytes reports the bytes live data occupies. Free pages are subtracted:
// DELETE moves pages to the freelist without lowering page_count, so counting
// them makes the trim loop delete every row before it sees progress.
func (s *Store) sizeBytes(ctx context.Context) (int64, error) {
	var pageCount, freeCount, pageSize int64
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return 0, fmt.Errorf("read page_count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freeCount); err != nil {
		return 0, fmt.Errorf("read freelist_count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, fmt.Errorf("read page_size: %w", err)
	}
	return (pageCount - freeCount) * pageSize, nil
}

func (s *Store) sidecarSizeBytes() (int64, error) {
	return fileSizes(s.path+"-wal", s.path+"-shm")
}

func (s *Store) physicalSizeBytes() (int64, error) {
	return fileSizes(s.path, s.path+"-wal", s.path+"-shm")
}

func fileSizes(paths ...string) (int64, error) {
	var total int64
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("stat %s: %w", path, err)
		}
		total += info.Size()
	}
	return total, nil
}
