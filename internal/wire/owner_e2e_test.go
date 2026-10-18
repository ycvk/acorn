package wire

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The name the owner saves reaches the next run's instruction without a
// restart, clearing it takes it out again, and a restart keeps it.
func TestTheNextRunKnowsWhatToCallTheOwner(t *testing.T) {
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
	// Memory extraction may call the model between runs, so each run gets
	// spare replies and its request is the one carrying its input.
	runRequest := func(input string) string {
		t.Helper()
		provider.mu.Lock()
		provider.replies = []string{textReply("好。"), textReply("好。"), textReply("好。")}
		provider.mu.Unlock()
		if result, err := c.RunOnce(ctx, input); err != nil || result.Status != "succeeded" {
			t.Fatalf("run %q: %+v %v", input, result, err)
		}
		provider.mu.Lock()
		defer provider.mu.Unlock()
		for i := len(provider.requests) - 1; i >= 0; i-- {
			body, err := json.Marshal(provider.requests[i])
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), input) && strings.Contains(string(body), "You are Acorn") {
				return string(body)
			}
		}
		t.Fatalf("no model request carried run %q", input)
		return ""
	}

	if body := runRequest("在吗"); strings.Contains(body, "The owner is") {
		t.Fatalf("an unnamed owner must not be named: %s", body)
	}
	if _, err := c.Owner().SetName(ctx, "温遥"); err != nil {
		t.Fatal(err)
	}
	if body := runRequest("打个招呼"); !strings.Contains(body, "The owner is 温遥.") {
		t.Fatalf("the next run must know the saved name: %s", body)
	}

	reopened, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if owner, err := reopened.Owner().Current(ctx); err != nil || owner.Name != "温遥" {
		t.Fatalf("a restart must keep the owner's name: %+v, %v", owner, err)
	}

	if _, err := c.Owner().SetName(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if body := runRequest("还在吗"); strings.Contains(body, "The owner is") {
		t.Fatalf("a cleared name must leave the next run: %s", body)
	}
}
