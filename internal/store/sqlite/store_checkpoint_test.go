package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/compose"
)

func TestCompressionDoesNotRewriteCheckpointPayload(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	const runID = "run_compression"
	if err := store.CreateRun(ctx, runID, "summarize this run", runID); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	original := []byte("original-checkpoint-payload")
	if err := store.Set(ctx, runID, original); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := store.AppendEventContext(context.Background(), runID, "context.compressed", map[string]any{
		"context_compressed": map[string]any{
			"first_index":     1,
			"last_index":      4,
			"tokens_before":   1200,
			"tokens_after":    420,
			"summary_snippet": "Older context summary",
		},
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	got, ok, err := store.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("expected checkpoint payload to remain present")
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("checkpoint payload = %q, want %q", string(got), string(original))
	}
}

func TestResumeUsesOriginalCheckpointPayload(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var (
		mu            sync.Mutex
		receivedInput string
		callCount     int
	)

	graph := compose.NewGraph[string, string]()
	if err := graph.AddLambdaNode("1", compose.InvokableLambda(func(ctx context.Context, input string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		callCount++
		receivedInput = input
		if callCount == 1 {
			time.Sleep(200 * time.Millisecond)
		}
		return input + "_processed", nil
	})); err != nil {
		t.Fatalf("AddLambdaNode: %v", err)
	}
	if err := graph.AddEdge(compose.START, "1"); err != nil {
		t.Fatalf("AddEdge START->1: %v", err)
	}
	if err := graph.AddEdge("1", compose.END); err != nil {
		t.Fatalf("AddEdge 1->END: %v", err)
	}

	ctx := context.Background()
	runner, err := graph.Compile(
		ctx,
		compose.WithNodeTriggerMode(compose.AllPredecessor),
		compose.WithCheckPointStore(store),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	interruptedCtx, cancel := compose.WithGraphInterrupt(ctx)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel(compose.WithGraphInterruptTimeout(0))
	}()

	const checkpointID = "run_resume_original"
	_, err = runner.Invoke(interruptedCtx, "original_input", compose.WithCheckPointID(checkpointID))
	if err == nil {
		t.Fatal("expected interrupted invoke to fail")
	}
	info, ok := compose.ExtractInterruptInfo(err)
	if !ok {
		t.Fatalf("expected interrupt info, got %v", err)
	}
	if got, want := info.RerunNodes, []string{"1"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("RerunNodes = %#v, want %#v", got, want)
	}

	if _, err := store.AppendEventContext(context.Background(), checkpointID, "context.compressed", map[string]any{
		"context_compressed": map[string]any{
			"first_index":     0,
			"last_index":      0,
			"tokens_before":   10,
			"tokens_after":    4,
			"summary_snippet": "resume should still use original checkpoint payload",
		},
	}); err != nil {
		t.Fatalf("AppendEvent(context.compressed): %v", err)
	}

	result, err := runner.Invoke(ctx, "", compose.WithCheckPointID(checkpointID))
	if err != nil {
		t.Fatalf("resume invoke: %v", err)
	}
	if got, want := result, "original_input_processed"; got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}

	mu.Lock()
	defer mu.Unlock()
	if got, want := receivedInput, "original_input"; got != want {
		t.Fatalf("received input = %q, want %q", got, want)
	}
	if got, want := callCount, 2; got != want {
		t.Fatalf("callCount = %d, want %d", got, want)
	}
}
