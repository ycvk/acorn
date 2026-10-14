package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

// ownerSaid writes a finished turn in a thread and a fact drawn from what the
// owner said in it.
func ownerSaid(t *testing.T, c *Container, threadID, runID, said, fact string) core.MemoryRecord {
	t.Helper()
	ctx := context.Background()
	if _, err := c.store.CreateSession(ctx, threadID, said); err != nil {
		t.Fatal(err)
	}
	if err := c.store.CreateRun(ctx, core.RunCreateParams{RunID: runID, SessionID: threadID, TurnIndex: 1, Input: said}); err != nil {
		t.Fatal(err)
	}
	message, err := c.store.AppendSessionMessage(ctx, threadID, 1, "user", said, runID)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.store.FinishRun(ctx, runID, core.RunStatusSucceeded, "记住了", ""); err != nil {
		t.Fatal(err)
	}
	records, err := c.store.CommitMemory(ctx, core.MemoryMutation{Now: time.Now(), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: fact, Evidence: []core.MemoryEvidence{{SourceID: "message:" + strconv.FormatInt(message.ID, 10), Quote: said, Relation: "supports"}}}, Reason: "owner statement"}}})
	if err != nil {
		t.Fatal(err)
	}
	return records[0]
}

func TestDeletingAThreadForgetsIt(t *testing.T) {
	server := httptest.NewServer(&fakeOpenAI{})
	defer server.Close()
	c, err := NewContainer(context.Background(), writeTestConfig(t, server.URL, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	client := newPairedClient(t, c)

	gone := ownerSaid(t, c, "session-tea", "run-tea", "我只喝乌龙茶", "只喝乌龙茶")
	kept := ownerSaid(t, c, "session-run", "run-run", "我每周跑三次步", "每周跑三次步")

	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/threads/session-tea", nil), http.StatusNoContent, nil)

	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/threads/session-tea", nil), http.StatusNotFound, nil)
	var list api.MemoryListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories", nil), http.StatusOK, &list)
	if got := memoryIDs(list); !slices.Equal(got, []string{kept.ID}) {
		t.Fatalf("memories after deleting the thread = %v, want only %s (forgot %s)", got, kept.ID, gone.ID)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories/"+gone.ID, nil), http.StatusNotFound, nil)
	if _, err := c.store.LoadRun(context.Background(), "run-tea"); err == nil {
		t.Fatal("the deleted thread's run is still stored")
	}
	exclusion, err := c.store.MemoryExclusions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(exclusion.RecordIDs, gone.ID) || slices.Contains(exclusion.RecordIDs, kept.ID) {
		t.Fatalf("excluded records = %v", exclusion.RecordIDs)
	}
	client.do(t, httptest.NewRequest(http.MethodDelete, "/v1/threads/session-tea", nil), http.StatusNotFound, nil)
}
