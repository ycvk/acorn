package runtime

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

func BuildCatalogSpecs(
	ctx context.Context,
	cfg *config.Config,
	source string,
	kind core.ToolKind,
	baseTools []einotool.BaseTool,
) ([]core.ToolSpec, error) {
	specs := make([]core.ToolSpec, 0, len(baseTools))
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
	kind core.ToolKind,
	tool einotool.BaseTool,
) (core.ToolSpec, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return core.ToolSpec{}, fmt.Errorf("read tool info for %s spec: %w", source, err)
	}
	if info == nil {
		return core.ToolSpec{}, fmt.Errorf("read tool info for %s spec: nil ToolInfo", source)
	}
	name := strings.TrimSpace(info.Name)
	if name == "" {
		return core.ToolSpec{}, fmt.Errorf("%s tool has empty name", source)
	}

	if localSpec, ok := tools.ConfiguredLocalSpec(cfg, name); ok {
		localSpec.Tool = tool
		return localSpec, nil
	}

	if contract, ok := tools.BuiltinToolSpec(name, source); ok {
		return core.ToolSpec{ToolContract: contract, Tool: tool}, nil
	}

	spec := core.ToolSpec{
		ToolContract: core.ToolContract{
			Name:     name,
			Source:   source,
			Kind:     kind,
			Category: core.ToolCategoryInspect,
			Loading:  core.EagerLoadingPolicy(),
		},
		Tool: tool,
	}
	if kind == core.ToolKindMCP {
		spec.Category = core.ToolCategoryIntegration
	}
	return spec, nil
}
