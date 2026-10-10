package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type MemoryConfig struct {
	DailyTokens   int                   `yaml:"daily_tokens"`
	BatchTokens   int                   `yaml:"batch_tokens"`
	ContextTokens int                   `yaml:"context_tokens"`
	HistoryTokens int                   `yaml:"history_tokens"`
	Embedding     MemoryEmbeddingConfig `yaml:"embedding"`
}

type MemoryEmbeddingConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

func (c *Config) validateMemory() error {
	m := c.Memory
	if m.DailyTokens < 0 {
		return errors.New("memory.daily_tokens must be >= 0")
	}
	if m.BatchTokens < 256 || m.ContextTokens < 256 || m.HistoryTokens < 256 {
		return errors.New("memory.batch_tokens, context_tokens and history_tokens must be >= 256")
	}
	u, err := url.Parse(m.Embedding.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("memory.embedding.base_url must be an absolute http(s) URL without query or fragment")
	}
	if strings.TrimSpace(m.Embedding.Model) == "" {
		return errors.New("memory.embedding.model is required")
	}
	if m.Embedding.Dimensions <= 0 || m.Embedding.Dimensions > 4096 {
		return errors.New("memory.embedding.dimensions must be between 1 and 4096")
	}
	return nil
}

func (c *Config) validateMemoryBudget() error {
	budget, err := c.InputTokenBudget()
	if err != nil {
		return err
	}
	if c.Memory.BatchTokens >= budget {
		return fmt.Errorf("memory.batch_tokens must be smaller than available model input (%d)", budget)
	}
	return nil
}
