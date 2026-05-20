package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/contextplane"
)

type stubDeferredPlane struct {
	contextplane.ContextPlane
	lastReq contextplane.DeferredLoadRequest
	result  *contextplane.DeferredLoadResult
	err     error
}

func (s *stubDeferredPlane) DeferredLoad(_ context.Context, req contextplane.DeferredLoadRequest) (*contextplane.DeferredLoadResult, error) {
	s.lastReq = req
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestLoadToolsToolCallsDeferredLoad(t *testing.T) {
	plane := &stubDeferredPlane{
		result: &contextplane.DeferredLoadResult{
			Messages: []*schema.Message{
				schema.UserMessage("<deferred-tool-definitions>\n- memory_search: Search memory records\n</deferred-tool-definitions>"),
			},
			LoadedToolNames: []string{"memory_search"},
			AlreadyLoaded:   []string{"read_file"},
		},
	}
	baseTool, err := newLoadToolsTool(plane)
	if err != nil {
		t.Fatalf("newLoadToolsTool: %v", err)
	}
	invokable, ok := baseTool.(tool.InvokableTool)
	if !ok {
		t.Fatal("load_tools tool is not invokable")
	}

	ctx := withRunID(withSessionID(context.Background(), "sess_load_tools"), "run_load_tools")
	result, err := invokable.InvokableRun(ctx, `{"query":"knowledge","tool_names":["memory_search"],"limit":2}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if plane.lastReq.RunID != "run_load_tools" || plane.lastReq.SessionID != "sess_load_tools" {
		t.Fatalf("deferred load context mismatch: %+v", plane.lastReq)
	}
	if !strings.Contains(result, "memory_search") || !strings.Contains(result, "read_file") {
		t.Fatalf("unexpected load_tools output: %s", result)
	}
	if !strings.Contains(result, "\"messages\"") {
		t.Fatalf("load_tools output should include deferred definition messages: %s", result)
	}
}
