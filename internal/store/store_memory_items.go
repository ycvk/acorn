package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

const memoryItemColumns = `id, kind, content, status, session_id, source_run_id, wake_at, recurrence, expires_at, created_at, updated_at`

func (s *Store) AddMemoryItem(ctx context.Context, item core.MemoryItem) (core.MemoryItem, error) {
	if strings.TrimSpace(string(item.Kind)) == "" || strings.TrimSpace(string(item.Status)) == "" {
		return core.MemoryItem{}, errors.New("add memory item: kind and status are required")
	}
	if strings.TrimSpace(item.Content) == "" {
		return core.MemoryItem{}, errors.New("add memory item: content is required")
	}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_items(kind, content, status, session_id, source_run_id, wake_at, recurrence, expires_at, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		string(item.Kind), item.Content, string(item.Status), item.SessionID, item.SourceRunID,
		formatZeroableTimestamp(item.WakeAt), item.Recurrence, formatZeroableTimestamp(item.ExpiresAt),
		formatTimestamp(now), formatTimestamp(now),
	)
	if err != nil {
		return core.MemoryItem{}, fmt.Errorf("add memory item: %w", err)
	}
	if item.ID, err = result.LastInsertId(); err != nil {
		return core.MemoryItem{}, fmt.Errorf("add memory item id: %w", err)
	}
	return item, nil
}

func (s *Store) ListMemoryItems(ctx context.Context, statuses []core.MemoryStatus) ([]core.MemoryItem, error) {
	if len(statuses) == 0 {
		return nil, errors.New("list memory items: at least one status is required")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, 0, len(statuses))
	for _, status := range statuses {
		args = append(args, string(status))
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+memoryItemColumns+` FROM memory_items WHERE status IN (`+placeholders+`) ORDER BY id ASC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list memory items: %w", err)
	}
	return scanMemoryItems(rows)
}

func (s *Store) LoadMemoryItem(ctx context.Context, id int64) (*core.MemoryItem, error) {
	item, err := scanMemoryItem(s.db.QueryRowContext(ctx, `SELECT `+memoryItemColumns+` FROM memory_items WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %d", core.ErrMemoryItemNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("load memory item %d: %w", id, err)
	}
	return &item, nil
}

func (s *Store) UpdateMemoryItem(ctx context.Context, item core.MemoryItem) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE memory_items SET content = ?, status = ?, wake_at = ?, expires_at = ?, updated_at = ? WHERE id = ?`,
		item.Content, string(item.Status), formatZeroableTimestamp(item.WakeAt), formatZeroableTimestamp(item.ExpiresAt),
		formatTimestamp(time.Now().UTC()), item.ID,
	)
	if err != nil {
		return fmt.Errorf("update memory item %d: %w", item.ID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update memory item %d rows affected: %w", item.ID, err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %d", core.ErrMemoryItemNotFound, item.ID)
	}
	return nil
}

func (s *Store) ListDueCommitments(ctx context.Context, now time.Time) ([]core.MemoryItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+memoryItemColumns+` FROM memory_items
		 WHERE kind = ? AND status = ? AND wake_at <> '' AND wake_at <= ?
		 ORDER BY wake_at ASC, id ASC`,
		string(core.MemoryCommitment), string(core.MemoryActive), formatTimestamp(now))
	if err != nil {
		return nil, fmt.Errorf("list due commitments: %w", err)
	}
	return scanMemoryItems(rows)
}

func (s *Store) ClaimDueCommitment(ctx context.Context, id int64, now time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE memory_items SET status = ?, updated_at = ?
		 WHERE id = ? AND kind = ? AND status = ? AND wake_at <> '' AND wake_at <= ?`,
		string(core.MemoryWoken), formatTimestamp(now), id,
		string(core.MemoryCommitment), string(core.MemoryActive), formatTimestamp(now))
	if err != nil {
		return fmt.Errorf("claim commitment %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("claim commitment %d rows affected: %w", id, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %d", core.ErrMemoryItemNotDue, id)
	}
	return nil
}

