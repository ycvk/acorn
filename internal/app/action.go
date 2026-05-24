package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/notifications"
	"github.com/ycvk/acorn/internal/store"
)

var ErrPendingActionDecisionInvalid = errors.New("pending action decision invalid")

type PendingActionService struct {
	store pendingActionDecisionStore
}

func NewPendingActionService(store pendingActionDecisionStore) *PendingActionService {
	return &PendingActionService{store: store}
}

type PendingActionDetail struct {
	PendingActionSummary
	Payload map[string]any
	Reason  string
	Rule    string
}

type PendingActionDecisionInput struct {
	Decision         string
	SelectedOptionID string
	Answer           string
}

func (s *PendingActionService) List(ctx context.Context, limit int) ([]PendingActionSummary, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	records, err := s.store.ListPendingActions(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]PendingActionSummary, 0, len(records))
	for _, record := range records {
		run, err := s.store.LoadRun(ctx, record.RunID)
		if err != nil {
			return nil, err
		}
		summary, err := buildPendingActionSummary(record, *run)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return items, nil
}

func (s *PendingActionService) Get(ctx context.Context, actionID string) (*PendingActionDetail, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("pending action id is required")
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.Status != events.PendingActionStatusPending {
		return nil, fmt.Errorf("%w: status %q", store.ErrPendingActionDecided, record.Status)
	}
	run, err := s.store.LoadRun(ctx, record.RunID)
	if err != nil {
		return nil, err
	}
	summary, err := buildPendingActionSummary(*record, *run)
	if err != nil {
		return nil, err
	}
	payload, err := pendingActionPayload(*record)
	if err != nil {
		return nil, err
	}
	return &PendingActionDetail{
		PendingActionSummary: summary,
		Payload:              payload,
		Reason:               record.Reason,
		Rule:                 record.Rule,
	}, nil
}

func (s *PendingActionService) Decide(ctx context.Context, actionID string, input PendingActionDecisionInput) (*events.PendingActionRecord, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("pending action store is nil")
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil, errors.New("pending action id is required")
	}

	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}

	status, decisionJSON, eventKind, eventPayload, err := buildPendingActionDecision(*record, input)
	if err != nil {
		return nil, err
	}

	record, err = s.store.DecidePendingAction(ctx, actionID, status, events.PendingActionModeDeferred, string(decisionJSON))
	if err != nil {
		return nil, err
	}
	if err := s.store.SyncDecisionMessageForPendingAction(ctx, actionID); err != nil {
		return nil, err
	}
	if _, err := s.store.AppendEventContext(ctx, record.RunID, eventKind, eventPayload); err != nil {
		return nil, fmt.Errorf("append %s event: %w", eventKind, err)
	}
	return record, nil
}

func buildPendingActionDecision(record events.PendingActionRecord, input PendingActionDecisionInput) (events.PendingActionStatus, []byte, string, map[string]any, error) {
	switch record.Kind {
	case events.PendingActionKindElicitation:
		return buildElicitationDecision(record, input)
	case events.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionDecision(record, input)
	default:
		return "", nil, "", nil, fmt.Errorf("unsupported pending action kind %q", record.Kind)
	}
}

func buildElicitationDecision(record events.PendingActionRecord, input PendingActionDecisionInput) (events.PendingActionStatus, []byte, string, map[string]any, error) {
	if strings.TrimSpace(input.SelectedOptionID) != "" || strings.TrimSpace(input.Answer) != "" {
		return "", nil, "", nil, fmt.Errorf("%w: elicitation accepts decision only", ErrPendingActionDecisionInvalid)
	}
	status, err := pendingActionDecisionStatus(input.Decision)
	if err != nil {
		return "", nil, "", nil, err
	}

	decisionJSON, err := json.Marshal(map[string]any{
		"action": statusToDecisionAction(status),
	})
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("marshal pending action decision: %w", err)
	}
	return status, decisionJSON, "elicitation.decided", map[string]any{
		"action_id": record.ActionID,
		"decision":  statusToDecisionAction(status),
	}, nil
}

