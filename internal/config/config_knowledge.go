package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// KnowledgeConfig locates the knowledge base: markdown notes in a directory
// that is also a git repository.
type KnowledgeConfig struct {
	Dir string `yaml:"dir"`
}

// KnowledgeDir is knowledge.dir, or {storage_dir}/knowledge when unset.
func (c *Config) KnowledgeDir() string {
	if c.Knowledge.Dir != "" {
		return c.Knowledge.Dir
	}
	return filepath.Join(c.Runtime.StorageDir, "knowledge")
}

func (c *Config) validateKnowledge() error {
	info, err := os.Stat(c.KnowledgeDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("knowledge.dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("knowledge.dir %s is not a directory", c.KnowledgeDir())
	}
	return nil
}
