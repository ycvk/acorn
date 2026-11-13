package wire

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

// The agent writes its plan with the first step and again as the step ends,
// then replies without another plan call: the run closes the step still
// running and the pending step marked as the reply's as it succeeds. The
// run's live events carry each plan without the calls that wrote it or the
// reply mark, and the reply keeps the closed plan and no plan steps.
func TestThePlanOfARunReachesTheApp(t *testing.T) {
	first := []map[string]any{{"label": "读 A", "status": "running"}, {"label": "读 B", "status": "pending"}, {"label": "写汇总", "status": "pending", "reply": true}}
	second := []map[string]any{{"label": "读 A", "status": "done", "note": "1/1"}, {"label": "读 B", "status": "running"}, {"label": "写汇总", "status": "pending", "reply": true}}
	provider := &routedOpenAI{}
	provider.respond = func(request map[string]any) string {
		switch calls := modelCalls(request); len(calls) {
		case 0:
			return toolCallChunk("plan_1", "plan", map[string]any{"steps": first})
		case 1:
			return toolCallChunk("plan_2", "plan", map[string]any{"steps": second})
		default:
			return textReply("A 和 B 讲了三件事。")
		}
	}
	server := httptest.NewServer(provider)
	defer server.Close()
	c, err := NewContainer(context.Background(), writeTestConfig(t, server.URL, ""))
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()
	ctx := context.Background()
	client := newPairedClient(t, c)
	thread, err := c.threads.CreateThread(ctx, "读文章")
	if err != nil {
		t.Fatal(err)
	}
	var started api.CreateRunResponse
	client.postJSON(t, "/v1/threads/"+thread.ID+"/runs", map[string]any{"input": "读一下 A 和 B 再汇总"}, http.StatusCreated, &started)
	waitRunStatusWithin(t, c, started.Run.ID, "completed", 30*time.Second)

	for _, request := range provider.recorded() {
		if tools := requestTools(request); !slices.Contains(tools, "plan") {
			t.Fatalf("the run's tools = %v", tools)
		}
	}
	detail, err := c.events.LoadRunEventsForDetail(ctx, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var plans []string
	for _, event := range detail.Events {
		switch data := event.Data.(type) {
		case api.ToolCallData:
			if data.ToolName == core.PlanToolName {
				t.Fatalf("live event %s carries a plan call", event.Type)
			}
		case api.PlanData:
			plans = append(plans, fmt.Sprint(data.Steps))
		}
	}
	if want := []string{"[{读 A running } {读 B pending } {写汇总 pending }]", "[{读 A done 1/1} {读 B running } {写汇总 pending }]", "[{读 A done 1/1} {读 B done } {写汇总 done }]"}; !slices.Equal(plans, want) {
		t.Fatalf("live plans = %q, want %q", plans, want)
	}

	var listed api.MessageListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/"+thread.ID+"/messages", nil), http.StatusOK, &listed)
	if got := messageLines(listed); !slices.Equal(got, []string{"user:读一下 A 和 B 再汇总", "assistant:A 和 B 讲了三件事。"}) {
		t.Fatalf("messages = %v", got)
	}
	reply := listed.Items[1]
	wantPlan := []api.PlanStepData{{Label: "读 A", Status: "done", Note: "1/1"}, {Label: "读 B", Status: "done"}, {Label: "写汇总", Status: "done"}}
	if !slices.Equal(reply.Plan, wantPlan) || len(reply.Steps) != 0 {
		t.Fatalf("reply plan = %+v, steps = %+v", reply.Plan, reply.Steps)
	}
}
