package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
	"gopkg.in/yaml.v3"
)

func runSkills(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("skills requires a subcommand: list, inspect, check, create, patch, or delete")
	}
	switch args[0] {
	case "list":
		return runSkillsList(ctx, args[1:])
	case "inspect":
		return runSkillsInspect(ctx, args[1:])
	case "check":
		return runSkillsCheck(ctx, args[1:])
	case "create":
		return runSkillsCreate(ctx, args[1:])
	case "patch":
		return runSkillsPatch(ctx, args[1:])
	case "delete":
		return runSkillsDelete(ctx, args[1:])
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
	return withContainer(ctx, *configPath, func(container *app.Container) error {
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
	return withContainer(ctx, *configPath, func(container *app.Container) error {
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
	fixturesPath := fs.String("fixtures", "", "YAML file with routing fixtures")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fixtures, err := loadRoutingFixtures(*fixturesPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	service := app.NewSkillService(cfg, skills.NewLoader(cfg))
	report, err := service.Health(ctx, fixtures)
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

func loadRoutingFixtures(path string) ([]skills.RoutingFixture, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill routing fixtures %s: %w", path, err)
	}
	var fixtures []skills.RoutingFixture
	if err := yaml.Unmarshal(body, &fixtures); err != nil {
		return nil, fmt.Errorf("parse skill routing fixtures %s: %w", path, err)
	}
	return fixtures, nil
}

func runSkillsCreate(ctx context.Context, args []string) error {
	fs := newFlagSet("skills create")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print created skill as JSON")
	id := fs.String("id", "", "skill id")
	name := fs.String("name", "", "skill name")
	category := fs.String("category", "", "skill category")
	summary := fs.String("summary", "", "skill summary")
	instruction := fs.String("instruction", "", "skill instruction")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		item, err := container.Skills().Create(ctx, app.CreateSkillInput{
			ID:          *id,
			Name:        *name,
			Category:    *category,
			Summary:     *summary,
			Instruction: *instruction,
		})
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

func runSkillsPatch(ctx context.Context, args []string) error {
	fs := newFlagSet("skills patch")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print patched skill as JSON")
	source := fs.String("source", "cli", "patch source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return fmt.Errorf("skills patch requires a skill id and patch text")
	}
	skillID := fs.Arg(0)
	content := strings.Join(fs.Args()[1:], " ")
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		item, err := container.Skills().Patch(ctx, skillID, content, *source)
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

func runSkillsDelete(ctx context.Context, args []string) error {
	fs := newFlagSet("skills delete")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("skills delete requires a skill id")
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		if err := container.Skills().Delete(ctx, fs.Arg(0)); err != nil {
			return err
		}
		fmt.Println("deleted " + fs.Arg(0))
		return nil
	})
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
		line := fmt.Sprintf("- %s (%s) %s", item.ID, item.Source, state)
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
		"  Source: " + item.Source,
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
	for _, observation := range report.Observations {
		lines = append(lines, "- observation "+renderHealthObservation(observation))
	}
	for _, fixture := range report.Fixtures {
		lines = append(lines, "- fixture "+renderRoutingFixtureResult(fixture))
	}
	if len(report.Failures) == 0 && len(report.Observations) == 0 && len(report.Fixtures) == 0 {
		lines = append(lines, "- no health findings")
	}
	return strings.Join(lines, "\n")
}

func renderHealthFailure(failure skills.HealthFailure) string {
	parts := []string{string(failure.Kind)}
	if label := firstNonEmpty(failure.SkillID, failure.Fixture, failure.Path); label != "" {
		parts = append(parts, label)
	}
	if failure.Message != "" {
		parts = append(parts, failure.Message)
	}
	return strings.Join(parts, ": ")
}

func renderHealthObservation(observation skills.HealthObservation) string {
	parts := []string{string(observation.Kind)}
	if label := firstNonEmpty(observation.SkillID, observation.Path); label != "" {
		parts = append(parts, label)
	}
	if observation.Message != "" {
		parts = append(parts, observation.Message)
	}
	return strings.Join(parts, ": ")
}

func renderRoutingFixtureResult(fixture skills.RoutingFixtureResult) string {
	parts := []string{fixture.ID, string(fixture.Status)}
	if fixture.ExpectedSkill != "" {
		parts = append(parts, "expected="+fixture.ExpectedSkill)
	}
	if fixture.ActualSkill != "" {
		parts = append(parts, "actual="+fixture.ActualSkill)
	}
	if len(fixture.Candidates) > 0 {
		parts = append(parts, "candidates="+strings.Join(fixture.Candidates, ","))
	}
	if fixture.Error != "" {
		parts = append(parts, "error="+fixture.Error)
	}
	return strings.Join(parts, " ")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
