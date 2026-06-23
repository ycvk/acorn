package runtime

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/domain"
)

func TestResolveRunID(t *testing.T) {
	tests := []struct {
		name string
		req  domain.ExecuteRequest
		want string
	}{
		{
			name: "uses provided run id",
			req:  domain.ExecuteRequest{RunID: "run_123"},
			want: "run_123",
		},
		{
			name: "uses provided run id with whitespace",
			req:  domain.ExecuteRequest{RunID: "  run_trim  "},
			want: "run_trim",
		},
		{
			name: "generates new run id when empty",
			req:  domain.ExecuteRequest{},
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
	item := domain.StreamItem{
		Kind: domain.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &StreamAssistantDelta{
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
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &StreamAssistantDelta{
				Delta: "Hello",
			},
		},
	})
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &StreamAssistantDelta{
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
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantMessage,
		Payload: map[string]any{
			"message": &StreamMessage{
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
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantMessage,
		Payload: map[string]any{
			"message": &StreamMessage{
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
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindRunInterrupted,
		Payload: map[string]any{
			"interrupt": &StreamInterrupt{
				ContextCount: 1,
				Contexts: []StreamInterruptContext{
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
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindRunFailed,
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
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindRunFailed,
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
	state.applyStreamItem(domain.StreamItem{
		Kind:    domain.StreamKindRunFailed,
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
	state.applyStreamItem(domain.StreamItem{
		Kind:    domain.StreamKindToolCallStarted,
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

	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &StreamAssistantDelta{Delta: "Hello"},
		},
	})
	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantDelta,
		Payload: map[string]any{
			"assistant_delta": &StreamAssistantDelta{Delta: " world"},
		},
	})
	if state.lastOutput != "Hello world" {
		t.Fatalf("after deltas: lastOutput = %q, want 'Hello world'", state.lastOutput)
	}

	state.applyStreamItem(domain.StreamItem{
		Kind: domain.StreamKindAssistantMessage,
		Payload: map[string]any{
			"message": &StreamMessage{Content: "Hello world!"},
		},
	})
	if state.lastOutput != "Hello world!" {
		t.Fatalf("after message: lastOutput = %q, want 'Hello world!'", state.lastOutput)
	}
}

func TestCompactArchiveTextShort(t *testing.T) {
	result := compactArchiveText("short text")
	if result != "short text" {
		t.Errorf("compactArchiveText = %q, want 'short text'", result)
	}
}

func TestCompactArchiveTextTrimsWhitespace(t *testing.T) {
	result := compactArchiveText("  hello  ")
	if result != "hello" {
		t.Errorf("compactArchiveText = %q, want 'hello'", result)
	}
}

func TestCompactArchiveTextTruncatesLong(t *testing.T) {
	long := strings.Repeat("x", 300)
	result := compactArchiveText(long)
	if len(result) != 283 {
		t.Errorf("compactArchiveText len = %d, want 283 (280 + ...)", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("compactArchiveText should end with '...', got %q", result)
	}
}

func TestCompactArchiveTextBoundary280(t *testing.T) {
	exact := strings.Repeat("y", 280)
	result := compactArchiveText(exact)
	if result != exact {
		t.Errorf("compactArchiveText at boundary 280 should not truncate, got len=%d", len(result))
	}
}

func TestCompactArchiveTextBoundary281(t *testing.T) {
	over := strings.Repeat("z", 281)
	result := compactArchiveText(over)
	if !strings.HasSuffix(result, "...") {
		t.Errorf("compactArchiveText at 281 should truncate with '...', got %q", result[:10])
	}
}

func TestCompactArchiveTextEmpty(t *testing.T) {
	result := compactArchiveText("")
	if result != "" {
		t.Errorf("compactArchiveText('') = %q, want ''", result)
	}
}

func TestCompactArchiveTextWhitespaceOnly(t *testing.T) {
	result := compactArchiveText("   ")
	if result != "" {
		t.Errorf("compactArchiveText('   ') = %q, want ''", result)
	}
}

func TestFailureReasonForStatusFailedWithOutput(t *testing.T) {
	reason := failureReasonForStatus(domain.RunStatusFailed, "some output")
	if reason != "run_failed:with_output" {
		t.Errorf("failureReasonForStatus(failed, 'some output') = %q, want 'run_failed:with_output'", reason)
	}
}

func TestFailureReasonForStatusFailedEmptyOutput(t *testing.T) {
	reason := failureReasonForStatus(domain.RunStatusFailed, "")
	if reason != "run_failed" {
		t.Errorf("failureReasonForStatus(failed, '') = %q, want 'run_failed'", reason)
	}
}

func TestFailureReasonForStatusFailedWhitespaceOutput(t *testing.T) {
	reason := failureReasonForStatus(domain.RunStatusFailed, "   ")
	if reason != "run_failed" {
		t.Errorf("failureReasonForStatus(failed, '   ') = %q, want 'run_failed'", reason)
	}
}

func TestFailureReasonForStatusNonFailedReturnsEmpty(t *testing.T) {
	for _, status := range []domain.RunStatus{
		domain.RunStatusSucceeded,
		domain.RunStatusInterrupted,
		domain.RunStatusRunning,
	} {
		reason := failureReasonForStatus(status, "some output")
		if reason != "" {
			t.Errorf("failureReasonForStatus(%q, ...) = %q, want ''", status, reason)
		}
	}
}
