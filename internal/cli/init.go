package cli

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/presence"
)

//go:embed acorn.init.yaml
var initConfigTemplate string

// runInit writes a minimal working self-hosted starter config to the resolved
// config path so a freshly built binary has a runnable config.
// It refuses to clobber an existing config unless --force, and supports --print to
// emit the template to stdout (for `acorn init --print > path` style headless setup).
func runInit(_ context.Context, args []string) error {
	fs := newFlagSet("init")
	configPath := addConfigFlag(fs)
	force := fs.Bool("force", false, "overwrite an existing config file")
	printOnly := fs.Bool("print", false, "write the starter config to stdout instead of a file")
	personaOnly := fs.Bool("persona-only", false, "only write the default persona next to an existing config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *personaOnly {
		return writeDefaultPersona(*configPath)
	}

	if *printOnly {
		fmt.Print(initConfigTemplate)
		return nil
	}

	absPath, err := resolveInitConfigPath(*configPath)
	if err != nil {
		return err
	}

	if info, statErr := os.Stat(absPath); statErr == nil {
		if info.IsDir() {
			return fmt.Errorf("config path %s is a directory", absPath)
		}
		if !*force {
			return fmt.Errorf("config already exists at %s — use --force to overwrite, or --print to emit to stdout", absPath)
		}
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat config %s: %w", absPath, statErr)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return fmt.Errorf("create config dir %s: %w", filepath.Dir(absPath), err)
	}
	if err := os.WriteFile(absPath, []byte(initConfigTemplate), 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", absPath, err)
	}

	fmt.Printf("Wrote starter config to %s\n", absPath)
	if err := writeDefaultPersona(absPath); err != nil {
		return err
	}
	fmt.Println("Next: set OPENAI_API_KEY in your environment, then run 'acorn doctor' and 'acorn smoke \"hello\"'.")
	return nil
}

// writeDefaultPersona writes the default persona into the storage dir of the
// config at configPath. An existing persona is the owner's and is kept.
func writeDefaultPersona(configPath string) error {
	absPath, err := resolveInitConfigPath(configPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load(absPath)
	if err != nil {
		return err
	}
	path := cfg.PersonaPath()
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("Keeping existing persona: %s\n", path)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat persona %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create storage dir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(presence.DefaultPersona), 0o600); err != nil {
		return fmt.Errorf("write persona %s: %w", path, err)
	}
	fmt.Printf("Wrote persona to %s (edit it to shape how Acorn talks and acts)\n", path)
	return nil
}

func resolveInitConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultConfigPath
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir for %s: %w", path, err)
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	return filepath.Abs(path)
}
