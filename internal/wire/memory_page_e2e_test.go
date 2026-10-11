package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/core"
)

func TestOwnerReadsCorrectsAndForgetsMemoriesFromTheApp(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(&fakeOpenAI{})
	defer server.Close()
	c, err := NewContainer(ctx, writeTestConfig(t, server.URL, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	client := newPairedClient(t, c)

	now := time.Now()
	source, err := c.store.RegisterMemorySource(ctx, core.MemorySource{ID: "owner-coffee", Kind: "standalone", ObjectID: "coffee", Version: "1", Speaker: "owner", SessionID: "session-coffee", Body: "我每天早上喝咖啡", RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := c.store.CommitMemory(ctx, core.MemoryMutation{Now: now, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: "每天早上喝咖啡", Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: source.Body, Relation: "supports"}}}, Reason: "owner statement"}}})
	if err != nil {
		t.Fatal(err)
	}
	fact := facts[0]
	insights, err := c.store.CommitMemory(ctx, core.MemoryMutation{Now: now.Add(time.Second), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "insight", Content: "早上需要提神", Parents: []string{fact.ID}}, Reason: "drawn from the morning habit"}}})
	if err != nil {
		t.Fatal(err)
	}
	insight := insights[0]

	var list api.MemoryListResponse
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories", nil), http.StatusOK, &list)
	if got := memoryIDs(list); !slices.Equal(got, []string{insight.ID, fact.ID}) || list.HasMore {
		t.Fatalf("listing = %v has_more=%v, want insight then fact", got, list.HasMore)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories?kind=fact&limit=1", nil), http.StatusOK, &list)
	if got := memoryIDs(list); !slices.Equal(got, []string{fact.ID}) || list.HasMore {
		t.Fatalf("facts = %v has_more=%v", got, list.HasMore)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories?limit=1", nil), http.StatusOK, &list)
	if !list.HasMore {
		t.Fatal("a listing cut by its limit must report more")
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories?q=咖啡", nil), http.StatusOK, &list)
	if got := memoryIDs(list); !slices.Equal(got, []string{fact.ID}) {
		t.Fatalf("search = %v", got)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories?kind=note", nil), http.StatusBadRequest, nil)

	var detail api.MemoryDetailDTO
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories/"+fact.ID, nil), http.StatusOK, &detail)
	if len(detail.Evidence) != 1 || detail.Evidence[0].Quote != "我每天早上喝咖啡" || detail.Evidence[0].Speaker != "owner" || detail.Evidence[0].ThreadID != "session-coffee" {
		t.Fatalf("fact evidence = %+v", detail.Evidence)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories/"+insight.ID, nil), http.StatusOK, &detail)
	if len(detail.BasedOn) != 1 || detail.BasedOn[0].ID != fact.ID {
		t.Fatalf("insight based_on = %+v", detail.BasedOn)
	}

	client.do(t, jsonRequest(http.MethodPost, "/v1/memories/"+insight.ID+":correct", `{"revision":1,"content":"不需要提神"}`), http.StatusBadRequest, nil)
	client.do(t, jsonRequest(http.MethodPost, "/v1/memories/"+fact.ID+":correct", `{"revision":7,"content":"我现在只喝茶"}`), http.StatusConflict, nil)
	client.do(t, jsonRequest(http.MethodPost, "/v1/memories/"+fact.ID+":correct", `{"revision":1,"content":"  "}`), http.StatusBadRequest, nil)
	var corrected api.MemorySummaryDTO
	client.do(t, jsonRequest(http.MethodPost, "/v1/memories/"+fact.ID+":correct", `{"revision":1,"content":"我现在只喝茶"}`), http.StatusOK, &corrected)
	if corrected.ID == fact.ID || corrected.Content != "我现在只喝茶" || corrected.Kind != "fact" {
		t.Fatalf("corrected = %+v", corrected)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories/"+corrected.ID, nil), http.StatusOK, &detail)
	if len(detail.Evidence) != 1 || detail.Evidence[0].SourceKind != core.MemoryRequestSource || detail.Evidence[0].Speaker != "owner" || detail.Evidence[0].Quote != "我现在只喝茶" {
		t.Fatalf("corrected evidence = %+v", detail.Evidence)
	}
	if len(detail.Earlier) != 1 || detail.Earlier[0].Content != "每天早上喝咖啡" {
		t.Fatalf("corrected earlier = %+v", detail.Earlier)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories?kind=fact", nil), http.StatusOK, &list)
	if got := memoryIDs(list); !slices.Equal(got, []string{corrected.ID}) {
		t.Fatalf("facts after correction = %v", got)
	}

	var forgotten api.MemoryForgetResponse
	client.do(t, httptest.NewRequest(http.MethodPost, "/v1/memories/"+corrected.ID+":forget", nil), http.StatusOK, &forgotten)
	if !slices.Contains(forgotten.ForgottenIDs, corrected.ID) {
		t.Fatalf("forgotten = %v", forgotten.ForgottenIDs)
	}
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories/"+corrected.ID, nil), http.StatusNotFound, nil)
	client.do(t, httptest.NewRequest(http.MethodPost, "/v1/memories/"+corrected.ID+":forget", nil), http.StatusNotFound, nil)
	client.do(t, httptest.NewRequest(http.MethodGet, "/v1/memories?q=喝茶", nil), http.StatusOK, &list)
	if len(list.Memories) != 0 {
		t.Fatalf("forgotten memory still found: %+v", list.Memories)
	}
	exclusion, err := c.store.MemoryExclusions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if exclusion.Epoch == 0 || !slices.ContainsFunc(exclusion.SourceIDs, func(id string) bool { return strings.HasPrefix(id, "request:") }) {
		t.Fatalf("the owner's correction and forget request must be excluded with the memory: %+v", exclusion)
	}
}

func memoryIDs(list api.MemoryListResponse) []string {
	ids := make([]string, 0, len(list.Memories))
	for _, memory := range list.Memories {
		ids = append(ids, memory.ID)
	}
	return ids
}

func jsonRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}
