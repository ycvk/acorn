package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/review"
)

type fakeReviewer struct {
	mu    sync.Mutex
	calls []review.Call
	err   error
}

// Review asks for approval of anything that submits an order.
func (r *fakeReviewer) Review(_ context.Context, call review.Call) (review.Verdict, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
	if r.err != nil {
		return review.Verdict{}, r.err
	}
	if call.Browser != nil && strings.Contains(call.Browser.Target, "提交订单") || strings.Contains(call.Command, "POST") {
		return review.Verdict{NeedsApproval: true, Reasons: []string{"涉及付款"}, Scores: map[string]float64{"spends_money": 0.9}, Model: "jev-test"}, nil
	}
	return review.Verdict{Model: "jev-test"}, nil
}

const checkoutObservation = `url=https://trade.jd.com/shopping/order/getOrderInfo.action
L1 page
  RootWebArea "订单结算页"
    @e2 textbox "收货人"
    @e3 button "提交订单"
`

// fakeShell answers like the shell tool: observe prints the checkout page.
func fakeShell(ran *[]string) adk.InvokableToolCallEndpoint {
	return func(_ context.Context, args string, _ ...einotool.Option) (string, error) {
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(args), &input); err != nil {
			return "", err
		}
		*ran = append(*ran, input.Command)
		stdout := ""
		if strings.Contains(input.Command, "observe") {
			stdout = checkoutObservation
		}
		out, err := json.Marshal(map[string]any{"exit_code": 0, "stdout": stdout})
		return string(out), err
	}
}

func shellArgs(command string) string {
	out, _ := json.Marshal(map[string]string{"command": command})
	return string(out)
}

type gateFixture struct {
	mw       *approvalMiddleware
	store    *approvalTestStore
	reviewer *fakeReviewer
	ran      []string
}

func newGateFixture(t *testing.T, rules core.ApprovalRules) *gateFixture {
	t.Helper()
	f := &gateFixture{store: &approvalTestStore{input: "帮我把购物车里的东西买了"}, reviewer: &fakeReviewer{}}
	mw, err := newApprovalMiddleware(rules, f.reviewer, f.store)
	if err != nil {
		t.Fatal(err)
	}
	f.mw = mw
	return f
}

func (f *gateFixture) call(t *testing.T, tool, args string) (string, error) {
	t.Helper()
	endpoint, err := f.mw.WrapInvokableToolCall(context.Background(), fakeShell(&f.ran), &adk.ToolContext{Name: tool, CallID: "call_1"})
	if err != nil {
		t.Fatal(err)
	}
	return endpoint(core.WithRunID(context.Background(), "run_gate"), args)
}

func TestGateRunsReadOnlyShellWithoutReview(t *testing.T) {
	f := newGateFixture(t, core.ApprovalRules{})
	if _, err := f.call(t, "shell", shellArgs("bsk observe --session s1 | head -n 80")); err != nil {
		t.Fatal(err)
	}
	if len(f.reviewer.calls) != 0 || len(f.ran) != 1 || len(f.store.events) != 0 {
		t.Fatalf("reviews=%d ran=%v events=%v", len(f.reviewer.calls), f.ran, f.store.events)
	}
}

