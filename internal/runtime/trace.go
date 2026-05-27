package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/stream"
)

// --- Trace types ---

type Trace struct {
	Run     *events.RunRecord   `json:"run,omitempty"`
	Summary *TraceSummary       `json:"summary,omitempty"`
	Items   []stream.StreamItem `json:"items,omitempty"`
}

type TraceSummary struct {
	ItemCount                  int                   `json:"item_count"`
	LastKind                   stream.StreamItemKind `json:"last_kind,omitempty"`
	AssistantMessageCount      int                   `json:"assistant_message_count,omitempty"`
	AssistantDeltaCount        int                   `json:"assistant_delta_count,omitempty"`
	AssistantDeltaMessageCount int                   `json:"assistant_delta_message_count,omitempty"`
	AssistantDeltaCharCount    int                   `json:"assistant_delta_char_count,omitempty"`
	ToolCallCount              int                   `json:"tool_call_count,omitempty"`
	DecisionEventCount         int                   `json:"decision_event_count,omitempty"`
	SkillEventCount            int                   `json:"skill_event_count,omitempty"`
	PlanEventCount             int                   `json:"plan_event_count,omitempty"`
	DecisionSelected           bool                  `json:"decision_selected,omitempty"`
	DecisionBlocked            bool                  `json:"decision_blocked,omitempty"`
	SkillSelected              bool                  `json:"skill_selected,omitempty"`
	Interrupted                bool                  `json:"interrupted,omitempty"`
	Failed                     bool                  `json:"failed,omitempty"`
	Completed                  bool                  `json:"completed,omitempty"`
}

func BuildTrace(run *events.RunRecord, raw []events.EventRecord) *Trace {
	items := make([]stream.StreamItem, 0, len(raw))
	for _, event := range raw {
		items = append(items, projectEventToStreamItem(event))
	}
	return &Trace{Run: run, Summary: summarizeStreamItems(items), Items: items}
}

func BuildTraceSummary(raw []events.EventRecord) *TraceSummary {
	items := make([]stream.StreamItem, 0, len(raw))
	for _, event := range raw {
		items = append(items, projectEventToStreamItem(event))
	}
	return summarizeStreamItems(items)
}

