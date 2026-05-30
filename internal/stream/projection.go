package stream

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime/api"
)

// StreamSink receives a StreamItem for delivery.
type StreamSink func(item StreamItem) error

func AppendStreamItem(ctx context.Context, store api.EventAppender, sink StreamSink, item StreamItem) (events.EventRecord, error) {
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
		return streamKindToEventKind(item.Kind), map[string]any{}, nil
	}

	payload, err := streamPayloadMap(item.Kind, item.Payload)
	if err != nil {
		return "", nil, err
	}
	if err := normalizeToolCallPayload(item.Kind, payload); err != nil {
		return "", nil, err
	}

	return streamKindToEventKind(item.Kind), payload, nil
}

func streamKindToEventKind(kind StreamItemKind) string {
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
	default:
		return string(kind)
	}
}

func streamPayloadMap(kind StreamItemKind, payload any) (map[string]any, error) {
	out := map[string]any{}
	if payload == nil {
		return out, nil
	}
	if err := reencodeViaJSON(payload, &out); err != nil {
		return nil, fmt.Errorf("stream %s payload: %w", kind, err)
	}
	return out, nil
}

func normalizeToolCallPayload(kind StreamItemKind, payload map[string]any) error {
	switch kind {
	case StreamKindToolCallStarted,
		StreamKindToolCallSucceeded,
		StreamKindToolCallFailed,
		StreamKindToolCallInterrupted:
	default:
		return nil
	}

	raw, exists := payload["tool_call"]
	if !exists || raw == nil {
		delete(payload, "tool_call")
		return nil
	}
	toolMap, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("stream %s payload tool_call must be object", kind)
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
	return nil
}

func reencodeViaJSON(in any, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("re-encode marshal: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("re-encode unmarshal: %w", err)
	}
	return nil
}
