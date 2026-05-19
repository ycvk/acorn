package contextplane

import (
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

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