func buildOperatorQuestionDecision(record events.PendingActionRecord, input PendingActionDecisionInput) (events.PendingActionStatus, []byte, string, map[string]any, error) {
	payload, err := operatorQuestionPayload(record)
	if err != nil {
		return "", nil, "", nil, err
	}
	action := strings.TrimSpace(strings.ToLower(input.Decision))
	selectedOptionID := strings.TrimSpace(input.SelectedOptionID)
	answer := strings.TrimSpace(input.Answer)

	var status events.PendingActionStatus
	switch action {
	case events.OperatorQuestionDecisionAnswer:
		status = events.PendingActionStatusApproved
		if err := validateOperatorAnswer(payload, selectedOptionID, answer); err != nil {
			return "", nil, "", nil, err
		}
	case events.OperatorQuestionDecisionDecline:
		status = events.PendingActionStatusRejected
		if selectedOptionID != "" || answer != "" {
			return "", nil, "", nil, fmt.Errorf("%w: declined operator question must not include selected_option_id or answer", ErrPendingActionDecisionInvalid)
		}
	default:
		return "", nil, "", nil, fmt.Errorf("%w: %q", ErrPendingActionDecisionInvalid, input.Decision)
	}

	decision := events.OperatorQuestionDecision{
		Action:           action,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
	}
	decisionJSON, err := json.Marshal(decision)
	if err != nil {
		return "", nil, "", nil, fmt.Errorf("marshal operator question decision: %w", err)
	}
	eventPayload := map[string]any{
		"action_id": record.ActionID,
		"question":  payload.Question,
		"decision":  action,
	}
	if selectedOptionID != "" {
		eventPayload["selected_option_id"] = selectedOptionID
	}
	if answer != "" {
		eventPayload["answer"] = answer
	}
	return status, decisionJSON, "operator_question.decided", eventPayload, nil
}

