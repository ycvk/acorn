package runtime

import (
	"context"
	"fmt"

	"encoding/json"

	"github.com/ycvk/acorn/internal/core"
)

func AppendStreamItem(ctx context.Context, store core.EventAppender, sink core.StreamSink, item core.StreamItem) (core.EventRecord, error) {
	if store == nil {
		return core.EventRecord{}, fmt.Errorf("append stream item: nil store")
	}
	kind, payload, err := ProjectStreamItemToEvent(item)
	if err != nil {
		return core.EventRecord{}, err
	}
	saved, err := store.AppendEvent(ctx, item.RunID, kind, payload)
	if err != nil {
		return core.EventRecord{}, err
	}
	item.Sequence = saved.Sequence
	item.CreatedAt = saved.CreatedAt
	if sink != nil {
		if err := sink(item); err != nil {
			return core.EventRecord{}, err
		}
	}
	return saved, nil
}

func ProjectStreamItemToEvent(item core.StreamItem) (string, any, error) {
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

func streamKindToEventKind(kind core.StreamItemKind) string {
	switch kind {
	case core.StreamKindRunStarted:
		return "run.started"
	case core.StreamKindRunCompleted:
		return "run.completed"
	case core.StreamKindRunFailed:
		return "run.failed"
	case core.StreamKindRunInterrupted:
		return "run.interrupted"
	case core.StreamKindRunResumeRequested:
		return "run.resume_requested"
	case core.StreamKindAssistantDelta:
		return string(kind)
	case core.StreamKindAssistantMessage:
		return "runtime.message"
	case core.StreamKindToolCallStarted:
		return "tool.call.started"
	case core.StreamKindToolCallSucceeded:
		return "tool.call.succeeded"
	case core.StreamKindToolCallFailed:
		return "tool.call.failed"
	case core.StreamKindToolCallInterrupted:
		return "tool.call.interrupted"
	case core.StreamKindSkillDiscovered:
		return "skill.discovered"
	case core.StreamKindSkillSelected:
		return "skill.selected"
	case core.StreamKindSkillLoaded:
		return "skill.loaded"
	case core.StreamKindSkillFailed:
		return "skill.failed"
	case core.StreamKindProcedureActivation:
		return "procedure.activation"
	case core.StreamKindMemoryPrepared:
		return "memory.prepared"
	default:
		return string(kind)
	}
}

func streamPayloadMap(kind core.StreamItemKind, payload any) (map[string]any, error) {
	out := map[string]any{}
	if payload == nil {
		return out, nil
	}
	if err := reencodeViaJSON(payload, &out); err != nil {
		return nil, fmt.Errorf("stream %s payload: %w", kind, err)
	}
	return out, nil
}

func normalizeToolCallPayload(kind core.StreamItemKind, payload map[string]any) error {
	switch kind {
	case core.StreamKindToolCallStarted,
		core.StreamKindToolCallSucceeded,
		core.StreamKindToolCallFailed,
		core.StreamKindToolCallInterrupted:
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
