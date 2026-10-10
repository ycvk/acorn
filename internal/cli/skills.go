package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/wire"
)

func runSkills(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("skills requires a subcommand: list, inspect, or check")
	}
	switch args[0] {
	case "list":
		return runSkillsList(ctx, args[1:])
	case "inspect":
		return runSkillsInspect(ctx, args[1:])
	case "check":
		return runSkillsCheck(ctx, args[1:])
	default:
		return fmt.Errorf("unknown skills subcommand %q", args[0])
	}
}

func runSkillsList(ctx context.Context, args []string) error {
	fs := newFlagSet("skills list")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print skills as JSON")
	limit := fs.Int("limit", 0, "max skills to return")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		items, err := container.Skills().List(ctx, *limit)
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(items)
		}
		fmt.Println(renderSkillsList(items))
		return nil
	})
}

func runSkillsInspect(ctx context.Context, args []string) error {
	fs := newFlagSet("skills inspect")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print skill as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("skills inspect requires a skill id")
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		item, err := container.Skills().Get(ctx, fs.Arg(0))
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(item)
		}
		fmt.Println(renderSkillDetail(*item))
		return nil
	})
}

func runSkillsCheck(ctx context.Context, args []string) error {
	fs := newFlagSet("skills check")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print skill health report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	service := api.NewSkillService(cfg, skills.NewLoader(cfg.Skills.Dir))
	report, err := service.Health(ctx)
	if err != nil {
		return err
	}
	if *jsonMode {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		fmt.Println(renderSkillsCheck(*report))
	}
	return skillCheckError(*report)
}

func renderSkillsList(items []skills.View) string {
	if len(items) == 0 {
		return "No skills found."
	}
	lines := []string{"Skills"}
	for _, item := range items {
		state := "eligible"
		if !item.Eligible {
			state = "ineligible: " + strings.Join(item.DisabledReasons, ";")
		}
		line := fmt.Sprintf("- %s %s", item.ID, state)
		if summary := strings.TrimSpace(item.Summary); summary != "" {
			line += " - " + summary
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func renderSkillDetail(item skills.View) string {
	lines := []string{
		"Skill",
		"  ID: " + item.ID,
		"  Name: " + item.Name,
		"  Path: " + item.Path,
	}
	if item.Category != "" {
		lines = append(lines, "  Category: "+item.Category)
	}
	if item.Summary != "" {
		lines = append(lines, "  Summary: "+item.Summary)
	}
	if len(item.Requires.Tools) > 0 {
		lines = append(lines, "  Required tools: "+strings.Join(item.Requires.Tools, ", "))
	}
	if len(item.Requires.Toolsets) > 0 {
		lines = append(lines, "  Required toolsets: "+strings.Join(item.Requires.Toolsets, ", "))
	}
	if len(item.Requires.Bins) > 0 {
		lines = append(lines, "  Required bins: "+strings.Join(item.Requires.Bins, ", "))
	}
	if len(item.Requires.Env) > 0 {
		lines = append(lines, "  Required env: "+strings.Join(item.Requires.Env, ", "))
	}
	if len(item.Scripts) > 0 {
		lines = append(lines, "  Files: "+strings.Join(item.Scripts, ", "))
	}
	if !item.Eligible {
		lines = append(lines, "  Disabled: "+strings.Join(item.DisabledReasons, "; "))
	}
	if instruction := strings.TrimSpace(item.Instruction); instruction != "" {
		lines = append(lines, "", instruction)
	}
	return strings.Join(lines, "\n")
}

func renderSkillsCheck(report skills.HealthReport) string {
	lines := []string{"Skills check"}
	lines = append(lines, "Status: "+string(report.Status))
	for _, failure := range report.Failures {
		lines = append(lines, "- failure "+renderHealthFailure(failure))
	}
	if len(report.Failures) == 0 {
		lines = append(lines, "- no health findings")
	}
	return strings.Join(lines, "\n")
}

func renderHealthFailure(failure skills.HealthFailure) string {
	parts := []string{string(failure.Kind)}
	if label := skills.FirstNonEmpty(failure.SkillID, failure.Path); label != "" {
		parts = append(parts, label)
	}
	if failure.Message != "" {
		parts = append(parts, failure.Message)
	}
	return strings.Join(parts, ": ")
}

func skillCheckError(report skills.HealthReport) error {
	if report.Status != skills.HealthFailed {
		return nil
	}
	failures := make([]string, 0, len(report.Failures))
	for _, failure := range report.Failures {
		failures = append(failures, renderHealthFailure(failure))
	}
	return errors.New("skill check failed: " + strings.Join(failures, " | "))
}