func LatestRootInterruptContexts(raw []events.EventRecord) ([]stream.StreamInterruptContext, error) {
	for i := len(raw) - 1; i >= 0; i-- {
		item := projectEventToStreamItem(raw[i])
		if item.Kind != stream.StreamKindRunInterrupted {
			continue
		}
		interrupt := item.GetInterrupt()
		if interrupt == nil {
			return nil, errors.New("run.interrupted payload missing interrupt")
		}
		contexts := make([]stream.StreamInterruptContext, 0, len(interrupt.Contexts))
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

func projectEventToStreamItem(event events.EventRecord) stream.StreamItem {
	item := stream.StreamItem{RunID: event.RunID, Sequence: event.Sequence, CreatedAt: event.CreatedAt}

	kind := eventKindToStreamKind(event.Kind)
	item.Kind = kind

	data, err := json.Marshal(event.Payload)
	if err != nil {
		return item
	}

	payload, err := stream.UnmarshalPayload(kind, data)
	if err != nil {
		return item
	}
	item.Payload = payload

	switch p := payload.(type) {
	case *stream.ToolCallStartedPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	case *stream.ToolCallProgressPayload:
		p.ToolCall = extractToolCallProgressFromMergedPayload(event.Payload)
	case *stream.ToolCallSucceededPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	case *stream.ToolCallFailedPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	case *stream.ToolCallInterruptedPayload:
		p.ToolCall = extractToolCallFromMergedPayload(event.Payload)
	}

	return item
}

func eventKindToStreamKind(eventKind string) stream.StreamItemKind {
	switch eventKind {
	case "run.started":
		return stream.StreamKindRunStarted
	case "run.completed":
		return stream.StreamKindRunCompleted
	case "run.failed":
		return stream.StreamKindRunFailed
	case "run.interrupted":
		return stream.StreamKindRunInterrupted
	case "run.resume_requested":
		return stream.StreamKindRunResumeRequested
	case "decision_selected":
		return stream.StreamKindDecisionSelected
	case "decision_blocked":
		return stream.StreamKindDecisionBlocked
	case "skill.discovered":
		return stream.StreamKindSkillDiscovered
	case "skill.selected":
		return stream.StreamKindSkillSelected
	case "skill.loaded":
		return stream.StreamKindSkillLoaded
	case "skill.failed":
		return stream.StreamKindSkillFailed
	case "skill.lifecycle":
		return stream.StreamKindSkillLifecycle
	case "memory.prepared":
		return stream.StreamKindMemoryPrepared
	case "context.pressure":
		return stream.StreamKindContextPressure
	case "context.compressed":
		return stream.StreamKindContextCompressed
	case "assistant.delta":
		return stream.StreamKindAssistantDelta
	case "stream.heartbeat":
		return stream.StreamKindHeartbeat
	case "agent.message":
		return stream.StreamKindAssistantMessage
	case "tool.call.started":
		return stream.StreamKindToolCallStarted
	case "tool.call.progress":
		return stream.StreamKindToolCallProgress
	case "tool.call.succeeded":
		return stream.StreamKindToolCallSucceeded
	case "tool.call.failed":
		return stream.StreamKindToolCallFailed
	case "tool.call.interrupted":
		return stream.StreamKindToolCallInterrupted
	case "subagent.started":
		return stream.StreamKindSubagentStarted
	case "subagent.completed":
		return stream.StreamKindSubagentCompleted
	case "subagent.failed":
		return stream.StreamKindSubagentFailed
	case "tool.parallel_batch.started":
		return stream.StreamKindToolParallelBatchStarted
	case "tool.parallel_batch.completed":
		return stream.StreamKindToolParallelBatchCompleted
	case "run.archived":
		return stream.StreamKindRunArchived
	case "plan.created":
		return stream.StreamKindPlanCreated
	case "plan.updated":
		return stream.StreamKindPlanUpdated
	case "plan.cleared":
		return stream.StreamKindPlanCleared
	case "step.started":
		return stream.StreamKindStepStarted
	case "step.completed":
		return stream.StreamKindStepCompleted
	case "step.failed":
		return stream.StreamKindStepFailed
	default:
		return stream.StreamItemKind(eventKind)
	}
}

func extractToolCallFromMergedPayload(payload any) *stream.StreamToolCall {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var tool stream.StreamToolCall
	if err := json.Unmarshal(data, &tool); err != nil {
		return nil
	}
	if tool.Name == "" {
		if m, ok := payload.(map[string]any); ok {
			if v, ok := m["tool_name"].(string); ok {
				tool.Name = v
			}
		}
	}
	if tool.Name == "" && tool.Provider == "" && tool.Output == "" && tool.Error == "" {
		return nil
	}
	return &tool
}

func extractToolCallProgressFromMergedPayload(payload any) *stream.StreamToolCallProgress {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var tool stream.StreamToolCallProgress
	if err := json.Unmarshal(data, &tool); err != nil {
		return nil
	}
	if tool.Name == "" {
		if m, ok := payload.(map[string]any); ok {
			if v, ok := m["tool_name"].(string); ok {
				tool.Name = v
			}
		}
	}
	if tool.Name == "" && tool.Provider == "" && tool.Delta == "" {
		return nil
	}
	return &tool
}

func summarizeStreamItems(items []stream.StreamItem) *TraceSummary {
	summary := &TraceSummary{ItemCount: len(items)}
	assistantDeltaMessageIDs := make(map[string]struct{})
	for _, item := range items {
		summary.LastKind = item.Kind
		switch item.Kind {
		case stream.StreamKindAssistantDelta:
			summary.AssistantDeltaCount++
			if delta := item.GetAssistantDelta(); delta != nil {
				summary.AssistantDeltaCharCount += len([]rune(delta.Delta))
				messageID := strings.TrimSpace(delta.MessageID)
				if messageID != "" {
					assistantDeltaMessageIDs[messageID] = struct{}{}
				}
			}
		case stream.StreamKindAssistantMessage:
			summary.AssistantMessageCount++
		case stream.StreamKindToolCallStarted, stream.StreamKindToolCallSucceeded, stream.StreamKindToolCallFailed, stream.StreamKindToolCallInterrupted:
			summary.ToolCallCount++
		case stream.StreamKindDecisionSelected, stream.StreamKindDecisionBlocked:
			summary.DecisionEventCount++
			if item.Kind == stream.StreamKindDecisionSelected {
				summary.DecisionSelected = true
			}
			if item.Kind == stream.StreamKindDecisionBlocked {
				summary.DecisionBlocked = true
			}
		case stream.StreamKindSkillDiscovered, stream.StreamKindSkillSelected, stream.StreamKindSkillLoaded, stream.StreamKindSkillFailed, stream.StreamKindSkillLifecycle:
			summary.SkillEventCount++
			if item.Kind == stream.StreamKindSkillSelected {
				summary.SkillSelected = true
			}
		case stream.StreamKindRunInterrupted:
			summary.Interrupted = true
		case stream.StreamKindRunFailed:
			summary.Failed = true
		case stream.StreamKindRunCompleted:
			summary.Completed = true
		case stream.StreamKindPlanCreated, stream.StreamKindPlanUpdated, stream.StreamKindPlanCleared, stream.StreamKindStepStarted, stream.StreamKindStepCompleted, stream.StreamKindStepFailed:
			summary.PlanEventCount++
		}
	}
	summary.AssistantDeltaMessageCount = len(assistantDeltaMessageIDs)
	return summary
}

func SelectedSkillFromEvents(raw []events.EventRecord) *SelectedSkill {
	for i := len(raw) - 1; i >= 0; i-- {
		item := projectEventToStreamItem(raw[i])
		if (item.Kind != stream.StreamKindSkillLoaded && item.Kind != stream.StreamKindSkillSelected) || item.GetSkill() == nil {
			continue
		}
		skill := item.GetSkill()
		selectedID := strings.TrimSpace(skill.SelectedID)
		if selectedID == "" {
			continue
		}
		return &SelectedSkill{
			Skill: skills.Spec{
				ID:          selectedID,
				Name:        firstNonEmpty(skill.Name, selectedID),
				Summary:     skill.Summary,
				Instruction: skill.Instruction,
				Source:      skill.Source,
				Path:        skill.Path,
				Scripts:     append([]string(nil), skill.Scripts...),
				Requires: skills.Requirements{
					Tools:    append([]string(nil), skill.Requirements.Tools...),
					Toolsets: append([]string(nil), skill.Requirements.Toolsets...),
					Bins:     append([]string(nil), skill.Requirements.Bins...),
					Env:      append([]string(nil), skill.Requirements.Env...),
				},
			},
			Score:        skill.Score,
			MatchedTerms: append([]string(nil), skill.MatchedTerms...),
		}
	}
	return nil
}

type PendingResumeStore interface {
	FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error)
}

type PendingResumeInfo struct {
	RunID     string    `json:"run_id"`
	SessionID string    `json:"session_id"`
	Input     string    `json:"input"`
	CreatedAt time.Time `json:"created_at"`
}

func FindPendingResume(ctx context.Context, store PendingResumeStore) (*PendingResumeInfo, error) {
	run, err := store.FindLatestInterruptedRun(ctx)
	if err != nil {
		return nil, fmt.Errorf("find latest interrupted run: %w", err)
	}
	if run == nil {
		return nil, nil
	}
	return &PendingResumeInfo{
		RunID:     run.RunID,
		SessionID: run.SessionID,
		Input:     run.Input,
		CreatedAt: run.CreatedAt,
	}, nil
}

func resolveRootOrchestrationMode(req runtimeapi.ExecuteRequest) events.OrchestrationMode {
	mode := events.OrchestrationMode(req.OrchestrationMode).Normalize()
	if req.OrchestrationMode != "" {
		return mode
	}
	if strings.TrimSpace(req.ParentRunID) != "" {
		return events.ModeSingleAgent
	}
	if strings.TrimSpace(req.SkillID) != "" {
		return events.ModePlanExecute
	}
	return events.ModeDirectResponse
}
