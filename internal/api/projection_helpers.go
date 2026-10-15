package api

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

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
// write reads as the skill's name and description.
func toolApprovalSummary(toolName, arguments string) string {
	if toolName == "skill_write" {
		if spec, err := skills.Parse(skillWriteArgs(arguments).Markdown); err == nil {
			return clip(spec.Name+": "+spec.Description, reviewSummaryRunes)
		}
	}
	return toolCallSummary(arguments)
}

// toolCallDetail is the whole text a tool call acts on when its summary
// cannot hold it: the skill skill_write would store.
func toolCallDetail(toolName, arguments string) string {
	if toolName != "skill_write" {
		return ""
	}
	args := skillWriteArgs(arguments)
	files := make([]core.SkillFile, 0, len(args.Files))
	for path, content := range args.Files {
		files = append(files, core.SkillFile{Path: path, Content: []byte(content)})
	}
	return skillPackageDetail(args.Markdown, files, args.Delete)
}

type skillWriteArguments struct {
	Markdown string            `json:"markdown"`
	Files    map[string]string `json:"files"`
	Delete   []string          `json:"delete"`
}

func skillWriteArgs(arguments string) skillWriteArguments {
	var args skillWriteArguments
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return skillWriteArguments{}
	}
	return args
}

// skillDetailBytes bounds the file contents an approval shows; files past it
// are listed by path and size.
const skillDetailBytes = 256 << 10

// skillPackageDetail shows a skill's SKILL.md, then each file it adds or
// replaces and each file it removes.
func skillPackageDetail(markdown string, files []core.SkillFile, deletes []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(markdown))
	sorted := slices.Clone(files)
	slices.SortFunc(sorted, func(a, b core.SkillFile) int { return strings.Compare(a.Path, b.Path) })
	shown := 0
	for _, f := range sorted {
		fmt.Fprintf(&b, "\n\n── %s (%d bytes) ──", f.Path, len(f.Content))
		switch {
		case !utf8.Valid(f.Content):
			b.WriteString("\n(binary)")
		case shown+len(f.Content) > skillDetailBytes:
			b.WriteString("\n(not shown)")
		default:
			shown += len(f.Content)
			b.WriteString("\n" + strings.TrimRight(string(f.Content), "\n"))
		}
	}
	for _, p := range slices.Sorted(slices.Values(deletes)) {
		fmt.Fprintf(&b, "\n\n── %s (removed) ──", p)
	}
	return b.String()
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
