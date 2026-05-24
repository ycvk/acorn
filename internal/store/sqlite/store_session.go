package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtimehistory"
)

type SessionMessagePart struct {
	Kind             string                  `json:"kind"`
	Text             string                  `json:"text,omitempty"`
	Reasoning        string                  `json:"reasoning,omitempty"`
	Status           string                  `json:"status,omitempty"`
	Title            string                  `json:"title,omitempty"`
	Summary          string                  `json:"summary,omitempty"`
	Changed          []string                `json:"changed,omitempty"`
	Verified         []string                `json:"verified,omitempty"`
	Risks            []string                `json:"risks,omitempty"`
	Items            []SessionDisclosureItem `json:"items,omitempty"`
	DetailRunID      string                  `json:"detail_run_id,omitempty"`
	RunID            string                  `json:"run_id,omitempty"`
	Label            string                  `json:"label,omitempty"`
	DecisionID       string                  `json:"decision_id,omitempty"`
	Question         string                  `json:"question,omitempty"`
	SelectedOptionID string                  `json:"selected_option_id,omitempty"`
	Answer           string                  `json:"answer,omitempty"`
	Options          []SessionDecisionOption `json:"options,omitempty"`
	Action           *SessionMessageAction   `json:"action,omitempty"`
}

type SessionDisclosureItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

type SessionDecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type SessionMessageAction struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

func (s *Store) CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO sessions(session_id, title, created_at, updated_at) VALUES(?, ?, ?, ?)`,
		sessionID,
		title,
		formatTimestamp(now),
		formatTimestamp(now),
	)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &events.SessionRecord{SessionID: sessionID, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) LoadSession(ctx context.Context, sessionID string) (*events.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT session_id, title, created_at, updated_at FROM sessions WHERE session_id = ?`, sessionID)
	var (
		rec     events.SessionRecord
		created string
		updated string
	)
	if err := row.Scan(&rec.SessionID, &rec.Title, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
		}
		return nil, fmt.Errorf("load session: %w", err)
	}
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session.created_at")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimestamp(time.RFC3339Nano, updated, "session.updated_at")
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = createdAt
	rec.UpdatedAt = updatedAt
	return &rec, nil
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]events.SessionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT session_id, title, created_at, updated_at
         FROM sessions
         ORDER BY updated_at DESC, created_at DESC
         LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	items := make([]events.SessionRecord, 0)
	for rows.Next() {
		var (
			rec     events.SessionRecord
			created string
			updated string
		)
		if err := rows.Scan(&rec.SessionID, &rec.Title, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session.created_at")
		if err != nil {
			return nil, err
		}
		updatedAt, err := parseTimestamp(time.RFC3339Nano, updated, "session.updated_at")
		if err != nil {
			return nil, err
		}
		rec.CreatedAt = createdAt
		rec.UpdatedAt = updatedAt
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM session_messages WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete session_messages: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM conversation_segments WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete conversation_segments: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	return nil
}

func (s *Store) UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE session_id = ? AND trim(title) = ''`,
		title,
		formatTimestamp(time.Now()),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	if _, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	return nil
}

func (s *Store) UpdateSessionTitle(ctx context.Context, sessionID, title string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE session_id = ?`,
		strings.TrimSpace(title),
		formatTimestamp(time.Now()),
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session title rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID)
	}
	return nil
}

func (s *Store) CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin create fresh session turn: %w", err)
	}
	defer func() {
		if tx == nil {
			return
		}
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("create fresh session turn rollback: %w", rollbackErr))
		}
	}()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO sessions(session_id, title, created_at, updated_at) VALUES(?, ?, ?, ?)`,
		sessionID,
		title,
		formatTimestamp(now),
		formatTimestamp(now),
	); err != nil {
		return 0, fmt.Errorf("create fresh session turn: create session: %w", err)
	}

	const turnIndex = 1
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO session_messages(session_id, turn_index, role, content, content_parts, run_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		turnIndex,
		"user",
		input,
		"",
		"",
		formatTimestamp(now),
	); err != nil {
		return 0, fmt.Errorf("create fresh session turn: append session message: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit create fresh session turn: %w", err)
	}
	tx = nil
	return turnIndex, nil
}

func (s *Store) NextTurnIndex(ctx context.Context, sessionID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) FROM runs WHERE session_id = ?`, sessionID)
	var current int
	if err := row.Scan(&current); err != nil {
		return 0, fmt.Errorf("next turn index: %w", err)
	}
	return current + 1, nil
}

