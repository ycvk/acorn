package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/workspace"
)

func (c *Config) Workspace() (*workspace.Workspace, error) {
	if c == nil {
		return nil, errors.New("config is required")
	}

	rootDir, err := c.workspaceRootDir()
	if err != nil {
		return nil, err
	}

	return workspace.New(workspace.Config{
		RootDir:                  rootDir,
		StorageDir:               strings.TrimSpace(c.Runtime.StorageDir),
		MutationDenylist:         append([]string(nil), c.Tools.Mutation.Denylist...),
		RunCommandDefaultTimeout: c.Tools.RunCommand.DefaultTimeout,
		RunCommandEnvWhitelist:   append([]string(nil), c.Tools.RunCommand.EnvWhitelist...),
	})
}

func (c *Config) workspaceRootDir() (string, error) {
	candidates := make([]string, 0, 3)
	for _, candidate := range []string{
		strings.TrimSpace(c.Tools.Workspace.RootDir),
		strings.TrimSpace(c.Tools.Mutation.RootDir),
		strings.TrimSpace(c.Tools.RunCommand.WorkDir),
	} {
		if candidate != "" {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		if configDir := strings.TrimSpace(c.ConfigDir); configDir != "" {
			candidates = append(candidates, configDir)
		}
	}
	if len(candidates) == 0 {
		if storageDir := strings.TrimSpace(c.Runtime.StorageDir); storageDir != "" {
			candidates = append(candidates, filepath.Join(storageDir, ".."))
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("workspace root dir is not configured")
	}

	absRoots := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		absRoot, err := filepath.Abs(candidate)
		if err != nil {
			return "", fmt.Errorf("resolve workspace root candidate %q: %w", candidate, err)
		}
		absRoots = append(absRoots, filepath.Clean(absRoot))
	}
	rootDir := absRoots[0]
	for _, candidate := range absRoots[1:] {
		if candidate != rootDir {
			return "", fmt.Errorf("workspace root mismatch: local tool roots/workdir must all resolve to one root (%q != %q)", candidate, rootDir)
		}
	}
	return rootDir, nil
}
