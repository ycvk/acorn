package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/redact"
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
	// Only the owner's own words are extracted automatically. Tool results,
	// external documents and watch items stay registered as sources that
	// evidence can cite, but nothing is extracted from them.
	if source.Speaker == "owner" && source.Kind != core.MemoryRequestSource {
		return enqueueMemoryJob(ctx, q, "extract", source.ID, source.Version, source.RecordedAt)
	}
	return nil
}

// sourceView selects how a resolved source's content is rendered.
type sourceView int

const (
	// sourceStored is the unfiltered content, for the store's own checks.
	sourceStored sourceView = iota
	// sourceThread is the conversation the agent continues: owner exclusions
	// apply and credentials stay, so a key or code the owner hands over remains
	// usable in its thread.
	sourceThread
	// sourceMemory is what memory processing, evidence checks, recall and
	// memory_read see: owner exclusions apply and credentials are redacted.
	sourceMemory
)

// LoadMemorySource reads a source as memory sees it.
func (s *Store) LoadMemorySource(ctx context.Context, id string) (core.MemorySource, error) {
	return loadMemorySource(ctx, s.db, id, sourceMemory)
}

// LoadThreadSource reads a source as thread history.
func (s *Store) LoadThreadSource(ctx context.Context, id string) (core.MemorySource, error) {
	return loadMemorySource(ctx, s.db, id, sourceThread)
}

func loadMemorySource(ctx context.Context, q memorySQL, id string, view sourceView) (core.MemorySource, error) {
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
	// A wholly excluded source is read without its content, which a deleted
	// thread no longer has.
	if view != sourceStored {
		var whole int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_exclusions WHERE source_id=? AND quote=''`, id).Scan(&whole); err != nil {
			return source, err
		}
		if whole > 0 {
			source.Excluded, source.Content = true, ""
			return source, fmt.Errorf("%w: source %s", core.ErrMemoryExcluded, id)
		}
	}
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
	if view == sourceStored {
		return source, nil
	}
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
	if len(quotes) > 0 {
		source.Excluded = true
	}
	if !slices.Contains(quotes, "") {
		source.Content = renderSource(source.Content, quotes, view == sourceMemory)
	} else {
		source.Content = ""
	}
	if source.Content == "" && source.Excluded {
		return source, fmt.Errorf("%w: source %s", core.ErrMemoryExcluded, id)
	}
	return source, nil
}

const excludedPlaceholder = "[owner excluded this memory]"

// renderSource replaces excluded quotes and, when redacted, credentials.
// Excluded quotes come from either view: a quote of memory's redacted text is
// located there and mapped back onto the raw text, so forgetting hides the same
// words in thread history. A credential that an excluded range touches is
// excluded with it, so no part of it survives at the range's edge.
func renderSource(raw string, quotes []string, redacted bool) string {
	if !redacted && len(quotes) == 0 {
		return raw
	}
	secrets := redact.Find(raw)
	if len(secrets) == 0 && len(quotes) == 0 {
		return raw
	}
	var excluded []textRange
	if len(quotes) > 0 {
		view := newRedactedView(raw, secrets)
		for _, quote := range quotes {
			excluded = append(excluded, occurrences(raw, quote)...)
			for _, r := range occurrences(view.text, quote) {
				excluded = append(excluded, view.raw(r))
			}
		}
	}
	if redacted {
		for _, s := range secrets {
			for i, r := range excluded {
				if s.Start < r.end && r.start < s.End {
					excluded[i] = textRange{min(r.start, s.Start), max(r.end, s.End)}
				}
			}
		}
	}
	excluded = mergeRanges(excluded)
	var b strings.Builder
	at, next := 0, 0
	for _, r := range excluded {
		if redacted {
			next = writeRedacted(&b, raw, at, r.start, secrets, next)
		} else {
			b.WriteString(raw[at:r.start])
		}
		b.WriteString(excludedPlaceholder)
		at = r.end
	}
	if redacted {
		writeRedacted(&b, raw, at, len(raw), secrets, next)
	} else {
		b.WriteString(raw[at:])
	}
	return b.String()
}

type textRange struct{ start, end int }

// occurrences lists the non-overlapping positions of quote in text.
func occurrences(text, quote string) []textRange {
	var out []textRange
	for at := 0; quote != ""; {
		i := strings.Index(text[at:], quote)
		if i < 0 {
			break
		}
		out = append(out, textRange{at + i, at + i + len(quote)})
		at += i + len(quote)
	}
	return out
}

func mergeRanges(ranges []textRange) []textRange {
	slices.SortFunc(ranges, func(a, b textRange) int { return a.start - b.start })
	var out []textRange
	for _, r := range ranges {
		if n := len(out); n > 0 && r.start < out[n-1].end {
			out[n-1].end = max(out[n-1].end, r.end)
			continue
		}
		out = append(out, r)
	}
	return out
}

// writeRedacted writes raw[from:to] with the credentials wholly inside it
// replaced, starting the search at secrets[next], and returns the next unused
// secret. Excluded ranges absorb the credentials they touch.
func writeRedacted(b *strings.Builder, raw string, from, to int, secrets []redact.Span, next int) int {
	for next < len(secrets) && secrets[next].End <= from {
		next++
	}
	for ; next < len(secrets) && secrets[next].End <= to; next++ {
		s := secrets[next]
		b.WriteString(raw[from:s.Start])
		b.WriteString(redact.Placeholder(s.Kind))
		from = s.End
	}
	b.WriteString(raw[from:to])
	return next
}

// redactedView is raw with every credential replaced, plus the segments that
// map its offsets back onto raw.
type redactedView struct {
	text     string
	segments []viewSegment
}

type viewSegment struct {
	start, end, rawStart, rawEnd int
	secret                       bool
}

func newRedactedView(raw string, secrets []redact.Span) redactedView {
	var v redactedView
	var b strings.Builder
	at := 0
	add := func(text string, rawEnd int, secret bool) {
		start := b.Len()
		b.WriteString(text)
		v.segments = append(v.segments, viewSegment{start, b.Len(), at, rawEnd, secret})
		at = rawEnd
	}
	for _, s := range secrets {
		add(raw[at:s.Start], s.Start, false)
		add(redact.Placeholder(s.Kind), s.End, true)
	}
	add(raw[at:], len(raw), false)
	v.text = b.String()
	return v
}

// raw maps a range of the view onto raw; a range that touches a placeholder
// covers the whole credential behind it.
func (v redactedView) raw(r textRange) textRange {
	out := textRange{v.segments[len(v.segments)-1].rawEnd, 0}
	for _, s := range v.segments {
		if s.end <= r.start || s.start >= r.end {
			continue
		}
		if s.secret {
			out.start, out.end = min(out.start, s.rawStart), max(out.end, s.rawEnd)
			continue
		}
		out.start = min(out.start, s.rawStart+max(r.start, s.start)-s.start)
		out.end = max(out.end, s.rawStart+min(r.end, s.end)-s.start)
	}
	return out
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
	return resolveMemorySources(ctx, s.db, ids, sourceThread)
}

func resolveMemorySources(ctx context.Context, q memorySQL, ids []string, view sourceView) ([]core.MemorySource, error) {
	result := make([]core.MemorySource, 0, len(ids))
	for _, id := range ids {
		source, err := loadMemorySource(ctx, q, id, view)
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
	sources, err := resolveMemorySources(ctx, s.read, ids, sourceMemory)
	if err != nil {
		return nil, 0, err
	}
	return sources, count, nil
}
