package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
)

type namedTool struct{ name string }

func (t namedTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.name}, nil
}

func (t namedTool) InvokableRun(context.Context, string, ...einotool.Option) (string, error) {
	return t.name, nil
}

func TestSplitToolsByLoading(t *testing.T) {
	specs := []core.ToolSpec{
		{ToolContract: core.ToolContract{Name: "eager_tool", Loading: core.ToolLoadingPolicy{Mode: core.ToolLoadingModeEager}}},
		{ToolContract: core.ToolContract{Name: "web_fetch", Loading: core.ToolLoadingPolicy{Mode: core.ToolLoadingModeDeferred}}},
		{ToolContract: core.ToolContract{Name: "hidden_tool", Loading: core.ToolLoadingPolicy{Mode: core.ToolLoadingModeHidden}}},
	}
	built := []einotool.BaseTool{namedTool{"eager_tool"}, namedTool{"web_fetch"}, namedTool{"hidden_tool"}}
	eager, deferred, err := splitToolsByLoading(context.Background(), specs, built)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if names := toolNames(t, eager); len(names) != 1 || names[0] != "eager_tool" {
		t.Fatalf("eager = %v", names)
	}
	if names := toolNames(t, deferred); len(names) != 1 || names[0] != "web_fetch" {
		t.Fatalf("deferred = %v", names)
	}
}

func TestStableInstructionJoinsPersonaAndRules(t *testing.T) {
	got := buildStableInstruction("You are Acorn.")
	if !strings.HasPrefix(got, "You are Acorn.") || !strings.Contains(got, operatingRules) {
		t.Fatalf("instruction = %s", got)
	}
}

func TestBuildAgentHandlersOrder(t *testing.T) {
	deps := handlerTestDeps(config.DefaultConfig())
	req := agentRunnerRequest{RunID: "run_1", ChatModel: &scriptedModel{}, Instruction: "You are Acorn.", Memory: &runMemory{}}

	withoutDeferred, err := buildAgentHandlers(context.Background(), deps, req, nil)
	if err != nil {
		t.Fatalf("handlers: %v", err)
	}
	if len(withoutDeferred) != 8 {
		t.Fatalf("handlers without deferred tools = %d, want patch, summarize, reduce, skill, presence, approval, tool errors", len(withoutDeferred))
	}
	if _, ok := withoutDeferred[5].(*presenceMiddleware); !ok {
		t.Fatalf("presence must follow the skill handler, got %T at index 4", withoutDeferred[5])
	}
	if _, ok := withoutDeferred[6].(*approvalMiddleware); !ok {
		t.Fatalf("approval must wrap the tool error handler, got %T at index 5", withoutDeferred[5])
	}
	if _, ok := withoutDeferred[7].(*toolErrorMiddleware); !ok {
		t.Fatalf("tool error handler must be innermost, got %T at index 6", withoutDeferred[6])
	}

	withDeferred, err := buildAgentHandlers(context.Background(), deps, req, []einotool.BaseTool{namedTool{"web_fetch"}})
	if err != nil {
		t.Fatalf("handlers: %v", err)
	}
	if len(withDeferred) != 9 {
		t.Fatalf("handlers with deferred tools = %d, want tool search added", len(withDeferred))
	}
}

func TestBuildAgentHandlersRejectsBadApprovalPattern(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Approval.Require = []string{"["}
	_, err := buildAgentHandlers(context.Background(), handlerTestDeps(cfg), agentRunnerRequest{ChatModel: &scriptedModel{}, Memory: &runMemory{}}, nil)
	if err == nil {
		t.Fatal("expected malformed approval pattern to fail handler assembly")
	}
}

func handlerTestDeps(cfg *config.Config) RuntimeDeps {
	return RuntimeDeps{
		Config:             cfg,
		Memory:             handlerMemory{},
		Store:              approvalHandlersStore{},
		Activity:           newMemPresenceStore(),
		MemoryStore:        newMemPresenceStore(),
		Commitments:        newMemPresenceStore(),
		PhoneNotifications: newMemPresenceStore(),
		Clock:              time.Now,
		Location:           time.UTC,
	}
}

// approvalHandlersStore satisfies RuntimeStore for handler assembly; no
// method is called while building handlers.
type approvalHandlersStore struct {
	RuntimeStore
}

func toolNames(t *testing.T, items []einotool.BaseTool) []string {
	t.Helper()
	names := make([]string, 0, len(items))
	for _, item := range items {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		names = append(names, info.Name)
	}
	return names
}

var _ adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage] = (*approvalMiddleware)(nil)

type handlerMemory struct{ MemoryContextService }

func (handlerMemory) MeterCompaction(model einomodel.AgenticModel) einomodel.AgenticModel {
	return model
}
