package runtime

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestResolveRunID(t *testing.T) {
	tests := []struct {
		name string
		req  core.ExecuteRequest
		want string
	}{
		{
			name: "uses provided run id",
			req:  core.ExecuteRequest{RunID: "run_123"},
			want: "run_123",
		},
		{
			name: "uses provided run id with whitespace",
			req:  core.ExecuteRequest{RunID: "  run_trim  "},
			want: "run_trim",
		},
		{
			name: "generates new run id when empty",
			req:  core.ExecuteRequest{},
			want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveRunID(tc.req)
			if tc.want == "" {
				if got == "" {
					t.Error("expected non-empty generated run id, got empty")
				}
				if !strings.HasPrefix(got, "run_") {
					t.Errorf("generated run id %q should start with run_", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("resolveRunID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunStateApplyAssistantDelta(t *testing.T) {
	state := RunState{}
	item := core.StreamItem{
		Kind: core.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{
				Delta: "Hello",
			},
		},
	}
	state.applyStreamItem(item)
	if state.lastOutput != "Hello" {
		t.Errorf("lastOutput = %q, want Hello", state.lastOutput)
	}
}

func TestRunStateApplyAssistantDeltaAccumulates(t *testing.T) {
	state := RunState{}
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{
				Delta: "Hello",
			},
		},
	})
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{
				Delta: " world",
			},
		},
	})
	if state.lastOutput != "Hello world" {
		t.Errorf("lastOutput = %q, want 'Hello world'", state.lastOutput)
	}
}

func TestRunStateApplyMessageReplacesOutput(t *testing.T) {
	state := RunState{lastOutput: "streaming partial..."}
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantMessage,
		Payload: map[string]any{
			"message": &core.StreamMessage{
				Role:    "assistant",
				Content: "final answer",
			},
		},
	})
	if state.lastOutput != "final answer" {
		t.Errorf("lastOutput = %q, want 'final answer'", state.lastOutput)
	}
}

func TestRunStateApplyMessageEmptyContentPreservesOutput(t *testing.T) {
	state := RunState{lastOutput: "existing output"}
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantMessage,
		Payload: map[string]any{
			"message": &core.StreamMessage{
				Role:    "assistant",
				Content: "",
			},
		},
	})
	if state.lastOutput != "existing output" {
		t.Errorf("lastOutput = %q, want 'existing output' (empty content should not replace)", state.lastOutput)
	}
}

func TestRunStateApplyInterrupt(t *testing.T) {
	state := RunState{}
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindRunInterrupted,
		Payload: map[string]any{
			"interrupt": &core.StreamInterrupt{
				ContextCount: 1,
				Contexts: []core.StreamInterruptContext{
					{ID: "ictx_1", Address: "0x123", IsRootCause: true},
				},
			},
		},
	})
	if state.interrupt == nil {
		t.Fatal("interrupt should not be nil after applying interrupt item")
	}
	if state.interrupt["context_count"] != 1 {
		t.Errorf("interrupt context_count = %v, want 1", state.interrupt["context_count"])
	}
}

func TestRunStateApplyRunFailed(t *testing.T) {
	state := RunState{}
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindRunFailed,
		Payload: map[string]any{
			"error": "model stream error: connection reset",
		},
	})
	if state.failure == nil {
		t.Fatal("failure should not be nil after run.failed item")
	}
	if state.failure.Error() != "model stream error: connection reset" {
		t.Errorf("failure = %q, want 'model stream error: connection reset'", state.failure.Error())
	}
	if !state.emittedRunFailed {
		t.Error("emittedRunFailed should be true after run.failed item")
	}
}

func TestRunStateApplyRunFailedEmptyError(t *testing.T) {
	state := RunState{}
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindRunFailed,
		Payload: map[string]any{
			"error": "",
		},
	})
	if state.failure != nil {
		t.Errorf("failure should be nil for empty error, got %v", state.failure)
	}
	if state.emittedRunFailed {
		t.Error("emittedRunFailed should be false for empty error")
	}
}

func TestRunStateApplyRunFailedNoErrorField(t *testing.T) {
	state := RunState{}
	state.applyStreamItem(core.StreamItem{
		Kind:    core.StreamKindRunFailed,
		Payload: map[string]any{},
	})
	if state.failure != nil {
		t.Errorf("failure should be nil for missing error, got %v", state.failure)
	}
}

func TestRunStateApplyUnknownItemIsNoOp(t *testing.T) {
	state := RunState{
		lastOutput: "existing",
	}
	state.applyStreamItem(core.StreamItem{
		Kind:    core.StreamKindToolCallStarted,
		Payload: map[string]any{},
	})
	if state.lastOutput != "existing" {
		t.Errorf("lastOutput = %q, want 'existing' (unchanged)", state.lastOutput)
	}
	if state.interrupt != nil {
		t.Error("interrupt should be nil for unknown item")
	}
	if state.failure != nil {
		t.Error("failure should be nil for unknown item")
	}
}

func TestRunStateApplyMultipleItemsLifecycle(t *testing.T) {
	state := RunState{}

	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{Delta: "Hello"},
		},
	})
	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &core.StreamAssistantDelta{Delta: " world"},
		},
	})
	if state.lastOutput != "Hello world" {
		t.Fatalf("after deltas: lastOutput = %q, want 'Hello world'", state.lastOutput)
	}

	state.applyStreamItem(core.StreamItem{
		Kind: core.StreamKindAssistantMessage,
		Payload: map[string]any{
			"message": &core.StreamMessage{Content: "Hello world!"},
		},
	})
	if state.lastOutput != "Hello world!" {
		t.Fatalf("after message: lastOutput = %q, want 'Hello world!'", state.lastOutput)
	}
}

func TestFailureReasonForStatusFailedWithOutput(t *testing.T) {
	reason := failureReasonForStatus(core.RunStatusFailed, "some output")
	if reason != "run_failed:with_output" {
		t.Errorf("failureReasonForStatus(failed, 'some output') = %q, want 'run_failed:with_output'", reason)
	}
}

func TestFailureReasonForStatusFailedEmptyOutput(t *testing.T) {
	reason := failureReasonForStatus(core.RunStatusFailed, "")
	if reason != "run_failed" {
		t.Errorf("failureReasonForStatus(failed, '') = %q, want 'run_failed'", reason)
	}
}

func TestFailureReasonForStatusFailedWhitespaceOutput(t *testing.T) {
	reason := failureReasonForStatus(core.RunStatusFailed, "   ")
	if reason != "run_failed" {
		t.Errorf("failureReasonForStatus(failed, '   ') = %q, want 'run_failed'", reason)
	}
}

func TestFailureReasonForStatusNonFailedReturnsEmpty(t *testing.T) {
	for _, status := range []core.RunStatus{
		core.RunStatusSucceeded,
		core.RunStatusInterrupted,
		core.RunStatusRunning,
	} {
		reason := failureReasonForStatus(status, "some output")
		if reason != "" {
			t.Errorf("failureReasonForStatus(%q, ...) = %q, want ''", status, reason)
		}
	}
}