func validateOperatorAnswer(payload events.OperatorQuestionPayload, selectedOptionID string, answer string) error {
	if selectedOptionID == "" && answer == "" {
		return fmt.Errorf("%w: operator answer requires selected_option_id or answer", ErrPendingActionDecisionInvalid)
	}
	if answer != "" && !payload.AllowFreeform {
		return fmt.Errorf("%w: operator question does not allow freeform answer", ErrPendingActionDecisionInvalid)
	}
	if selectedOptionID == "" {
		if len(payload.Options) > 0 && !payload.AllowFreeform {
			return fmt.Errorf("%w: operator answer requires selected_option_id", ErrPendingActionDecisionInvalid)
		}
		return nil
	}
	for _, option := range payload.Options {
		if strings.TrimSpace(option.ID) == selectedOptionID {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown selected_option_id %q", ErrPendingActionDecisionInvalid, selectedOptionID)
}

func pendingActionDecisionStatus(decision string) (events.PendingActionStatus, error) {
	switch strings.TrimSpace(strings.ToLower(decision)) {
	case "accept":
		return events.PendingActionStatusApproved, nil
	case "decline":
		return events.PendingActionStatusRejected, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrPendingActionDecisionInvalid, decision)
	}
}

func statusToDecisionAction(status events.PendingActionStatus) string {
	switch status {
	case events.PendingActionStatusApproved:
		return "accept"
	case events.PendingActionStatusRejected:
		return "decline"
	default:
		return "decline"
	}
}

const (
	defaultInboxPendingActionLimit = 20
	defaultInboxActiveRunLimit     = 20
	defaultInboxTerminalRunLimit   = 20
	runSummaryPreviewMaxRunes      = 96
	runSummaryTitleMaxRunes        = 64
)

type InboxService struct {
	store        inboxStore
	capabilities inboxCapabilityService
}

type inboxCapabilityService interface {
	Snapshot(ctx context.Context, opts CapabilitySnapshotOptions) SystemCapabilities
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

func NewInboxService(store inboxStore, capabilities inboxCapabilityService) *InboxService {
	return &InboxService{store: store, capabilities: capabilities}
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

type runListFunc func(context.Context, int) ([]events.RunRecord, error)

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

func (s *InboxService) projectPendingActionSummary(ctx context.Context, record events.PendingActionRecord) (PendingActionSummary, error) {
	run, err := s.store.LoadRun(ctx, record.RunID)
	if err != nil {
		return PendingActionSummary{}, err
	}
	return buildPendingActionSummary(record, *run)
}

func buildPendingActionSummary(record events.PendingActionRecord, run events.RunRecord) (PendingActionSummary, error) {
	if record.Status != events.PendingActionStatusPending {
		return PendingActionSummary{}, fmt.Errorf("%w: unsupported pending action status %q", ErrClientProjectionFailed, record.Status)
	}
	switch record.Kind {
	case events.PendingActionKindElicitation:
		return buildElicitationPendingActionSummary(record, run)
	case events.PendingActionKindOperatorQuestion:
		return buildOperatorQuestionPendingActionSummary(record, run)
	default:
		return PendingActionSummary{}, fmt.Errorf("%w: unsupported pending action kind %q", ErrClientProjectionFailed, record.Kind)
	}
}

func buildElicitationPendingActionSummary(record events.PendingActionRecord, run events.RunRecord) (PendingActionSummary, error) {
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

func buildOperatorQuestionPendingActionSummary(record events.PendingActionRecord, run events.RunRecord) (PendingActionSummary, error) {
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

func pendingActionBody(record events.PendingActionRecord) (string, error) {
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

func pendingActionPayload(record events.PendingActionRecord) (map[string]any, error) {
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

func operatorQuestionPayload(record events.PendingActionRecord) (events.OperatorQuestionPayload, error) {
	raw := strings.TrimSpace(record.PayloadJSON)
	if raw == "" {
		return events.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s payload_json is empty", ErrClientProjectionFailed, record.ActionID)
	}
	var payload events.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return events.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s payload_json: %v", ErrClientProjectionFailed, record.ActionID, err)
	}
	if strings.TrimSpace(payload.Question) == "" {
		return events.OperatorQuestionPayload{}, fmt.Errorf("%w: pending action %s operator question is empty", ErrClientProjectionFailed, record.ActionID)
	}
	return payload, nil
}

func pendingActionOptionsFromEventOptions(items []events.PendingActionOption) []PendingActionOption {
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

func (s *InboxService) projectRunSummary(ctx context.Context, record events.RunRecord) (RunSummary, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return RunSummary{}, err
	}
	mode, err := projectRunMode(record.OrchestrationMode)
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
		Mode:           mode,
		Preview:        runSummaryPreview(record),
		LastEventLabel: runSummaryLastEventLabel(record.Status),
		AttentionLevel: runSummaryAttentionLevel(record.Status),
		DurationMS:     runSummaryDurationMS(record),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}, nil
}

func runSummaryThreadTitle(session events.SessionRecord, run events.RunRecord) string {
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

func runSummaryPreview(record events.RunRecord) string {
	switch record.Status {
	case events.RunStatusFailed:
		if preview := previewText(record.Error); preview != "" {
			return preview
		}
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case events.RunStatusSucceeded:
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case events.RunStatusInterrupted, events.RunStatusRunning:
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

func runSummaryLastEventLabel(status events.RunStatus) string {
	switch status {
	case events.RunStatusRunning:
		return "Run is running"
	case events.RunStatusSucceeded:
		return "Run completed"
	case events.RunStatusInterrupted:
		return "Run interrupted"
	case events.RunStatusFailed:
		return "Run failed"
	default:
		return string(status)
	}
}

func runSummaryAttentionLevel(status events.RunStatus) string {
	switch status {
	case events.RunStatusRunning:
		return "running"
	case events.RunStatusFailed:
		return "failed"
	case events.RunStatusInterrupted:
		return "needs_action"
	default:
		return "normal"
	}
}

func runSummaryDurationMS(record events.RunRecord) int64 {
	if record.UpdatedAt.IsZero() || record.CreatedAt.IsZero() {
		return 0
	}
	duration := record.UpdatedAt.Sub(record.CreatedAt)
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}

var (
	ErrDevicePushTokenForbidden = errors.New("device push token belongs to another device")
	ErrInvalidPushProvider      = errors.New("invalid push provider")
)

type PushDispatcher interface {
	Dispatch(ctx context.Context, req notifications.DispatchRequest) error
}

type DevicePushTokenInput struct {
	DeviceID string
	Provider string
	Platform string
	Token    string
}

type DevicePushTokenView struct {
	DeviceID  string
	Provider  string
	Platform  string
	UpdatedAt time.Time
}

type NotificationService struct {
	store      notificationStore
	dispatcher PushDispatcher
	now        func() time.Time
	rand       io.Reader
}

func NewNotificationService(store notificationStore, dispatcher PushDispatcher) *NotificationService {
	return &NotificationService{
		store:      store,
		dispatcher: dispatcher,
		now:        func() time.Time { return time.Now().UTC() },
		rand:       rand.Reader,
	}
}

func (s *NotificationService) RegisterDevicePushToken(ctx context.Context, auth *DeviceAuthContext, input DevicePushTokenInput) (*DevicePushTokenView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("notification store is nil")
	}
	if auth == nil {
		return nil, ErrUnauthenticated
	}
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID == "" {
		return nil, ErrDeviceNotFound
	}
	if auth.Device.DeviceID != deviceID {
		return nil, ErrDevicePushTokenForbidden
	}
	provider, err := notifications.NormalizeProvider(input.Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPushProvider, input.Provider)
	}
	tokenValue := strings.TrimSpace(input.Token)
	if tokenValue == "" {
		return nil, errors.New("push token is required")
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = auth.Device.Platform
	}
	now := s.now()
	pushTokenID, err := generatePrefixedID(s.rand, "push", 16)
	if err != nil {
		return nil, err
	}
	record, err := s.store.UpsertDevicePushToken(ctx, &store.DevicePushToken{
		PushTokenID: pushTokenID,
		DeviceID:    deviceID,
		Provider:    provider,
		Platform:    platform,
		TokenValue:  tokenValue,
		TokenHash:   hashSecret(tokenValue),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, err
	}
	view := devicePushTokenView(*record)
	return &view, nil
}

func (s *NotificationService) RevokeDevicePushToken(ctx context.Context, auth *DeviceAuthContext, deviceID, provider string) error {
	if s == nil || s.store == nil {
		return errors.New("notification store is nil")
	}
	if auth == nil {
		return ErrUnauthenticated
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ErrDeviceNotFound
	}
	if auth.Device.DeviceID != deviceID {
		return ErrDevicePushTokenForbidden
	}
	normalizedProvider, err := notifications.NormalizeProvider(provider)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPushProvider, provider)
	}
	return s.store.RevokeDevicePushToken(ctx, deviceID, normalizedProvider, s.now())
}

func (s *NotificationService) NotifyPendingAction(ctx context.Context, action events.PendingActionRecord) error {
	if s == nil || s.store == nil {
		return errors.New("notification store is nil")
	}
	if strings.TrimSpace(action.ActionID) == "" {
		return errors.New("pending action id is required")
	}
	if strings.TrimSpace(action.RunID) == "" {
		return errors.New("pending action run id is required")
	}
	now := s.now()
	notificationID, err := generatePrefixedID(s.rand, "notif", 16)
	if err != nil {
		return err
	}
	notification := &store.Notification{
		NotificationID: notificationID,
		Kind:           notifications.KindPendingAction,
		RunID:          action.RunID,
		ActionID:       action.ActionID,
		CreatedAt:      now,
	}
	if err := s.store.CreateNotification(ctx, notification); err != nil {
		return err
	}
	tokens, err := s.store.ListActiveDevicePushTokens(ctx)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if err := s.createAndDispatchDelivery(ctx, *notification, token); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) createAndDispatchDelivery(ctx context.Context, notification store.Notification, token store.DevicePushToken) error {
	now := s.now()
	deliveryID, err := generatePrefixedID(s.rand, "delivery", 16)
	if err != nil {
		return err
	}
	delivery := &store.NotificationDelivery{
		DeliveryID:     deliveryID,
		NotificationID: notification.NotificationID,
		DeviceID:       token.DeviceID,
		PushTokenID:    token.PushTokenID,
		Provider:       token.Provider,
		Status:         notifications.DeliveryStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateNotificationDelivery(ctx, delivery); err != nil {
		return err
	}
	status := notifications.DeliveryStatusSent
	errorText := ""
	if s.dispatcher == nil {
		status = notifications.DeliveryStatusNotConfigured
		errorText = notifications.ErrDispatcherNotConfigured.Error()
	} else if err := s.dispatcher.Dispatch(ctx, notifications.DispatchRequest{
		Provider:       token.Provider,
		Token:          token.TokenValue,
		NotificationID: notification.NotificationID,
		Kind:           notification.Kind,
		CreatedAt:      notification.CreatedAt,
		Data:           notifications.WakeData(notification.NotificationID, notification.Kind),
	}); err != nil {
		if errors.Is(err, notifications.ErrDispatcherNotConfigured) {
			status = notifications.DeliveryStatusNotConfigured
		} else {
			status = notifications.DeliveryStatusFailed
		}
		errorText = err.Error()
	}
	if err := s.store.UpdateNotificationDeliveryStatus(ctx, deliveryID, status, errorText, s.now()); err != nil {
		return err
	}
	if status == notifications.DeliveryStatusFailed {
		return errors.New(errorText)
	}
	return nil
}

func devicePushTokenView(record store.DevicePushToken) DevicePushTokenView {
	return DevicePushTokenView{
		DeviceID:  record.DeviceID,
		Provider:  record.Provider,
		Platform:  record.Platform,
		UpdatedAt: record.UpdatedAt,
	}
}

type notifyingPendingActionStore struct {
	PendingActionCreateStore
	notifications *NotificationService
}

type PendingActionCreateStore interface {
	ListPendingActions(ctx context.Context, limit int) ([]events.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status events.PendingActionStatus, mode events.PendingActionDecisionMode, decisionJSON string) (*events.PendingActionRecord, error)
	SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
	CreatePendingAction(context.Context, store.CreatePendingActionInput) (*events.PendingActionRecord, error)
}

func NewNotifyingPendingActionStore(store PendingActionCreateStore, notifications *NotificationService) PendingActionCreateStore {
	if notifications == nil {
		return store
	}
	return &notifyingPendingActionStore{
		PendingActionCreateStore: store,
		notifications:            notifications,
	}
}

func (s *notifyingPendingActionStore) CreatePendingAction(ctx context.Context, input store.CreatePendingActionInput) (*events.PendingActionRecord, error) {
	record, err := s.PendingActionCreateStore.CreatePendingAction(ctx, input)
	if err != nil {
		return nil, err
	}
	if record.Status == events.PendingActionStatusPending {
		if err := s.notifications.NotifyPendingAction(ctx, *record); err != nil {
			return nil, fmt.Errorf("notify pending action: %w", err)
		}
	}
	return record, nil
}

var (
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrDeviceRevoked      = errors.New("device revoked")
	ErrInvalidPairingCode = errors.New("invalid pairing code")
	ErrDeviceNotFound     = errors.New("device not found")
)

type PairingCodeView struct {
	Code      string
	ExpiresAt time.Time
}

type PairDeviceInput struct {
	PairingCode string
	DeviceName  string
	Platform    string
}

type PairDeviceResult struct {
	Device      DeviceView
	AccessToken string
}

type DeviceAuthContext struct {
	Device DeviceView
}

type DeviceView struct {
	DeviceID   string
	Name       string
	Platform   string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

type DeviceAuthService struct {
	store deviceAuthStore
	now   func() time.Time
	rand  io.Reader
}

func NewDeviceAuthService(store deviceAuthStore) *DeviceAuthService {
	return &DeviceAuthService{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		rand:  rand.Reader,
	}
}

func (s *DeviceAuthService) CreatePairingCode(ctx context.Context, ttl time.Duration) (*PairingCodeView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	if ttl <= 0 {
		return nil, errors.New("pairing code ttl must be positive")
	}
	code, err := generatePairingCode(s.rand)
	if err != nil {
		return nil, err
	}
	now := s.now()
	record := &store.PairingCode{
		CodeHash:  hashSecret(normalizePairingCode(code)),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.store.SavePairingCode(ctx, record); err != nil {
		return nil, err
	}
	return &PairingCodeView{Code: code, ExpiresAt: record.ExpiresAt}, nil
}

func (s *DeviceAuthService) PairDevice(ctx context.Context, input PairDeviceInput) (*PairDeviceResult, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	code := normalizePairingCode(input.PairingCode)
	if code == "" {
		return nil, ErrInvalidPairingCode
	}
	name := strings.TrimSpace(input.DeviceName)
	if name == "" {
		return nil, errors.New("device name is required")
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		return nil, errors.New("device platform is required")
	}
	now := s.now()
	if _, err := s.store.ConsumePairingCode(ctx, hashSecret(code), now); err != nil {
		if errors.Is(err, store.ErrPairingCodeNotFound) || errors.Is(err, store.ErrPairingCodeUsed) || errors.Is(err, store.ErrPairingCodeExpired) {
			return nil, ErrInvalidPairingCode
		}
		return nil, err
	}
	deviceID, err := generatePrefixedID(s.rand, "device", 16)
	if err != nil {
		return nil, err
	}
	token, err := generateDeviceToken(s.rand)
	if err != nil {
		return nil, err
	}
	device := &store.Device{
		DeviceID:   deviceID,
		Name:       name,
		Platform:   platform,
		TokenHash:  hashSecret(token),
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if err := s.store.SaveDevice(ctx, device); err != nil {
		return nil, err
	}
	return &PairDeviceResult{
		Device:      deviceViewFromRecord(*device),
		AccessToken: token,
	}, nil
}

func (s *DeviceAuthService) Authenticate(ctx context.Context, rawToken string) (*DeviceAuthContext, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return nil, ErrUnauthenticated
	}
	device, err := s.store.LoadDeviceByTokenHash(ctx, hashSecret(token))
	if err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	if device.RevokedAt != nil {
		return nil, ErrDeviceRevoked
	}
	now := s.now()
	if err := s.store.TouchDevice(ctx, device.DeviceID, now); err != nil {
		return nil, err
	}
	device.LastSeenAt = now
	return &DeviceAuthContext{Device: deviceViewFromRecord(*device)}, nil
}

func (s *DeviceAuthService) ListDevices(ctx context.Context) ([]DeviceView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	records, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices := make([]DeviceView, 0, len(records))
	for _, record := range records {
		devices = append(devices, deviceViewFromRecord(record))
	}
	return devices, nil
}

func (s *DeviceAuthService) RevokeDevice(ctx context.Context, deviceID string) error {
	if s == nil || s.store == nil {
		return errors.New("device auth store is nil")
	}
	trimmed := strings.TrimSpace(deviceID)
	if trimmed == "" {
		return ErrDeviceNotFound
	}
	if err := s.store.RevokeDevice(ctx, trimmed, s.now()); err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			return ErrDeviceNotFound
		}
		return err
	}
	return nil
}

func deviceViewFromRecord(record store.Device) DeviceView {
	return DeviceView{
		DeviceID:   record.DeviceID,
		Name:       record.Name,
		Platform:   record.Platform,
		CreatedAt:  record.CreatedAt,
		LastSeenAt: record.LastSeenAt,
		RevokedAt:  record.RevokedAt,
	}
}

func normalizePairingCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func generatePairingCode(reader io.Reader) (string, error) {
	raw := make([]byte, 10)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate pairing code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	if len(encoded) < 16 {
		return "", errors.New("generated pairing code is too short")
	}
	encoded = encoded[:16]
	return encoded[0:4] + "-" + encoded[4:8] + "-" + encoded[8:12] + "-" + encoded[12:16], nil
}

func generateDeviceToken(reader io.Reader) (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate device token: %w", err)
	}
	return "acorn_dev_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func generatePrefixedID(reader io.Reader, prefix string, bytesLen int) (string, error) {
	raw := make([]byte, bytesLen)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

type deviceAuthContextKey struct{}

func ContextWithDeviceAuth(ctx context.Context, auth *DeviceAuthContext) context.Context {
	return context.WithValue(ctx, deviceAuthContextKey{}, auth)
}

func DeviceAuthFromContext(ctx context.Context) (*DeviceAuthContext, bool) {
	auth, ok := ctx.Value(deviceAuthContextKey{}).(*DeviceAuthContext)
	return auth, ok && auth != nil
}
