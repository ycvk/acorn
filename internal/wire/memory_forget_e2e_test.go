package wire

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestMemoryForgetRebuildsNativeAgentStateBeforeAcknowledgement(t *testing.T) {
	ctx := context.Background()
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "")
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	now := time.Now()
	source, err := c.store.RegisterMemorySource(ctx, core.MemorySource{ID: "private-source", Kind: "standalone", ObjectID: "private", Version: "1", Speaker: "owner", Body: "我的私密偏好是TEST_SECRET", RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	records, err := c.store.CommitMemory(ctx, core.MemoryMutation{Now: now, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "fact", Content: source.Content, Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: source.Content, Relation: "supports"}}}, Reason: "owner statement"}}})
	if err != nil {
		t.Fatal(err)
	}
	provider.replies = []string{toolCallChunk("forget-call", "memory_forget", map[string]any{"record_ids": []string{records[0].ID}, "request_source_id": "message:1", "reason": "owner requested forgetting"}), textReply("已忘记该偏好。")}
	result, err := c.RunOnce(ctx, "请忘记已保存的私密偏好")
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("result %+v %v", result, err)
	}
	provider.mu.Lock()
	requests := append([]map[string]any(nil), provider.requests...)
	provider.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("model requests %d", len(requests))
	}
	first, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(requests[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), "TEST_SECRET") {
		t.Fatal("initial run never received the relevant memory")
	}
	if strings.Contains(string(second), "TEST_SECRET") {
		t.Fatal("post-forget model input leaked forgotten memory")
	}
	if !strings.Contains(string(second), "You are Acorn, the owner") {
		t.Fatal("stable persona and operating instruction disappeared after forgetting")
	}
	if !strings.Contains(string(second), "Affected executions have stopped") {
		t.Fatalf("successful cancellation outcome did not survive state filtering: %s", second)
	}
}
