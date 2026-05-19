package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/app"
)

const defaultConfigPath = "~/.acorn/acorn.yaml"

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	if strings.HasPrefix(args[0], "-") {
		return usageError()
	}
	switch args[0] {
	case "doctor":
		return runDoctor(ctx, args[1:])
	case "decision":
		return runDecision(ctx, args[1:])
	case "skills":
		return runSkills(ctx, args[1:])
	case "memory":
		return runMemory(ctx, args[1:])
	case "pair":
		return runPair(ctx, args[1:])
	case "serve":
		return runServe(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func usageError() error {
	return errors.New(usageText())
}

func usageText() string {
	return strings.TrimSpace(`acorn - self-hosted Go + Eino agent backend

Usage:
  acorn doctor [-c path] [--json]
  acorn decision check [-c path] [--json]
  acorn decision inspect [-c path] [--json] RUN_ID
  acorn skills list [-c path] [--json]
  acorn skills inspect [-c path] [--json] SKILL_ID
  acorn skills check [-c path] [--json] [--fixtures path]
  acorn skills create [-c path] --id id --name name --instruction text [--summary text]
  acorn skills patch [-c path] SKILL_ID "patch text"
  acorn skills delete [-c path] SKILL_ID
  acorn pair [-c path] [--json] [--qr] [--ttl duration] [--server-url url]
  acorn memory procedure create [-c path] [--json] --title title --task-pattern pattern --source-run run --evidence-refs refs "procedure body"
  acorn memory semantic rebuild [-c path] [--json]
  acorn serve [-c path] [--listen addr]`)
}

func runDoctor(ctx context.Context, args []string) error {
	fs := newFlagSet("doctor")
	configPath := addConfigFlag(fs)
	jsonMode := fs.Bool("json", false, "print canonical capability snapshot as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return withContainer(ctx, *configPath, func(container *app.Container) error {
		snapshot := container.Capabilities().Snapshot(ctx, app.CapabilitySnapshotOptions{ProbeMCP: true})
		return printDoctorOutput(snapshot, *jsonMode)
	})
}
