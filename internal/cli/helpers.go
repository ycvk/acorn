package cli

import (
	"context"
	"flag"
	"os"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/config"
)

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

func withContainer(ctx context.Context, configPath string, fn func(*app.Container) error) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	container, err := app.NewContainer(ctx, cfg)
	if err != nil {
		return err
	}
	defer container.Close()
	return fn(container)
}
