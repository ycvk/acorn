package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	storerepo "github.com/ycvk/acorn/internal/store"
)

func TestStoreToolResultsAppendLoadListAndBacklink(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	firstAt := time.Unix(1_710_000_000, 0).UTC()
	initial, err := store.Append(context.Background(), storerepo.ToolResultAppendRequest{
		RunID:         "run_1",
		SessionID:     "sess_1",
		TurnIndex:     2,
		CallID:        "call_1",
		ToolName:      "read_file",
		ArgumentsJSON: `{"path":"README.md"}`,
		Status:        storerepo.ToolResultStatusSucceeded,
		FullText:      "first result body",
		TokenEstimate: 9,
		SideEffects: []storerepo.SideEffectRef{{
			Kind: "workspace_read",
			Path: "README.md",
		}},
		EvidenceRefs: []storerepo.EvidenceRef{{
			Kind: "plan_evidence",
			Ref:  "step-evidence-1",
		}},
		CreatedAt: firstAt,
	})
	if err != nil {
		t.Fatalf("append tool result: %v", err)
	}
	if got, want := initial.ResultRef, "tool_result:run_1:call_1"; got != want {
		t.Fatalf("result ref = %q, want %q", got, want)
	}
	if got, want := initial.Preview, "first result body"; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}

	secondAt := time.Unix(1_710_000_060, 0).UTC()
	updated, err := store.Append(context.Background(), storerepo.ToolResultAppendRequest{
		RunID:         "run_1",
		SessionID:     "sess_1",
		TurnIndex:     2,
		CallID:        "call_1",
		ToolName:      "read_file",
		ArgumentsJSON: `{"path":"README.md"}`,
		Status:        storerepo.ToolResultStatusSucceeded,
		FullText:      "second result body",
		TokenEstimate: 11,
		SideEffects: []storerepo.SideEffectRef{{
			Kind: "workspace_read",
			Path: "README.md",
		}},
		EvidenceRefs: []storerepo.EvidenceRef{{
			Kind: "plan_evidence",
			Ref:  "step-evidence-1",
		}},
		CreatedAt: secondAt,
	})
	if err != nil {
		t.Fatalf("upsert tool result: %v", err)
	}
	if got, want := updated.FullText, "second result body"; got != want {
		t.Fatalf("updated full text = %q, want %q", got, want)
	}
	if got, want := updated.TokenEstimate, 11; got != want {
		t.Fatalf("updated token estimate = %d, want %d", got, want)
	}

	loaded, err := store.Load(context.Background(), updated.ResultRef)
	if err != nil {
		t.Fatalf("load tool result: %v", err)
	}
	if got, want := loaded.FullText, "second result body"; got != want {
		t.Fatalf("loaded full text = %q, want %q", got, want)
	}
	if got, want := loaded.Preview, "second result body"; got != want {
		t.Fatalf("loaded preview = %q, want %q", got, want)
	}
	if got, want := loaded.ArgumentsJSON, `{"path":"README.md"}`; got != want {
		t.Fatalf("loaded arguments json = %q, want %q", got, want)
	}
	if len(loaded.EvidenceRefs) != 1 {
		t.Fatalf("loaded evidence refs count = %d, want 1", len(loaded.EvidenceRefs))
	}

	items, err := store.ListByRun(context.Background(), "run_1")
	if err != nil {
		t.Fatalf("list by run: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("list count = %d, want 1", len(items))
	}

	backlinked, err := store.AppendEvidenceRef(context.Background(), updated.ResultRef, storerepo.EvidenceRef{
		Kind: "plan_evidence",
		Ref:  "step-evidence-2",
	})
	if err != nil {
		t.Fatalf("append evidence ref: %v", err)
	}
	if len(backlinked.EvidenceRefs) != 2 {
		t.Fatalf("backlinked evidence refs count = %d, want 2", len(backlinked.EvidenceRefs))
	}

	missing, err := store.Load(context.Background(), "tool_result:missing:call")
	if err == nil {
		t.Fatalf("load missing tool result = %#v, want error", missing)
	}
	if err != storerepo.ErrToolResultNotFound {
		t.Fatalf("missing tool result error = %v, want ErrToolResultNotFound", err)
	}
}

func TestStoreToolResultsAppendRejectsRequiredFields(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.Append(context.Background(), storerepo.ToolResultAppendRequest{
		SessionID: "sess_1",
		CallID:    "call_1",
		ToolName:  "read_file",
		Status:    storerepo.ToolResultStatusSucceeded,
	})
	if err == nil {
		t.Fatal("expected missing run_id error")
	}
	_, err = store.Append(context.Background(), storerepo.ToolResultAppendRequest{
		RunID:     "run_1",
		SessionID: "sess_1",
		CallID:    "call_1",
		ToolName:  "read_file",
		Status:    "bad",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
}