func (s *Store) SaveContextSnapshot(ctx context.Context, hash, content string) error {
	if _, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO context_snapshots(hash, content, created_at) VALUES(?, ?, ?)`,
		hash, content, formatTimestamp(time.Now().UTC())); err != nil {
		return fmt.Errorf("save context snapshot %s: %w", hash, err)
	}
	return nil
}

func (s *Store) CountWakesSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE kind = ? AND created_at >= ?`,
		core.EventWakeFired, formatTimestamp(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count wakes: %w", err)
	}
	return n, nil
}

// trigramMinRunes is the shortest query the FTS5 trigram tokenizer can match;
// shorter queries fall back to LIKE.
const trigramMinRunes = 3

type rankedHit struct {
	hit  core.ExperienceHit
	rank float64
}

// SearchExperience searches past runs (input and output) and working memory.
// Hits are ordered by relevance for full-text queries and by recency for short
// LIKE queries.
func (s *Store) SearchExperience(ctx context.Context, query string, limit int) ([]core.ExperienceHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("search experience: query is required")
	}
	if limit <= 0 {
		return nil, errors.New("search experience: limit must be positive")
	}
	var (
		hits []rankedHit
		err  error
	)
	if utf8.RuneCountInString(query) >= trigramMinRunes {
		hits, err = s.searchExperienceFTS(ctx, query, limit)
	} else {
		hits, err = s.searchExperienceLike(ctx, query, limit)
	}
	if err != nil {
		return nil, err
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]core.ExperienceHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.hit)
	}
	return out, nil
}

func (s *Store) searchExperienceFTS(ctx context.Context, query string, limit int) ([]rankedHit, error) {
	// Quote the query as one phrase so punctuation in owner text is not parsed
	// as FTS5 syntax.
	match := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	var hits []rankedHit
	runRows, err := s.db.QueryContext(ctx,
		`SELECT r.run_id, snippet(runs_fts, -1, '', '', '…', 16), bm25(runs_fts), r.created_at
		 FROM runs_fts JOIN runs r ON r.run_id = runs_fts.run_id
		 WHERE runs_fts MATCH ? ORDER BY bm25(runs_fts) LIMIT ?`, match, limit)
	if err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	if err := scanRows(runRows, func(scan func(...any) error) error {
		var (
			h         rankedHit
			createdAt string
		)
		if err := scan(&h.hit.RunID, &h.hit.Snippet, &h.rank, &createdAt); err != nil {
			return err
		}
		h.hit.Source = "run"
		h.hit.CreatedAt, err = parseTimestamp(fixedTimestampLayout, createdAt, "runs.created_at")
		hits = append(hits, h)
		return err
	}); err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	memRows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.kind, m.source_run_id, snippet(memory_items_fts, 0, '', '', '…', 16), bm25(memory_items_fts), m.created_at
		 FROM memory_items_fts JOIN memory_items m ON m.id = memory_items_fts.rowid
		 WHERE memory_items_fts MATCH ? ORDER BY bm25(memory_items_fts) LIMIT ?`, match, limit)
	if err != nil {
		return nil, fmt.Errorf("search memory items: %w", err)
	}
	if err := scanRows(memRows, func(scan func(...any) error) error {
		var (
			h         rankedHit
			kind      string
			createdAt string
		)
		if err := scan(&h.hit.MemoryID, &kind, &h.hit.RunID, &h.hit.Snippet, &h.rank, &createdAt); err != nil {
			return err
		}
		h.hit.Source, h.hit.Kind = "memory", core.MemoryKind(kind)
		h.hit.CreatedAt, err = parseTimestamp(fixedTimestampLayout, createdAt, "memory_items.created_at")
		hits = append(hits, h)
		return err
	}); err != nil {
		return nil, fmt.Errorf("search memory items: %w", err)
	}
	return hits, nil
}

func (s *Store) searchExperienceLike(ctx context.Context, query string, limit int) ([]rankedHit, error) {
	pattern := "%" + escapeLike(query) + "%"
	var hits []rankedHit
	runRows, err := s.db.QueryContext(ctx,
		`SELECT run_id, input_text, output_text, created_at FROM runs
		 WHERE input_text LIKE ? ESCAPE '\' OR output_text LIKE ? ESCAPE '\'
		 ORDER BY created_at DESC LIMIT ?`, pattern, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	if err := scanRows(runRows, func(scan func(...any) error) error {
		var (
			h                    rankedHit
			input, output, stamp string
		)
		if err := scan(&h.hit.RunID, &input, &output, &stamp); err != nil {
			return err
		}
		text := input
		if !strings.Contains(input, query) {
			text = output
		}
		h.hit.Source, h.hit.Snippet = "run", snippetAround(text, query)
		h.hit.CreatedAt, err = parseTimestamp(fixedTimestampLayout, stamp, "runs.created_at")
		h.rank = -float64(h.hit.CreatedAt.UnixNano())
		hits = append(hits, h)
		return err
	}); err != nil {
		return nil, fmt.Errorf("search runs: %w", err)
	}
	memRows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, source_run_id, content, created_at FROM memory_items
		 WHERE content LIKE ? ESCAPE '\' ORDER BY created_at DESC LIMIT ?`, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search memory items: %w", err)
	}
	if err := scanRows(memRows, func(scan func(...any) error) error {
		var (
			h                    rankedHit
			kind, content, stamp string
		)
		if err := scan(&h.hit.MemoryID, &kind, &h.hit.RunID, &content, &stamp); err != nil {
			return err
		}
		h.hit.Source, h.hit.Kind, h.hit.Snippet = "memory", core.MemoryKind(kind), snippetAround(content, query)
		h.hit.CreatedAt, err = parseTimestamp(fixedTimestampLayout, stamp, "memory_items.created_at")
		h.rank = -float64(h.hit.CreatedAt.UnixNano())
		hits = append(hits, h)
		return err
	}); err != nil {
		return nil, fmt.Errorf("search memory items: %w", err)
	}
	return hits, nil
}

