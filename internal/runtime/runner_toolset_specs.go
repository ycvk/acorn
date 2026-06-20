package runtime

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
)

func assembleToolsetCatalog(ctx context.Context, cfg *config.Config, localCatalog *tools.Catalog, aux auxTools, includePlanning bool) (*tooling.Catalog, error) {
	core, err := buildCoreToolSpecs(ctx, cfg, localCatalog, aux)
	if err != nil {
		return nil, err
	}
	extra, err := buildExtraToolSpecs(ctx, cfg, aux, includePlanning)
	if err != nil {
		return nil, err
	}
	specs := append(core, extra...)
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return catalog, nil
}

func buildCoreToolSpecs(ctx context.Context, cfg *config.Config, localCatalog *tools.Catalog, aux auxTools) ([]tooling.ToolSpec, error) {
	profiles := []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}
	specs, err := tool.BuildCatalogSpecs(ctx, cfg, "local", tooling.ToolKindNative, profiles, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	checkpointSpecs, err := tool.BuildCatalogSpecs(ctx, cfg, "workingstate", tooling.ToolKindMemory, profiles, aux.checkpoint)
	if err != nil {
		return nil, err
	}
	memorySpecs, err := tool.BuildCatalogSpecs(ctx, cfg, "memory", tooling.ToolKindMemory, profiles, aux.memory)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := tool.BuildCatalogSpecs(ctx, cfg, "skill", tooling.ToolKindSkill, profiles, aux.skill)
	if err != nil {
		return nil, err
	}
	specs = append(specs, checkpointSpecs...)
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	return specs, nil
}

func buildExtraToolSpecs(ctx context.Context, cfg *config.Config, aux auxTools, includePlanning bool) ([]tooling.ToolSpec, error) {
	lifecycleSpecs, err := tool.BuildCatalogSpecs(ctx, cfg, "skill.lifecycle", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun}, aux.lifecycle)
	if err != nil {
		return nil, err
	}
	specs := lifecycleSpecs
	if !includePlanning {
		return specs, nil
	}
	loadToolsTool, err := tool.NewLoadToolsTool()
	if err != nil {
		return nil, fmt.Errorf("build load_tools tool: %w", err)
	}
	planningSpecs, err := tool.BuildCatalogSpecs(ctx, cfg, "runtime", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, []einotool.BaseTool{loadToolsTool})
	if err != nil {
		return nil, err
	}
	return append(specs, planningSpecs...), nil
}
