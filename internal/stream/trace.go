package stream

import (
	"encoding/json"
	"fmt"

	"github.com/ycvk/acorn/internal/events"
)

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
	case "subagent.started":
		return StreamKindSubagentStarted
	case "subagent.completed":
		return StreamKindSubagentCompleted
	case "subagent.failed":
		return StreamKindSubagentFailed
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
