package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/decision"
)

func runDecision(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("decision requires a subcommand: check or inspect")
	}
	switch args[0] {
	case "check":
		return runDecisionCheck(ctx, args[1:])
	case "inspect":
		return runDecisionInspect(ctx, args[1:])
	default:
		return fmt.Errorf("unknown decision subcommand %q", args[0])
	}
}

func runDecisionCheck(ctx context.Context, args []string) error {
	fs := newFlagSet("decision check")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print decision profile as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		profile, err := container.DecisionProfile()
		if err != nil {
			return err
		}
		if *jsonMode {
			return printJSON(profile)
		}
		fmt.Println(renderDecisionProfile(profile))
		return nil
	})
}

func runDecisionInspect(ctx context.Context, args []string) error {
	fs := newFlagSet("decision inspect")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print saved decision as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("decision inspect requires a run id")
	}
	runID := fs.Arg(0)
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		record, err := container.InspectRunDecision(ctx, runID)
		if err != nil {
			return err
		}
		if record == nil {
			return fmt.Errorf("decision for run %s not found", runID)
		}
		if *jsonMode {
			return printJSON(record)
		}
		fmt.Println(renderDecisionRecord(record))
		return nil
	})
}

func renderDecisionProfile(profile *decision.ParsedProfile) string {
	if profile == nil {
		return "Decision Profile\n  none"
	}
	lines := []string{
		"Decision Profile",
		fmt.Sprintf("  Path: %s", valueOrNone(profile.Path)),
		fmt.Sprintf("  Hash: %s", valueOrNone(profile.Hash)),
		fmt.Sprintf("  Missing context: %s", profile.Profile.Defaults.MissingContext),
		fmt.Sprintf("  Missing capability: %s", profile.Profile.Defaults.MissingRequiredCapability),
		fmt.Sprintf("  Routes: %d", len(profile.Profile.Routes)),
	}
	for _, route := range profile.Profile.Routes {
		lines = append(lines, fmt.Sprintf("    - %s -> %s %s", route.Intent, route.Action, compactCLIText(route.SkillID, 80)))
	}
	return strings.Join(lines, "\n")
}

func renderDecisionRecord(record *decision.Record) string {
	if record == nil {
		return "Decision\n  none"
	}
	lines := []string{
		"Decision",
		fmt.Sprintf("  Run ID: %s", valueOrNone(record.RunID)),
		fmt.Sprintf("  Action: %s", valueOrNone(string(record.Action))),
		fmt.Sprintf("  Intent: %s", valueOrNone(record.Intent)),
		fmt.Sprintf("  Skill: %s", valueOrNone(record.SelectedSkillID)),
		fmt.Sprintf("  Reason: %s", valueOrNone(record.DecisionReason)),
		fmt.Sprintf("  Profile hash: %s", valueOrNone(record.DecisionProfileHash)),
		fmt.Sprintf("  Created at: %s", formatTime(record.CreatedAt)),
	}
	return strings.Join(lines, "\n")
}