func (s *Store) PrepareChatTurn(ctx context.Context, sessionID, input, title string, historyLimit int) (int, []events.SessionMessageRecord, error) {
	if _, err := s.LoadSession(ctx, sessionID); err != nil {
		return 0, nil, err
	}
	turnIndex, err := s.NextTurnIndex(ctx, sessionID)
	if err != nil {
		return 0, nil, err
	}
	if _, err := s.AppendSessionMessage(sessionID, turnIndex, "user", input, ""); err != nil {
		return 0, nil, err
	}
	if err := s.UpdateSessionTitleIfEmpty(ctx, sessionID, title); err != nil {
		return 0, nil, err
	}
	items, err := s.ListSessionMessages(ctx, sessionID, historyLimit)
	if err != nil {
		return 0, nil, err
	}
	return turnIndex, items, nil
}

func (s *Store) AppendSessionMessage(sessionID string, turnIndex int, role, content, runID string) (*events.SessionMessageRecord, error) {
	return s.AppendSessionMessageWithParts(sessionID, turnIndex, role, content, nil, runID)
}

func (s *Store) AppendSessionMessageWithParts(sessionID string, turnIndex int, role, content string, parts []SessionMessagePart, runID string) (*events.SessionMessageRecord, error) {
	now := time.Now().UTC()
	contentParts, err := encodeSessionMessageParts(parts)
	if err != nil {
		return nil, err
	}
	result, err := s.db.Exec(
		`INSERT INTO session_messages(session_id, turn_index, role, content, content_parts, run_id, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		sessionID,
		turnIndex,
		role,
		content,
		string(contentParts),
		runID,
		formatTimestamp(now),
	)
	if err != nil {
		return nil, fmt.Errorf("append session message: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read session message id: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(now), sessionID); err != nil {
		return nil, fmt.Errorf("touch session updated_at: %w", err)
	}
	return &events.SessionMessageRecord{
		ID:           id,
		SessionID:    sessionID,
		TurnIndex:    turnIndex,
		Role:         role,
		Content:      content,
		ContentParts: contentParts,
		RunID:        runID,
		CreatedAt:    now,
	}, nil
}

func (s *Store) UpdateSessionMessageWithParts(ctx context.Context, id int64, content string, parts []SessionMessagePart) error {
	contentParts, err := encodeSessionMessageParts(parts)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE session_messages SET content = ?, content_parts = ? WHERE id = ?`,
		content,
		string(contentParts),
		id,
	)
	if err != nil {
		return fmt.Errorf("update session message: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update session message rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: %d", ErrSessionMessageNotFound, id)
	}
	return nil
}

func (s *Store) ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, session_id, turn_index, role, content, content_parts, run_id, created_at
         FROM session_messages
         WHERE session_id = ?
         ORDER BY id DESC
         LIMIT ?`,
		sessionID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	defer rows.Close()

	items := make([]events.SessionMessageRecord, 0)
	for rows.Next() {
		var (
			rec          events.SessionMessageRecord
			contentParts string
			created      string
		)
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &contentParts, &rec.RunID, &created); err != nil {
			return nil, fmt.Errorf("scan session message: %w", err)
		}
		createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session_message.created_at")
		if err != nil {
			return nil, err
		}
		rec.ContentParts = json.RawMessage(contentParts)
		rec.CreatedAt = createdAt
		items = append(items, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func (s *Store) NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) FROM session_messages WHERE session_id = ?`, sessionID)
	var current int
	if err := row.Scan(&current); err != nil {
		return 0, fmt.Errorf("next session message turn index: %w", err)
	}
	return current + 1, nil
}

func (s *Store) LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*events.SessionMessageRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, turn_index, role, content, content_parts, run_id, created_at
		 FROM session_messages
		 WHERE session_id = ? AND role = 'user' AND run_id = ''
		 ORDER BY id DESC
		 LIMIT 1`,
		sessionID,
	)
	var (
		rec          events.SessionMessageRecord
		contentParts string
		created      string
	)
	if err := row.Scan(&rec.ID, &rec.SessionID, &rec.TurnIndex, &rec.Role, &rec.Content, &contentParts, &rec.RunID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: latest unbound user message for %s", ErrSessionMessageNotFound, sessionID)
		}
		return nil, fmt.Errorf("load latest unbound user message: %w", err)
	}
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "session_message.created_at")
	if err != nil {
		return nil, err
	}
	rec.ContentParts = json.RawMessage(contentParts)
	rec.CreatedAt = createdAt
	return &rec, nil
}

func (s *Store) BindLatestUserMessageRunID(ctx context.Context, sessionID string, turnIndex int, runID string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE session_messages
		 SET run_id = ?
		 WHERE id = (
			 SELECT id
			 FROM session_messages
			 WHERE session_id = ?
			   AND turn_index = ?
			   AND role = 'user'
			   AND run_id = ''
			 ORDER BY id DESC
			 LIMIT 1
		 )`,
		runID,
		sessionID,
		turnIndex,
	)
	if err != nil {
		return fmt.Errorf("bind latest user message run id: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("bind latest user message run id rows affected: %w", err)
	}
	if affected == 0 {
		return errors.New("latest user session message not found")
	}
	return nil
}

