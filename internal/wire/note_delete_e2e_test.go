package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

func TestDeletingANoteForgetsIt(t *testing.T) {
	server := httptest.NewServer(&fakeOpenAI{})
	defer server.Close()
	c, err := NewContainer(context.Background(), writeTestConfig(t, server.URL, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	client := newPairedClient(t, c)
	ctx := context.Background()

	// Written outside a run, the note is an external source a fact may cite.
	note, err := c.vault.Write(ctx, knowledge.WriteNote{Path: "inbox/tokio.md", Title: "Tokio", Body: "Tokio 用 work stealing 调度任务。"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.vault.Write(ctx, knowledge.WriteNote{Path: "inbox/smol.md", Title: "smol", Body: "smol 是一个小运行时。", RunID: "run-capture"}); err != nil {
		t.Fatal(err)
	}
	records, err := c.store.CommitMemory(ctx, core.MemoryMutation{Now: time.Now(), Changes: []core.MemoryChange{{
		Draft:  core.MemoryDraft{Kind: "fact", Content: "Tokio 用 work stealing 调度", Evidence: []core.MemoryEvidence{{SourceID: core.KnowledgeSourceID(note.Path, note.Revision), Quote: "work stealing", Relation: "supports"}}},
		Reason: "note statement",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	derived := records[0]

	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/knowledge/note?path=inbox/tokio.md", nil), http.StatusNoContent, nil)

	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/knowledge/note?path=inbox/tokio.md", nil), http.StatusNotFound, nil)
	var kept api.KnowledgeNoteDTO
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/knowledge/note?path=inbox/smol.md", nil), http.StatusOK, &kept)
	if kept.Title != "smol" {
		t.Fatalf("kept note = %+v", kept)
	}
	exclusion, err := c.store.MemoryExclusions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(exclusion.RecordIDs, derived.ID) {
		t.Fatalf("excluded records = %v, want the fact drawn from the deleted note", exclusion.RecordIDs)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories/"+derived.ID, nil), http.StatusNotFound, nil)
	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/knowledge/note?path=inbox/tokio.md", nil), http.StatusNotFound, nil)
}
