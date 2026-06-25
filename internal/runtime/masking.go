package runtime

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// applyMasking replaces tool result messages older than maskAfterTurns with a
// compact placeholder. This is the first-line defense against context bloat.
// It is a pure in-memory transformation — nothing is persisted.
func applyMasking(messages []adk.Message, currentTurn int, maskAfterTurns int) []adk.Message {
	if maskAfterTurns <= 0 || len(messages) == 0 {
		return messages
	}
	result := make([]adk.Message, len(messages))
	copy(result, messages)
	for i, msg := range result {
		if msg == nil {
			continue
		}
		if msg.Role != schema.Tool {
			continue
		}
		callID := strings.TrimSpace(msg.ToolCallID)
		if callID == "" {
			continue
		}
		msgTurn := turnIndexFromMessage(msg)
		if currentTurn-msgTurn <= maskAfterTurns {
			continue // keep recent tool results
		}
		result[i] = maskToolMessage(msg)
	}
	return result
}

// maskToolMessage replaces the tool result content with a compact placeholder.
func maskToolMessage(msg adk.Message) adk.Message {
	clone := msg
	clone.Content = fmt.Sprintf("[tool result elided: call_id=%s]", msg.ToolCallID)
	return clone
}

// turnIndexFromMessage extracts the turn index annotated by AnnotateMessageTurn.
func turnIndexFromMessage(msg adk.Message) int {
	if msg == nil || msg.Extra == nil {
		return 0
	}
	v, ok := msg.Extra[core.TurnIndexExtraKey]
	if !ok {
		return 0
	}
	if t, ok := v.(int); ok {
		return t
	}
	return 0
}
