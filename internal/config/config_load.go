package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func Load(path string) (*Config, error) {
	if path == "" {
		path = "configs/acorn.example.yaml"
	}
	path = expandHome(path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found at %s — run 'acorn init' to create one (or pass -c <path> to point elsewhere)", absPath)
		}
		return nil, fmt.Errorf("read config %s: %w", absPath, err)
	}

	cfg := defaultConfig()
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		// KnownFields(true) reports unknown/misspelled keys as a *yaml.TypeError
		// whose Errors carry "line N: field X not found in type ...". Surface
		// those (with the file path) instead of a single opaque wrapped error so
		// a typo points straight at the offending field and line.
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) && len(typeErr.Errors) > 0 {
			return nil, fmt.Errorf("parse config %s: %s", absPath, strings.Join(typeErr.Errors, "; "))
		}
		return nil, fmt.Errorf("parse config %s: %w", absPath, err)
	}

	expandConfigEnv(cfg)
	cfg.ConfigPath = absPath
	cfg.ConfigDir = filepath.Dir(absPath)
	cfg.Runtime.StorageDir = resolveDir(cfg.ConfigDir, cfg.Runtime.StorageDir)
	if strings.TrimSpace(cfg.Memory.Semantic.Bleve.Path) != "" {
		cfg.Memory.Semantic.Bleve.Path = resolveDir(cfg.ConfigDir, cfg.Memory.Semantic.Bleve.Path)
	}
	cfg.Browser.ExecutablePath = resolveExecutable(cfg.ConfigDir, cfg.Browser.ExecutablePath)
	cfg.Tools.Workspace.RootDir = resolveDir(cfg.ConfigDir, cfg.Tools.Workspace.RootDir)
	cfg.Tools.Mutation.RootDir = resolveDir(cfg.ConfigDir, cfg.Tools.Mutation.RootDir)
	cfg.Tools.RunCommand.WorkDir = resolveDir(cfg.ConfigDir, cfg.Tools.RunCommand.WorkDir)
	for i := range cfg.MCP.Providers {
		cfg.MCP.Providers[i].WorkDir = resolveDir(cfg.ConfigDir, cfg.MCP.Providers[i].WorkDir)
		cfg.MCP.Providers[i].Command = resolveExecutable(cfg.ConfigDir, cfg.MCP.Providers[i].Command)
		cfg.MCP.Providers[i].Transport = NormalizeProviderTransport(cfg.MCP.Providers[i].Transport)
	}

	if err := cfg.ValidateBase(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func expandConfigEnv(cfg *Config) {
	if cfg == nil {
		return
	}
	for i := range cfg.Providers {
		cfg.Providers[i].APIKey = os.ExpandEnv(cfg.Providers[i].APIKey)
	}
	cfg.Memory.Semantic.Embedding.APIKey = os.ExpandEnv(cfg.Memory.Semantic.Embedding.APIKey)
	cfg.WebAccess.Search.APIKey = os.ExpandEnv(cfg.WebAccess.Search.APIKey)
}

func resolveDir(configDir, value string) string {
	value = expandHome(value)
	if strings.TrimSpace(value) == "" {
		return configDir
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(configDir, value))
}

// expandHome expands a leading ~/ to the user's home directory.
// Paths like ~user/ or bare ~ without a separator are left unchanged.
func expandHome(path string) string {
	if len(path) == 0 || path[0] != '~' {
		return path
	}
	if len(path) == 1 || path[1] == '/' || path[1] == os.PathSeparator {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		if len(path) == 1 {
			return home
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func resolveExecutable(configDir, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, os.PathSeparator) {
		return resolveDir(configDir, trimmed)
	}
	return trimmed
}
