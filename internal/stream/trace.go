package stream

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
)

type TraceSummary struct {
	ItemCount                  int            `json:"item_count"`
	LastKind                   StreamItemKind `json:"last_kind,omitempty"`
	AssistantMessageCount      int            `json:"assistant_message_count,omitempty"`
	AssistantDeltaCount        int            `json:"assistant_delta_count,omitempty"`
	AssistantDeltaMessageCount int            `json:"assistant_delta_message_count,omitempty"`
	AssistantDeltaCharCount    int            `json:"assistant_delta_char_count,omitempty"`
	ToolCallCount              int            `json:"tool_call_count,omitempty"`
	DecisionEventCount         int            `json:"decision_event_count,omitempty"`
	SkillEventCount            int            `json:"skill_event_count,omitempty"`
	PlanEventCount             int            `json:"plan_event_count,omitempty"`
	DecisionSelected           bool           `json:"decision_selected,omitempty"`
	DecisionBlocked            bool           `json:"decision_blocked,omitempty"`
	SkillSelected              bool           `json:"skill_selected,omitempty"`
	Interrupted                bool           `json:"interrupted,omitempty"`
	Failed                     bool           `json:"failed,omitempty"`
	Completed                  bool           `json:"completed,omitempty"`
}

func BuildTraceSummary(raw []events.EventRecord) (*TraceSummary, error) {
	items, err := ProjectEventsToStreamItems(raw)
	if err != nil {
		return nil, err
	}
	return SummarizeStreamItems(items), nil
}

