package api

import (
	"encoding/json"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

func topLevelString(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func topLevelBool(payload map[string]any, key string) bool {
	value, ok := payload[key].(bool)
	return ok && value
}

func objectField(payload map[string]any, key string) (map[string]any, bool) {
	value, ok := payload[key].(map[string]any)
	if !ok {
		return nil, false
	}
	return cloneMap(value), true
}

func pendingActionOptionsFromAny(raw any) []core.PendingActionOption {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]core.PendingActionOption, 0, len(items))
	for _, item := range items {
		option, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, core.PendingActionOption{
			ID:          topLevelString(option, "id"),
			Label:       topLevelString(option, "label"),
			Description: topLevelString(option, "description"),
		})
	}
	return out
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

const (
	toolCallSummaryRunes = 80
	reviewSummaryRunes   = 160
	toolCallErrorRunes   = 300
)

// toolCallSummaryKeys are the argument fields that say what a call is about,
// most telling first: a search query, a URL, a note path.
var toolCallSummaryKeys = []string{"query", "url", "command", "path", "prefix", "title", "question", "message", "content", "text", "name", "target", "id"}

// toolCallSummary is one line describing a tool call from its JSON
// arguments, or empty when no telling field is present.
func toolCallSummary(arguments string) string {
	var args map[string]any
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return ""
	}
	for _, key := range toolCallSummaryKeys {
		if value := topLevelString(args, key); value != "" {
			return clip(strings.Join(strings.Fields(value), " "), toolCallSummaryRunes)
		}
	}
	return ""
}

// toolApprovalSummary says what a call waiting for approval is about. A skill
// write reads as the skill's name and summary.
func toolApprovalSummary(toolName, arguments string) string {
	if toolName == "skill_write" {
		if spec, err := skills.Parse(skillWriteMarkdown(arguments)); err == nil {
			return clip(spec.Name+": "+spec.Summary, reviewSummaryRunes)
		}
	}
	return toolCallSummary(arguments)
}

// toolCallDetail is the whole text a tool call acts on when its summary
// cannot hold it: the skill document of skill_write.
func toolCallDetail(toolName, arguments string) string {
	if toolName != "skill_write" {
		return ""
	}
	return skillWriteMarkdown(arguments)
}

func skillWriteMarkdown(arguments string) string {
	var args map[string]any
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return ""
	}
	return topLevelString(args, "markdown")
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
