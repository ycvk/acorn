package contextplane

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/tooling"
)

var ErrMicrocompactNotInitialized = errors.New("microcompact engine is not initialized")

const clearedPlaceholder = "[Previous tool result content cleared]"

type defaultMicrocompactEngine struct {
	counter *CompressionTokenCounter
	catalog *tooling.Catalog
}

func newMicrocompactEngine(counter *CompressionTokenCounter, catalog *tooling.Catalog) MicrocompactEngine {
	return &defaultMicrocompactEngine{counter: counter, catalog: catalog}
}

func (e *defaultMicrocompactEngine) Compact(ctx context.Context, req MicrocompactRequest) (*MicrocompactResult, error) {
	if e == nil {
		return nil, ErrMicrocompactNotInitialized
	}

	messages := cloneMessages(req.Messages)
	var cleared []string
	freed := 0

	for i, msg := range messages {
		if !isToolResultMessage(msg) {
			continue
		}
		toolName := extractToolName(msg)
		if toolName == "" {
			continue
		}
		if !e.isCompressibleTool(toolName) {
			continue
		}
		if isRecentResult(msg, req.TurnIndex) {
			continue
		}

		beforeTokens, err := e.countMessage(ctx, msg)
		if err != nil {
			return nil, fmt.Errorf("count tool result before: %w", err)
		}
		messages[i] = replaceWithPlaceholder(msg)
		afterTokens, err := e.countMessage(ctx, messages[i])
		if err != nil {
			return nil, fmt.Errorf("count tool result after: %w", err)
		}
		freed += beforeTokens - afterTokens
		cleared = append(cleared, toolName)
	}

	return &MicrocompactResult{
		Messages:     messages,
		TokensFreed:  freed,
		ClearedTools: cleared,
	}, nil
}

func (e *defaultMicrocompactEngine) countMessage(ctx context.Context, msg adk.Message) (int, error) {
	if e.counter == nil {
		return 0, nil
	}
	return e.counter.CountMessages(ctx, []adk.Message{msg}, nil)
}

func isToolResultMessage(msg adk.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Role == schema.Tool
}

func extractToolName(msg adk.Message) string {
	if msg == nil {
		return ""
	}
	name := strings.TrimSpace(msg.ToolName)
	if name != "" {
		return name
	}
	content := strings.TrimSpace(msg.Content)
	if idx := strings.Index(content, "tool:"); idx == 0 {
		rest := strings.TrimSpace(content[5:])
		if space := strings.IndexFunc(rest, func(r rune) bool { return r == ' ' || r == '\n' }); space > 0 {
			return rest[:space]
		}
	}
	return ""
}

func (e *defaultMicrocompactEngine) isCompressibleTool(name string) bool {
	if e.catalog != nil {
		return e.catalog.IsCompressible(name)
	}
	return false
}

const turnIndexExtraKey = "acorn_turn_index"

func AnnotateMessageTurn(msg adk.Message, turnIndex int) adk.Message {
	if msg == nil {
		return msg
	}
	if msg.Extra == nil {
		msg.Extra = make(map[string]any)
	}
	msg.Extra[turnIndexExtraKey] = turnIndex
	return msg
}

func isRecentResult(msg adk.Message, currentTurn int) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	v, ok := msg.Extra[turnIndexExtraKey]
	if !ok {
		return false
	}
	msgTurn, ok := v.(int)
	if !ok {
		return false
	}
	return msgTurn == currentTurn
}

func replaceWithPlaceholder(msg adk.Message) adk.Message {
	if msg == nil {
		return msg
	}
	clone := *msg
	clone.Content = fmt.Sprintf("%s (tool: %s)", clearedPlaceholder, extractToolName(msg))
	clone.AssistantGenMultiContent = nil
	clone.UserInputMultiContent = nil
	return &clone
}
