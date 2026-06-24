package runtime

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/context"
	"github.com/ycvk/acorn/internal/domain"
	"github.com/ycvk/acorn/internal/tools"
)

func BuildCatalogSpecs(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind tools.ToolKind,
	baseTools []einotool.BaseTool,
) ([]tools.ToolSpec, error) {
	specs := make([]tools.ToolSpec, 0, len(baseTools))
	for _, tool := range baseTools {
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
	kind tools.ToolKind,
	tool einotool.BaseTool,
) (tools.ToolSpec, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return tools.ToolSpec{}, fmt.Errorf("read tool info for %s spec: %w", source, err)
	}
	if info == nil {
		return tools.ToolSpec{}, fmt.Errorf("read tool info for %s spec: nil ToolInfo", source)
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		return tools.ToolSpec{}, fmt.Errorf("%s tool has empty name", source)
	}

	if localSpec, ok := tools.ConfiguredLocalSpec(cfg, name); ok {
		localSpec.Tool = tool
		return localSpec, nil
	}

	if contract, ok := tools.BuiltinToolSpec(name, source); ok {
		return tools.ToolSpec{ToolContract: contract, Tool: tool}, nil
	}

	spec := tools.ToolSpec{
		ToolContract: tools.ToolContract{
			Name:     name,
			Source:   source,
			Kind:     kind,
			Category: tools.ToolCategoryInspect,
			Loading:  tools.EagerLoadingPolicy(),
			Execution: tools.ToolExecutionPolicy{
				ParallelPolicy: tools.ParallelPolicyReadOnly,
			},
		},
		Tool: tool,
	}

	switch kind {
	case tools.ToolKindMCP:
		spec.Kind = kind
		spec.Category = tools.ToolCategoryIntegration
		spec.Execution.ParallelPolicy = tools.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	default:
		spec.Category = tools.ToolCategoryInspect
		spec.Execution.ParallelPolicy = tools.ParallelPolicyReadOnly
		spec.Execution.PathArg = "path"
	}
	return spec, nil
}

func MCPToolParallelPolicy(cfg *config.Config, providerName string) (tools.ParallelPolicy, error) {
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
		return tools.ParseParallelPolicy(provider.ToolSafety)
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
		result, err := context.DeferredLoad(ctx, context.DeferredLoadRequest{
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