func encodeSessionMessageParts(parts []SessionMessagePart) (json.RawMessage, error) {
	if len(parts) == 0 {
		return nil, nil
	}
	payload, err := json.Marshal(parts)
	if err != nil {
		return nil, fmt.Errorf("marshal session message parts: %w", err)
	}
	return json.RawMessage(payload), nil
}

func (s *Store) UpsertSessionSummary(ctx context.Context, summary runtimehistory.SessionSummary) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_summaries(session_id, source_run_id, run_status, summary, updated_at)
		 VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
		     source_run_id = excluded.source_run_id,
		     run_status = excluded.run_status,
		     summary = excluded.summary,
		     updated_at = excluded.updated_at`,
		strings.TrimSpace(summary.SessionID),
		strings.TrimSpace(summary.SourceRunID),
		strings.TrimSpace(summary.RunStatus),
		strings.TrimSpace(summary.Summary),
		formatTimestamp(summary.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert session summary: %w", err)
	}
	return nil
}

func (s *Store) GetSessionSummary(ctx context.Context, sessionID string) (*runtimehistory.SessionSummary, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT session_id, source_run_id, run_status, summary, updated_at
		 FROM session_summaries WHERE session_id = ?`,
		strings.TrimSpace(sessionID),
	)
	var (
		record    runtimehistory.SessionSummary
		updatedAt string
	)
	if err := row.Scan(&record.SessionID, &record.SourceRunID, &record.RunStatus, &record.Summary, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get session summary: %w", err)
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, updatedAt, "session_summary.updated_at")
	if err != nil {
		return nil, err
	}
	record.UpdatedAt = parsed
	return &record, nil
}

func (s *Store) ListAllSessionSummaries(ctx context.Context) ([]runtimehistory.SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, source_run_id, run_status, summary, updated_at
		 FROM session_summaries
		 ORDER BY session_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all session summaries: %w", err)
	}
	defer rows.Close()

	items := make([]runtimehistory.SessionSummary, 0)
	for rows.Next() {
		var (
			record    runtimehistory.SessionSummary
			updatedAt string
		)
		if err := rows.Scan(&record.SessionID, &record.SourceRunID, &record.RunStatus, &record.Summary, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan session summary: %w", err)
		}
		parsed, err := parseTimestamp(fixedTimestampLayout, updatedAt, "session_summary.updated_at")
		if err != nil {
			return nil, err
		}
		record.UpdatedAt = parsed
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session summaries: %w", err)
	}
	return items, nil
}

type sessionMessageResultSummary struct {
	changed     []string
	verified    []string
	risks       []string
	disclosures []SessionDisclosureItem
	reasoning   string
}

type sessionMessageResultSummaryBuilder struct {
	changed   map[string]struct{}
	verified  map[string]struct{}
	risks     map[string]struct{}
	memory    *SessionDisclosureItem
	skill     *SessionDisclosureItem
	reasoning string
}

func (s *Store) buildSessionMessageResultSummary(ctx context.Context, runID string) (sessionMessageResultSummary, error) {
	records, err := s.LoadEvents(ctx, runID)
	if err != nil {
		return sessionMessageResultSummary{}, fmt.Errorf("build session result summary: load events: %w", err)
	}
	builder := newSessionMessageResultSummaryBuilder()
	if err := builder.addEvents(records); err != nil {
		return sessionMessageResultSummary{}, err
	}

	plan, err := s.LoadPlanByRun(ctx, runID)
	if err != nil && !errors.Is(err, ErrPlanNotFound) {
		return sessionMessageResultSummary{}, fmt.Errorf("build session result summary: load plan: %w", err)
	}
	if plan != nil {
		builder.addPlan(plan)
	}
	return builder.summary(), nil
}

func newSessionMessageResultSummaryBuilder() *sessionMessageResultSummaryBuilder {
	return &sessionMessageResultSummaryBuilder{
		changed:  make(map[string]struct{}),
		verified: make(map[string]struct{}),
		risks:    make(map[string]struct{}),
	}
}

func (b *sessionMessageResultSummaryBuilder) addEvents(records []events.EventRecord) error {
	for _, record := range records {
		if !sessionSummaryKnownEvent(record.Kind) {
			continue
		}
		payload, err := sessionSummaryPayload(record)
		if err != nil {
			return err
		}
		toolName := sessionSummaryToolName(payload)
		args := strings.TrimSpace(sessionSummaryString(payload["arguments_json"]))

		switch record.Kind {
		case "tool.call.succeeded":
			if command, err := sessionSummaryCommand(toolName, args); err != nil {
				return fmt.Errorf("build session result summary: event sequence=%d command: %w", record.Sequence, err)
			} else if command != "" {
				b.addVerified(command)
			}
			if sessionSummaryMutationTool(toolName) {
				paths, err := sessionSummaryPathsFromArguments(args)
				if err != nil {
					return fmt.Errorf("build session result summary: event sequence=%d paths: %w", record.Sequence, err)
				}
				b.addChanged(paths...)
			}
		case "tool.call.failed", "tool.call.interrupted":
			b.addRisk(sessionSummaryToolRisk(toolName, payload))
		case "subagent.completed":
			if summary := strings.TrimSpace(sessionSummaryString(payload["summary"])); summary != "" {
				b.addVerified(summary)
			}
		case "subagent.failed":
			b.addRisk(sessionSummaryEventRisk(record.Kind, payload))
		case "memory.prepared":
			if err := b.addMemoryDisclosure(record.Sequence, payload); err != nil {
				return err
			}
		case "skill.selected", "skill.loaded":
			if err := b.addSkillDisclosure(record.Sequence, record.Kind, payload); err != nil {
				return err
			}
		case "agent.message", "run.completed":
			b.addReasoning(payload)
		}
	}
	return nil
}

