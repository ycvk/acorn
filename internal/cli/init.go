package cli

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	storageDir := fs.String("storage-dir", "", "absolute directory for runtime storage")
	backupDir := fs.String("backup-dir", "", "absolute directory for nightly backups")
	workstationURL := fs.String("workstation-url", "", "URL of the agent's workstation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, err := initConfig(*storageDir, *backupDir, *workstationURL)
	if err != nil {
		return err
	}

	if *printOnly {
		fmt.Print(body)
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
	if err := os.WriteFile(absPath, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", absPath, err)
	}

	fmt.Printf("Wrote starter config to %s\n", absPath)
	fmt.Println("Next: set OPENAI_API_KEY and VOYAGE_API_KEY in your environment, then run 'acorn doctor' and 'acorn smoke \"hello\"'.")
	return nil
}

// initConfig returns the starter config, with runtime storage in storageDir,
// backups in backupDir and the workstation at workstationURL when they are
// given.
func initConfig(storageDir, backupDir, workstationURL string) (string, error) {
	body := initConfigTemplate
	if workstationURL != "" {
		line := "  url: \"\"\n"
		if !strings.Contains(body, line) {
			return "", errors.New("init template is missing workstation url")
		}
		body = strings.Replace(body, line, "  url: "+strconv.Quote(workstationURL)+"\n", 1)
	}
	for _, setting := range []struct{ flag, value, line, key string }{
		{"--storage-dir", storageDir, "  storage_dir: ~/.acorn\n", "  storage_dir: "},
		{"--backup-dir", backupDir, "  dir: \"\"\n", "  dir: "},
	} {
		if setting.value == "" {
			continue
		}
		if !filepath.IsAbs(setting.value) {
			return "", fmt.Errorf("%s must be an absolute path: %s", setting.flag, setting.value)
		}
		if !strings.Contains(body, setting.line) {
			return "", fmt.Errorf("init template is missing %q", strings.TrimSpace(setting.line))
		}
		body = strings.Replace(body, setting.line, setting.key+strconv.Quote(setting.value)+"\n", 1)
	}
	return body, nil
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
