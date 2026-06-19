package cli

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed acorn.init.yaml
var initConfigTemplate string

// runInit writes a minimal working self-hosted starter config to the resolved
// config path so a freshly built binary no longer dies on "config file not found".
// It refuses to clobber an existing config unless --force, and supports --print to
// emit the template to stdout (for `acorn init --print > path` style headless setup).
func runInit(_ context.Context, args []string) error {
	fs := newFlagSet("init")
	configPath := addConfigFlag(fs)
	force := fs.Bool("force", false, "overwrite an existing config file")
	printOnly := fs.Bool("print", false, "write the starter config to stdout instead of a file")
	if err := fs.Parse(args); err != nil {
		return err
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
	fmt.Println("Next: set OPENAI_API_KEY in your environment, then run 'acorn doctor' and 'acorn smoke \"hello\"'.")
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
