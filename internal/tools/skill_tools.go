package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// SkillLibrary is the skill collection as the skill tools change it.
type SkillLibrary interface {
	Write(ctx context.Context, markdown string, baseRevision int64, runID string) (core.Skill, error)
	Retire(ctx context.Context, id string, baseRevision int64, runID string) (core.Skill, error)
}

// SkillToolDeps are what the skill tools need.
type SkillToolDeps struct {
	Library  SkillLibrary
	Context  core.ToolCallContextBridge
	Location *time.Location
}

// SkillWriteInput is the input of skill_write.
type SkillWriteInput struct {
	Markdown     string `json:"markdown" jsonschema_description:"The whole skill document. It starts with YAML frontmatter, for example:\n---\nid: skill.shop.orders\nsummary: Look up the owner's recent orders on the shop site.\ntrigger_hints: [订单, orders]\nrequires:\n  tools: [shell]\n---\nthen a '# Name' heading and the numbered steps. id is lowercase letters, digits, dots, dashes or underscores. summary says when to use the skill in at most 200 characters. trigger_hints and requires are optional; requires.tools lists only tools from your tool list that the steps cannot run without."`
	BaseRevision int64  `json:"base_revision,omitempty" jsonschema_description:"For an existing skill, the revision shown when you loaded it. Omit for a new skill."`
}

// SkillRetireInput is the input of skill_retire.
type SkillRetireInput struct {
	ID           string `json:"id" jsonschema_description:"Id of the skill to retire."`
	BaseRevision int64  `json:"base_revision" jsonschema_description:"The revision shown when you loaded the skill."`
}

// SkillWriteOutput reports the stored revision of a skill.
type SkillWriteOutput struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
	Retired  bool   `json:"retired,omitempty"`
	Updated  string `json:"updated"`
}

func buildSkillWriteTool(deps SkillToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("skill_write",
		"Save a reusable procedure as a skill, or replace a skill's whole document. Write one after finishing multi-step work the owner is likely to ask for again, and fix a skill whose steps turned out wrong. A skill holds steps and when they apply; facts about the owner belong in memory. Every write is kept as a new revision, and writing a retired skill brings it back.",
		func(ctx context.Context, input SkillWriteInput, _ ToolProgressEmitter) (SkillWriteOutput, error) {
			if err := checkRequiredTools(input.Markdown); err != nil {
				return SkillWriteOutput{}, fmt.Errorf("skill_write: %w", err)
			}
			skill, err := deps.Library.Write(ctx, input.Markdown, input.BaseRevision, deps.Context.CurrentRunID(ctx))
			if err != nil {
				return SkillWriteOutput{}, fmt.Errorf("skill_write: %w", err)
			}
			return skillOutput(skill, deps.Location), nil
		})
}

func buildSkillRetireTool(deps SkillToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("skill_retire",
		"Take a skill out of use when it no longer applies or another skill covers it. The skill stays in its history and skill_write brings it back.",
		func(ctx context.Context, input SkillRetireInput, _ ToolProgressEmitter) (SkillWriteOutput, error) {
			skill, err := deps.Library.Retire(ctx, input.ID, input.BaseRevision, deps.Context.CurrentRunID(ctx))
			if err != nil {
				return SkillWriteOutput{}, fmt.Errorf("skill_retire: %w", err)
			}
			return skillOutput(skill, deps.Location), nil
		})
}

// checkRequiredTools rejects a skill document that does not parse, or that
// requires a tool no run has, which would leave the skill out of every run.
func checkRequiredTools(markdown string) error {
	spec, err := skills.Parse(markdown)
	if err != nil {
		return err
	}
	known := map[string]struct{}{}
	for _, name := range localToolNames {
		known[name] = struct{}{}
	}
	for _, name := range BuiltinToolNames() {
		known[name] = struct{}{}
	}
	var unknown []string
	for _, name := range spec.Requires.Tools {
		if _, ok := known[name]; !ok && !strings.HasPrefix(name, "mcp__") {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("requires.tools names %s, which no run has; list only tools from your tool list that the steps cannot run without", strings.Join(unknown, ", "))
	}
	return nil
}

func skillOutput(skill core.Skill, loc *time.Location) SkillWriteOutput {
	return SkillWriteOutput{
		ID:       skill.ID,
		Revision: skill.Revision,
		Retired:  skill.Retired,
		Updated:  skill.UpdatedAt.In(loc).Format(localTimeLayout),
	}
}
