package runtime

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/skills"
)

// SelectedSkill is the skill matched for the current run (if any).
type SelectedSkill struct {
	Skill        skills.Spec
	Score        int
	MatchedTerms []string
	Explicit     bool
}

// buildContextEnvelopeMessage wraps content sections in an XML-style envelope
// marker so downstream consumers (model, tests) can identify the section.
func buildContextEnvelopeMessage(marker string, parts ...string) *schema.Message {
	trimmed := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		trimmed = append(trimmed, p)
	}
	if len(trimmed) == 0 {
		return nil
	}
	content := fmt.Sprintf("<%s>\n%s\n</%s>", marker, strings.Join(trimmed, "\n\n"), marker)
	return schema.UserMessage(content)
}

func buildSkillContextMessage(selected *SelectedSkill) *schema.Message {
	if selected == nil {
		return nil
	}
	brief := skillBrief(selected)
	if strings.TrimSpace(brief) == "" {
		return nil
	}
	return buildContextEnvelopeMessage("skill-context", brief)
}

func skillBrief(selected *SelectedSkill) string {
	if selected == nil {
		return ""
	}
	lines := []string{
		fmt.Sprintf("Selected skill: %s", selected.Skill.Name),
		fmt.Sprintf("Skill ID: %s", selected.Skill.ID),
		"Treat this skill as reusable runtime context. Keep using the normal tools; the skill itself is not a tool.",
		"The skill content below is already in your context. Do not spend tool calls reopening the skill directory or rereading SKILL.md unless the user explicitly asks.",
		"Prefer a small number of high-signal reads, and stop exploring once you have enough evidence to answer.",
	}
	if strings.TrimSpace(selected.Skill.Summary) != "" {
		lines = append(lines, "Summary: "+selected.Skill.Summary)
	}
	if len(selected.Skill.Requires.Tools) > 0 {
		lines = append(lines, "Required tools: "+strings.Join(selected.Skill.Requires.Tools, ", "))
	}
	if len(selected.Skill.Requires.Toolsets) > 0 {
		lines = append(lines, "Required toolsets: "+strings.Join(selected.Skill.Requires.Toolsets, ", "))
	}
	if len(selected.Skill.Requires.Bins) > 0 {
		lines = append(lines, "Required binaries: "+strings.Join(selected.Skill.Requires.Bins, ", "))
	}
	if len(selected.Skill.Requires.Env) > 0 {
		lines = append(lines, "Required environment: "+strings.Join(selected.Skill.Requires.Env, ", "))
	}
	if strings.TrimSpace(selected.Skill.Path) != "" {
		lines = append(lines, "Skill directory: "+selected.Skill.Path)
	}
	if len(selected.Skill.Scripts) > 0 {
		lines = append(lines, "Helper scripts: "+strings.Join(selected.Skill.Scripts, ", "))
		lines = append(lines, "If you need a helper script, open or run the concrete script path under the skill directory instead of rereading the whole skill folder.")
	}
	if strings.TrimSpace(selected.Skill.Instruction) != "" {
		lines = append(lines, "Skill content:\n"+strings.TrimSpace(selected.Skill.Instruction))
	}
	return strings.Join(lines, "\n")
}

func buildSkillCatalogMessage(snapshot *skills.Snapshot) *schema.Message {
	brief := skillCatalogBrief(snapshot)
	if strings.TrimSpace(brief) == "" {
		return nil
	}
	return buildContextEnvelopeMessage("skill-catalog", brief)
}

func skillCatalogBrief(snapshot *skills.Snapshot) string {
	if snapshot == nil || len(snapshot.Skills) == 0 {
		return ""
	}
	views := make([]skills.View, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		views = append(views, skills.CopyView(item))
	}
	if len(views) == 0 {
		return ""
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].ID != views[j].ID {
			return views[i].ID < views[j].ID
		}
		return views[i].Name < views[j].Name
	})

	lines := []string{
		"Available skills are runtime capabilities, not user input.",
		"When the user asks what you can do or which skill fits, inspect this catalog first and prefer the matching skill over a generic answer.",
		"If the catalog summary is not enough, call skill_list or skill_view before answering.",
		"Catalog:",
	}
	for _, view := range views {
		status := "eligible"
		if !view.Eligible {
			status = "ineligible"
		}
		label := fmt.Sprintf("- %s (%s) [%s]", view.ID, view.Name, status)
		if summary := strings.TrimSpace(view.Summary); summary != "" {
			label += ": " + summary
		}
		lines = append(lines, label)
		details := make([]string, 0, 3)
		if len(view.TriggerHints) > 0 {
			limit := min(2, len(view.TriggerHints))
			details = append(details, "triggers="+strings.Join(view.TriggerHints[:limit], ", "))
		}
		if len(view.Requires.Tools) > 0 {
			details = append(details, "tools="+strings.Join(view.Requires.Tools, ", "))
		}
		if len(view.DisabledReasons) > 0 {
			details = append(details, "disabled="+strings.Join(view.DisabledReasons, "; "))
		}
		if len(details) > 0 {
			lines = append(lines, "  "+strings.Join(details, " | "))
		}
	}
	if len(snapshot.Problems) > 0 {
		lines = append(lines, "Catalog problems:")
		for _, problem := range snapshot.Problems {
			label := "- " + strings.TrimSpace(problem.Error)
			if strings.TrimSpace(problem.ID) != "" {
				label = fmt.Sprintf("- %s (%s)", problem.ID, strings.TrimSpace(problem.Error))
			}
			lines = append(lines, label)
		}
	}
	return strings.Join(lines, "\n")
}

const TurnIndexExtraKey = "acorn_turn_index"

func CloneMessage(msg adk.Message) *schema.Message {
	message := *msg
	if msg.Extra != nil {
		message.Extra = CloneAnyMap(msg.Extra)
	}
	if msg.UserInputMultiContent != nil {
		message.UserInputMultiContent = append([]schema.MessageInputPart(nil), msg.UserInputMultiContent...)
		for i := range message.UserInputMultiContent {
			if message.UserInputMultiContent[i].Extra != nil {
				message.UserInputMultiContent[i].Extra = CloneAnyMap(message.UserInputMultiContent[i].Extra)
			}
		}
	}
	if msg.AssistantGenMultiContent != nil {
		message.AssistantGenMultiContent = append([]schema.MessageOutputPart(nil), msg.AssistantGenMultiContent...)
		for i := range message.AssistantGenMultiContent {
			if message.AssistantGenMultiContent[i].Extra != nil {
				message.AssistantGenMultiContent[i].Extra = CloneAnyMap(message.AssistantGenMultiContent[i].Extra)
			}
		}
	}
	if msg.ToolCalls != nil {
		message.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
	}
	return &message
}

func CloneAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
