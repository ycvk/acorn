package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/core"
)

// describedCalls describes a call the owner decides on by its tool and
// arguments.
type describedCalls struct{}

func (describedCalls) DescribeApproval(_ context.Context, toolName, arguments string) (string, error) {
	return "owner decides " + toolName + " " + arguments, nil
}

type approvalTestStore struct {
	mu       sync.Mutex
	actions  []core.PendingActionInput
	events   []string
	payloads []any
	input    string
	woken    bool
	// subRuns are the sub-runs the run delegated.
	subRuns []core.SubRun
}

func (s *approvalTestStore) SubRuns(context.Context, string) ([]core.SubRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.subRuns), nil
}

func (s *approvalTestStore) LoadRun(_ context.Context, runID string) (*core.RunRecord, error) {
	return &core.RunRecord{RunID: runID, Input: s.input}, nil
}

func (s *approvalTestStore) RunWoken(context.Context, string) (bool, error) {
	return s.woken, nil
}

func (s *approvalTestStore) CreatePendingAction(_ context.Context, input core.PendingActionInput) (*core.PendingActionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.actions = append(s.actions, input)
	return &core.PendingActionRecord{ActionID: input.ActionID, RunID: input.RunID, Kind: input.Kind, Status: core.PendingActionStatusPending}, nil
}

func (s *approvalTestStore) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, kind)
	s.payloads = append(s.payloads, payload)
	return core.EventRecord{RunID: runID, Kind: kind, Payload: payload}, nil
}

type memoryCheckpointStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (s *memoryCheckpointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.data[id]
	return data, ok, nil
}

func (s *memoryCheckpointStore) Set(_ context.Context, id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = append([]byte(nil), data...)
	return nil
}

type echoTool struct {
	mu    sync.Mutex
	calls []string
}

func (t *echoTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "echo_tool", Desc: "echoes its arguments"}, nil
}

func (t *echoTool) InvokableRun(_ context.Context, args string, _ ...einotool.Option) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, args)
	return "echo:" + args, nil
}

func (t *echoTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func newApprovalTestRunner(t *testing.T, model *scriptedModel, tool *echoTool, store *approvalTestStore, checkpoints adk.CheckPointStore) *adk.TypedRunner[*schema.AgenticMessage] {
	t.Helper()
	ctx := context.Background()
	mw, err := newApprovalMiddleware(core.ApprovalRules{Require: []string{"echo_*"}}, nil, describedCalls{}, store, nil)
	if err != nil {
		t.Fatalf("approval middleware: %v", err)
	}
	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        "approval_test",
		Description: "approval test agent",
		Model:       model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               []einotool.BaseTool{tool},
			ExecuteSequentially: true,
		}},
		Handlers:      []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{mw},
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("new chat model agent: %v", err)
	}
	return adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true, CheckPointStore: checkpoints})
}

// drainEvents consumes every event, closing message streams, and returns the
// interrupt (if any) plus the first error event.
func drainEvents(t *testing.T, iter *adk.AsyncIterator[*adk.TypedAgentEvent[*schema.AgenticMessage]]) (*adk.InterruptInfo, error) {
	t.Helper()
	var interrupted *adk.InterruptInfo
	for {
		event, ok := iter.Next()
		if !ok {
			return interrupted, nil
		}
		if event.Err != nil {
			return interrupted, event.Err
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			if _, err := event.Output.MessageOutput.GetMessage(); err != nil {
				t.Fatalf("read event message: %v", err)
			}
		}
		if event.Action != nil && event.Action.Interrupted != nil {
			interrupted = event.Action.Interrupted
		}
	}
}

