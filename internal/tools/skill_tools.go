package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// SkillLibrary is the skill collection as the skill tools change it.
type SkillLibrary interface {
	Skill(ctx context.Context, name string) (core.Skill, error)
	Write(ctx context.Context, req skills.WriteRequest) (core.Skill, error)
	Retire(ctx context.Context, name string, baseRevision int64, runID string) (core.Skill, error)
	Stage(ctx context.Context, entries []core.SkillFile) (core.SkillPackage, error)
	ImportStaged(ctx context.Context, name, sha256 string, baseRevision int64, runID string) (core.Skill, error)
}

// SkillPackageReader reads a directory or zip on the workstation.
type SkillPackageReader interface {
	ReadPackage(ctx context.Context, path string) ([]core.SkillFile, error)
}

// SkillToolDeps are what the skill tools need. Without Packages, skill_inspect
// is disabled for PackagesDisabledReason.
type SkillToolDeps struct {
	Library                SkillLibrary
	Packages               SkillPackageReader
	PackagesDisabledReason string
	Context                core.ToolCallContextBridge
	Location               *time.Location
}

// SkillWriteInput is the input of skill_write.
type SkillWriteInput struct {
	Markdown     string            `json:"markdown" jsonschema_description:"The whole SKILL.md. It starts with YAML frontmatter, for example:\n---\nname: shop-orders\ndescription: Look up the owner's recent orders on the shop site. Use when they ask about an order, a delivery or a refund (订单, 快递).\nmetadata:\n  acorn-requires-tools: shell\n---\nthen the steps. name is lowercase letters, digits and single hyphens, at most 64 characters. description says what the skill does and when to use it, with words the owner would say, in at most 1024 characters. metadata.acorn-requires-tools is optional and lists, separated by spaces, only tools from your tool list that the steps cannot run without."`
	Files        map[string]string `json:"files,omitempty" jsonschema_description:"Text files to add or replace in the skill, by relative path such as scripts/fill.py or references/forms.md. Scripts go under scripts/, longer documentation under references/, templates under assets/. Unlisted files of the base revision stay."`
	Delete       []string          `json:"delete,omitempty" jsonschema_description:"Relative paths of files to remove from the skill."`
	BaseRevision int64             `json:"base_revision,omitempty" jsonschema_description:"For an existing skill, the revision shown when you loaded it. Omit for a new skill."`
}

// SkillRetireInput is the input of skill_retire.
type SkillRetireInput struct {
	Name         string `json:"name" jsonschema_description:"Name of the skill to retire."`
	BaseRevision int64  `json:"base_revision" jsonschema_description:"The revision shown when you loaded the skill."`
}

// SkillInspectInput is the input of skill_inspect.
type SkillInspectInput struct {
	Path string `json:"path" jsonschema_description:"A skill directory or .zip on the workstation, absolute or relative to the home directory."`
}

// SkillImportInput is the input of skill_import.
type SkillImportInput struct {
	Name         string `json:"name" jsonschema_description:"Name of the skill skill_inspect reported."`
	SHA256       string `json:"sha256" jsonschema_description:"The sha256 skill_inspect reported for the package."`
	BaseRevision int64  `json:"base_revision,omitempty" jsonschema_description:"When a skill of that name exists, the current_revision skill_inspect reported. Omit for a new skill."`
}

// SkillWriteOutput reports the stored revision of a skill.
type SkillWriteOutput struct {
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
	Retired  bool   `json:"retired,omitempty"`
	Files    int    `json:"files,omitempty"`
	Updated  string `json:"updated"`
}

// SkillInspectOutput describes a package ready for skill_import.
type SkillInspectOutput struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	License         string            `json:"license,omitempty"`
	Compatibility   string            `json:"compatibility,omitempty"`
	SHA256          string            `json:"sha256"`
	Files           []SkillFileOutput `json:"files,omitempty"`
	CurrentRevision int64             `json:"current_revision,omitempty"`
	CurrentRetired  bool              `json:"current_retired,omitempty"`
}

type SkillFileOutput struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