func (b *sessionMessageResultSummaryBuilder) addPlan(plan *PlanRecord) {
	for _, step := range plan.Steps {
		for _, item := range step.Evidence {
			status := strings.TrimSpace(item.Status)
			kind := strings.TrimSpace(item.Kind)
			if status == "failed" {
				b.addRisk(sessionSummaryPlanRisk(item))
				continue
			}
			switch kind {
			case "diff":
				b.addChanged(item.Paths...)
			}
			if status != "passed" && status != "confirmed" {
				continue
			}
			switch kind {
			case "command", "test":
				if command := sessionSummaryCommandText(item.Command); command != "" {
					b.addVerified(command)
				} else {
					b.addVerified(item.Summary)
				}
			case "manual", "subagent":
				b.addVerified(item.Summary)
			}
		}
	}
}

func (b *sessionMessageResultSummaryBuilder) summary() sessionMessageResultSummary {
	disclosures := make([]SessionDisclosureItem, 0, 2)
	if b.memory != nil {
		disclosures = append(disclosures, *b.memory)
	}
	if b.skill != nil {
		disclosures = append(disclosures, *b.skill)
	}
	return sessionMessageResultSummary{
		changed:     sessionSummarySortedKeys(b.changed),
		verified:    sessionSummarySortedKeys(b.verified),
		risks:       sessionSummarySortedKeys(b.risks),
		disclosures: disclosures,
		reasoning:   b.reasoning,
	}
}

func (b *sessionMessageResultSummaryBuilder) addChanged(items ...string) {
	sessionSummaryAddTrimmed(b.changed, items...)
}

func (b *sessionMessageResultSummaryBuilder) addVerified(items ...string) {
	sessionSummaryAddTrimmed(b.verified, items...)
}

func (b *sessionMessageResultSummaryBuilder) addRisk(items ...string) {
	sessionSummaryAddTrimmed(b.risks, items...)
}

func (b *sessionMessageResultSummaryBuilder) addReasoning(payload map[string]any) {
	if strings.TrimSpace(b.reasoning) != "" {
		return
	}
	message, ok := payload["message"].(map[string]any)
	if !ok {
		return
	}
	reasoning := strings.TrimSpace(sessionSummaryString(message["reasoning"]))
	if reasoning == "" {
		return
	}
	b.reasoning = reasoning
}

func (b *sessionMessageResultSummaryBuilder) addMemoryDisclosure(sequence int64, payload map[string]any) error {
	if b.memory != nil {
		return nil
	}
	prepared, ok := payload["memory_prepared"].(map[string]any)
	if !ok {
		return fmt.Errorf("build session result summary: event sequence=%d memory.prepared missing memory_prepared object", sequence)
	}
	count := sessionSummaryInt(prepared["entry_count"])
	if count <= 0 {
		if entries, ok := prepared["entries"].([]any); ok {
			count = len(entries)
		}
	}
	if count <= 0 {
		return nil
	}
	label := fmt.Sprintf("Prepared %d memory entries", count)
	if count == 1 {
		label = "Prepared 1 memory entry"
	}
	detail := strings.TrimSpace(sessionSummaryString(prepared["workspace_scope"]))
	item := SessionDisclosureItem{
		Kind:   "memory",
		Label:  label,
		Detail: detail,
		Tone:   "memory",
	}
	b.memory = &item
	return nil
}

func (b *sessionMessageResultSummaryBuilder) addSkillDisclosure(sequence int64, kind string, payload map[string]any) error {
	if b.skill != nil {
		return nil
	}
	skill, ok := payload["skill"].(map[string]any)
	if !ok {
		return fmt.Errorf("build session result summary: event sequence=%d %s missing skill object", sequence, kind)
	}
	name := sessionSummaryFirstString(skill, "name", "selected_id")
	if name == "" {
		return fmt.Errorf("build session result summary: event sequence=%d %s missing skill name", sequence, kind)
	}
	skillID := strings.TrimSpace(sessionSummaryString(skill["selected_id"]))
	label := "Used skill"
	tone := "skill"
	if sessionSummaryString(skill["origin"]) == "distilled" {
		label = "Used procedure"
		tone = "procedure"
	}
	item := SessionDisclosureItem{
		Kind:    "skill",
		Label:   label,
		Detail:  name,
		Tone:    tone,
		SkillID: skillID,
	}
	b.skill = &item
	return nil
}

