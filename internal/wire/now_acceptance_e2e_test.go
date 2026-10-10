package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/knowledge"
)

func TestNowPageReadsAndChangesTheStore(t *testing.T) {
	start := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	server := httptest.NewServer(&fakeOpenAI{})
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, briefingOff)
	clock := &testClock{now: start}
	ctx := context.Background()
	c, err := buildContainer(ctx, cfg, buildOptions{clock: clock.Now})
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer func() { _ = c.Close() }()
	client := newPairedClient(t, c)

	if _, err := c.vault.Write(ctx, knowledge.WriteNote{Path: "briefings/2026-10-11.md", Title: "早安 2026-10-11", Body: "今天有一个约定。"}); err != nil {
		t.Fatalf("write briefing: %v", err)
	}
	commitment, err := c.store.AddCommitment(ctx, core.Commitment{State: "scheduled", Content: "看 X", SessionID: "thread_a", WakeAt: start.Add(time.Hour), CreatedAt: start})
	if err != nil {
		t.Fatalf("add commitment: %v", err)
	}
	watch, err := c.store.AddWatch(ctx, core.Watch{Name: "Go releases", Kind: core.WatchGitHub, Target: "golang/go", Selector: "releases", Mode: core.WatchModeDigest, Status: core.WatchActive, Interval: time.Hour, NextCheckAt: start.Add(time.Hour), CreatedAt: start, UpdatedAt: start})
	if err != nil {
		t.Fatalf("add watch: %v", err)
	}
	kept := ownerSource(t, c, "goal", "我要准备 Go 面试")
	forgotten := ownerSource(t, c, "trip", "我想去京都")
	for title, source := range map[string]core.MemorySource{"准备 Go 面试": kept, "京都旅行": forgotten} {
		if _, err := c.store.SaveConcern(ctx, core.MemoryConcern{Title: title, State: "active", Reason: "owner goal", SourceID: source.ID, CreatedAt: start, UpdatedAt: start}, 0); err != nil {
			t.Fatalf("save concern: %v", err)
		}
	}
	request := ownerSource(t, c, "forget", "忘掉京都的事")
	if _, err := c.store.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{forgotten.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: start}); err != nil {
		t.Fatalf("forget: %v", err)
	}

	now := loadNow(t, client)
	if now.Briefing == nil || now.Briefing.Path != "briefings/2026-10-11.md" {
		t.Fatalf("briefing = %+v", now.Briefing)
	}
	if len(now.Commitments) != 1 || now.Commitments[0].ID != commitment.ID || now.Commitments[0].State != "scheduled" {
		t.Fatalf("commitments = %+v", now.Commitments)
	}
	if len(now.Concerns) != 1 || now.Concerns[0].Title != "准备 Go 面试" {
		t.Fatalf("concerns = %+v, the forgotten one must be gone", now.Concerns)
	}
	if len(now.Watches) != 1 || now.Watches[0].Status != "active" {
		t.Fatalf("watches = %+v", now.Watches)
	}

	cancel := "/v1/commitments/" + strconv.FormatInt(commitment.ID, 10) + ":cancel"
	client.do(t, httptest.NewRequest(http.MethodPost, cancel, nil), http.StatusNoContent, nil)
	client.do(t, httptest.NewRequest(http.MethodPost, cancel, nil), http.StatusConflict, nil)
	if stored, err := c.store.LoadCommitment(ctx, commitment.ID); err != nil || stored.State != "cancelled" {
		t.Fatalf("stored commitment = %+v err=%v", stored, err)
	}

	watchPath := "/v1/watches/" + strconv.FormatInt(watch.ID, 10)
	client.do(t, httptest.NewRequest(http.MethodPost, watchPath+":pause", nil), http.StatusNoContent, nil)
	clock.Set(start.Add(time.Minute))
	if due, err := c.store.ListDueWatches(ctx, start.Add(2*time.Hour), 10); err != nil || len(due) != 0 {
		t.Fatalf("paused watch is due: %+v err=%v", due, err)
	}
	client.do(t, httptest.NewRequest(http.MethodPost, watchPath+":resume", nil), http.StatusNoContent, nil)
	now = loadNow(t, client)
	if len(now.Commitments) != 0 || now.Watches[0].Status != "active" || !now.Watches[0].NextCheckAt.Equal(clock.Now()) {
		t.Fatalf("after changes: commitments %+v watches %+v", now.Commitments, now.Watches)
	}
}

func loadNow(t *testing.T, client pairedClient) api.NowResponse {
	t.Helper()
	var now api.NowResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/now", nil), http.StatusOK, &now)
	return now
}

func ownerSource(t *testing.T, c *Container, id, body string) core.MemorySource {
	t.Helper()
	source, err := c.store.RegisterMemorySource(context.Background(), core.MemorySource{ID: id, Kind: "fixture", ObjectID: id, Version: "1", Speaker: "owner", Body: body, RecordedAt: c.clock()})
	if err != nil {
		t.Fatalf("register source: %v", err)
	}
	return source
}
