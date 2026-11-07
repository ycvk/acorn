package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/redact"
)

// portraitSchema is shared with the migration that adds it. Every line cites
// at least one source, so a line left without sources is one whose source was
// forgotten.
const portraitSchema = `
CREATE TABLE IF NOT EXISTS portrait_lines (
 id INTEGER PRIMARY KEY AUTOINCREMENT, position INTEGER NOT NULL, content TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('shown','removed')), epoch INTEGER NOT NULL,
 created_at TEXT NOT NULL, removed_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS portrait_line_sources (
 line_id INTEGER NOT NULL REFERENCES portrait_lines(id), source_id TEXT NOT NULL REFERENCES memory_sources(id),
 PRIMARY KEY(line_id,source_id)
);
CREATE INDEX IF NOT EXISTS idx_portrait_line_sources_source ON portrait_line_sources(source_id,line_id);
CREATE INDEX IF NOT EXISTS idx_memory_sources_speaker ON memory_sources(speaker,recorded_at);
`

// addPortrait (34 to 35) adds the portrait and, when the owner has said
// anything, queues its first refresh.
func addPortrait(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, portraitSchema); err != nil {
		return err
	}
	now := formatTimestamp(time.Now())
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_jobs(operation,object_id,version,available_at,created_at)
 SELECT ?,?,'bootstrap',?,? WHERE EXISTS(SELECT 1 FROM memory_sources WHERE speaker='owner' AND kind<>?)`, core.PortraitOperation, core.PortraitObject, now, now, core.MemoryRequestSource)
	return err
}

// A fragment excluded after a line was written hides it; a line written later
// read its source without the fragment.
const portraitLineVisible = `NOT EXISTS(SELECT 1 FROM portrait_line_sources s JOIN memory_exclusions x ON x.source_id=s.source_id WHERE s.line_id=l.id AND (x.quote='' OR x.epoch>l.epoch))`

func (s *Store) Portrait(ctx context.Context) ([]core.PortraitLine, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT l.id,l.content,l.state,l.created_at,l.removed_at,
 (SELECT json_group_array(s.source_id) FROM (SELECT source_id FROM portrait_line_sources WHERE line_id=l.id ORDER BY rowid) s)
 FROM portrait_lines l WHERE `+portraitLineVisible+` ORDER BY l.state='removed',l.position,l.id`)
	if err != nil {
		return nil, err
	}
	var lines []core.PortraitLine
	err = scanRows(rows, func(scan func(...any) error) error {
		var line core.PortraitLine
		var created, removed, sources string
		if err := scan(&line.ID, &line.Content, &line.State, &created, &removed, &sources); err != nil {
			return err
		}
		var err error
		if line.CreatedAt, err = parseTimestamp(created, "portrait_lines.created_at"); err != nil {
			return err
		}
		if line.RemovedAt, err = parseOptionalTime(removed, "portrait_lines.removed_at"); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(sources), &line.SourceIDs); err != nil {
			return fmt.Errorf("portrait line %d sources: %w", line.ID, err)
		}
		lines = append(lines, line)
		return nil
	})
	return lines, err
}

func (s *Store) QueuePortrait(ctx context.Context, version string, now time.Time) error {
	return enqueueMemoryJob(ctx, s.db, core.PortraitOperation, core.PortraitObject, version, now)
}

func (s *Store) PortraitRefreshedAt(ctx context.Context) (time.Time, error) {
	var at string
	if err := s.read.QueryRowContext(ctx, `SELECT COALESCE(MAX(completed_at),'') FROM memory_jobs WHERE operation=? AND state='done'`, core.PortraitOperation).Scan(&at); err != nil {
		return time.Time{}, err
	}
	return parseOptionalTime(at, "memory_jobs.completed_at")
}