// snippetAround returns up to 40 runes on each side of the first match.
func snippetAround(text, query string) string {
	runes := []rune(text)
	idx := strings.Index(text, query)
	if idx < 0 {
		idx = 0
	}
	start := utf8.RuneCountInString(text[:idx])
	from, to := max(start-40, 0), min(start+utf8.RuneCountInString(query)+40, len(runes))
	out := string(runes[from:to])
	if from > 0 {
		out = "…" + out
	}
	if to < len(runes) {
		out += "…"
	}
	return out
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// scanRows iterates rows, calls fn per row, closes rows and surfaces rows.Err.
func scanRows(rows *sql.Rows, fn func(scan func(...any) error) error) error {
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			return err
		}
	}
	return rows.Err()
}

func scanMemoryItems(rows *sql.Rows) ([]core.MemoryItem, error) {
	var items []core.MemoryItem
	err := scanRows(rows, func(scan func(...any) error) error {
		item, err := scanMemoryItem(scannerFunc(scan))
		if err != nil {
			return err
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan memory items: %w", err)
	}
	return items, nil
}

type scannerFunc func(...any) error

func (f scannerFunc) Scan(dest ...any) error { return f(dest...) }

func scanMemoryItem(row interface{ Scan(dest ...any) error }) (core.MemoryItem, error) {
	var (
		item                                         core.MemoryItem
		kind, status, wakeAt, expiresAt, created, up string
	)
	if err := row.Scan(&item.ID, &kind, &item.Content, &status, &item.SessionID, &item.SourceRunID,
		&wakeAt, &item.Recurrence, &expiresAt, &created, &up); err != nil {
		return core.MemoryItem{}, err
	}
	item.Kind, item.Status = core.MemoryKind(kind), core.MemoryStatus(status)
	var err error
	if item.WakeAt, err = parseOptionalTime(wakeAt, "memory_items.wake_at"); err != nil {
		return core.MemoryItem{}, err
	}
	if item.ExpiresAt, err = parseOptionalTime(expiresAt, "memory_items.expires_at"); err != nil {
		return core.MemoryItem{}, err
	}
	if item.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "memory_items.created_at"); err != nil {
		return core.MemoryItem{}, err
	}
	if item.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, up, "memory_items.updated_at"); err != nil {
		return core.MemoryItem{}, err
	}
	return item, nil
}

func formatZeroableTimestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTimestamp(value)
}

func parseOptionalTime(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return parseTimestamp(fixedTimestampLayout, value, field)
}