// interruptAcrossRunners runs the agent until the approval interrupt with one
// runner and returns the interrupt id so a second, fresh runner can resume.
func interruptAcrossRunners(t *testing.T, ctx context.Context, store *approvalTestStore, checkpoints adk.CheckPointStore, tool *echoTool) string {
	t.Helper()
	first := &scriptedModel{replies: []*schema.AgenticMessage{toolCallReply("call_1", "echo_tool", `{"v":1}`)}}
	first.replies[0].ContentBlocks = append([]*schema.ContentBlock{{Type: schema.ContentBlockTypeReasoning, Reasoning: &schema.Reasoning{Text: "prepare the tool", Signature: "signed-state"}}}, first.replies[0].ContentBlocks...)
	runner := newApprovalTestRunner(t, first, tool, store, checkpoints)
	interrupted, err := drainEvents(t, runner.Run(ctx, []adk.AgenticMessage{schema.UserAgenticMessage("go")}, adk.WithCheckPointID("run_approval")))
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if interrupted == nil || len(interrupted.InterruptContexts) == 0 {
		t.Fatal("expected the approval interrupt")
	}
	if len(store.actions) != 1 || store.actions[0].Kind != core.PendingActionKindToolApproval || store.actions[0].RunID != "run_approval" {
		t.Fatalf("pending actions = %+v", store.actions)
	}
	var rootID string
	for _, ic := range interrupted.InterruptContexts {
		if !ic.IsRootCause {
			continue
		}
		info, ok := ic.Info.(map[string]any)
		if !ok || info["kind"] != toolApprovalInterruptKind || info["action_id"] != store.actions[0].ActionID {
			t.Fatalf("unexpected interrupt info: %#v", ic.Info)
		}
		rootID = ic.ID
	}
	if rootID == "" {
		t.Fatal("no root-cause interrupt context")
	}
	if tool.callCount() != 0 {
		t.Fatalf("tool ran %d times before approval", tool.callCount())
	}
	if len(store.events) != 1 || store.events[0] != "tool_approval.pending" {
		t.Fatalf("events = %v", store.events)
	}
	return rootID
}

func TestApprovalAcceptRunsRecordedCallInFreshRunner(t *testing.T) {
	RegisterTypes()
	ctx := core.WithRunID(context.Background(), "run_approval")
	store := &approvalTestStore{}
	checkpoints := &memoryCheckpointStore{data: map[string][]byte{}}
	tool := &echoTool{}
	interruptID := interruptAcrossRunners(t, ctx, store, checkpoints, tool)

	second := &scriptedModel{replies: []*schema.AgenticMessage{assistantMessage("done", nil)}}
	runner := newApprovalTestRunner(t, second, tool, store, checkpoints)
	iter, err := runner.ResumeWithParams(ctx, "run_approval", &adk.ResumeParams{Targets: map[string]any{
		interruptID: map[string]any{"action": "accept", "action_id": store.actions[0].ActionID},
	}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if interrupted, err := drainEvents(t, iter); err != nil || interrupted != nil {
		t.Fatalf("resumed run: interrupted=%v err=%v", interrupted, err)
	}
	if tool.callCount() != 1 || tool.calls[0] != `{"v":1}` {
		t.Fatalf("tool calls = %v, want exactly the approved arguments", tool.calls)
	}
	assertLastToolMessage(t, second, "echo:{\"v\":1}")
	foundSignature := false
	for _, msg := range second.lastInput() {
		for _, block := range msg.ContentBlocks {
			if block.Reasoning != nil && block.Reasoning.Signature == "signed-state" {
				foundSignature = true
			}
		}
	}
	if !foundSignature {
		t.Fatal("reasoning signature missing after checkpoint resume")
	}
}

func TestApprovalDeclineSkipsCall(t *testing.T) {
	RegisterTypes()
	ctx := core.WithRunID(context.Background(), "run_approval")
	store := &approvalTestStore{}
	checkpoints := &memoryCheckpointStore{data: map[string][]byte{}}
	tool := &echoTool{}
	interruptID := interruptAcrossRunners(t, ctx, store, checkpoints, tool)

	second := &scriptedModel{replies: []*schema.AgenticMessage{assistantMessage("ok, skipped", nil)}}
	runner := newApprovalTestRunner(t, second, tool, store, checkpoints)
	iter, err := runner.ResumeWithParams(ctx, "run_approval", &adk.ResumeParams{Targets: map[string]any{
		interruptID: map[string]any{"action": "decline"},
	}})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err := drainEvents(t, iter); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if tool.callCount() != 0 {
		t.Fatalf("declined tool ran %d times", tool.callCount())
	}
	assertLastToolMessage(t, second, "The owner declined this echo_tool call")
}

func TestApprovalRejectsMalformedPatterns(t *testing.T) {
	for _, rules := range []core.ApprovalRules{{Require: []string{"["}}, {Block: []string{"["}}, {Allow: []string{"["}}} {
		if _, err := newApprovalMiddleware(rules, nil, describedCalls{}, &approvalTestStore{}, nil); err == nil {
			t.Fatalf("expected malformed pattern in %+v to be rejected", rules)
		}
	}
}

func assertLastToolMessage(t *testing.T, model *scriptedModel, want string) {
	t.Helper()
	for _, msg := range model.lastInput() {
		if msg.Role == schema.AgenticRoleTypeUser && toolResultID(msg) == "call_1" {
			if !strings.Contains(messageText(msg), want) {
				t.Fatalf("tool message = %q, want it to contain %q", messageText(msg), want)
			}
			return
		}
	}
	t.Fatalf("model input has no tool message for call_1: %+v", model.lastInput())
}

// newsSourceTool stands in for news_source.
type newsSourceTool struct{ echoTool }

func (t *newsSourceTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "news_source", Desc: "changes a news source"}, nil
}

func TestNewsSourceWaitsForTheOwnerUnlessAllowed(t *testing.T) {
	RegisterTypes()
	for _, tc := range []struct {
		name  string
		rules core.ApprovalRules
		gated bool
	}{
		{name: "no rule", rules: core.ApprovalRules{Require: []string{"skill_*"}}, gated: true},
		{name: "allowed", rules: core.ApprovalRules{Allow: []string{"news_*"}}, gated: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := core.WithRunID(context.Background(), "run_news")
			store := &approvalTestStore{}
			tool := &newsSourceTool{}
			mw, err := newApprovalMiddleware(tc.rules, nil, describedCalls{}, store, nil)
			if err != nil {
				t.Fatal(err)
			}
			model := &scriptedModel{replies: []*schema.AgenticMessage{
				toolCallReply("call_1", "news_source", `{"action":"promote","id":12,"reason":"一周 40 条，独家 5 条"}`),
				assistantMessage("done", nil),
			}}
			agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
				Name: "news_source_test", Description: "news source approval", Model: model,
				ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []einotool.BaseTool{tool}, ExecuteSequentially: true}},
				Handlers:    []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{mw}, MaxIterations: 5,
			})
			if err != nil {
				t.Fatal(err)
			}
			runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true, CheckPointStore: &memoryCheckpointStore{data: map[string][]byte{}}})
			interrupted, err := drainEvents(t, runner.Run(ctx, []adk.AgenticMessage{schema.UserAgenticMessage("go")}, adk.WithCheckPointID("run_news")))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.gated {
				if interrupted != nil || len(store.actions) != 0 || tool.callCount() != 1 {
					t.Fatalf("interrupted %v, actions %+v, calls %d", interrupted, store.actions, tool.callCount())
				}
				return
			}
			if interrupted == nil || len(store.actions) != 1 || store.actions[0].Reason != ownerDecidesReason || tool.callCount() != 0 {
				t.Fatalf("interrupted %v, actions %+v, calls %d", interrupted, store.actions, tool.callCount())
			}
			var payload struct{ Summary, Message string }
			if err := json.Unmarshal([]byte(store.actions[0].PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			want := `owner decides news_source {"action":"promote","id":12,"reason":"一周 40 条，独家 5 条"}`
			if payload.Summary != want || payload.Message != want {
				t.Fatalf("the approval the owner reads = %+v", payload)
			}
		})
	}
}

