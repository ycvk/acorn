package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

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

func decisionSelectedOptionID(action *events.PendingActionRecord) string {
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
