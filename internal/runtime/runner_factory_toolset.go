package runtime

import (
	"context"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/skilllifecycle"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
	"github.com/ycvk/acorn/internal/workingstate"
)

type Toolset struct {
	catalog *tooling.Catalog
	profile tooling.ToolProfile
}

func (t Toolset) All() []einotool.BaseTool {
	if t.catalog == nil {
		return nil
	}
	return t.catalog.ToolsForProfile(t.profile)
}

func (t Toolset) Catalog() *tooling.Catalog {
	return t.catalog
}

func (f *RunnerFactory) BuildServeToolset(ctx context.Context) (*Toolset, error) {
	return f.buildToolset(ctx, "", nil, false, tooling.ToolProfileServe)
}

type delegateTaskBridge struct{}

func (delegateTaskBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (delegateTaskBridge) CurrentSessionID(ctx context.Context) string {
	return sessionIDFromContext(ctx)
}

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string, childExec orchestration.ChildAgentExecutor) (*Toolset, error) {
	return f.buildToolset(ctx, sessionID, childExec, true, tooling.ToolProfileRun)
}

func (f *RunnerFactory) buildToolset(
	ctx context.Context,
	sessionID string,
	childExec orchestration.ChildAgentExecutor,
	includePlanning bool,
	profile tooling.ToolProfile,
) (*Toolset, error) {
	if f == nil || f.cfg == nil {
		return nil, errors.New("runner factory is not initialized")
	}
	if f.workspaceErr != nil {
		return nil, fmt.Errorf("workspace contract: %w", f.workspaceErr)
	}
	if f.workspace == nil {
		return nil, errors.New("workspace contract is not initialized")
	}

	localCatalog, err := tools.BuildCatalog(tools.CatalogConfig{
		Workspace:         f.workspace,
		MutationEnabled:   !f.cfg.Tools.Mutation.Disabled,
		RunCommandEnabled: !f.cfg.Tools.RunCommand.Disabled,
	}, f.extraLocalTools, childExec, delegateTaskBridge{})
	if err != nil {
		return nil, err
	}

	checkpointService := f.checkpointService
	effectiveSessionID := sessionID
	if !includePlanning {
		checkpointService = nil
		effectiveSessionID = ""
	}
	checkpointTools, err := workingstate.BuildWorkingCheckpointTools(checkpointService, effectiveSessionID)
	if err != nil {
		return nil, fmt.Errorf("build working checkpoint tools: %w", err)
	}
	var memoryTools []einotool.BaseTool
	if f.memoryModule != nil {
		fileTools, err := buildMemoryFileTools(ctx, f.memoryModule)
		if err != nil {
			return nil, err
		}
		memoryTools = append(memoryTools, fileTools...)
	}

	skillTools, err := skills.BuildAgentTools(f.loader)
	if err != nil {
		return nil, fmt.Errorf("build skill tools: %w", err)
	}
	var skillLifecycleTools []einotool.BaseTool
	if includePlanning {
		skillLifecycleTools, err = skilllifecycle.BuildAgentTools(skilllifecycle.ToolOptions{
			Loader: f.loader,
			Store:  f.store,
			Bridge: delegateTaskBridge{},
		})
		if err != nil {
			return nil, fmt.Errorf("build skill lifecycle tools: %w", err)
		}
	}

	specs, err := buildCatalogSpecs(ctx, f.cfg, "local", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, append([]einotool.BaseTool(nil), localCatalog.Tools...))
	if err != nil {
		return nil, err
	}
	checkpointSpecs, err := buildCatalogSpecs(ctx, f.cfg, "workingstate", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, checkpointTools)
	if err != nil {
		return nil, err
	}
	memorySpecs, err := buildCatalogSpecs(ctx, f.cfg, "memory", tooling.ToolKindMemory, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, memoryTools)
	if err != nil {
		return nil, err
	}
	skillSpecs, err := buildCatalogSpecs(ctx, f.cfg, "skill", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun, tooling.ToolProfileServe}, skillTools)
	if err != nil {
		return nil, err
	}
	skillLifecycleSpecs, err := buildCatalogSpecs(ctx, f.cfg, "skill.lifecycle", tooling.ToolKindSkill, []tooling.ToolProfile{tooling.ToolProfileRun}, skillLifecycleTools)
	if err != nil {
		return nil, err
	}
	specs = append(specs, checkpointSpecs...)
	specs = append(specs, memorySpecs...)
	specs = append(specs, skillSpecs...)
	specs = append(specs, skillLifecycleSpecs...)

	if includePlanning {
		loadToolsTool, err := newLoadToolsTool(f.contextPlane)
		if err != nil {
			return nil, fmt.Errorf("build load_tools tool: %w", err)
		}
		planningSpecs, err := buildCatalogSpecs(ctx, f.cfg, "runtime", tooling.ToolKindNative, []tooling.ToolProfile{tooling.ToolProfileRun}, []einotool.BaseTool{loadToolsTool})
		if err != nil {
			return nil, err
		}
		specs = append(specs, planningSpecs...)
	}
	catalog, err := tooling.NewCatalog(ctx, specs)
	if err != nil {
		return nil, fmt.Errorf("build toolset catalog: %w", err)
	}
	return &Toolset{catalog: catalog, profile: profile}, nil
}