// CommitPortrait checks the lease and that every source is still there to
// cite, writes the lines in their order and finishes the refresh.
func (s *Store) CommitPortrait(ctx context.Context, m core.PortraitMutation) error {
	if m.Now.IsZero() {
		return errors.New("portrait mutation requires now")
	}
	if m.Lease.Operation != core.PortraitOperation {
		return fmt.Errorf("portrait mutation lease is a %q job", m.Lease.Operation)
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if err := validateMemoryLease(ctx, tx, m.Lease, m.Now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM portrait_line_sources WHERE line_id IN(SELECT id FROM portrait_lines WHERE state='shown')`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM portrait_lines WHERE state='shown'`); err != nil {
			return err
		}
		for i, line := range m.Lines {
			content := strings.TrimSpace(line.Content)
			if content == "" {
				return fmt.Errorf("portrait line %d is empty", i+1)
			}
			if spans := redact.Find(content); len(spans) > 0 {
				return fmt.Errorf("%w: %s in portrait line %d", core.ErrMemoryCredential, spans[0].Kind, i+1)
			}
			if len(line.SourceIDs) == 0 {
				return fmt.Errorf("portrait line %d cites no source", i+1)
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO portrait_lines(position,content,state,epoch,created_at) VALUES(?,?,'shown',?,?)`, i, content, m.Lease.Epoch, formatTimestamp(m.Now))
			if err != nil {
				return err
			}
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			for _, source := range line.SourceIDs {
				if _, err := loadMemorySource(ctx, tx, source, sourceMemory); err != nil {
					return fmt.Errorf("portrait line %d: %w", i+1, err)
				}
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO portrait_line_sources(line_id,source_id) VALUES(?,?)`, id, source); err != nil {
					return err
				}
			}
		}
		return finishMemoryJob(ctx, tx, m.Lease, "", m.Now)
	})
}

// RemovePortraitLine keeps the line as removed, so a refresh does not write
// it again.
func (s *Store) RemovePortraitLine(ctx context.Context, id int64, now time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE portrait_lines AS l SET state='removed',removed_at=? WHERE l.id=? AND l.state='shown' AND `+portraitLineVisible, formatTimestamp(now), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", core.ErrPortraitLineNotFound, id)
	}
	return nil
}

func (s *Store) OwnerMemorySources(ctx context.Context, since time.Time, limit int) ([]core.MemorySource, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("owner memory sources limit %d must be positive", limit)
	}
	rows, err := s.read.QueryContext(ctx, `SELECT m.id FROM memory_sources m WHERE m.speaker='owner' AND m.kind<>? AND m.recorded_at>=?
 AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=m.id AND x.quote='') ORDER BY m.recorded_at DESC,m.id LIMIT ?`, core.MemoryRequestSource, formatTimestamp(since), limit)
	if err != nil {
		return nil, err
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return nil, err
	}
	return resolveMemorySources(ctx, s.read, ids, sourceMemory)
}

// withdrawPortraitLines deletes the shown lines that cite a source of the
// withdrawn memory r and queues a refresh without them. Lines the owner
// removed stay removed.
func withdrawPortraitLines(ctx context.Context, tx *sql.Tx, r core.MemoryRecord, now time.Time) error {
	sources, err := json.Marshal(r.SourceIDs)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT l.id FROM portrait_lines l JOIN portrait_line_sources s ON s.line_id=l.id
 WHERE l.state='shown' AND s.source_id IN(SELECT value FROM json_each(?))`, string(sources))
	if err != nil {
		return err
	}
	var lines []int64
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id int64
		if err := scan(&id); err != nil {
			return err
		}
		lines = append(lines, id)
		return nil
	}); err != nil || len(lines) == 0 {
		return err
	}
	ids, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM portrait_line_sources WHERE line_id IN(SELECT value FROM json_each(?))`, string(ids)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM portrait_lines WHERE id IN(SELECT value FROM json_each(?))`, string(ids)); err != nil {
		return err
	}
	return enqueueMemoryJob(ctx, tx, core.PortraitOperation, core.PortraitObject, fmt.Sprintf("withdraw:%s:%d", r.ID, r.Revision), now)
}

// forgetPortrait deletes the lines, removed ones included, that cite a source
// excluded at epoch, and queues a refresh without them when a shown line goes.
func forgetPortrait(ctx context.Context, tx *sql.Tx, epoch int64, now time.Time) error {
	cited := `SELECT s.line_id FROM portrait_line_sources s JOIN memory_exclusions x ON x.source_id=s.source_id WHERE x.epoch=?`
	var shown int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM portrait_lines WHERE state='shown' AND id IN(`+cited+`)`, epoch).Scan(&shown); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM portrait_line_sources WHERE line_id IN(`+cited+`)`, epoch); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM portrait_lines WHERE NOT EXISTS(SELECT 1 FROM portrait_line_sources s WHERE s.line_id=portrait_lines.id)`); err != nil {
		return err
	}
	if shown == 0 {
		return nil
	}
	return enqueueMemoryJob(ctx, tx, core.PortraitOperation, core.PortraitObject, fmt.Sprintf("forget:%d", epoch), now)
}