func sessionSummaryKnownEvent(kind string) bool {
	switch kind {
	case "tool.call.succeeded",
		"tool.call.failed",
		"tool.call.interrupted",
		"subagent.completed",
		"subagent.failed",
		"memory.prepared",
		"skill.selected",
		"skill.loaded",
		"agent.message",
		"run.completed":
		return true
	default:
		return false
	}
}

func sessionSummaryPayload(record events.EventRecord) (map[string]any, error) {
	payload, ok := record.Payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("build session result summary: event payload must be object run_id=%s sequence=%d kind=%s", record.RunID, record.Sequence, record.Kind)
	}
	return payload, nil
}

func sessionSummaryToolName(payload map[string]any) string {
	for _, key := range []string{"tool_name", "name"} {
		if value := strings.TrimSpace(sessionSummaryString(payload[key])); value != "" {
			return value
		}
	}
	toolCall, ok := payload["tool_call"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"tool_name", "name"} {
		if value := strings.TrimSpace(sessionSummaryString(toolCall[key])); value != "" {
			return value
		}
	}
	return ""
}

func sessionSummaryToolRisk(toolName string, payload map[string]any) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "tool"
	}
	for _, key := range []string{"error", "reason"} {
		if value := compactContinuationText(sessionSummaryString(payload[key]), 180); value != "" {
			return fmt.Sprintf("%s failed: %s", name, value)
		}
	}
	return fmt.Sprintf("%s failed", name)
}

func sessionSummaryEventRisk(kind string, payload map[string]any) string {
	for _, key := range []string{"error", "summary"} {
		if value := compactContinuationText(sessionSummaryString(payload[key]), 180); value != "" {
			return fmt.Sprintf("%s: %s", kind, value)
		}
	}
	return kind
}

func sessionSummaryPlanRisk(item PlanEvidence) string {
	if errText := compactContinuationText(item.Error, 180); errText != "" {
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			return fmt.Sprintf("%s: %s", summary, errText)
		}
		return errText
	}
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		return summary
	}
	if command := sessionSummaryCommandText(item.Command); command != "" {
		return command
	}
	if kind := strings.TrimSpace(item.Kind); kind != "" {
		return kind + " failed"
	}
	return "plan evidence failed"
}