func buildSkillWriteTool(deps SkillToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("skill_write",
		"Save a reusable procedure as a skill, or change one. Write one after finishing multi-step work the owner is likely to ask for again, and fix a skill whose steps turned out wrong. A skill holds steps and when they apply, and may carry scripts and reference files; facts about the owner belong in memory. Every write is kept as a new revision, and writing a retired skill brings it back.",
		func(ctx context.Context, input SkillWriteInput, _ ToolProgressEmitter) (SkillWriteOutput, error) {
			if err := checkRequiredTools(input.Markdown); err != nil {
				return SkillWriteOutput{}, fmt.Errorf("skill_write: %w", err)
			}
			skill, err := deps.Library.Write(ctx, skills.WriteRequest{
				Markdown:     input.Markdown,
				Put:          input.Files,
				Delete:       input.Delete,
				BaseRevision: input.BaseRevision,
				RunID:        deps.Context.CurrentRunID(ctx),
			})
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
			skill, err := deps.Library.Retire(ctx, input.Name, input.BaseRevision, deps.Context.CurrentRunID(ctx))
			if err != nil {
				return SkillWriteOutput{}, fmt.Errorf("skill_retire: %w", err)
			}
			return skillOutput(skill, deps.Location), nil
		})
}

func buildSkillInspectTool(deps SkillToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("skill_inspect",
		"Check a skill package in the Agent Skills format, a directory or .zip with a SKILL.md, that you downloaded or wrote on the workstation. It reports the skill and the sha256 that skill_import takes. Read the package's SKILL.md and scripts before importing it.",
		func(ctx context.Context, input SkillInspectInput, _ ToolProgressEmitter) (SkillInspectOutput, error) {
			entries, err := deps.Packages.ReadPackage(ctx, input.Path)
			if err != nil {
				return SkillInspectOutput{}, fmt.Errorf("skill_inspect: %w", err)
			}
			pkg, err := deps.Library.Stage(ctx, entries)
			if err != nil {
				return SkillInspectOutput{}, fmt.Errorf("skill_inspect: %w", err)
			}
			spec, err := skills.Parse(pkg.Markdown)
			if err != nil {
				return SkillInspectOutput{}, fmt.Errorf("skill_inspect: %w", err)
			}
			out := SkillInspectOutput{Name: pkg.Name, Description: spec.Description, License: spec.License, Compatibility: spec.Compatibility, SHA256: pkg.SHA256}
			for _, f := range pkg.Files {
				out.Files = append(out.Files, SkillFileOutput{Path: f.Path, Bytes: len(f.Content)})
			}
			current, err := deps.Library.Skill(ctx, pkg.Name)
			switch {
			case errors.Is(err, core.ErrSkillNotFound):
			case err != nil:
				return SkillInspectOutput{}, fmt.Errorf("skill_inspect: %w", err)
			default:
				out.CurrentRevision, out.CurrentRetired = current.Revision, current.Retired
			}
			return out, nil
		})
}

func buildSkillImportTool(deps SkillToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("skill_import",
		"Add a package that skill_inspect checked to your skills, or replace a skill with it. The owner sees the package's SKILL.md and files when approving.",
		func(ctx context.Context, input SkillImportInput, _ ToolProgressEmitter) (SkillWriteOutput, error) {
			skill, err := deps.Library.ImportStaged(ctx, input.Name, input.SHA256, input.BaseRevision, deps.Context.CurrentRunID(ctx))
			if err != nil {
				return SkillWriteOutput{}, fmt.Errorf("skill_import: %w", err)
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
	for _, name := range spec.RequiresTools {
		if _, ok := known[name]; !ok && !strings.HasPrefix(name, "mcp__") {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("metadata.%s names %s, which no run has; list only tools from your tool list that the steps cannot run without", skills.RequiresToolsKey, strings.Join(unknown, ", "))
	}
	return nil
}

func skillOutput(skill core.Skill, loc *time.Location) SkillWriteOutput {
	return SkillWriteOutput{
		Name:     skill.ID,
		Revision: skill.Revision,
		Retired:  skill.Retired,
		Files:    len(skill.Files),
		Updated:  skill.UpdatedAt.In(loc).Format(localTimeLayout),
	}
}
