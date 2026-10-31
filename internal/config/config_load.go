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
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("config path is required")
	}
	path, err := expandHome(path)
	if err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found at %s — run 'acorn init -c %s' to create one (or pass -c <path> to point elsewhere): %w", absPath, absPath, err)
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
	if strings.TrimSpace(cfg.Runtime.StorageDir) != "" {
		if cfg.Runtime.StorageDir, err = resolveDir(cfg.ConfigDir, cfg.Runtime.StorageDir); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(cfg.Backup.Dir) != "" {
		if cfg.Backup.Dir, err = resolveDir(cfg.ConfigDir, cfg.Backup.Dir); err != nil {
			return nil, err
		}
	}
	if cfg.Notify.FCM.ServiceAccountFile != "" {
		if cfg.Notify.FCM.ServiceAccountFile, err = resolveDir(cfg.ConfigDir, cfg.Notify.FCM.ServiceAccountFile); err != nil {
			return nil, err
		}
	}
	for i := range cfg.MCP.Providers {
		provider := &cfg.MCP.Providers[i]
		if provider.WorkDir, err = resolveDir(cfg.ConfigDir, provider.WorkDir); err != nil {
			return nil, err
		}
		if provider.Command, err = resolveExecutable(cfg.ConfigDir, provider.Command); err != nil {
			return nil, err
		}
		provider.Transport = strings.TrimSpace(provider.Transport)
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
	cfg.Memory.Embedding.APIKey = os.ExpandEnv(cfg.Memory.Embedding.APIKey)
	cfg.WebAccess.Search.APIKey = os.ExpandEnv(cfg.WebAccess.Search.APIKey)
	cfg.Watch.GitHubToken = os.ExpandEnv(cfg.Watch.GitHubToken)
	cfg.Workstation.Token = os.ExpandEnv(cfg.Workstation.Token)
	cfg.Review.APIKey = os.ExpandEnv(cfg.Review.APIKey)
}

func resolveDir(configDir, value string) (string, error) {
	value, err := expandHome(value)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return configDir, nil
	}
	if filepath.IsAbs(value) {
		return value, nil
	}
	return filepath.Clean(filepath.Join(configDir, value)), nil
}

// expandHome expands a bare ~ or a leading ~/ to the user's home directory.
// Paths like ~user/ are left unchanged.
func expandHome(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	if len(path) == 1 || path[1] == '/' || path[1] == os.PathSeparator {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~ in %q: %w", path, err)
		}
		if len(path) == 1 {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

func resolveExecutable(configDir, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if filepath.IsAbs(trimmed) || strings.ContainsRune(trimmed, os.PathSeparator) {
		return resolveDir(configDir, trimmed)
	}
	return trimmed, nil
}
