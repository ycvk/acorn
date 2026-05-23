package runprojection

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ycvk/acorn/internal/events"
)

type EventAppender interface {
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

// ToolAuditStore is the minimal interface required by tool audit wrappers.
type ToolAuditStore interface {
	EventAppender
}

func AppendStreamItem(ctx context.Context, store EventAppender, sink StreamSink, item StreamItem) (events.EventRecord, error) {
	if store == nil {
		return events.EventRecord{}, fmt.Errorf("append stream item: nil store")
	}
	kind, payload, err := ProjectStreamItemToEvent(item)
	if err != nil {
		return events.EventRecord{}, err
	}
	saved, err := store.AppendEventContext(ctx, item.RunID, kind, payload)
	if err != nil {
		return events.EventRecord{}, err
	}
	item.Sequence = saved.Sequence
	item.CreatedAt = saved.CreatedAt
	if sink != nil {
		if err := sink(item); err != nil {
			return events.EventRecord{}, err
		}
	}
	return saved, nil
}

func ProjectStreamItemToEvent(item StreamItem) (string, any, error) {
	if item.Payload == nil {
		return StreamKindToEventKind(item.Kind), map[string]any{}, nil
	}
	payload, err := StreamPayloadMap(item.Kind, item.Payload)
	if err != nil {
		return "", nil, err
	}

	switch p := item.Payload.(type) {
	case *ToolCallStartedPayload:
		if p.ToolCall != nil {
			MergeToolCallIntoPayload(payload, p.ToolCall)
		}
	case *ToolCallProgressPayload:
		if p.ToolCall != nil {
			MergeToolCallProgressIntoPayload(payload, p.ToolCall)
		}
	case *ToolCallSucceededPayload:
		if p.ToolCall != nil {
			MergeToolCallIntoPayload(payload, p.ToolCall)
		}
	case *ToolCallFailedPayload:
		if p.ToolCall != nil {
			MergeToolCallIntoPayload(payload, p.ToolCall)
		}
	case *ToolCallInterruptedPayload:
		if p.ToolCall != nil {
			MergeToolCallIntoPayload(payload, p.ToolCall)
		}
	}

	return StreamKindToEventKind(item.Kind), payload, nil
}

func StreamKindToEventKind(kind StreamItemKind) string {
	switch kind {
	case StreamKindRunStarted:
		return "run.started"
	case StreamKindRunCompleted:
		return "run.completed"
	case StreamKindRunFailed:
		return "run.failed"
	case StreamKindRunInterrupted:
		return "run.interrupted"
	case StreamKindRunResumeRequested:
		return "run.resume_requested"
	case StreamKindAssistantDelta:
		return string(kind)
	case StreamKindAssistantMessage:
		return "agent.message"
	case StreamKindToolCallStarted:
		return "tool.call.started"
	case StreamKindToolCallProgress:
		return "tool.call.progress"
	case StreamKindToolCallSucceeded:
		return "tool.call.succeeded"
	case StreamKindToolCallFailed:
		return "tool.call.failed"
	case StreamKindToolCallInterrupted:
		return "tool.call.interrupted"
	case StreamKindSkillDiscovered:
		return "skill.discovered"
	case StreamKindSkillSelected:
		return "skill.selected"
	case StreamKindSkillLoaded:
		return "skill.loaded"
	case StreamKindSkillFailed:
		return "skill.failed"
	case StreamKindSkillLifecycle:
		return "skill.lifecycle"
	case StreamKindProcedureActivation:
		return "procedure.activation"
	case StreamKindMemoryPrepared:
		return "memory.prepared"
	case StreamKindContextPressure:
		return "context.pressure"
	case StreamKindContextCompressed:
		return "context.compressed"
	case StreamKindHeartbeat:
		return "stream.heartbeat"
	case StreamKindToolParallelBatchStarted:
		return "tool.parallel_batch.started"
	case StreamKindToolParallelBatchCompleted:
		return "tool.parallel_batch.completed"
	case StreamKindRunArchived:
		return "run.archived"
	default:
		return string(kind)
	}
}

func StreamPayloadMap(kind StreamItemKind, payload any) (map[string]any, error) {
	out := map[string]any{}
	if payload == nil {
		return out, nil
	}
	if err := ReencodeViaJSON(payload, &out); err != nil {
		return nil, fmt.Errorf("stream %s payload: %w", kind, err)
	}
	return out, nil
}

func MergeToolCallIntoPayload(payload map[string]any, tool *StreamToolCall) {
	toolMap, err := StreamPayloadMap(StreamKindToolCallStarted, tool)
	if err != nil {
		return
	}
	for k, v := range toolMap {
		dst := k
		if k == "name" {
			dst = "tool_name"
		}
		if _, exists := payload[dst]; !exists {
			payload[dst] = v
		}
	}
	delete(payload, "tool_call")
}

func MergeToolCallProgressIntoPayload(payload map[string]any, tool *StreamToolCallProgress) {
	toolMap, err := StreamPayloadMap(StreamKindToolCallProgress, tool)
	if err != nil {
		return
	}
	for k, v := range toolMap {
		dst := k
		if k == "name" {
			dst = "tool_name"
		}
		if _, exists := payload[dst]; !exists {
			payload[dst] = v
		}
	}
	delete(payload, "tool_call")
}

func ReencodeViaJSON(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("re-encode marshal: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("re-encode unmarshal: %w", err)
	}
	return nil
}
