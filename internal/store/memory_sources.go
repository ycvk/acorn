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
)

func (s *Store) RegisterMemorySource(ctx context.Context, source core.MemorySource) (core.MemorySource, error) {
	if source.ID == "" || source.Kind == "" || source.ObjectID == "" || source.Version == "" || source.Speaker == "" || source.RecordedAt.IsZero() {
		return source, errors.New("memory source requires id, kind, object_id, version, speaker and recorded_at")
	}
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error { return registerMemorySource(ctx, tx, source) })
	if err != nil {
		return core.MemorySource{}, err
	}
	return s.LoadMemorySource(ctx, source.ID)
}

func registerMemorySource(ctx context.Context, q memorySQL, source core.MemorySource) error {
	result, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO memory_sources(id,kind,object_id,version,speaker,session_id,run_id,body,recorded_at,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, source.ID, source.Kind, source.ObjectID, source.Version, source.Speaker, source.SessionID, source.RunID, source.Body, formatTimestamp(source.RecordedAt), formatZeroableTimestamp(source.OccurredAt))
	if err != nil {
		return fmt.Errorf("register source %s: %w", source.ID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var body, version, speaker string
		if err := q.QueryRowContext(ctx, `SELECT body,version,speaker FROM memory_sources WHERE id=?`, source.ID).Scan(&body, &version, &speaker); err != nil {
			return err
		}
		if body != source.Body || version != source.Version || speaker != source.Speaker {
			return fmt.Errorf("%w: source %s already has a different version", core.ErrMemoryConflict, source.ID)
		}
		return nil
	}
	if source.Speaker == "assistant" && source.RunID != "" {
		if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO memory_source_links(source_id,parent_id) SELECT ?,source_id FROM context_snapshot_refs WHERE run_id=? AND source_id<>'' AND source_id<>?`, source.ID, source.RunID, source.ID); err != nil {
			return err
		}
	}
	if source.Speaker == "owner" || source.Speaker == "tool" || source.Speaker == "external" {
		return enqueueMemoryJob(ctx, q, "extract", source.ID, source.Version, source.RecordedAt)
	}
	return nil
}

func (s *Store) LoadMemorySource(ctx context.Context, id string) (core.MemorySource, error) {
	return loadMemorySource(ctx, s.db, id, true)
}

func loadMemorySource(ctx context.Context, q memorySQL, id string, filter bool) (core.MemorySource, error) {
	var source core.MemorySource
	var recorded, occurred string
	err := q.QueryRowContext(ctx, `SELECT id,kind,object_id,version,speaker,session_id,run_id,body,recorded_at,occurred_at FROM memory_sources WHERE id=?`, id).Scan(&source.ID, &source.Kind, &source.ObjectID, &source.Version, &source.Speaker, &source.SessionID, &source.RunID, &source.Body, &recorded, &occurred)
	if errors.Is(err, sql.ErrNoRows) {
		return source, fmt.Errorf("%w: source %s", core.ErrMemoryNotFound, id)
	}
	if err != nil {
		return source, err
	}
	if source.RecordedAt, err = time.Parse(time.RFC3339Nano, recorded); err != nil {
		return source, err
	}
	if occurred != "" {
		if source.OccurredAt, err = time.Parse(time.RFC3339Nano, occurred); err != nil {
			return source, err
		}
	}
	source.Content = source.Body
	switch source.Kind {
	case "knowledge":
		err = q.QueryRowContext(ctx, `SELECT body FROM knowledge_revisions WHERE path=? AND revision=?`, source.ObjectID, source.Version).Scan(&source.Content)
	case "watch":
		err = q.QueryRowContext(ctx, `SELECT title||char(10)||url||char(10)||summary FROM watch_items WHERE id=?`, source.ObjectID).Scan(&source.Content)
	case "message":
		err = q.QueryRowContext(ctx, `SELECT content FROM session_messages WHERE id=?`, source.ObjectID).Scan(&source.Content)
	case "event":
		var payload, kind string
		err = q.QueryRowContext(ctx, `SELECT payload_json,kind FROM events WHERE sequence=?`, source.ObjectID).Scan(&payload, &kind)
		if err == nil {
			var event struct {
				ToolName string `json:"tool_name"`
				Output   string `json:"output"`
				Error    string `json:"error"`
			}
			if err = json.Unmarshal([]byte(payload), &event); err == nil {
				source.ToolName = event.ToolName
				source.Outcome = strings.TrimPrefix(kind, "tool.call.")
				source.Content = event.Output
				if source.Outcome == "failed" {
					source.Content = event.Error
				}
			}
		}
	}
	if err != nil {
		return source, fmt.Errorf("resolve memory source %s: %w", id, err)
	}
	if filter {
		rows, err := q.QueryContext(ctx, `SELECT quote FROM memory_exclusions WHERE source_id=?`, id)
		if err != nil {
			return source, err
		}
		var quotes []string
		if err = scanRows(rows, func(scan func(...any) error) error {
			var quote string
			if err := scan(&quote); err != nil {
				return err
			}
			quotes = append(quotes, quote)
			return nil
		}); err != nil {
			return source, err
		}
		for _, quote := range quotes {
			source.Excluded = true
			if quote == "" {
				source.Content = ""
				break
			}
			source.Content = strings.ReplaceAll(source.Content, quote, "[owner excluded this memory]")
		}
		if source.Content == "" && source.Excluded {
			return source, fmt.Errorf("%w: source %s", core.ErrMemoryExcluded, id)
		}
	}
	return source, nil
}

func (s *Store) RunMemorySources(ctx context.Context, runID string) ([]core.MemorySource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM memory_sources WHERE run_id=? ORDER BY recorded_at,id`, runID)
	if err != nil {
		return nil, err
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return nil, err
	}
	return resolveMemorySources(ctx, s.db, ids)
}

func resolveMemorySources(ctx context.Context, q memorySQL, ids []string) ([]core.MemorySource, error) {
	result := make([]core.MemorySource, 0, len(ids))
	for _, id := range ids {
		source, err := loadMemorySource(ctx, q, id, true)
		if errors.Is(err, core.ErrMemoryExcluded) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, source)
	}
	return result, nil
}

