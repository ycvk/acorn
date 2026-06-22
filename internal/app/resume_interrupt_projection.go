package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/domain"
)

type resumeInterruptContext struct {
	ID          string `json:"id,omitempty"`
	Address     string `json:"address,omitempty"`
	Info        any    `json:"info,omitempty"`
	IsRootCause bool   `json:"is_root_cause,omitempty"`
}

type runInterruptedPayload struct {
	Interrupt *struct {
		Contexts []resumeInterruptContext `json:"contexts,omitempty"`
	} `json:"interrupt,omitempty"`
}

func latestRootInterruptContexts(raw []domain.EventRecord) ([]resumeInterruptContext, error) {
	for i := len(raw) - 1; i >= 0; i-- {
		record := raw[i]
		if record.Kind != "run.interrupted" {
			continue
		}
		var payload runInterruptedPayload
		data, err := json.Marshal(record.Payload)
		if err != nil {
			return nil, fmt.Errorf("project event %s seq %d payload: %w", record.Kind, record.Sequence, err)
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("project event %s seq %d payload object: %w", record.Kind, record.Sequence, err)
		}
		if payload.Interrupt == nil {
			return nil, errors.New("run.interrupted payload missing interrupt")
		}
		contexts := make([]resumeInterruptContext, 0, len(payload.Interrupt.Contexts))
		for _, ctx := range payload.Interrupt.Contexts {
			if !ctx.IsRootCause {
				continue
			}
			id := strings.TrimSpace(ctx.ID)
			if id == "" {
				return nil, errors.New("interrupt context id is empty")
			}
			ctx.ID = id
			ctx.Info = compactResumeInterruptInfo(ctx.Info)
			contexts = append(contexts, ctx)
		}
		if len(contexts) == 0 {
			return nil, errors.New("run.interrupted has no root interrupt contexts")
		}
		return contexts, nil
	}
	return nil, errors.New("run has no interrupt event to resume")
}

func latestRootInterruptIDs(raw []domain.EventRecord) ([]string, error) {
	contexts, err := latestRootInterruptContexts(raw)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(contexts))
	for _, ctx := range contexts {
		ids = append(ids, strings.TrimSpace(ctx.ID))
	}
	return ids, nil
}

func compactResumeInterruptInfo(value any) any {
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"kind", "message", "question", "action_id", "command", "command_name", "command_args", "cwd", "url", "tool_name", "interrupt_id", "arguments_json", "reason", "rule"} {
		if current, exists := data[key]; exists {
			out[key] = current
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
