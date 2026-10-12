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
	URL      string          `json:"url,omitempty"`
	State    string          `json:"state"`
	Home     string          `json:"home,omitempty"`
	Error    string          `json:"error,omitempty"`
	Browsers []BrowserStatus `json:"browsers,omitempty"`
}

// BrowserStatus reports one bsk daemon: "workstation" drives the
// workstation's own Chromium, "owner" waits for the owner's browser.
type BrowserStatus struct {
	Daemon    string   `json:"daemon"`
	State     string   `json:"state"`
	Connected []string `json:"connected,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// browserStatusCommand prints the status of both bsk daemons, separated by a
// line of dashes; "off" stands for an owner daemon without a public URL.
const browserStatusCommand = `bsk status --json; echo ---; if [ -n "${ACORN_BROWSER_PUBLIC_URL:-}" ]; then BSK_HOME=$HOME/.bsk-owner bsk status --json; else echo off; fi`

func parseBrowserStatus(daemon, out string) BrowserStatus {
	out = strings.TrimSpace(out)
	if out == "off" {
		return BrowserStatus{Daemon: daemon, State: "off"}
	}
	var status struct {
		Browsers []struct {
			InstanceID string `json:"instance_id"`
			Label      string `json:"label"`
		} `json:"browsers"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		return BrowserStatus{Daemon: daemon, State: "error", Error: "bsk status: " + out}
	}
	result := BrowserStatus{Daemon: daemon, State: "waiting"}
	for _, b := range status.Browsers {
		result.State = "connected"
		name := b.InstanceID
		if b.Label != "" {
			name += " (" + b.Label + ")"
		}
		result.Connected = append(result.Connected, name)
	}
	return result
}

func browserStatuses(stdout string) []BrowserStatus {
	local, owner, _ := strings.Cut(stdout, "\n---\n")
	return []BrowserStatus{parseBrowserStatus("workstation", local), parseBrowserStatus("owner", owner)}
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
	// bsk status waits several seconds on a daemon with no browser connected.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil {
		return WorkstationStatus{URL: c.cfg.Workstation.URL, State: "unreachable", Error: err.Error()}
	}
	result := WorkstationStatus{URL: c.cfg.Workstation.URL, State: "ok", Home: status.Home}
	browsers, err := client.Exec(ctx, workstation.ExecRequest{Command: browserStatusCommand, TimeoutSeconds: 25, MaxOutputBytes: 16384})
	if err != nil {
		result.Browsers = []BrowserStatus{{Daemon: "workstation", State: "error", Error: err.Error()}}
		return result
	}
	result.Browsers = browserStatuses(browsers.Stdout)
	return result
}
