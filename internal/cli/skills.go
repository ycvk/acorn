package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/wire"
)

func runSkills(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("skills requires a subcommand: list, inspect, check, import, or secret")
	}
	switch args[0] {
	case "secret":
		return runSkillsSecret(ctx, args[1:])
	case "list":
		return runSkillsList(ctx, args[1:])
	case "inspect":
		return runSkillsInspect(ctx, args[1:])
	case "check":
		return runSkillsCheck(ctx, args[1:])
	case "import":
		return runSkillsImport(ctx, args[1:])
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
		return fmt.Errorf("skills inspect requires a skill name")
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
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		report, err := container.Skills().Health(ctx)
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
	})
}

func runSkillsImport(ctx context.Context, args []string) error {
	fs := newFlagSet("skills import")
	configPath := addConfigFlag(fs)
	replace := fs.Bool("replace", false, "replace an existing skill of the same name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("skills import requires a skill directory or .zip")
	}
	entries, err := skills.ReadPath(fs.Arg(0))
	if err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		skill, err := container.Skills().Import(ctx, entries, *replace)
		if err != nil {
			return err
		}
		fmt.Printf("Imported %s at revision %d with %d files.\n", skill.ID, skill.Revision, len(skill.Files))
		return nil
	})
}

// maxSecretBytes bounds a secret's value.
const maxSecretBytes = 64 << 10

// runSkillsSecret sets a secret a skill's MCP server declares. The value is
// read from standard input, without echo from a terminal, and never printed.
func runSkillsSecret(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "set" {
		return errors.New("usage: acorn skills secret set [-c path] SKILL NAME < value")
	}
	fs := newFlagSet("skills secret set")
	configPath := addConfigFlag(fs)
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: acorn skills secret set [-c path] SKILL NAME < value")
	}
	skill, name := fs.Arg(0), fs.Arg(1)
	value, err := readSecret(os.Stdin, name)
	if err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		if err := container.Skills().SetSecret(ctx, skill, name, value); err != nil {
			return err
		}
		fmt.Printf("Set %s for %s; its MCP server gets it from the next run on.\n", name, skill)
		return nil
	})
}

// readSecret reads a secret's value: from a terminal as a hidden line,
// otherwise all of the input without one trailing newline.
func readSecret(in *os.File, name string) (string, error) {
	if term.IsTerminal(int(in.Fd())) {
		fmt.Fprintf(os.Stderr, "Value of %s: ", name)
		value, err := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", name, err)
		}
		return secretValue(value)
	}
	value, err := io.ReadAll(io.LimitReader(in, maxSecretBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s from standard input: %w", name, err)
	}
	return secretValue(value)
}

func secretValue(value []byte) (string, error) {
	if len(value) > maxSecretBytes {
		return "", fmt.Errorf("the value is longer than %d bytes", maxSecretBytes)
	}
	text := strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r")
	if text == "" {
		return "", errors.New("the value is empty")
	}
	return text, nil
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
		lines = append(lines, fmt.Sprintf("- %s %s - %s", item.Name, state, item.Description))
	}
	return strings.Join(lines, "\n")
}

func renderSkillDetail(item skills.View) string {
	lines := []string{
		"Skill",
		"  Name: " + item.Name,
		fmt.Sprintf("  Revision: %d", item.Revision),
		"  Description: " + item.Description,
	}
	if item.License != "" {
		lines = append(lines, "  License: "+item.License)
	}
	if item.Compatibility != "" {
		lines = append(lines, "  Compatibility: "+item.Compatibility)
	}
	if len(item.RequiresTools) > 0 {
		lines = append(lines, "  Required tools: "+strings.Join(item.RequiresTools, ", "))
	}
	if item.MCP != nil {
		lines = append(lines, "  MCP server: "+item.MCP.CommandLine())
		if len(item.MCP.Secrets) > 0 {
			lines = append(lines, "  Secrets: "+strings.Join(item.MCP.Secrets, ", "))
		}
	}
	for _, f := range item.Files {
		lines = append(lines, fmt.Sprintf("  File: %s (%d bytes)", f.Path, f.Size))
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
	if label := failure.Skill; label != "" {
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
