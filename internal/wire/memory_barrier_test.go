package wire

import (
	"context"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/store"
)

func TestMemoryBarrierWaitsForForeignRunTermination(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.CreateRun(ctx, core.RunCreateParams{RunID: "foreign-run", Input: "test"}); err != nil {
		t.Fatal(err)
	}
	barrier := memoryForgetBarrier(runtime.NewRunController(), db)
	done := make(chan error, 1)
	go func() {
		done <- barrier(ctx, core.MemoryExclusion{Epoch: 1, RunIDs: []string{"foreign-run"}}, "caller")
	}()
	select {
	case err := <-done:
		t.Fatalf("barrier returned while foreign execution still running: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := db.FinishRun(ctx, "foreign-run", core.RunStatusFailed, "", "cancelled by memory exclusion"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