func TestGateReviewsClickWithObservedTargetAndAsksOwner(t *testing.T) {
	f := newGateFixture(t, core.ApprovalRules{})
	if _, err := f.call(t, "shell", shellArgs("bsk observe --session s1")); err != nil {
		t.Fatal(err)
	}
	_, err := f.call(t, "shell", shellArgs("bsk click @e3 --session s1"))
	if err == nil {
		t.Fatal("expected the approval interrupt")
	}
	if len(f.ran) != 1 {
		t.Fatalf("click ran before approval: %v", f.ran)
	}
	got := f.reviewer.calls[0]
	want := review.BrowserAction{Action: "click", Target: `button "提交订单"`, URL: "https://trade.jd.com/shopping/order/getOrderInfo.action", Title: "订单结算页"}
	if got.OwnerRequest != "帮我把购物车里的东西买了" || got.Browser == nil || *got.Browser != want {
		t.Fatalf("review call = %+v %+v", got, got.Browser)
	}
	if len(f.store.actions) != 1 || f.store.actions[0].Reason != "review" {
		t.Fatalf("actions = %+v", f.store.actions)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(f.store.actions[0].PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["summary"] != "在浏览器里点击「提交订单」 · 订单结算页 —— 涉及付款" {
		t.Fatalf("summary = %v", payload["summary"])
	}
	if strings.Join(f.store.events, ",") != "tool_review,tool_approval.pending" {
		t.Fatalf("events = %v", f.store.events)
	}
	recorded := f.store.payloads[0].(map[string]any)
	if recorded["needs_approval"] != true || recorded["model"] != "jev-test" {
		t.Fatalf("tool_review = %v", recorded)
	}
}

func TestGateRunsCallsReviewAllows(t *testing.T) {
	f := newGateFixture(t, core.ApprovalRules{})
	if _, err := f.call(t, "shell", shellArgs("bsk observe --session s1")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(t, "shell", shellArgs(`bsk fill @e2 --value "张三" --session s1`)); err != nil {
		t.Fatal(err)
	}
	if len(f.ran) != 2 || len(f.store.actions) != 0 {
		t.Fatalf("ran=%v actions=%v", f.ran, f.store.actions)
	}
	if got := f.reviewer.calls[0].Browser; got.Action != "fill" || got.Value != "张三" || got.Target != `textbox "收货人"` {
		t.Fatalf("review call = %+v", got)
	}
}

func TestGateAsksOwnerWhenReviewFails(t *testing.T) {
	f := newGateFixture(t, core.ApprovalRules{})
	f.reviewer.err = errors.New("review: HTTP 529: overloaded")
	if _, err := f.call(t, "shell", shellArgs("curl -s https://example.com | python3 -c 'print(1)'")); err == nil {
		t.Fatal("expected the approval interrupt")
	}
	if len(f.ran) != 0 || len(f.store.actions) != 1 || !strings.Contains(f.store.actions[0].PayloadJSON, "审查服务不可用") {
		t.Fatalf("ran=%v actions=%+v", f.ran, f.store.actions)
	}
	if recorded := f.store.payloads[0].(map[string]any); recorded["error"] != "review: HTTP 529: overloaded" {
		t.Fatalf("tool_review = %v", recorded)
	}
}

func TestGateRulesComeBeforeReview(t *testing.T) {
	f := newGateFixture(t, core.ApprovalRules{Block: []string{"mcp__bank__*"}, Require: []string{"mcp__mail__*"}, Allow: []string{"mcp__calendar__*", "mcp__mail__*"}})
	out, err := f.call(t, "mcp__bank__transfer", `{"amount":100}`)
	if err != nil || !strings.Contains(out, "approval.block") || len(f.ran) != 0 {
		t.Fatalf("block: out=%q err=%v ran=%v", out, err, f.ran)
	}
	if _, err := f.call(t, "mcp__mail__send", `{}`); err == nil || len(f.store.actions) != 1 || f.store.actions[0].Reason != "approval.require" {
		t.Fatalf("require: err=%v actions=%+v", err, f.store.actions)
	}
	if _, err := f.call(t, "mcp__calendar__list", `{"command":"x"}`); err != nil || len(f.reviewer.calls) != 0 {
		t.Fatalf("allow: err=%v reviews=%v", err, f.reviewer.calls)
	}
	if _, err := f.call(t, "mcp__github__list_issues", `{"command":"x"}`); err != nil || len(f.reviewer.calls) != 1 || f.reviewer.calls[0].Arguments != `{"command":"x"}` {
		t.Fatalf("review: err=%v reviews=%+v", err, f.reviewer.calls)
	}
	if _, err := f.call(t, "knowledge_write", `{"command":"x"}`); err != nil || len(f.reviewer.calls) != 1 {
		t.Fatalf("native tools are not reviewed: err=%v reviews=%d", err, len(f.reviewer.calls))
	}
}
