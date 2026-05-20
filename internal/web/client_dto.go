package web

import (
	"encoding/json"
	"time"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime"
)

type ThreadDTO struct {
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	WorkspaceRoot string    `json:"workspace_root"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LatestRunID   string    `json:"latest_run_id,omitempty"`
	State         string    `json:"state"`
}

type ThreadListResponse struct {
	Items []ThreadDTO `json:"items"`
}

type CreateThreadRequest struct {
	Title string `json:"title,omitempty"`
}

type UpdateThreadRequest struct {
	Title string `json:"title"`
}

type MessageContentDTO struct {
	Type  string           `json:"type"`
	Text  string           `json:"text"`
	Parts []MessagePartDTO `json:"parts,omitempty"`
}

type MessageDTO struct {
	ID        string            `json:"id"`
	ThreadID  string            `json:"thread_id"`
	Role      string            `json:"role"`
	Content   MessageContentDTO `json:"content"`
	CreatedAt time.Time         `json:"created_at"`
	RunID     string            `json:"run_id,omitempty"`
}

type MessageListResponse struct {
	Items []MessageDTO `json:"items"`
}

type CreateMessageRequest struct {
	Content MessageContentDTO `json:"content" validate:"required"`
}

type MessagePartDTO struct {
	Kind             string              `json:"kind"`
	Text             string              `json:"text,omitempty"`
	Reasoning        string              `json:"reasoning,omitempty"`
	Status           string              `json:"status,omitempty"`
	Title            string              `json:"title,omitempty"`
	Summary          string              `json:"summary,omitempty"`
	Changed          []string            `json:"changed,omitempty"`
	Verified         []string            `json:"verified,omitempty"`
	Risks            []string            `json:"risks,omitempty"`
	Items            []DisclosureItemDTO `json:"items,omitempty"`
	DetailRunID      string              `json:"detail_run_id,omitempty"`
	RunID            string              `json:"run_id,omitempty"`
	Label            string              `json:"label,omitempty"`
	DecisionID       string              `json:"decision_id,omitempty"`
	Question         string              `json:"question,omitempty"`
	SelectedOptionID string              `json:"selected_option_id,omitempty"`
	Answer           string              `json:"answer,omitempty"`
	Options          []DecisionOptionDTO `json:"options,omitempty"`
	Action           *MessageActionDTO   `json:"action,omitempty"`
}

type DisclosureItemDTO struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Tone    string `json:"tone"`
	SkillID string `json:"skill_id,omitempty"`
}

func (part MessagePartDTO) MarshalJSON() ([]byte, error) {
	switch part.Kind {
	case "text":
		return json.Marshal(struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}{
			Kind: part.Kind,
			Text: part.Text,
		})
	case "reasoning":
		return json.Marshal(struct {
			Kind      string `json:"kind"`
			Reasoning string `json:"reasoning"`
		}{
			Kind:      part.Kind,
			Reasoning: part.Reasoning,
		})
	case "work_status":
		return json.Marshal(struct {
			Kind        string            `json:"kind"`
			Status      string            `json:"status"`
			Title       string            `json:"title"`
			Summary     string            `json:"summary"`
			DetailRunID string            `json:"detail_run_id,omitempty"`
			Action      *MessageActionDTO `json:"action,omitempty"`
		}{
			Kind:        part.Kind,
			Status:      part.Status,
			Title:       part.Title,
			Summary:     part.Summary,
			DetailRunID: part.DetailRunID,
			Action:      part.Action,
		})
	case "decision":
		return json.Marshal(struct {
			Kind             string              `json:"kind"`
			DecisionID       string              `json:"decision_id"`
			Question         string              `json:"question"`
			Status           string              `json:"status,omitempty"`
			SelectedOptionID string              `json:"selected_option_id,omitempty"`
			Answer           string              `json:"answer,omitempty"`
			Options          []DecisionOptionDTO `json:"options"`
		}{
			Kind:             part.Kind,
			DecisionID:       part.DecisionID,
			Question:         part.Question,
			Status:           part.Status,
			SelectedOptionID: part.SelectedOptionID,
			Answer:           part.Answer,
			Options:          part.Options,
		})
	case "result":
		return json.Marshal(struct {
			Kind        string   `json:"kind"`
			Title       string   `json:"title"`
			Changed     []string `json:"changed"`
			Verified    []string `json:"verified"`
			Risks       []string `json:"risks"`
			DetailRunID string   `json:"detail_run_id,omitempty"`
		}{
			Kind:        part.Kind,
			Title:       part.Title,
			Changed:     nonNilStrings(part.Changed),
			Verified:    nonNilStrings(part.Verified),
			Risks:       nonNilStrings(part.Risks),
			DetailRunID: part.DetailRunID,
		})
	case "disclosure":
		return json.Marshal(struct {
			Kind  string              `json:"kind"`
			Items []DisclosureItemDTO `json:"items"`
		}{
			Kind:  part.Kind,
			Items: part.Items,
		})
	case "technical_detail_link":
		return json.Marshal(struct {
			Kind        string `json:"kind"`
			DetailRunID string `json:"detail_run_id,omitempty"`
			RunID       string `json:"run_id"`
			Label       string `json:"label"`
		}{
			Kind:        part.Kind,
			DetailRunID: part.DetailRunID,
			RunID:       part.RunID,
			Label:       part.Label,
		})
	default:
		type alias MessagePartDTO
		return json.Marshal(alias(part))
	}
}

type DecisionOptionDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type MessageActionDTO struct {
	Kind  string `json:"kind"`
	RunID string `json:"run_id"`
	Label string `json:"label"`
}

type DecidePendingActionRequest struct {
	Decision         string `json:"decision"`
	SelectedOptionID string `json:"selected_option_id,omitempty"`
	Answer           string `json:"answer,omitempty"`
}

type PendingActionDecisionDTO struct {
	ActionID         string     `json:"action_id"`
	RunID            string     `json:"run_id"`
	Status           string     `json:"status"`
	Decision         string     `json:"decision"`
	SelectedOptionID string     `json:"selected_option_id,omitempty"`
	Answer           string     `json:"answer,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
}

