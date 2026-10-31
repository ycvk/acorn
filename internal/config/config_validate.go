package config

import (
	"errors"
	"fmt"
	"strings"
)

func (c *Config) ValidateBase() error {
	if strings.TrimSpace(c.Runtime.StorageDir) == "" {
		return errors.New("runtime.storage_dir is required")
	}
	if c.Backup.Dir != "" && c.Backup.Keep < 1 {
		return errors.New("backup.keep must be >= 1")
	}
	if strings.TrimSpace(c.Web.ListenAddr) == "" {
		return errors.New("web.listen_addr is required")
	}
	if err := c.validateWebAccessBase(); err != nil {
		return err
	}
	if err := c.validateMemory(); err != nil {
		return err
	}
	if err := c.validateNotify(); err != nil {
		return err
	}
	if err := c.validateWatch(); err != nil {
		return err
	}
	if err := c.validateWorkstation(); err != nil {
		return err
	}
	if err := c.validateFiles(); err != nil {
		return err
	}
	if err := c.validateReview(); err != nil {
		return err
	}
	return c.validateMCP()
}

func (c *Config) ValidateExecutionReady() error {
	if err := c.ValidateBase(); err != nil {
		return err
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	if _, err := c.InputTokenBudget(); err != nil {
		return err
	}
	if strings.TrimSpace(c.Memory.Embedding.APIKey) == "" || strings.Contains(c.Memory.Embedding.APIKey, "${") {
		return errors.New("memory.embedding.api_key is required; set VOYAGE_API_KEY")
	}
	return c.validateWorkstationToken()
}

func (c *Config) validateWebAccessBase() error {
	switch provider := strings.TrimSpace(c.WebAccess.Search.Provider); provider {
	case "exa":
	default:
		return fmt.Errorf("web_access.search.provider must be exa, got %q", provider)
	}
	return nil
}
