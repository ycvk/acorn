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
	"github.com/ycvk/acorn/internal/core"
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
	case "memory":
		return runMemory(ctx, args[1:])
	case "init":
		return runInit(ctx, args[1:])
	case "doctor":
		return runDoctor(ctx, args[1:])
	case "skills":
		return runSkills(ctx, args[1:])
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
  acorn memory preflight [-c path] [--json]
  acorn memory reindex [-c path] [--json]
  acorn skills list [-c path] [--json]
  acorn skills inspect [-c path] [--json] SKILL_ID
  acorn skills check [-c path] [--json] [--fixtures path]
  acorn pair [-c path] [--json] [--qr] [--ttl duration] [--server-url url]
  acorn token issue [-c path] [--json] [--name name] [--ttl duration]
  acorn devices list [-c path] [--json]
  acorn devices revoke [-c path] DEVICE_ID
  acorn smoke [-c path] [--json] "task input"
  acorn run [-c path] [--json] "task input"
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
		knowledgeStatus, err := container.KnowledgeStatus(ctx)
		if err != nil {
			return fmt.Errorf("knowledge base: %w", err)
		}
		watches, err := container.Watches(ctx)
		if err != nil {
			return fmt.Errorf("watches: %w", err)
		}
		thinking, err := container.ThinkingStatus(ctx)
		if err != nil {
			return fmt.Errorf("thinking status: %w", err)
		}
		memoryStatus, err := container.MemoryStatus(ctx)
		if err != nil {
			return fmt.Errorf("memory status: %w", err)
		}
		if *jsonMode {
			return printJSON(struct {
				api.SystemCapabilities
				Thinking wire.ThinkingStatus         `json:"thinking"`
				Memory   core.MemoryProcessingStatus `json:"memory"`
			}{snapshot, thinking, memoryStatus})
		}
		fmt.Println(renderDoctorSummary(snapshot, container.Config().ConfigPath))
		fmt.Println(renderDoctorKnowledge(knowledgeStatus))
		fmt.Println(renderDoctorWatches(container.Config(), watches))
		fmt.Println(renderDoctorThinking(container.Config(), thinking))
		fmt.Printf("Memory: tokens_today=%d daily_limit=%d pending=%d failed=%d oldest=%s last_completed=%s index=%s/%d generation=%d state=%s\n", memoryStatus.TokensToday, memoryStatus.DailyTokenLimit, memoryStatus.Pending, memoryStatus.Failed, memoryStatus.Oldest, memoryStatus.LastCompleted, memoryStatus.Index.Model, memoryStatus.Index.Dimensions, memoryStatus.Index.Generation, memoryStatus.Index.State)
		if memoryStatus.LastError != "" {
			fmt.Printf("Memory error: %s\n", memoryStatus.LastError)
		}
		return nil
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