type PendingActionListResponse struct {
	Items []PendingActionSummaryDTO `json:"items"`
}

type PendingActionDetailDTO struct {
	ActionID  string                   `json:"action_id"`
	RunID     string                   `json:"run_id"`
	ThreadID  string                   `json:"thread_id"`
	Kind      string                   `json:"kind"`
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Body      string                   `json:"body,omitempty"`
	Options   []PendingActionOptionDTO `json:"options"`
	Payload   map[string]any           `json:"payload"`
	Reason    string                   `json:"reason,omitempty"`
	Rule      string                   `json:"rule,omitempty"`
	CreatedAt time.Time                `json:"created_at"`
}

type CreateRunRequest struct {
	SkillID string `json:"skill_id,omitempty"`
	Mode    string `json:"mode,omitempty"`
}

type ResumeRunRequest struct{}

type RunDTO struct {
	ID          string     `json:"id"`
	ThreadID    string     `json:"thread_id"`
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type RunEventDTO struct {
	EventID string    `json:"event_id"`
	RunID   string    `json:"run_id"`
	Seq     int64     `json:"seq"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
}

type UnsupportedRunEventDTO struct {
	EventID string         `json:"event_id"`
	RunID   string         `json:"run_id"`
	Seq     int64          `json:"seq"`
	TS      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	Raw     map[string]any `json:"raw,omitempty"`
	Reason  string         `json:"reason"`
}

type RunDetailDTO struct {
	Run       RunDTO                `json:"run"`
	Thread    ThreadDTO             `json:"thread"`
	Events    []RunEventDTO         `json:"events"`
	Workbench *RuntimeWorkbenchDTO  `json:"workbench"`
	Trace     *runtime.TraceSummary `json:"trace"`
	Raw       *RunDetailRawDTO      `json:"raw,omitempty"`
}

type RunDetailRawDTO struct {
	UnsupportedEvents []UnsupportedRunEventDTO `json:"unsupported_events"`
}

type InterruptRunResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

type SystemStatusDTO struct {
	RuntimeReadiness  RuntimeReadinessDTO     `json:"runtime_readiness"`
	ProviderReadiness []ProviderReadinessDTO  `json:"provider_readiness,omitempty"`
	Model             CapabilitiesModelDTO    `json:"model"`
	WorkspaceRoot     string                  `json:"workspace_root"`
	Summary           CapabilitiesSummaryDTO  `json:"summary"`
	Features          CapabilitiesFeaturesDTO `json:"features"`
}

type InboxResponse struct {
	PendingActions     []PendingActionSummaryDTO `json:"pending_actions"`
	ActiveRuns         []RunSummaryDTO           `json:"active_runs"`
	RecentTerminalRuns []RunSummaryDTO           `json:"recent_terminal_runs"`
	System             SystemStatusDTO           `json:"system"`
}

type RunSummaryDTO struct {
	RunID     string    `json:"run_id"`
	ThreadID  string    `json:"thread_id"`
	Status    string    `json:"status"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PendingActionSummaryDTO struct {
	ActionID  string                   `json:"action_id"`
	RunID     string                   `json:"run_id"`
	ThreadID  string                   `json:"thread_id"`
	Kind      string                   `json:"kind"`
	Status    string                   `json:"status"`
	Title     string                   `json:"title"`
	Body      string                   `json:"body,omitempty"`
	Options   []PendingActionOptionDTO `json:"options"`
	CreatedAt time.Time                `json:"created_at"`
}

type PendingActionOptionDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type ToolSummaryDTO = CapabilitiesToolDTO

type ToolListResponse struct {
	Items []ToolSummaryDTO `json:"items"`
	Total int              `json:"total"`
}

type ClientProviderSettingsDTO struct {
	Name                string  `json:"name"`
	Model               string  `json:"model"`
	BaseURL             string  `json:"base_url,omitempty"`
	ReasoningEffort     string  `json:"reasoning_effort,omitempty"`
	TimeoutSeconds      int     `json:"timeout_seconds,omitempty"`
	Temperature         float32 `json:"temperature,omitempty"`
	MaxCompletionTokens int     `json:"max_completion_tokens,omitempty"`
	Enabled             bool    `json:"enabled"`
}

type ClientRuntimeSettingsDTO struct {
	StorageDir        string `json:"storage_dir"`
	RunTimeoutSeconds int    `json:"run_timeout_seconds"`
}

type ClientWebSettingsDTO struct {
	ListenAddr     string   `json:"listen_addr"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

type ClientSettingsDTO struct {
	ConfigPath    string                      `json:"config_path,omitempty"`
	ConfigDir     string                      `json:"config_dir,omitempty"`
	WorkspaceRoot string                      `json:"workspace_root"`
	Providers     []ClientProviderSettingsDTO `json:"providers"`
	Runtime       ClientRuntimeSettingsDTO    `json:"runtime"`
	Web           ClientWebSettingsDTO        `json:"web"`
}

func threadDTOFromDomain(thread app.Thread) ThreadDTO {
	return ThreadDTO{
		ID:            thread.ID,
		Title:         thread.Title,
		WorkspaceRoot: thread.WorkspaceRoot,
		CreatedAt:     thread.CreatedAt,
		UpdatedAt:     thread.UpdatedAt,
		LatestRunID:   thread.LatestRunID,
		State:         thread.State,
	}
}

func threadDTOsFromDomain(items []app.Thread) []ThreadDTO {
	result := make([]ThreadDTO, 0, len(items))
	for _, item := range items {
		result = append(result, threadDTOFromDomain(item))
	}
	return result
}

func messageDTOFromDomain(message app.Message) MessageDTO {
	return MessageDTO{
		ID:       message.ID,
		ThreadID: message.ThreadID,
		Role:     message.Role,
		Content: MessageContentDTO{
			Type:  message.Content.Type,
			Text:  message.Content.Text,
			Parts: messagePartDTOsFromDomain(message.Content.Parts),
		},
		CreatedAt: message.CreatedAt,
		RunID:     message.RunID,
	}
}

func messagePartDTOsFromDomain(parts []app.MessagePart) []MessagePartDTO {
	if len(parts) == 0 {
		return nil
	}
	items := make([]MessagePartDTO, 0, len(parts))
	for _, part := range parts {
		item := MessagePartDTO{
			Kind:             part.Kind,
			Text:             part.Text,
			Reasoning:        part.Reasoning,
			Status:           part.Status,
			Title:            part.Title,
			Summary:          part.Summary,
			Changed:          part.Changed,
			Verified:         part.Verified,
			Risks:            part.Risks,
			Items:            disclosureItemDTOsFromDomain(part.Items),
			DetailRunID:      part.DetailRunID,
			RunID:            part.RunID,
			Label:            part.Label,
			DecisionID:       part.DecisionID,
			Question:         part.Question,
			SelectedOptionID: part.SelectedOptionID,
			Options:          decisionOptionDTOsFromDomain(part.Options),
			Action:           messageActionDTOFromDomain(part.Action),
		}
		if part.Kind == "result" {
			item.Changed = nonNilStrings(item.Changed)
			item.Verified = nonNilStrings(item.Verified)
			item.Risks = nonNilStrings(item.Risks)
		}
		items = append(items, item)
	}
	return items
}

func disclosureItemDTOsFromDomain(items []app.DisclosureItem) []DisclosureItemDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]DisclosureItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, DisclosureItemDTO{
			Kind:    item.Kind,
			Label:   item.Label,
			Detail:  item.Detail,
			Tone:    item.Tone,
			SkillID: item.SkillID,
		})
	}
	return result
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func messageActionDTOFromDomain(action *app.MessageAction) *MessageActionDTO {
	if action == nil {
		return nil
	}
	return &MessageActionDTO{
		Kind:  action.Kind,
		RunID: action.RunID,
		Label: action.Label,
	}
}

func pendingActionDecisionDTOFromDomain(record events.PendingActionRecord) PendingActionDecisionDTO {
	status := string(record.Status)
	decision, selectedOptionID, answer := pendingActionDecisionFields(record)
	return PendingActionDecisionDTO{
		ActionID:         record.ActionID,
		RunID:            record.RunID,
		Status:           status,
		Decision:         decision,
		SelectedOptionID: selectedOptionID,
		Answer:           answer,
		DecidedAt:        record.DecidedAt,
	}
}

func pendingActionListResponseFromDomain(items []app.PendingActionSummary) PendingActionListResponse {
	return PendingActionListResponse{Items: pendingActionSummaryDTOsFromDomain(items)}
}

func pendingActionDetailDTOFromDomain(item app.PendingActionDetail) PendingActionDetailDTO {
	return PendingActionDetailDTO{
		ActionID:  item.ActionID,
		RunID:     item.RunID,
		ThreadID:  item.ThreadID,
		Kind:      item.Kind,
		Status:    item.Status,
		Title:     item.Title,
		Body:      item.Body,
		Options:   pendingActionOptionDTOsFromDomain(item.Options),
		Payload:   item.Payload,
		Reason:    item.Reason,
		Rule:      item.Rule,
		CreatedAt: item.CreatedAt,
	}
}

func pendingActionDecisionFields(record events.PendingActionRecord) (string, string, string) {
	var payload struct {
		Action           string `json:"action"`
		SelectedOptionID string `json:"selected_option_id"`
		Answer           string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(record.DecisionJSON), &payload); err == nil {
		return payload.Action, payload.SelectedOptionID, payload.Answer
	}
	return "", "", ""
}

func decisionOptionDTOsFromDomain(options []app.DecisionOption) []DecisionOptionDTO {
	if len(options) == 0 {
		return nil
	}
	items := make([]DecisionOptionDTO, 0, len(options))
	for _, option := range options {
		items = append(items, DecisionOptionDTO{
			ID:          option.ID,
			Label:       option.Label,
			Description: option.Description,
		})
	}
	return items
}

func messageDTOsFromDomain(items []app.Message) []MessageDTO {
	result := make([]MessageDTO, 0, len(items))
	for _, item := range items {
		result = append(result, messageDTOFromDomain(item))
	}
	return result
}

func runDTOFromDomain(run app.Run) RunDTO {
	dto := RunDTO{
		ID:        run.ID,
		ThreadID:  run.ThreadID,
		Status:    run.Status,
		Mode:      run.Mode,
		CreatedAt: run.CreatedAt,
	}
	if !run.CompletedAt.IsZero() {
		dto.CompletedAt = &run.CompletedAt
	}
	return dto
}

func runEventDTOFromDomain(event app.RunEvent) RunEventDTO {
	return RunEventDTO{
		EventID: event.EventID,
		RunID:   event.RunID,
		Seq:     event.Seq,
		TS:      event.TS,
		Type:    event.Type,
		Data:    event.Data,
	}
}

func runEventDTOsFromDomain(items []app.RunEvent) []RunEventDTO {
	result := make([]RunEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, runEventDTOFromDomain(item))
	}
	return result
}

func inboxDTOFromDomain(inbox app.MobileInbox, workspaceRoot string) InboxResponse {
	return InboxResponse{
		PendingActions:     pendingActionSummaryDTOsFromDomain(inbox.PendingActions),
		ActiveRuns:         runSummaryDTOsFromDomain(inbox.ActiveRuns),
		RecentTerminalRuns: runSummaryDTOsFromDomain(inbox.RecentTerminalRuns),
		System:             systemStatusDTOFromSnapshot(inbox.System, workspaceRoot),
	}
}

func pendingActionSummaryDTOsFromDomain(items []app.PendingActionSummary) []PendingActionSummaryDTO {
	result := make([]PendingActionSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, PendingActionSummaryDTO{
			ActionID:  item.ActionID,
			RunID:     item.RunID,
			ThreadID:  item.ThreadID,
			Kind:      item.Kind,
			Status:    item.Status,
			Title:     item.Title,
			Body:      item.Body,
			Options:   pendingActionOptionDTOsFromDomain(item.Options),
			CreatedAt: item.CreatedAt,
		})
	}
	return result
}

func pendingActionOptionDTOsFromDomain(items []app.PendingActionOption) []PendingActionOptionDTO {
	result := make([]PendingActionOptionDTO, 0, len(items))
	for _, item := range items {
		result = append(result, PendingActionOptionDTO{
			ID:          item.ID,
			Label:       item.Label,
			Description: item.Description,
		})
	}
	return result
}

func runSummaryDTOsFromDomain(items []app.RunSummary) []RunSummaryDTO {
	result := make([]RunSummaryDTO, 0, len(items))
	for _, item := range items {
		result = append(result, RunSummaryDTO{
			RunID:     item.RunID,
			ThreadID:  item.ThreadID,
			Status:    item.Status,
			Mode:      item.Mode,
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}
	return result
}

func systemStatusDTOFromSnapshot(snapshot app.SystemCapabilities, workspaceRoot string) SystemStatusDTO {
	return SystemStatusDTO{
		RuntimeReadiness:  runtimeReadinessDTOFromSnapshot(snapshot.RuntimeReadiness),
		ProviderReadiness: providerReadinessDTOsFromSnapshot(snapshot.ProviderReadiness),
		Model:             capabilitiesModelDTOFromSnapshot(snapshot.Model),
		WorkspaceRoot:     workspaceRoot,
		Summary:           capabilitiesSummaryDTOFromSnapshot(snapshot.Summary),
		Features:          capabilitiesFeaturesDTOFromSnapshot(snapshot.Features),
	}
}

func unsupportedRunEventDTOFromDomain(event app.UnsupportedRunEvent) UnsupportedRunEventDTO {
	return UnsupportedRunEventDTO{
		EventID: event.EventID,
		RunID:   event.RunID,
		Seq:     event.Seq,
		TS:      event.TS,
		Type:    event.Type,
		Raw:     event.Raw,
		Reason:  event.Reason,
	}
}

func unsupportedRunEventDTOsFromDomain(items []app.UnsupportedRunEvent) []UnsupportedRunEventDTO {
	result := make([]UnsupportedRunEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, unsupportedRunEventDTOFromDomain(item))
	}
	return result
}
