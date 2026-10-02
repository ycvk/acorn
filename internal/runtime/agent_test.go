package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
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

func TestBuildAgentInstructionKeepsContextInSystemPrompt(t *testing.T) {
	got := buildAgentInstruction("You are Acorn.", []*schema.Message{
		schema.UserMessage("<memory-context>\nlikes tea\n</memory-context>"),
	})
	for _, want := range []string{"You are Acorn.", operatingRules, "<memory-context>\nlikes tea\n</memory-context>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("instruction missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "You are Acorn.") > strings.Index(got, "<memory-context>") {
		t.Fatal("base prompt must come before the per-run context")
	}
}

func TestBuildAgentHandlersOrder(t *testing.T) {
	deps := handlerTestDeps(config.DefaultConfig())
	req := agentRunnerRequest{RunID: "run_1", ChatModel: &scriptedModel{}, Instruction: "You are Acorn."}

	withoutDeferred, err := buildAgentHandlers(context.Background(), deps, req, nil)
	if err != nil {
		t.Fatalf("handlers: %v", err)
	}
	if len(withoutDeferred) != 6 {
		t.Fatalf("handlers without deferred tools = %d, want patch, summarize, reduce, presence, approval, tool errors", len(withoutDeferred))
	}
	if _, ok := withoutDeferred[3].(*presenceMiddleware); !ok {
		t.Fatalf("presence must follow summarization and reduction, got %T at index 3", withoutDeferred[3])
	}
	if _, ok := withoutDeferred[4].(*approvalMiddleware); !ok {
		t.Fatalf("approval must wrap the tool error handler, got %T at index 4", withoutDeferred[4])
	}
	if _, ok := withoutDeferred[5].(*toolErrorMiddleware); !ok {
		t.Fatalf("tool error handler must be innermost, got %T at index 5", withoutDeferred[5])
	}

	withDeferred, err := buildAgentHandlers(context.Background(), deps, req, []einotool.BaseTool{namedTool{"web_fetch"}})
	if err != nil {
		t.Fatalf("handlers: %v", err)
	}
	if len(withDeferred) != 7 {
		t.Fatalf("handlers with deferred tools = %d, want tool search added", len(withDeferred))
	}
}

func TestBuildAgentHandlersRejectsBadApprovalPattern(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Approval.Require = []string{"["}
	_, err := buildAgentHandlers(context.Background(), handlerTestDeps(cfg), agentRunnerRequest{ChatModel: &scriptedModel{}}, nil)
	if err == nil {
		t.Fatal("expected malformed approval pattern to fail handler assembly")
	}
}

func handlerTestDeps(cfg *config.Config) RuntimeDeps {
	return RuntimeDeps{
		Config:   cfg,
		Store:    approvalHandlersStore{},
		Presence: newMemPresenceStore(),
		Clock:    time.Now,
		Location: time.UTC,
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

var _ adk.ChatModelAgentMiddleware = (*approvalMiddleware)(nil)
