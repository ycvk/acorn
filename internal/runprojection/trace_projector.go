package runprojection

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runstream"
	"github.com/ycvk/acorn/internal/skills"
)

func BuildTrace(run *events.RunRecord, raw []events.EventRecord) *Trace {
	items := make([]StreamItem, 0, len(raw))
	for _, event := range raw {
		items = append(items, ProjectEventToStreamItem(event))
	}
	return &Trace{Run: run, Summary: SummarizeStreamItems(items), Items: items}
}

func BuildTraceSummary(raw []events.EventRecord) *TraceSummary {
	items := make([]StreamItem, 0, len(raw))
	for _, event := range raw {
		items = append(items, ProjectEventToStreamItem(event))
	}
	return SummarizeStreamItems(items)
}

func LatestRootInterruptContexts(raw []events.EventRecord) ([]StreamInterruptContext, error) {
	for i := len(raw) - 1; i >= 0; i-- {
		item := ProjectEventToStreamItem(raw[i])
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

func ProjectEventToStreamItem(event events.EventRecord) StreamItem {
	item := StreamItem{RunID: event.RunID, Sequence: event.Sequence, CreatedAt: event.CreatedAt}

	kind := EventKindToStreamKind(event.Kind)
	item.Kind = kind

	data, err := json.Marshal(event.Payload)
	if err != nil {
		return item
	}

	payload, err := runstream.UnmarshalPayload(kind, data)
	if err != nil {
		return item
	}
	item.Payload = payload

	switch p := payload.(type) {
	case *ToolCallStartedPayload:
		p.ToolCall = ExtractToolCallFromMergedPayload(event.Payload)
	case *ToolCallProgressPayload:
		p.ToolCall = ExtractToolCallProgressFromMergedPayload(event.Payload)
	case *ToolCallSucceededPayload:
		p.ToolCall = ExtractToolCallFromMergedPayload(event.Payload)
	case *ToolCallFailedPayload:
		p.ToolCall = ExtractToolCallFromMergedPayload(event.Payload)
	case *ToolCallInterruptedPayload:
		p.ToolCall = ExtractToolCallFromMergedPayload(event.Payload)
	}

	return item
}

func EventKindToStreamKind(eventKind string) StreamItemKind {
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
	case "stream.heartbeat":
		return StreamKindHeartbeat
	case "agent.message":
		return StreamKindAssistantMessage
	case "tool.call.started":
		return StreamKindToolCallStarted
	case "tool.call.progress":
		return StreamKindToolCallProgress
	case "tool.call.succeeded":
		return StreamKindToolCallSucceeded
	case "tool.call.failed":
		return StreamKindToolCallFailed
	case "tool.call.interrupted":
		return StreamKindToolCallInterrupted
	case "subagent.started":
		return StreamKindSubagentStarted
	case "subagent.completed":
		return StreamKindSubagentCompleted
	case "subagent.failed":
		return StreamKindSubagentFailed
	case "tool.parallel_batch.started":
		return StreamKindToolParallelBatchStarted
	case "tool.parallel_batch.completed":
		return StreamKindToolParallelBatchCompleted
	case "run.archived":
		return StreamKindRunArchived
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

func ExtractToolCallFromMergedPayload(payload any) *StreamToolCall {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var tool StreamToolCall
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

func ExtractToolCallProgressFromMergedPayload(payload any) *StreamToolCallProgress {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var tool StreamToolCallProgress
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

func SelectedSkillFromEvents(raw []events.EventRecord) *SelectedSkill {
	for i := len(raw) - 1; i >= 0; i-- {
		item := ProjectEventToStreamItem(raw[i])
		if (item.Kind != StreamKindSkillLoaded && item.Kind != StreamKindSkillSelected) || item.GetSkill() == nil {
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
