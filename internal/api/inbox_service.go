package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	defaultInboxPendingActionLimit = 20
	defaultInboxActiveRunLimit     = 20
	defaultInboxTerminalRunLimit   = 20
	runSummaryPreviewMaxRunes      = 96
	runSummaryTitleMaxRunes        = 64
)

type InboxService struct {
	store        StoreView
	capabilities *CapabilitiesService
}

func NewInboxService(store StoreView, capabilities *CapabilitiesService) *InboxService {
	return &InboxService{store: store, capabilities: capabilities}
}

type MobileInbox struct {
	PendingActions     []PendingActionSummary
	ActiveRuns         []RunSummary
	RecentTerminalRuns []RunSummary
	System             SystemCapabilities
}

type RunSummary struct {
	RunID          string
	ThreadID       string
	ThreadTitle    string
	Status         string
	Mode           string
	Preview        string
	LastEventLabel string
	AttentionLevel string
	DurationMS     int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PendingActionSummary struct {
	ActionID  string
	RunID     string
	ThreadID  string
	Kind      string
	Status    string
	Title     string
	Body      string
	Options   []PendingActionOption
	CreatedAt time.Time
}

type PendingActionOption struct {
	ID          string
	Label       string
	Description string
}

func (s *InboxService) Load(ctx context.Context) (*MobileInbox, error) {
	if s == nil || s.store == nil || s.capabilities == nil {
		return nil, errors.New("inbox service is not initialized")
	}
	pendingActions, err := s.loadPendingActionSummaries(ctx)
	if err != nil {
		return nil, err
	}
	activeRuns, err := s.loadRunSummaries(ctx, s.store.ListActiveRuns, defaultInboxActiveRunLimit)
	if err != nil {
		return nil, err
	}
	recentTerminalRuns, err := s.loadRunSummaries(ctx, s.store.ListRecentTerminalRuns, defaultInboxTerminalRunLimit)
	if err != nil {
		return nil, err
	}
	snapshot := s.capabilities.Snapshot(ctx, CapabilitySnapshotOptions{ProbeMCP: false})
	return &MobileInbox{
		PendingActions:     pendingActions,
		ActiveRuns:         activeRuns,
		RecentTerminalRuns: recentTerminalRuns,
		System:             snapshot,
	}, nil
}

func (s *InboxService) loadPendingActionSummaries(ctx context.Context) ([]PendingActionSummary, error) {
	records, err := s.store.ListPendingActions(ctx, defaultInboxPendingActionLimit)
	if err != nil {
		return nil, err
	}
	items := make([]PendingActionSummary, 0, len(records))
	for _, record := range records {
		summary, err := s.projectPendingActionSummary(ctx, record)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

type runListFunc func(context.Context, int) ([]core.RunRecord, error)

func (s *InboxService) loadRunSummaries(ctx context.Context, list runListFunc, limit int) ([]RunSummary, error) {
	records, err := list(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunSummary, 0, len(records))
	for _, record := range records {
		summary, err := s.projectRunSummary(ctx, record)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

func (s *InboxService) projectPendingActionSummary(ctx context.Context, record core.PendingActionRecord) (PendingActionSummary, error) {
	run, err := s.store.LoadRun(ctx, record.RunID)
	if err != nil {
		return PendingActionSummary{}, err
	}
	return buildPendingActionSummary(record, *run)
}

func buildPendingActionSummary(record core.PendingActionRecord, run core.RunRecord) (PendingActionSummary, error) {
	if record.Status != core.PendingActionStatusPending {
		return PendingActionSummary{}, fmt.Errorf("%w: unsupported pending action status %q", ErrClientProjectionFailed, record.Status)
	}
	switch record.Kind {
	case core.PendingActionKindElicitation:
		return buildElicitationPendingActionSummary(record, run)
	case core.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionPendingActionSummary(record, run)
	default:
		return PendingActionSummary{}, fmt.Errorf("%w: unsupported pending action kind %q", ErrClientProjectionFailed, record.Kind)
	}
}

func buildElicitationPendingActionSummary(record core.PendingActionRecord, run core.RunRecord) (PendingActionSummary, error) {
	body, err := pendingActionBody(record)
	if err != nil {
		return PendingActionSummary{}, err
	}
	title := strings.TrimSpace(record.Subject)
	if title == "" {
		title = "Approval required"
	}
	return PendingActionSummary{
		ActionID: record.ActionID,
		RunID:    record.RunID,
		ThreadID: run.SessionID,
		Kind:     string(record.Kind),
		Status:   string(record.Status),
		Title:    title,
		Body:     body,
		Options: []PendingActionOption{
			{ID: "accept", Label: "Accept"},
			{ID: "decline", Label: "Decline"},
		},
		CreatedAt: record.CreatedAt,
	}, nil
}

func buildOperatorQuestionPendingActionSummary(record core.PendingActionRecord, run core.RunRecord) (PendingActionSummary, error) {
	payload, err := operatorQuestionPayload(record)
	if err != nil {
		return PendingActionSummary{}, err
	}
	title := strings.TrimSpace(record.Subject)
	if title == "" {
		title = "Operator question"
	}
	return PendingActionSummary{
		ActionID:  record.ActionID,
		RunID:     record.RunID,
		ThreadID:  run.SessionID,
		Kind:      string(record.Kind),
		Status:    string(record.Status),
		Title:     title,
		Body:      strings.TrimSpace(payload.Question),
		Options:   pendingActionOptionsFromEventOptions(payload.Options),
		CreatedAt: record.CreatedAt,
	}, nil
}

func pendingActionBody(record core.PendingActionRecord) (string, error) {
	payload, err := pendingActionPayload(record)
	if err != nil {
		return "", err
	}
	message, ok := payload["message"].(string)
	if !ok {
		return "", nil
	}
	return strings.TrimSpace(message), nil
}

func pendingActionPayload(record core.PendingActionRecord) (map[string]any, error) {
	raw := strings.TrimSpace(record.PayloadJSON)
	if raw == "" {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("%w: pending action %s payload_json: %v", ErrClientProjectionFailed, record.ActionID, err)
	}
	return payload, nil
}

func operatorQuestionPayload(record core.PendingActionRecord) (core.OperatorQuestionPayload, error) {
	raw := strings.TrimSpace(record.PayloadJSON)
	if raw == "" {
		return core.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s payload_json is empty", ErrClientProjectionFailed, record.ActionID)
	}
	var payload core.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return core.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s payload_json: %v", ErrClientProjectionFailed, record.ActionID, err)
	}
	if strings.TrimSpace(payload.Question) == "" {
		return core.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s operator question is empty", ErrClientProjectionFailed, record.ActionID)
	}
	return payload, nil
}

func pendingActionOptionsFromEventOptions(items []core.PendingActionOption) []PendingActionOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]PendingActionOption, 0, len(items))
	for _, item := range items {
		out = append(out, PendingActionOption{
			ID:          strings.TrimSpace(item.ID),
			Label:       strings.TrimSpace(item.Label),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func (s *InboxService) projectRunSummary(ctx context.Context, record core.RunRecord) (RunSummary, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return RunSummary{}, err
	}
	session, err := s.store.LoadSession(ctx, record.SessionID)
	if err != nil {
		return RunSummary{}, err
	}
	return RunSummary{
		RunID:          record.RunID,
		ThreadID:       record.SessionID,
		ThreadTitle:    runSummaryThreadTitle(*session, record),
		Status:         status,
		Preview:        runSummaryPreview(record),
		LastEventLabel: runSummaryLastEventLabel(record.Status),
		AttentionLevel: runSummaryAttentionLevel(record.Status),
		DurationMS:     runSummaryDurationMS(record),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.FinishedAt,
	}, nil
}

func runSummaryThreadTitle(session core.SessionRecord, run core.RunRecord) string {
	title := strings.TrimSpace(session.Title)
	if title != "" {
		return truncateRunes(title, runSummaryTitleMaxRunes)
	}
	title = strings.TrimSpace(run.Input)
	if title != "" {
		return truncateRunes(compactWhitespace(title), runSummaryTitleMaxRunes)
	}
	return "Untitled thread"
}

func runSummaryPreview(record core.RunRecord) string {
	switch record.Status {
	case core.RunStatusFailed:
		if preview := previewText(record.Error); preview != "" {
			return preview
		}
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case core.RunStatusSucceeded:
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case core.RunStatusInterrupted, core.RunStatusRunning:
		if preview := previewText(record.Input); preview != "" {
			return preview
		}
	}
	if preview := previewText(record.Input); preview != "" {
		return preview
	}
	if preview := previewText(record.Output); preview != "" {
		return preview
	}
	return ""
}

func previewText(value string) string {
	return truncateRunes(compactWhitespace(value), runSummaryPreviewMaxRunes)
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func runSummaryLastEventLabel(status core.RunStatus) string {
	switch status {
	case core.RunStatusRunning:
		return "Run is running"
	case core.RunStatusSucceeded:
		return "Run completed"
	case core.RunStatusInterrupted:
		return "Run interrupted"
	case core.RunStatusFailed:
		return "Run failed"
	default:
		return string(status)
	}
}

func runSummaryAttentionLevel(status core.RunStatus) string {
	switch status {
	case core.RunStatusRunning:
		return "running"
	case core.RunStatusFailed:
		return "failed"
	case core.RunStatusInterrupted:
		return "needs_action"
	default:
		return "normal"
	}
}

func runSummaryDurationMS(record core.RunRecord) int64 {
	if record.FinishedAt.IsZero() || record.CreatedAt.IsZero() {
		return 0
	}
	duration := record.FinishedAt.Sub(record.CreatedAt)
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}
