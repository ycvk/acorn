package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ycvk/acorn/internal/config"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/toolfactory"
)

func buildContainerMCPServer(cfg *config.Config, runnerFactory *runtime.RunnerFactory) (*mcp.Server, *toolfactory.Toolset, error) {
	if len(cfg.Serve.Tools.Allowlist) == 0 {
		return nil, nil, nil
	}
	ctx := context.Background()
	toolset, err := runnerFactory.BuildServeToolset(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("build serve toolset: %w", err)
	}
	mcpServer, err := mcpprovider.NewMCPServer(ctx, cfg.Serve, toolset.All())
	if err != nil {
		if closeErr := toolset.Close(); closeErr != nil {
			return nil, nil, errors.Join(fmt.Errorf("create MCP server: %w", err), fmt.Errorf("close serve toolset after MCP server failure: %w", closeErr))
		}
		return nil, nil, fmt.Errorf("create MCP server: %w", err)
	}
	return mcpServer, toolset, nil
}
