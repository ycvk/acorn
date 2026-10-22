package config

import (
	"errors"
	"net/url"
	"strings"
)

type MemoryConfig struct {
	Embedding MemoryEmbeddingConfig `yaml:"embedding"`
}

type MemoryEmbeddingConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Model      string `yaml:"model"`
	Dimensions int    `yaml:"dimensions"`
}

func (c *Config) validateMemory() error {
	m := c.Memory
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
