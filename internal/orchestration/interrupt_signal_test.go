package orchestration

import (
	"testing"

	"github.com/cloudwego/eino/adk"
)

func TestInterruptInfoFromSignalPreservesRootContextWithoutSubs(t *testing.T) {
	signal := &adk.InterruptSignal{ID: "root", Address: adk.Address{}}
	signal.Info = map[string]any{"kind": "run_command_pause"}
	signal.IsRootCause = false

	info := InterruptInfoFromSignal(signal)
	if info == nil {
		t.Fatal("InterruptInfoFromSignal returned nil")
	}
	if got, want := len(info.InterruptContexts), 1; got != want {
		t.Fatalf("len(info.InterruptContexts) = %d, want %d", got, want)
	}
	if got := info.InterruptContexts[0]; got.ID != "root" || got.IsRootCause != false {
		t.Fatalf("root interrupt context = %+v", got)
	}
}

func TestInterruptInfoFromSignalRecursivelyFlattensSubs(t *testing.T) {
	signal := &adk.InterruptSignal{ID: "wrapper", Address: adk.Address{}}
	signal.Info = map[string]any{"kind": "wrapper"}
	ctx1 := &adk.InterruptSignal{ID: "ctx_1", Address: adk.Address{}}
	ctx1.Info = map[string]any{"kind": "run_command_pause"}
	ctx1.IsRootCause = true
	ctx2 := &adk.InterruptSignal{ID: "ctx_2", Address: adk.Address{}}
	ctx2.Info = map[string]any{"kind": "run_command_pause"}
	ctx2.IsRootCause = false
	ctx21 := &adk.InterruptSignal{ID: "ctx_2_1", Address: adk.Address{}}
	ctx21.Info = map[string]any{"kind": "nested_child"}
	ctx21.IsRootCause = false
	ctx2.Subs = []*adk.InterruptSignal{ctx21}
	signal.Subs = []*adk.InterruptSignal{ctx1, ctx2}

	info := InterruptInfoFromSignal(signal)
	if info == nil {
		t.Fatal("InterruptInfoFromSignal returned nil")
	}
	if got, want := len(info.InterruptContexts), 3; got != want {
		t.Fatalf("len(info.InterruptContexts) = %d, want %d", got, want)
	}
	if got := info.InterruptContexts[0]; got.ID != "ctx_1" || got.IsRootCause != true {
		t.Fatalf("ctx_1 = %+v", got)
	}
	if got := info.InterruptContexts[1]; got.ID != "ctx_2" || got.IsRootCause != false {
		t.Fatalf("ctx_2 = %+v", got)
	}
	if got := info.InterruptContexts[2]; got.ID != "ctx_2_1" || got.IsRootCause != false {
		t.Fatalf("ctx_2_1 = %+v", got)
	}
}
