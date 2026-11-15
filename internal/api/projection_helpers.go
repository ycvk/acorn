package api

import (
	"encoding/json"
	"fmt"
	"maps"
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

// topLevelInt reads a whole number, which JSON decoding leaves a float64.
func topLevelInt(payload map[string]any, key string) int {
	value, ok := payload[key].(float64)
	if !ok || value != float64(int(value)) {
		return 0
	}
	return int(value)
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
	maps.Copy(out, value)
	return out
}

const (
	toolCallSummaryRunes = 80
	reviewSummaryRunes   = 160
	toolCallErrorRunes   = 300
)

// toolCallSummaryKeys are the argument fields that say what a call is about,
// most telling first: a search query, a URL, a note path, what a sub-run is
// to do.
var toolCallSummaryKeys = []string{"query", "url", "command", "path", "prefix", "title", "question", "objective", "feedback", "message", "content", "text", "name", "target", "id"}

// toolCallSummary is one line describing a tool call from its JSON
// arguments, or empty when no telling field is present.
func toolCallSummary(arguments string) string {
	var args map[string]any
	if json.Unmarshal([]byte(arguments), &args) != nil {
		return ""
	}
	for _, key := range toolCallSummaryKeys {
		if value := topLevelString(args, key); value != "" {
			return clip(compactWhitespace(value), toolCallSummaryRunes)
		}
	}
	return ""
}

// skillWriteFiles are the files a skill_write adds or replaces.
func skillWriteFiles(args skillWriteArguments) []core.SkillFile {
	files := make([]core.SkillFile, 0, len(args.Files))
	for path, content := range args.Files {
		files = append(files, core.SkillFile{Path: path, Content: []byte(content)})
	}
	return files
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

// skillApprovalDetail is what the owner reads before deciding on a skill
// write: what the write changes in the stored skills, what the package's
// manifest makes the workstation run, the lines it changes in the files the
// stored skill has, and the text it stores. Empty parts are left out.
func skillApprovalDetail(statement, manifest, changedLines, text string) string {
	if changedLines != "" && text != "" {
		text = "── 写入的全文 ──\n" + text
	}
	parts := make([]string, 0, 4)
	for _, part := range []string{statement, manifest, changedLines, text} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "\n\n")
}

// packageManifestDetail says what a package's manifest makes the workstation
// run, or that a write removes it. kept is the manifest a write keeps from
// its base revision, nil when it keeps none. A package without a manifest
// has nothing to say.
func packageManifestDetail(files []core.SkillFile, deletes []string, kept []byte) string {
	i := slices.IndexFunc(files, func(f core.SkillFile) bool { return f.Path == skills.ManifestFileName })
	switch {
	case i >= 0:
		return manifestDetail(files[i].Content, "")
	case slices.Contains(deletes, skills.ManifestFileName):
		return "Removes " + skills.ManifestFileName + ": the package no longer runs an MCP server or triggers."
	case kept != nil:
		return manifestDetail(kept, " (unchanged)")
	default:
		return ""
	}
}

// packageText is a skill's SKILL.md, each file a write adds or replaces and
// each file it removes.
func packageText(markdown string, files []core.SkillFile, deletes []string) string {
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
		line := fmt.Sprintf("Trigger %s at %q (owner's time), events %s: %s", t.Name, t.Schedule, deliverText(t.Deliver), t.Run)
		if t.Timeout > 0 {
			line += fmt.Sprintf(" (may run %d seconds)", t.Timeout)
		}
		lines = append(lines, line)
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
