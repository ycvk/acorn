package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"encoding/json"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/wire"
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
	case "init":
		return runInit(ctx, args[1:])
	case "doctor":
		return runDoctor(ctx, args[1:])
	case "skills":
		return runSkills(ctx, args[1:])
	case "memory":
		return runMemory(ctx, args[1:])
	case "pair":
		return runPair(ctx, args[1:])
	case "token":
		return runToken(ctx, args[1:])
	case "devices":
		return runDevices(ctx, args[1:])
	case "run":
		return runRun(ctx, args[1:])
	case "smoke":
		return runSmoke(ctx, args[1:])
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
  acorn init [-c path] [--force] [--print]
  acorn doctor [-c path] [--json]
  acorn skills list [-c path] [--json]
  acorn skills inspect [-c path] [--json] SKILL_ID
  acorn skills check [-c path] [--json] [--fixtures path]
  acorn skills create [-c path] --id id --name name --instruction text [--summary text]
  acorn skills patch [-c path] SKILL_ID "patch text"
  acorn skills delete [-c path] SKILL_ID
  acorn pair [-c path] [--json] [--qr] [--ttl duration] [--server-url url]
  acorn token issue [-c path] [--json] [--name name] [--ttl duration]
  acorn devices list [-c path] [--json]
  acorn devices revoke [-c path] DEVICE_ID
  acorn smoke [-c path] [--json] "task input"
  acorn run [-c path] [--json] "task input"
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
	return withContainer(ctx, *configPath, func(container *wire.Container) error {
		snapshot := container.Capabilities().Snapshot(ctx, api.CapabilitySnapshotOptions{ProbeMCP: true})
		return printDoctorOutput(snapshot, container.Config().ConfigPath, *jsonMode)
	})
}

func printJSON(value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(body))
	return nil
}

func runMemory(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory requires a subcommand")
	}
	return fmt.Errorf("unknown memory subcommand %q", args[0])
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	return fs
}

func addConfigFlag(fs *flag.FlagSet) *string {
	return fs.String("c", defaultConfigPath, "config file path")
}

func loadConfig(configPath string) (*config.Config, error) {
	return config.Load(configPath)
}

func withContainer(ctx context.Context, configPath string, fn func(*wire.Container) error) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	container, err := wire.NewContainer(ctx, cfg)
	if err != nil {
		return err
	}
	defer container.Close()
	return fn(container)
}
