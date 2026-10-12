package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
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

// policyRenderer checks a watch target against the shared URL policy before
// the workstation's browser renders it.
type policyRenderer struct {
	policy   webaccess.URLPolicy
	renderer watch.Renderer
}

func (r policyRenderer) RenderHTML(ctx context.Context, target string) (string, error) {
	if _, err := r.policy.Validate(ctx, target); err != nil {
		return "", err
	}
	return r.renderer.RenderHTML(ctx, target)
}

// WorkstationStatus is what acorn doctor reports about the workstation.
type WorkstationStatus struct {
	URL     string         `json:"url,omitempty"`
	State   string         `json:"state"`
	Home    string         `json:"home,omitempty"`
	Error   string         `json:"error,omitempty"`
	Browser *BrowserStatus `json:"browser,omitempty"`
}

// BrowserStatus reports whether the workstation's Chromium is connected to
// its bsk daemon.
type BrowserStatus struct {
	State     string   `json:"state"`
	Connected []string `json:"connected,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func parseBrowserStatus(out string) BrowserStatus {
	var status struct {
		Browsers []struct {
			InstanceID string `json:"instance_id"`
		} `json:"browsers"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		return BrowserStatus{State: "error", Error: "bsk status: " + strings.TrimSpace(out)}
	}
	result := BrowserStatus{State: "waiting"}
	for _, b := range status.Browsers {
		result.State = "connected"
		result.Connected = append(result.Connected, b.InstanceID)
	}
	return result
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
	// bsk status waits several seconds while no browser is connected.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil {
		return WorkstationStatus{URL: c.cfg.Workstation.URL, State: "unreachable", Error: err.Error()}
	}
	result := WorkstationStatus{URL: c.cfg.Workstation.URL, State: "ok", Home: status.Home}
	browser, err := client.Exec(ctx, workstation.ExecRequest{Command: "bsk status --json", TimeoutSeconds: 25, MaxOutputBytes: 16384})
	if err != nil {
		result.Browser = &BrowserStatus{State: "error", Error: err.Error()}
		return result
	}
	browserStatus := parseBrowserStatus(browser.Stdout + browser.Stderr)
	result.Browser = &browserStatus
	return result
}
