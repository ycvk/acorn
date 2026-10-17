package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/watch"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workstation"
)

// buildWorkstationTools connects the shell, files and send_file tools to the
// configured workstation, or disables them with the reason when none is
// configured.
func buildWorkstationTools(cfg *config.Config, client *workstation.Client, files core.ThreadFiles, bridge core.ToolCallContextBridge) tools.WorkstationToolsConfig {
	out := tools.WorkstationToolsConfig{
		Files:                 files,
		Context:               bridge,
		CommandTimeoutSeconds: cfg.Workstation.CommandTimeoutSeconds,
		MaxOutputBytes:        cfg.Workstation.MaxOutputBytes,
		MaxSendBytes:          cfg.Files.MaxSendBytes(),
	}
	if client == nil {
		out.DisabledReason = noWorkstation
		return out
	}
	out.Service = client
	return out
}

// noWorkstation is why the tools that need a workstation are disabled.
const noWorkstation = "workstation.url is not configured"

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

// PackageToolStatus is what acorn doctor reports about the MCP server a
// skill package declares. State is ready, failed, stopped, missing_secrets,
// off (no workstation) or unknown (the workstation did not answer).
type PackageToolStatus struct {
	Skill          string   `json:"skill"`
	Command        string   `json:"command"`
	Secrets        []string `json:"secrets,omitempty"`
	MissingSecrets []string `json:"missing_secrets,omitempty"`
	State          string   `json:"state"`
	Error          string   `json:"error,omitempty"`
}

// PackageToolsStatus reports the MCP server of every skill in use that
// declares one: whether its secrets are set and how the workstation runs it.
// It starts and stops nothing; servers start with the next run that uses
// their skill.
func (c *Container) PackageToolsStatus(ctx context.Context) ([]PackageToolStatus, error) {
	scan, err := c.skillLibrary.Scan(ctx)
	if err != nil {
		return nil, err
	}
	var specs []skills.Spec
	for _, spec := range scan.Skills {
		if spec.MCP != nil {
			specs = append(specs, spec)
		}
	}
	if len(specs) == 0 {
		return nil, nil
	}
	client, err := buildWorkstationClient(c.cfg)
	if err != nil {
		return nil, err
	}
	running := map[string]workstation.MCPServerStatus{}
	var unreachable error
	if client != nil {
		statuses, err := client.MCPServers(ctx)
		unreachable = err
		for _, status := range statuses {
			running[status.Name] = status
		}
	}
	out := make([]PackageToolStatus, 0, len(specs))
	for _, spec := range specs {
		status := PackageToolStatus{Skill: spec.Name, Command: spec.MCP.CommandLine(), Secrets: spec.MCP.Secrets}
		missing, err := c.skillLibrary.MissingSecrets(ctx, spec)
		if err != nil {
			return nil, err
		}
		server, ok := running[spec.Name]
		switch {
		case len(missing) > 0:
			status.State, status.MissingSecrets = "missing_secrets", missing
		case client == nil:
			status.State, status.Error = "off", noWorkstation
		case unreachable != nil:
			status.State, status.Error = "unknown", unreachable.Error()
		case !ok:
			status.State = "stopped"
		case server.Hash != spec.Hash():
			status.State = "stopped"
			status.Error = "the workstation runs an older revision until the next run"
		case server.Ready:
			status.State = "ready"
		default:
			status.State, status.Error = "failed", server.Error
		}
		out = append(out, status)
	}
	return out, nil
}
