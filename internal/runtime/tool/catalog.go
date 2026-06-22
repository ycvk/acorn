package tool

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/contextplane"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/toolkit"
)

func BuildCatalogSpecs(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind toolkit.ToolKind,
	tools []einotool.BaseTool,
) ([]toolkit.ToolSpec, error) {
	specs := make([]toolkit.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		spec, err := RuntimeToolSpec(ctx, cfg, source, kind, tool)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func RuntimeToolSpec(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind toolkit.ToolKind,
	tool einotool.BaseTool,
) (toolkit.ToolSpec, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return toolkit.ToolSpec{}, fmt.Errorf("read tool info for %s spec: %w", source, err)
	}
	if info == nil {
		return toolkit.ToolSpec{}, fmt.Errorf("read tool info for %s spec: nil ToolInfo", source)
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		return toolkit.ToolSpec{}, fmt.Errorf("%s tool has empty name", source)
	}

	if localSpec, ok := toolkit.ConfiguredLocalSpec(cfg, name); ok {
		localSpec.Tool = tool
		return localSpec, nil
	}

	if contract, ok := toolkit.BuiltinToolSpec(name, source); ok {
		return toolkit.ToolSpec{ToolContract: contract, Tool: tool}, nil
	}

	spec := toolkit.ToolSpec{
		ToolContract: toolkit.ToolContract{
			Name:     name,
			Source:   source,
			Kind:     kind,
			Category: toolkit.ToolCategoryInspect,
			Loading:  toolkit.EagerLoadingPolicy(),
			Execution: toolkit.ToolExecutionPolicy{
				ParallelPolicy: toolkit.ParallelPolicyReadOnly,
			},
		},
		Tool: tool,
	}

	switch kind {
	case toolkit.ToolKindMCP:
		spec.Kind = kind
		spec.Category = toolkit.ToolCategoryIntegration
		spec.Execution.ParallelPolicy = toolkit.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	default:
		spec.Category = toolkit.ToolCategoryInspect
		spec.Execution.ParallelPolicy = toolkit.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	}
	return spec, nil
}

func MCPToolParallelPolicy(cfg *config.Config, providerName string) (toolkit.ParallelPolicy, error) {
	if cfg == nil {
		return "", fmt.Errorf("resolve MCP tool safety for provider %q: config is required", strings.TrimSpace(providerName))
	}
	for _, provider := range cfg.MCP.Providers {
		if strings.TrimSpace(provider.Name) != strings.TrimSpace(providerName) {
			continue
		}
		if strings.TrimSpace(provider.ToolSafety) == "" {
			return "", fmt.Errorf("mcp provider %q must declare tool_safety", strings.TrimSpace(providerName))
		}
		return toolkit.ParseParallelPolicy(provider.ToolSafety)
	}
	return "", fmt.Errorf("mcp provider %q is not configured", strings.TrimSpace(providerName))
}

type loadToolsInput struct {
	Query     string   `json:"query,omitempty"`
	ToolNames []string `json:"tool_names,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type loadToolsOutput struct {
	Messages        []string `json:"messages,omitempty"`
	LoadedToolNames []string `json:"loaded_tool_names,omitempty"`
	AlreadyLoaded   []string `json:"already_loaded,omitempty"`
}

func NewLoadToolsTool() (einotool.BaseTool, error) {
	return toolutils.InferTool("load_tools", "Load deferred tool definitions by query or exact tool names.", func(ctx context.Context, input loadToolsInput) (loadToolsOutput, error) {
		result, err := contextplane.DeferredLoad(ctx, contextplane.DeferredLoadRequest{
			RunID:     getRunID(ctx),
			SessionID: domain.SessionIDFromContext(ctx),
			Query:     strings.TrimSpace(input.Query),
			ToolNames: append([]string(nil), input.ToolNames...),
			Limit:     input.Limit,
		})
		if err != nil {
			return loadToolsOutput{}, err
		}
		messageTexts := make([]string, 0, len(result.Messages))
		for _, msg := range result.Messages {
			if msg == nil {
				continue
			}
			messageTexts = append(messageTexts, strings.TrimSpace(msg.Content))
		}
		return loadToolsOutput{
			Messages:        messageTexts,
			LoadedToolNames: append([]string(nil), result.LoadedToolNames...),
			AlreadyLoaded:   append([]string(nil), result.AlreadyLoaded...),
		}, nil
	})
}

func IsLoadToolsCall(call schema.ToolCall) bool {
	return strings.TrimSpace(call.Function.Name) == "load_tools"
}
