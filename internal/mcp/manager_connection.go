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
	"unicode"

	"github.com/cloudwego/eino-ext/components/tool/mcp/officialmcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/port"
)

func connectProvider(ctx context.Context, cfg ProviderConfig, opts *mcp.ClientOptions, store port.MCPTokenStore, onAuthStatusChanged func(status string)) (*provider, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("provider name is required")
	}

	transport, cleanup, metadata, err := NewTransportWithStore(cfg, store, onAuthStatusChanged)
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
	if err != nil {
		p := &provider{
			cfg:         cfg,
			commandPath: commandPath,
			session:     session,
			cleanup:     cleanup,
		}
		return nil, errors.Join(fmt.Errorf("discover MCP tools: %w", err), p.close())
	}

	toolNames, err := collectToolNames(connectCtx, tools)
	if err != nil {
		p := &provider{
			cfg:         cfg,
			commandPath: commandPath,
			session:     session,
			cleanup:     cleanup,
		}
		return nil, errors.Join(err, p.close())
	}

	p := &provider{
		cfg:         cfg,
		commandPath: commandPath,
		session:     session,
		cleanup:     cleanup,
		tools:       tools,
		toolNames:   toolNames,
	}

	if resResult, err := session.ListResources(connectCtx, nil); err == nil {
		p.resources = resResult.Resources
	} else {
		slog.Warn("MCP resource discovery failed, continuing with tools only",
			"provider", cfg.Name, "error", err)
	}
	if promptResult, err := session.ListPrompts(connectCtx, nil); err == nil {
		p.prompts = promptResult.Prompts
	} else {
		slog.Warn("MCP prompt discovery failed, continuing with tools only",
			"provider", cfg.Name, "error", err)
	}

	return p, nil
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
	name := sanitizeProviderName(providerName)
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

func sanitizeProviderName(providerName string) string {
	name := strings.TrimSpace(providerName)
	if name == "" {
		return "provider"
	}
	name = strings.ReplaceAll(name, "..", "-")
	var builder strings.Builder
	lastDash := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	cleaned := strings.Trim(builder.String(), "-_.")
	if cleaned == "" {
		return "provider"
	}
	return cleaned
}
