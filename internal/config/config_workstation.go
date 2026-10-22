package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ycvk/acorn/internal/workstation"
)

// WorkstationConfig points at the agent's workstation, which serves
// `acorn workstation serve` inside its own container. An empty URL turns the
// shell and files tools off; the token expands environment variables.
type WorkstationConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

func (c *Config) validateWorkstation() error {
	base := strings.TrimSpace(c.Workstation.URL)
	if base == "" {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("workstation.url %q must be an http(s) URL", base)
	}
	return nil
}

// validateWorkstationToken runs with execution readiness, like the model and
// embedding keys: commands that only load the config (backup, init) run
// without the env file that holds the token.
func (c *Config) validateWorkstationToken() error {
	if strings.TrimSpace(c.Workstation.URL) == "" {
		return nil
	}
	if len(c.Workstation.Token) < workstation.MinTokenLength {
		return errors.New("workstation.token must be at least 32 characters when workstation.url is set; set ACORN_WORKSTATION_TOKEN")
	}
	return nil
}