func memoryStringRows(rows *sql.Rows) ([]string, error) {
	var out []string
	err := scanRows(rows, func(scan func(...any) error) error {
		var id string
		if err := scan(&id); err != nil {
			return err
		}
		out = append(out, id)
		return nil
	})
	return out, err
}

// Unfinished jobs are few; the unary plus keeps the planner on the state index.
const memoryPendingSourcesWhere = `+j.operation='extract' AND j.state IN ('pending','running','failed') AND NOT EXISTS(SELECT 1 FROM memory_exclusions x WHERE x.source_id=s.id AND x.quote='')`

func (s *Store) PendingMemorySources(ctx context.Context, query core.MemoryQuery) ([]core.MemorySource, int, error) {
	where := memoryPendingSourcesWhere
	args := []any{}
	if query.SessionID != "" {
		where += " AND s.session_id=?"
		args = append(args, query.SessionID)
	}
	if !query.From.IsZero() {
		where += " AND s.recorded_at>=?"
		args = append(args, formatTimestamp(query.From))
	}
	if !query.To.IsZero() {
		where += " AND s.recorded_at<?"
		args = append(args, formatTimestamp(query.To))
	}
	var count int
	if err := s.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_jobs j JOIN memory_sources s ON s.id=j.object_id WHERE `+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := s.read.QueryContext(ctx, `SELECT s.id FROM memory_jobs j JOIN memory_sources s ON s.id=j.object_id WHERE `+where+` ORDER BY s.recorded_at DESC LIMIT 100`, args...)
	if err != nil {
		return nil, 0, err
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return nil, 0, err
	}
	sources, err := resolveMemorySources(ctx, s.read, ids)
	if err != nil {
		return nil, 0, err
	}
	return sources, count, nil
}