func sessionSummaryCommand(toolName string, argumentsJSON string) (string, error) {
	if strings.TrimSpace(toolName) != "run_command" {
		return "", nil
	}
	if strings.TrimSpace(argumentsJSON) == "" {
		return "", nil
	}
	var payload struct {
		Command any `json:"command"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return "", err
	}
	return sessionSummaryCommandValue(payload.Command), nil
}

func sessionSummaryCommandValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return sessionSummaryCommandText(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, sessionSummaryString(item))
		}
		return sessionSummaryCommandText(parts)
	default:
		return ""
	}
}

func sessionSummaryCommandText(command []string) string {
	parts := make([]string, 0, len(command))
	for _, item := range command {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

func sessionSummaryMutationTool(toolName string) bool {
	name := strings.TrimSpace(toolName)
	switch name {
	case "create_file", "write_file", "edit_file", "delete_file", "move_file", "rename_file", "apply_patch":
		return true
	}
	for _, token := range []string{"create", "write", "edit", "delete", "move", "rename", "patch"} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func sessionSummaryPathsFromArguments(argumentsJSON string) ([]string, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil, err
	}
	paths := make([]string, 0, 4)
	for _, key := range []string{"path", "file_path", "target", "root_dir", "work_dir"} {
		if value := strings.TrimSpace(sessionSummaryString(payload[key])); value != "" {
			paths = append(paths, value)
		}
	}
	paths = append(paths, sessionSummaryStringSlice(payload["paths"])...)
	return paths, nil
}

func sessionSummaryString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func sessionSummaryFirstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(sessionSummaryString(payload[key])); value != "" {
			return value
		}
	}
	return ""
}

func sessionSummaryInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func sessionSummaryStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, sessionSummaryString(item))
		}
		return out
	default:
		return nil
	}
}

func sessionSummaryAddTrimmed(target map[string]struct{}, items ...string) {
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			target[trimmed] = struct{}{}
		}
	}
}

func sessionSummarySortedKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (s *Store) HasAssistantMessageForRunContent(runID, content string) (bool, error) {
	row := s.db.QueryRow(`SELECT COUNT(1) FROM session_messages WHERE run_id = ? AND role = 'assistant' AND content = ?`, runID, content)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("assistant message for run: %w", err)
	}
	return count > 0, nil
}

func (s *Store) SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error {
	action, err := s.LoadPendingAction(ctx, actionID)
	if err != nil {
		return err
	}
	run, err := s.LoadRun(ctx, action.RunID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(run.SessionID) == "" {
		return nil
	}
	content, parts, err := decisionSessionMessageForPendingAction(action)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}

	messageID, found, err := s.findDecisionSessionMessageID(ctx, action.RunID, action.ActionID)
	if err != nil {
		return err
	}
	if found {
		return s.UpdateSessionMessageWithParts(ctx, messageID, content, parts)
	}
	_, err = s.AppendSessionMessageWithParts(run.SessionID, run.TurnIndex, "assistant", content, parts, action.RunID)
	return err
}

func (s *Store) findDecisionSessionMessageID(ctx context.Context, runID, actionID string) (int64, bool, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, content_parts
		 FROM session_messages
		 WHERE run_id = ? AND role = 'assistant'
		 ORDER BY id ASC`,
		runID,
	)
	if err != nil {
		return 0, false, fmt.Errorf("find decision session message: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id           int64
			contentParts string
		)
		if err := rows.Scan(&id, &contentParts); err != nil {
			return 0, false, fmt.Errorf("find decision session message scan: %w", err)
		}
		matches, err := decisionMessageHasActionID(contentParts, actionID)
		if err != nil {
			return 0, false, fmt.Errorf("find decision session message %d content parts: %w", id, err)
		}
		if matches {
			return id, true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return 0, false, fmt.Errorf("find decision session message rows: %w", err)
	}
	return 0, false, nil
}

func decisionMessageHasActionID(contentParts string, actionID string) (bool, error) {
	if strings.TrimSpace(contentParts) == "" {
		return false, nil
	}
	var parts []SessionMessagePart
	if err := json.Unmarshal([]byte(contentParts), &parts); err != nil {
		return false, err
	}
	for _, part := range parts {
		if part.Kind == "decision" && strings.TrimSpace(part.DecisionID) == strings.TrimSpace(actionID) {
			return true, nil
		}
	}
	return false, nil
}

func decisionSessionMessageForPendingAction(action *events.PendingActionRecord) (string, []SessionMessagePart, error) {
	if action == nil {
		return "", nil, errors.New("pending action is nil")
	}
	switch action.Kind {
	case events.PendingActionKindElicitation:
		return elicitationDecisionSessionMessage(action)
	case events.PendingActionKindOperatorQuestion:
		return operatorQuestionDecisionSessionMessage(action)
	default:
		return "", nil, fmt.Errorf("pending action %s has unsupported kind %q", action.ActionID, action.Kind)
	}
}

func elicitationDecisionSessionMessage(action *events.PendingActionRecord) (string, []SessionMessagePart, error) {
	message, err := decodeElicitationPayload(action.PayloadJSON)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(message) == "" {
		return "", nil, fmt.Errorf("pending action %s has empty elicitation message", action.ActionID)
	}
	parts := []SessionMessagePart{{
		Kind:             "decision",
		DecisionID:       action.ActionID,
		Question:         message,
		Status:           string(action.Status),
		SelectedOptionID: decisionSelectedOptionID(action),
		Options: []SessionDecisionOption{
			{ID: "accept", Label: "Accept"},
			{ID: "decline", Label: "Decline"},
		},
	}}
	if strings.TrimSpace(action.RunID) != "" {
		parts = append(parts, SessionMessagePart{
			Kind:        "technical_detail_link",
			RunID:       action.RunID,
			DetailRunID: action.RunID,
			Label:       "View technical details",
		})
	}
	return message, parts, nil
}

func operatorQuestionDecisionSessionMessage(action *events.PendingActionRecord) (string, []SessionMessagePart, error) {
	payload, err := decodeOperatorQuestionPayload(action.PayloadJSON)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(payload.Question) == "" {
		return "", nil, fmt.Errorf("pending action %s has empty operator question", action.ActionID)
	}
	decision := decodeOperatorQuestionDecision(action.DecisionJSON)
	parts := []SessionMessagePart{{
		Kind:             "decision",
		DecisionID:       action.ActionID,
		Question:         payload.Question,
		Status:           string(action.Status),
		SelectedOptionID: decision.SelectedOptionID,
		Answer:           decision.Answer,
		Options:          sessionDecisionOptionsFromPendingActionOptions(payload.Options),
	}}
	if strings.TrimSpace(action.RunID) != "" {
		parts = append(parts, SessionMessagePart{
			Kind:        "technical_detail_link",
			RunID:       action.RunID,
			DetailRunID: action.RunID,
			Label:       "View technical details",
		})
	}
	return payload.Question, parts, nil
}

func decisionSelectedOptionID(action *events.PendingActionRecord) string {
	if action != nil && action.Kind == events.PendingActionKindOperatorQuestion {
		return decodeOperatorQuestionDecision(action.DecisionJSON).SelectedOptionID
	}
	switch action.Status {
	case events.PendingActionStatusApproved:
		return "accept"
	case events.PendingActionStatusRejected:
		return "decline"
	case events.PendingActionStatusResolved:
		var payload map[string]any
		if err := json.Unmarshal([]byte(action.DecisionJSON), &payload); err == nil {
			if value, ok := payload["action"].(string); ok {
				switch strings.TrimSpace(strings.ToLower(value)) {
				case "accept":
					return "accept"
				case "decline":
					return "decline"
				}
			}
		}
	}
	return ""
}

func decodeOperatorQuestionPayload(payloadJSON string) (events.OperatorQuestionPayload, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return events.OperatorQuestionPayload{}, errors.New("operator question payload is empty")
	}
	var payload events.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return events.OperatorQuestionPayload{}, fmt.Errorf("decode operator question payload: %w", err)
	}
	return payload, nil
}

