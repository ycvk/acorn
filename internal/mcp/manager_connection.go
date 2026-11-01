package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/core"
)

func connectProvider(ctx context.Context, cfg core.ProviderConfig, opts *mcp.ClientOptions, store core.ArtifactStore) (*provider, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("provider name is required")
	}

	transport, cleanup, metadata, err := NewTransportWithStore(cfg, store)
	if err != nil {
		return nil, err
	}

	var commandPath string
	if metadata.Kind == "stdio" {
		resolved, err := resolveCommandPath(cfg.Command)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("resolve MCP provider command path for %q: %w", cfg.Name, err)
		}
		commandPath = resolved
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "acorn", Version: "v0.1.0"}, opts)
	connectCtx, cancel := context.WithTimeout(ctx, timeoutDuration(cfg.StartupTimeoutSeconds))
	defer cancel()

	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("start MCP session: %w", err)
	}

	tools, err := officialmcp.GetTools(connectCtx, &officialmcp.Config{
		Cli:          session,
		ToolNameList: cfg.ToolNames,
	})
	p := &provider{
		cfg:         cfg,
		commandPath: commandPath,
		session:     session,
		cleanup:     cleanup,
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("discover MCP tools: %w", err), p.close())
	}

	toolNames, err := collectToolNames(connectCtx, tools)
	if err != nil {
		return nil, errors.Join(err, p.close())
	}
	p.tools, p.toolNames = tools, toolNames

	var capabilities *mcp.ServerCapabilities
	if result := session.InitializeResult(); result != nil {
		capabilities = result.Capabilities
	}
	if capabilities != nil && capabilities.Resources != nil {
		if resResult, err := session.ListResources(connectCtx, nil); err == nil {
			p.resources = resResult.Resources
		} else {
			slog.Warn("MCP resource discovery failed, continuing with tools only",
				"provider", cfg.Name, "error", err)
		}
	}
	if capabilities != nil && capabilities.Prompts != nil {
		if promptResult, err := session.ListPrompts(connectCtx, nil); err == nil {
			p.prompts = promptResult.Prompts
		} else {
			slog.Warn("MCP prompt discovery failed, continuing with tools only",
				"provider", cfg.Name, "error", err)
		}
	}

	return p, nil
}

// Connection is a session with one MCP provider and the tools it offered
// when it connected.
type Connection struct {
	p *provider
}

// Connect opens a session with the provider and lists its tools.
func Connect(ctx context.Context, cfg core.ProviderConfig) (*Connection, error) {
	p, err := connectProvider(ctx, cfg, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("connect MCP provider %s: %w", cfg.Name, err)
	}
	return &Connection{p: p}, nil
}

// Tools are the provider's tools under their own names.
func (c *Connection) Tools() []einotool.BaseTool {
	return c.p.tools
}

func (c *Connection) Close() error {
	return c.p.close()
}

func collectToolNames(ctx context.Context, tools []einotool.BaseTool) ([]string, error) {
	names := make([]string, 0, len(tools))
	for _, item := range tools {
		info, err := item.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool info: %w", err)
		}
		names = append(names, info.Name)
	}
	return names, nil
}

func resolveCommandPath(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("command is required")
	}
	if filepath.IsAbs(command) || strings.ContainsRune(command, os.PathSeparator) {
		abs, err := filepath.Abs(command)
		if err != nil {
			return "", fmt.Errorf("resolve command path: %w", err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("command %q not found: %w", abs, err)
		}
		return abs, nil
	}
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("command %q not found in PATH: %w", command, err)
	}
	return path, nil
}

func mergeEnv(base []string, override map[string]string) []string {
	if len(override) == 0 {
		return append([]string(nil), base...)
	}
	values := make(map[string]string, len(base)+len(override))
	order := make([]string, 0, len(base)+len(override))
	for _, item := range base {
		parts := strings.SplitN(item, "=", 2)
		key := parts[0]
		val := ""
		if len(parts) == 2 {
			val = parts[1]
		}
		if _, ok := values[key]; !ok {
			order = append(order, key)
		}
		values[key] = val
	}
	for key, val := range override {
		if _, ok := values[key]; !ok {
			order = append(order, key)
		}
		values[key] = val
	}
	merged := make([]string, 0, len(order))
	for _, key := range order {
		merged = append(merged, key+"="+values[key])
	}
	return merged
}

func timeoutDuration(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = 20
	}
	return time.Duration(seconds) * time.Second
}

func (p *provider) close() error {
	if p == nil {
		return nil
	}
	var errs []error
	if p.session != nil {
		if err := p.session.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p.cleanup != nil {
		p.cleanup()
	}
	return errors.Join(errs...)
}

func openProviderLogFile(providerName string) (*os.File, error) {
	dir := filepath.Join(os.TempDir(), "acorn-mcp-logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create MCP log dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("chmod MCP log dir: %w", err)
	}
	name := ToolNamespace(providerName)
	path := filepath.Join(dir, name+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open MCP log file: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			slog.Warn("failed to close MCP log file after chmod error", "error", closeErr)
		}
		return nil, fmt.Errorf("chmod MCP log file: %w", err)
	}
	return file, nil
}

// ToolNamespace is the provider part of the provider's tool names,
// mcp__<namespace>__<tool>: ASCII letters and digits, with every other
// character turned into "-", since model APIs accept no others in tool names.
func ToolNamespace(provider string) string {
	var b strings.Builder
	for _, r := range provider {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "provider"
	}
	return result
}