func ProjectEventsToStreamItems(raw []events.EventRecord) ([]StreamItem, error) {
	items := make([]StreamItem, 0, len(raw))
	for _, event := range raw {
		item, err := ProjectEventToStreamItem(event)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func LatestRootInterruptContexts(raw []events.EventRecord) ([]StreamInterruptContext, error) {
	for i := len(raw) - 1; i >= 0; i-- {
		item, err := ProjectEventToStreamItem(raw[i])
		if err != nil {
			return nil, err
		}
		if item.Kind != StreamKindRunInterrupted {
			continue
		}
		interrupt := item.GetInterrupt()
		if interrupt == nil {
			return nil, errors.New("run.interrupted payload missing interrupt")
		}
		contexts := make([]StreamInterruptContext, 0, len(interrupt.Contexts))
		for _, ctx := range interrupt.Contexts {
			if !ctx.IsRootCause {
				continue
			}
			id := strings.TrimSpace(ctx.ID)
			if id == "" {
				return nil, errors.New("interrupt context id is empty")
			}
			contexts = append(contexts, ctx)
		}
		if len(contexts) == 0 {
			return nil, errors.New("run.interrupted has no root interrupt contexts")
		}
		return contexts, nil
	}
	return nil, errors.New("run has no interrupt event to resume")
}

func LatestRootInterruptIDs(raw []events.EventRecord) ([]string, error) {
	contexts, err := LatestRootInterruptContexts(raw)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(contexts))
	for _, ctx := range contexts {
		ids = append(ids, strings.TrimSpace(ctx.ID))
	}
	return ids, nil
}

func ProjectEventToStreamItem(event events.EventRecord) (StreamItem, error) {
	item := StreamItem{RunID: event.RunID, Sequence: event.Sequence, CreatedAt: event.CreatedAt}

	kind := eventKindToStreamKind(event.Kind)
	item.Kind = kind

	payload := map[string]any{}
	if event.Payload != nil {
		data, err := json.Marshal(event.Payload)
		if err != nil {
			return item, fmt.Errorf("project event %s seq %d payload: %w", event.Kind, event.Sequence, err)
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return item, fmt.Errorf("project event %s seq %d payload object: %w", event.Kind, event.Sequence, err)
		}
	}
	switch kind {
	case StreamKindToolCallStarted,
		StreamKindToolCallSucceeded,
		StreamKindToolCallFailed,
		StreamKindToolCallInterrupted:
		toolCall, err := extractToolCallFromMergedPayload(event.Payload)
		if err != nil {
			return item, fmt.Errorf("project event %s seq %d tool_call: %w", event.Kind, event.Sequence, err)
		}
		item.Payload = map[string]any{"tool_call": toolCall}
	default:
		item.Payload = payload
	}

	return item, nil
}

func eventKindToStreamKind(eventKind string) StreamItemKind {
	switch eventKind {
	case "run.started":
		return StreamKindRunStarted
	case "run.completed":
		return StreamKindRunCompleted
	case "run.failed":
		return StreamKindRunFailed
	case "run.interrupted":
		return StreamKindRunInterrupted
	case "run.resume_requested":
		return StreamKindRunResumeRequested
	case "decision_selected":
		return StreamKindDecisionSelected
	case "decision_blocked":
		return StreamKindDecisionBlocked
	case "skill.discovered":
		return StreamKindSkillDiscovered
	case "skill.selected":
		return StreamKindSkillSelected
	case "skill.loaded":
		return StreamKindSkillLoaded
	case "skill.failed":
		return StreamKindSkillFailed
	case "skill.lifecycle":
		return StreamKindSkillLifecycle
	case "memory.prepared":
		return StreamKindMemoryPrepared
	case "context.pressure":
		return StreamKindContextPressure
	case "context.compressed":
		return StreamKindContextCompressed
	case "assistant.delta":
		return StreamKindAssistantDelta
	case "agent.message":
		return StreamKindAssistantMessage
	case "tool.call.started":
		return StreamKindToolCallStarted
	case "tool.call.succeeded":
		return StreamKindToolCallSucceeded
	case "tool.call.failed":
		return StreamKindToolCallFailed
	case "tool.call.interrupted":
		return StreamKindToolCallInterrupted
	case "mcp.tool_catalog_refreshed":
		return StreamKindMCPToolCatalogRefreshed
	case "mcp.tool_catalog_refresh_failed":
		return StreamKindMCPToolCatalogRefreshFailed
	case "mcp.provider_added":
		return StreamKindMCPProviderAdded
	case "mcp.provider_removed":
		return StreamKindMCPProviderRemoved
	case "mcp.provider_restarted":
		return StreamKindMCPProviderRestarted
	case "mcp.resource_catalog_refreshed":
		return StreamKindMCPResourceCatalogRefreshed
	case "mcp.resource_catalog_refresh_failed":
		return StreamKindMCPResourceCatalogRefreshFailed
	case "mcp.prompt_catalog_refreshed":
		return StreamKindMCPPromptCatalogRefreshed
	case "mcp.prompt_catalog_refresh_failed":
		return StreamKindMCPPromptCatalogRefreshFailed
	case "mcp.auth_status_changed":
		return StreamKindMCPAuthStatusChanged
	case "subagent.started":
		return StreamKindSubagentStarted
	case "subagent.completed":
		return StreamKindSubagentCompleted
	case "subagent.failed":
		return StreamKindSubagentFailed
	case "plan.created":
		return StreamKindPlanCreated
	case "plan.updated":
		return StreamKindPlanUpdated
	case "plan.cleared":
		return StreamKindPlanCleared
	case "step.started":
		return StreamKindStepStarted
	case "step.completed":
		return StreamKindStepCompleted
	case "step.failed":
		return StreamKindStepFailed
	default:
		return StreamItemKind(eventKind)
	}
}

func extractToolCallFromMergedPayload(payload any) (*StreamToolCall, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var tool StreamToolCall
	if err := json.Unmarshal(data, &tool); err != nil {
		return nil, err
	}
	if tool.Name == "" {
		if m, ok := payload.(map[string]any); ok {
			if v, ok := m["tool_name"].(string); ok {
				tool.Name = v
			}
		}
	}
	if tool.Name == "" && tool.Provider == "" && tool.Output == "" && tool.Error == "" {
		return nil, nil
	}
	return &tool, nil
}

func SummarizeStreamItems(items []StreamItem) *TraceSummary {
	summary := &TraceSummary{ItemCount: len(items)}
	assistantDeltaMessageIDs := make(map[string]struct{})
	for _, item := range items {
		summary.LastKind = item.Kind
		switch item.Kind {
		case StreamKindAssistantDelta:
			summary.AssistantDeltaCount++
			if delta := item.GetAssistantDelta(); delta != nil {
				summary.AssistantDeltaCharCount += len([]rune(delta.Delta))
				messageID := strings.TrimSpace(delta.MessageID)
				if messageID != "" {
					assistantDeltaMessageIDs[messageID] = struct{}{}
				}
			}
		case StreamKindAssistantMessage:
			summary.AssistantMessageCount++
		case StreamKindToolCallStarted, StreamKindToolCallSucceeded, StreamKindToolCallFailed, StreamKindToolCallInterrupted:
			summary.ToolCallCount++
		case StreamKindDecisionSelected, StreamKindDecisionBlocked:
			summary.DecisionEventCount++
			if item.Kind == StreamKindDecisionSelected {
				summary.DecisionSelected = true
			}
			if item.Kind == StreamKindDecisionBlocked {
				summary.DecisionBlocked = true
			}
		case StreamKindSkillDiscovered, StreamKindSkillSelected, StreamKindSkillLoaded, StreamKindSkillFailed, StreamKindSkillLifecycle:
			summary.SkillEventCount++
			if item.Kind == StreamKindSkillSelected {
				summary.SkillSelected = true
			}
		case StreamKindRunInterrupted:
			summary.Interrupted = true
		case StreamKindRunFailed:
			summary.Failed = true
		case StreamKindRunCompleted:
			summary.Completed = true
		case StreamKindPlanCreated, StreamKindPlanUpdated, StreamKindPlanCleared, StreamKindStepStarted, StreamKindStepCompleted, StreamKindStepFailed:
			summary.PlanEventCount++
		}
	}
	summary.AssistantDeltaMessageCount = len(assistantDeltaMessageIDs)
	return summary
}