func decodeOperatorQuestionDecision(decisionJSON string) events.OperatorQuestionDecision {
	if strings.TrimSpace(decisionJSON) == "" {
		return events.OperatorQuestionDecision{}
	}
	var decision events.OperatorQuestionDecision
	if err := json.Unmarshal([]byte(decisionJSON), &decision); err != nil {
		return events.OperatorQuestionDecision{}
	}
	return decision
}

func sessionDecisionOptionsFromPendingActionOptions(items []events.PendingActionOption) []SessionDecisionOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]SessionDecisionOption, 0, len(items))
	for _, item := range items {
		out = append(out, SessionDecisionOption{
			ID:          strings.TrimSpace(item.ID),
			Label:       strings.TrimSpace(item.Label),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func decodeElicitationPayload(payloadJSON string) (string, error) {
	if strings.TrimSpace(payloadJSON) == "" {
		return "", errors.New("elicitation payload is empty")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return "", fmt.Errorf("decode elicitation payload: %w", err)
	}
	message := stringValue(payload, "message")
	if message == "" {
		message = stringValue(payload, "Message")
	}
	if message == "" {
		message = stringValue(payload, "content")
	}
	return message, nil
}

func stringValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func (s *Store) SyncAssistantMessageForRun(ctx context.Context, runID string) error {
	return s.syncAssistantMessageForRun(ctx, runID, "")
}

func (s *Store) SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status events.RunStatus) error {
	return s.syncAssistantMessageForRun(ctx, runID, status)
}

func (s *Store) syncAssistantMessageForRun(ctx context.Context, runID string, statusOverride events.RunStatus) error {
	run, err := s.LoadRun(ctx, runID)
	if err != nil {
		return err
	}
	if statusOverride != "" {
		run.Status = statusOverride
	}
	if strings.TrimSpace(run.SessionID) == "" {
		return nil
	}
	summary := sessionMessageResultSummary{}
	if run.Status == events.RunStatusSucceeded {
		summary, err = s.buildSessionMessageResultSummary(ctx, run.RunID)
		if err != nil {
			return err
		}
	}
	content, parts, err := assistantSessionMessageForRun(run, summary)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return nil
	}
	exists, err := s.HasAssistantMessageForRunContent(runID, content)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.AppendSessionMessageWithParts(run.SessionID, run.TurnIndex, "assistant", content, parts, runID)
	return err
}

func assistantSessionMessageForRun(run *events.RunRecord, summary sessionMessageResultSummary) (string, []SessionMessagePart, error) {
	if run == nil {
		return "", nil, nil
	}
	switch run.Status {
	case events.RunStatusSucceeded:
		text := strings.TrimSpace(run.Output)
		if text == "" {
			text = "Task completed."
		}
		parts := []SessionMessagePart{
			{Kind: "text", Text: text},
		}
		if strings.TrimSpace(summary.reasoning) != "" {
			parts = append(parts, SessionMessagePart{
				Kind:      "reasoning",
				Reasoning: summary.reasoning,
			})
		}
		if len(summary.disclosures) > 0 {
			parts = append(parts, SessionMessagePart{
				Kind:  "disclosure",
				Items: summary.disclosures,
			})
		}
		parts = append(parts, SessionMessagePart{
			Kind:        "result",
			Title:       "Task completed",
			Changed:     summary.changed,
			Verified:    summary.verified,
			Risks:       summary.risks,
			DetailRunID: run.RunID,
		})
		if strings.TrimSpace(run.RunID) != "" {
			parts = append(parts, SessionMessagePart{
				Kind:        "technical_detail_link",
				RunID:       run.RunID,
				DetailRunID: run.RunID,
				Label:       "View technical details",
			})
		}
		return text, parts, nil
	case events.RunStatusFailed:
		content := "Acorn could not finish this turn."
		parts := []SessionMessagePart{{
			Kind:        "work_status",
			Status:      "failed",
			Title:       "Acorn could not finish",
			Summary:     failureSummary(run),
			DetailRunID: run.RunID,
		}, {
			Kind:        "technical_detail_link",
			RunID:       run.RunID,
			DetailRunID: run.RunID,
			Label:       "View technical details",
		}}
		return content, parts, nil
	case events.RunStatusInterrupted:
		content := "Acorn paused before continuing."
		parts := []SessionMessagePart{{
			Kind:        "work_status",
			Status:      "interrupted",
			Title:       "Paused before continuing",
			Summary:     interruptedSummary(run),
			DetailRunID: run.RunID,
			Action: &SessionMessageAction{
				Kind:  "resume_run",
				RunID: run.RunID,
				Label: "Resume",
			},
		}, {
			Kind:        "technical_detail_link",
			RunID:       run.RunID,
			DetailRunID: run.RunID,
			Label:       "View technical details",
		}}
		return content, parts, nil
	default:
		return "", nil, nil
	}
}

func failureSummary(run *events.RunRecord) string {
	if run == nil {
		return "The run failed before producing a final answer."
	}
	if errText := compactContinuationText(run.Error, 220); errText != "" {
		return errText
	}
	if output := compactContinuationText(run.Output, 220); output != "" {
		return output
	}
	return "The run failed before producing a final answer."
}

func interruptedSummary(run *events.RunRecord) string {
	if run == nil {
		return "Acorn paused at a real interrupt."
	}
	if output := compactContinuationText(run.Output, 220); output != "" {
		return output
	}
	return "Acorn paused at a real interrupt. Resume this run only when you want to continue the same execution."
}

func compactContinuationText(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed
	}
	return string(runes[:limit]) + "..."
}

