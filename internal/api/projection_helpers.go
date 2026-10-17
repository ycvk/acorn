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

// skillWriteDetail is the skill skill_write would store. kept is the
// manifest the write keeps from its base revision, nil when it keeps none.
func skillWriteDetail(args skillWriteArguments, kept []byte) string {
	files := make([]core.SkillFile, 0, len(args.Files))
	for path, content := range args.Files {
		files = append(files, core.SkillFile{Path: path, Content: []byte(content)})
	}
	return skillPackageDetail(args.Markdown, files, args.Delete, kept)
}

type skillWriteArguments struct {
	Markdown     string            `json:"markdown"`
	Files        map[string]string `json:"files"`
	Delete       []string          `json:"delete"`
	BaseRevision int64             `json:"base_revision"`
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

// skillPackageDetail shows what a package's manifest makes the workstation
// run, then the skill's SKILL.md, each file it adds or replaces and each file
// it removes. kept is the manifest a write keeps from its base revision, nil
// when it keeps none.
func skillPackageDetail(markdown string, files []core.SkillFile, deletes []string, kept []byte) string {
	var b strings.Builder
	i := slices.IndexFunc(files, func(f core.SkillFile) bool { return f.Path == skills.ManifestFileName })
	switch {
	case i >= 0:
		b.WriteString(manifestDetail(files[i].Content, "") + "\n\n")
	case slices.Contains(deletes, skills.ManifestFileName):
		b.WriteString("Removes " + skills.ManifestFileName + ": the package no longer runs an MCP server or triggers.\n\n")
	case kept != nil:
		b.WriteString(manifestDetail(kept, " (unchanged)") + "\n\n")
	}
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

// manifestDetail says what a package's manifest runs on the workstation: its
// MCP server's command, then each trigger's schedule and command, and the
// secrets they get, by name.
func manifestDetail(content []byte, note string) string {
	m, err := skills.ParseManifest(content)
	if err != nil {
		return fmt.Sprintf("%s does not parse, and the write will fail: %v", skills.ManifestFileName, err)
	}
	lines := []string{"Runs on the workstation from the package's directory" + note + ":"}
	if m.MCP != nil {
		lines = append(lines, "MCP server: "+m.MCP.CommandLine())
	}
	for _, t := range m.Triggers {
		lines = append(lines, fmt.Sprintf("Trigger %s at %q (owner's time), events %s: %s", t.Name, t.Schedule, deliverText(t.Deliver), t.Run))
	}
	secrets := "none"
	if len(m.Secrets) > 0 {
		secrets = strings.Join(m.Secrets, ", ")
	}
	return strings.Join(append(lines, "Secrets: "+secrets), "\n")
}

func deliverText(d skills.Deliver) string {
	if d == skills.DeliverImmediate {
		return "start a run at once"
	}
	return "wait for the morning briefing"
}

func clip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
