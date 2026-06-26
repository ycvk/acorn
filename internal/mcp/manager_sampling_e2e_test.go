package mcp

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/store"
)

// samplingExecutorStub is a minimal SamplingExecutor that returns a canned response.
type samplingExecutorStub struct {
	output string
}

func (s samplingExecutorStub) ExecuteMessages(_ context.Context, _ []*schema.Message) (string, error) {
	return s.output, nil
}

// TestNewManagerBindsSamplingHandlerOnInitialConnect verifies that providers
// connected during NewManager carry a CreateMessageHandler, so sampling is not
// inert on the common startup path. Before the fix, NewManager passed nil
// ClientOptions, so no handlers were registered on the initial session.
//
// The fixture server's "sample" tool calls session.CreateMessage internally,
// which triggers HandleCreateMessage → samplingExecutorStub. If the handler
// were not bound, the tool would return a sampling error.
func TestNewManagerBindsSamplingHandlerOnInitialConnect(t *testing.T) {
	binary := buildFixtureServer(t)

	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	mgr, err := NewManager(context.Background(), []ProviderConfig{{
		Name:                  "fixture",
		Enabled:               true,
		Transport:             "stdio",
		Command:               binary,
		StartupTimeoutSeconds: 10,
	}}, WithStore(db), WithTokenStore(db))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	mgr.SetSamplingExecutor(samplingExecutorStub{output: "hello from LLM"})

	tools := mgr.Tools()
	sampleTool := findToolByName(t, tools, "sample")
	invokable, ok := sampleTool.(einotool.InvokableTool)
	if !ok {
		t.Fatalf("sample tool is not invokable: %T", sampleTool)
	}

	result, err := invokable.InvokableRun(context.Background(), `{"prompt":"hello"}`)
	if err != nil {
		t.Fatalf("invoke sample tool: %v", err)
	}
	if !strings.Contains(result, "sampled: hello from LLM") {
		t.Fatalf("expected sampling response in tool result, got: %s", result)
	}
}
