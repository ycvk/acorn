package wire

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheNextRunSpeaksWithTheSavedPersona(t *testing.T) {
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
	seeded, err := c.Persona().Current(ctx)
	if err != nil || seeded.Revision != 1 || seeded.Author != "release" {
		t.Fatalf("seeded persona = %+v, %v", seeded, err)
	}
	if _, err := c.Persona().Save(ctx, "", "你是小橡，说话像老朋友。", seeded.Revision); err != nil {
		t.Fatal(err)
	}
	provider.replies = []string{textReply("嗨。")}
	if result, err := c.RunOnce(ctx, "打个招呼"); err != nil || result.Status != "succeeded" {
		t.Fatalf("result %+v %v", result, err)
	}
	provider.mu.Lock()
	requests := append([]map[string]any(nil), provider.requests...)
	provider.mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("model requests %d", len(requests))
	}
	body, err := json.Marshal(requests[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "你是小橡，说话像老朋友。") || strings.Contains(string(body), "You are Acorn, the owner") {
		t.Fatalf("run instruction does not carry the saved persona: %s", body)
	}

	reopened, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if current, err := reopened.Persona().Current(ctx); err != nil || current.Revision != 2 {
		t.Fatalf("a restart must keep the owner's persona: %+v, %v", current, err)
	}
}
