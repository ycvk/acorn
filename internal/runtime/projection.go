package runtime

import (
	"context"
	"fmt"

	"encoding/json"
	"github.com/ycvk/acorn/internal/domain"
)

func AppendStreamItem(ctx context.Context, store domain.EventAppender, sink domain.StreamSink, item domain.StreamItem) (domain.EventRecord, error) {
	if store == nil {
		return domain.EventRecord{}, fmt.Errorf("append stream item: nil store")
	}
	kind, payload, err := ProjectStreamItemToEvent(item)
	if err != nil {
		return domain.EventRecord{}, err
	}
	saved, err := store.AppendEvent(ctx, item.RunID, kind, payload)
	if err != nil {
		return domain.EventRecord{}, err
	}
	item.Sequence = saved.Sequence
	item.CreatedAt = saved.CreatedAt
	if sink != nil {
		if err := sink(item); err != nil {
			return domain.EventRecord{}, err
		}
	}
	return saved, nil
}

func ProjectStreamItemToEvent(item domain.StreamItem) (string, any, error) {
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

func streamKindToEventKind(kind domain.StreamItemKind) string {
	switch kind {
	case domain.StreamKindRunStarted:
		return "run.started"
	case domain.StreamKindRunCompleted:
		return "run.completed"
	case domain.StreamKindRunFailed:
		return "run.failed"
	case domain.StreamKindRunInterrupted:
		return "run.interrupted"
	case domain.StreamKindRunResumeRequested:
		return "run.resume_requested"
	case domain.StreamKindAssistantDelta:
		return string(kind)
	case domain.StreamKindAssistantMessage:
		return "agent.message"
	case domain.StreamKindToolCallStarted:
		return "tool.call.started"
	case domain.StreamKindToolCallSucceeded:
		return "tool.call.succeeded"
	case domain.StreamKindToolCallFailed:
		return "tool.call.failed"
	case domain.StreamKindToolCallInterrupted:
		return "tool.call.interrupted"
	case domain.StreamKindSkillDiscovered:
		return "skill.discovered"
	case domain.StreamKindSkillSelected:
		return "skill.selected"
	case domain.StreamKindSkillLoaded:
		return "skill.loaded"
	case domain.StreamKindSkillFailed:
		return "skill.failed"
	case domain.StreamKindProcedureActivation:
		return "procedure.activation"
	case domain.StreamKindMemoryPrepared:
		return "memory.prepared"
	default:
		return string(kind)
	}
}

func streamPayloadMap(kind domain.StreamItemKind, payload any) (map[string]any, error) {
	out := map[string]any{}
	if payload == nil {
		return out, nil
	}
	if err := reencodeViaJSON(payload, &out); err != nil {
		return nil, fmt.Errorf("stream %s payload: %w", kind, err)
	}
	return out, nil
}

func normalizeToolCallPayload(kind domain.StreamItemKind, payload map[string]any) error {
	switch kind {
	case domain.StreamKindToolCallStarted,
		domain.StreamKindToolCallSucceeded,
		domain.StreamKindToolCallFailed,
		domain.StreamKindToolCallInterrupted:
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
