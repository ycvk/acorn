package contextplane

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/store/storetest"
	"github.com/ycvk/acorn/internal/workingstate"
)

func TestRunContextAssemblerLoadsFrozenSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newRunContextAssemblerTestStore(t)
	checkpoints := workingstate.NewService(store, 4000)

	if _, err := checkpoints.Update(ctx, "session-freeze", "initial checkpoint", "skill.inspect"); err != nil {
		t.Fatalf("update checkpoint: %v", err)
	}

	assembler := runContextAssembler{
		store:             store,
		checkpointService: checkpoints,
	}
	created, err := assembler.Assemble(ctx, AssembleRequest{
		SessionID: "session-freeze",
		RunID:     "run_freeze",
		Input:     "inspect the workspace",
	})
	if err != nil {
		t.Fatalf("Assemble(create): %v", err)
	}
	if created.snapshot == nil {
		t.Fatal("created snapshot is nil")
	}
	if !strings.Contains(created.checkpointSection, "initial checkpoint") {
		t.Fatalf("checkpoint section = %q", created.checkpointSection)
	}

	if _, err := checkpoints.Update(ctx, "session-freeze", "mutated checkpoint", "skill.other"); err != nil {
		t.Fatalf("mutate checkpoint: %v", err)
	}

	loaded, err := assembler.Assemble(ctx, AssembleRequest{
		SessionID: "session-freeze",
		RunID:     "run_freeze",
	})
	if err != nil {
		t.Fatalf("Assemble(load): %v", err)
	}
	if !strings.Contains(loaded.checkpointSection, "initial checkpoint") {
		t.Fatalf("loaded checkpoint section = %q, want frozen initial checkpoint", loaded.checkpointSection)
	}
	if strings.Contains(loaded.checkpointSection, "mutated checkpoint") {
		t.Fatalf("loaded checkpoint section used live mutation: %q", loaded.checkpointSection)
	}
}

func newRunContextAssemblerTestStore(t *testing.T) *storetest.FakeContextStore {
	t.Helper()
	return storetest.NewFakeContextStore()
}