func (s *Store) LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*events.RunRecord, error) {
	if len(sessionIDs) == 0 {
		return map[string]*events.RunRecord{}, nil
	}

	seen := make(map[string]struct{}, len(sessionIDs))
	args := make([]any, 0, len(sessionIDs))
	placeholders := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		trimmed := strings.TrimSpace(sessionID)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		args = append(args, trimmed)
		placeholders = append(placeholders, "?")
	}
	if len(placeholders) == 0 {
		return map[string]*events.RunRecord{}, nil
	}

	query := fmt.Sprintf(
		`WITH ranked_runs AS (
			SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at,
				ROW_NUMBER() OVER (
					PARTITION BY session_id
					ORDER BY turn_index DESC, updated_at DESC
				) AS row_num
			FROM runs
			WHERE session_id IN (%s)
		)
		SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at
		FROM ranked_runs
		WHERE row_num = 1`,
		strings.Join(placeholders, ", "),
	)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load latest runs for sessions: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*events.RunRecord, len(placeholders))
	for rows.Next() {
		rec, err := scanRunRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("load latest runs for sessions: %w", err)
		}
		result[rec.SessionID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load latest runs for sessions: %w", err)
	}
	return result, nil
}

func (s *Store) LoadLatestRunForSession(ctx context.Context, sessionID string) (*events.RunRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT run_id, session_id, turn_index, status, input_text, output_text, error_text, checkpoint_id, orchestration_mode, parent_run_id, depth, created_at, updated_at
         FROM runs
         WHERE session_id = ?
         ORDER BY turn_index DESC, updated_at DESC
         LIMIT 1`,
		sessionID,
	)
	rec, err := scanRunRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("load latest run for session: %w", err)
	}
	return rec, nil
}

func (s *Store) GetConversationHistorySegment(ctx context.Context, segmentID int64) (*runtimehistory.HistoryHit, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, run_status, user_content || char(10) || assistant_content, created_at
		 FROM conversation_segments WHERE id = ?`,
		segmentID,
	)
	hit, err := scanHistoryHit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation history segment: %w", err)
	}
	return hit, nil
}

func (s *Store) GetConversationHistorySegmentByRunID(ctx context.Context, runID string) (*runtimehistory.HistoryHit, error) {
	trimmedRunID := strings.TrimSpace(runID)
	if trimmedRunID == "" {
		return nil, errors.New("run id is required")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, run_status, user_content || char(10) || assistant_content, created_at
		 FROM conversation_segments WHERE run_id = ?`,
		trimmedRunID,
	)
	hit, err := scanHistoryHit(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get conversation history segment by run: %w", err)
	}
	return hit, nil
}

func scanHistoryHit(scanner interface{ Scan(dest ...any) error }) (*runtimehistory.HistoryHit, error) {
	var (
		hit       runtimehistory.HistoryHit
		createdAt string
	)
	if err := scanner.Scan(&hit.SegmentID, &hit.SessionID, &hit.RunID, &hit.RunStatus, &hit.Content, &createdAt); err != nil {
		return nil, err
	}
	timestamp, err := parseTimestamp(fixedTimestampLayout, createdAt, "conversation_segment.created_at")
	if err != nil {
		return nil, err
	}
	hit.Timestamp = timestamp
	return &hit, nil
}