// skillTool stands in for a skill tool of that name.
type skillTool struct {
	echoTool
	name string
}

func (t *skillTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: "stands in for " + t.name}, nil
}

// failingDescriber cannot say what a call does.
type failingDescriber struct{}

func (failingDescriber) DescribeApproval(context.Context, string, string) (string, error) {
	return "", errors.New("the skills cannot be read")
}

// A skill write the owner's rule sends to them waits with what it would
// change in the stored skills; other tools under a rule wait as they are.
func TestRequiredSkillWritesWaitWithWhatTheyChange(t *testing.T) {
	RegisterTypes()
	run := func(t *testing.T, describe ApprovalDescriber, toolName, args string) (*approvalTestStore, *skillTool, *adk.InterruptInfo, error) {
		t.Helper()
		ctx := core.WithRunID(context.Background(), "run_skill")
		store := &approvalTestStore{}
		tool := &skillTool{name: toolName}
		mw, err := newApprovalMiddleware(core.ApprovalRules{Require: []string{"skill_*"}}, nil, describe, store, nil)
		if err != nil {
			t.Fatal(err)
		}
		model := &scriptedModel{replies: []*schema.AgenticMessage{toolCallReply("call_1", toolName, args), assistantMessage("done", nil)}}
		agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
			Name: "skill_write_test", Description: "skill write approval", Model: model,
			ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []einotool.BaseTool{tool}, ExecuteSequentially: true}},
			Handlers:    []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{mw}, MaxIterations: 5,
		})
		if err != nil {
			t.Fatal(err)
		}
		runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true, CheckPointStore: &memoryCheckpointStore{data: map[string][]byte{}}})
		interrupted, err := drainEvents(t, runner.Run(ctx, []adk.AgenticMessage{schema.UserAgenticMessage("go")}, adk.WithCheckPointID("run_skill")))
		return store, tool, interrupted, err
	}
	payloadOf := func(t *testing.T, store *approvalTestStore) map[string]any {
		t.Helper()
		var payload map[string]any
		if err := json.Unmarshal([]byte(store.actions[0].PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	for toolName, args := range map[string]string{
		"skill_write":  `{"markdown":"---\nname: skill-creator\n---\ntext","base_revision":6}`,
		"skill_import": `{"markdown":"---\nname: skill-creator\n---\ntext","base_revision":6}`,
		"skill_retire": `{"name":"shop-orders","base_revision":3}`,
	} {
		store, tool, interrupted, err := run(t, describedCalls{}, toolName, args)
		if err != nil || interrupted == nil || len(store.actions) != 1 || store.actions[0].Reason != requireReason || tool.callCount() != 0 {
			t.Fatalf("%s: interrupted %v, actions %+v, calls %d, %v", toolName, interrupted, store.actions, tool.callCount(), err)
		}
		payload, want := payloadOf(t, store), "owner decides "+toolName+" "+args
		if payload["summary"] != want || payload["message"] != want || payload["rule"] != "skill_*" || payload["arguments"] != args {
			t.Fatalf("%s: the approval the owner reads = %+v", toolName, payload)
		}
	}

	// A write nobody can describe neither runs nor reaches the owner.
	store, tool, interrupted, err := run(t, failingDescriber{}, "skill_write", `{"markdown":"text"}`)
	if interrupted != nil || len(store.actions) != 0 || tool.callCount() != 0 {
		t.Fatalf("undescribed write: interrupted %v, actions %+v, calls %d, %v", interrupted, store.actions, tool.callCount(), err)
	}
}

// newsSectionsTool stands in for news_sections.
type newsSectionsTool struct{ echoTool }

func (t *newsSectionsTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "news_sections", Desc: "changes the briefing's sections"}, nil
}

func TestNewsSectionsWaitsOnlyForChangesInRunsAcornWoke(t *testing.T) {
	RegisterTypes()
	set := `{"action":"set","name":"国际与财经","preference":"1. 大国关系\n2. 小国央行少报","reason":"两次少报"}`
	for _, tc := range []struct {
		name  string
		woken bool
		args  string
		rules core.ApprovalRules
		gated bool
	}{
		{name: "the owner's conversation", args: set, gated: false},
		{name: "night reflection", woken: true, args: set, gated: true},
		{name: "night reflection lists", woken: true, args: `{"action":"list"}`, gated: false},
		{name: "night reflection allowed", woken: true, args: set, rules: core.ApprovalRules{Allow: []string{"news_sections"}}, gated: false},
		{name: "required in the owner's conversation", args: set, rules: core.ApprovalRules{Require: []string{"news_*"}}, gated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := core.WithRunID(context.Background(), "run_sections")
			store := &approvalTestStore{woken: tc.woken}
			tool := &newsSectionsTool{}
			mw, err := newApprovalMiddleware(tc.rules, nil, describedCalls{}, store, nil)
			if err != nil {
				t.Fatal(err)
			}
			model := &scriptedModel{replies: []*schema.AgenticMessage{
				toolCallReply("call_1", "news_sections", tc.args),
				assistantMessage("done", nil),
			}}
			agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
				Name: "news_sections_test", Description: "news sections approval", Model: model,
				ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []einotool.BaseTool{tool}, ExecuteSequentially: true}},
				Handlers:    []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{mw}, MaxIterations: 5,
			})
			if err != nil {
				t.Fatal(err)
			}
			runner := adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{Agent: agent, EnableStreaming: true, CheckPointStore: &memoryCheckpointStore{data: map[string][]byte{}}})
			interrupted, err := drainEvents(t, runner.Run(ctx, []adk.AgenticMessage{schema.UserAgenticMessage("go")}, adk.WithCheckPointID("run_sections")))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.gated {
				if interrupted != nil || len(store.actions) != 0 || tool.callCount() != 1 {
					t.Fatalf("interrupted %v, actions %+v, calls %d", interrupted, store.actions, tool.callCount())
				}
				return
			}
			if interrupted == nil || len(store.actions) != 1 || tool.callCount() != 0 {
				t.Fatalf("interrupted %v, actions %+v, calls %d", interrupted, store.actions, tool.callCount())
			}
			want := sectionsReason
			if len(tc.rules.Require) > 0 {
				want = requireReason
			}
			if store.actions[0].Reason != want {
				t.Fatalf("reason = %q, want %q", store.actions[0].Reason, want)
			}
		})
	}
}
