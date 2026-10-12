package wire

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workstation"
)

// buildWorkstationTools connects the shell and files tools to the configured
// workstation, or disables them with the reason when none is configured.
func buildWorkstationTools(cfg *config.Config) (tools.WorkstationToolsConfig, error) {
	out := tools.WorkstationToolsConfig{
		CommandTimeoutSeconds: cfg.Workstation.CommandTimeoutSeconds,
		MaxOutputBytes:        cfg.Workstation.MaxOutputBytes,
	}
	client, err := buildWorkstationClient(cfg)
	if err != nil {
		return tools.WorkstationToolsConfig{}, err
	}
	if client == nil {
		out.DisabledReason = "workstation.url is not configured"
		return out, nil
	}
	out.Service = client
	return out, nil
}

// buildWorkstationClient returns nil when no workstation is configured. The
// HTTP client has no overall timeout: each command carries its own.
func buildWorkstationClient(cfg *config.Config) (*workstation.Client, error) {
	base := strings.TrimSpace(cfg.Workstation.URL)
	if base == "" {
		return nil, nil
	}
	client, err := workstation.NewClient(base, cfg.Workstation.Token, &http.Client{})
	if err != nil {
		return nil, fmt.Errorf("workstation: %w", err)
	}
	return client, nil
}

// WorkstationStatus is what acorn doctor reports about the workstation.
type WorkstationStatus struct {
	URL   string `json:"url,omitempty"`
	State string `json:"state"`
	Home  string `json:"home,omitempty"`
	Error string `json:"error,omitempty"`
}

// WorkstationStatus asks the configured workstation for its status.
func (c *Container) WorkstationStatus(ctx context.Context) WorkstationStatus {
	client, err := buildWorkstationClient(c.cfg)
	if err != nil {
		return WorkstationStatus{URL: c.cfg.Workstation.URL, State: "error", Error: err.Error()}
	}
	if client == nil {
		return WorkstationStatus{State: "off"}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil {
		return WorkstationStatus{URL: c.cfg.Workstation.URL, State: "unreachable", Error: err.Error()}
	}
	return WorkstationStatus{URL: c.cfg.Workstation.URL, State: "ok", Home: status.Home}
}
