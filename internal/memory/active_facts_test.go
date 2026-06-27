package memory

import (
	"context"
	"strings"
	"testing"
)

func TestLoadActiveFactsEmpty(t *testing.T) {
	service := newTestService(t)
	facts, err := service.loadActiveFacts(context.Background(), 2200)
	if err != nil {
		t.Fatalf("loadActiveFacts: %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("expected 0 active facts from empty service, got %d", len(facts))
	}
}

func TestLoadActiveFactsReturnsVerifiedUserFacts(t *testing.T) {
	service := newTestService(t)
	// CreateFact auto-stamps status=verified, scope=user by default.
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "Owner timezone",
		Body:  "UTC+8",
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "VPS location",
		Body:  "Tokyo",
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	// Workspace-scoped fact should NOT appear in the always-on snapshot.
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "Deploy script",
		Body:  "make deploy",
		Scope: "workspace:acorn",
	}); err != nil {
		t.Fatalf("CreateFact workspace: %v", err)
	}

	// Rebuild index so the new facts are visible.
	if err := service.BuildIndex(context.Background()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	facts, err := service.loadActiveFacts(context.Background(), 2200)
	if err != nil {
		t.Fatalf("loadActiveFacts: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected 2 active facts (user-scoped non-retired), got %d: %+v", len(facts), facts)
	}
	for _, f := range facts {
		if strings.Contains(f.Content, "Deploy script") {
			t.Errorf("workspace-scoped fact leaked into active snapshot: %s", f.Content)
		}
	}
}

func TestLoadActiveFactsRespectsCharLimit(t *testing.T) {
	service := newTestService(t)
	// Create facts that exceed a small char limit.
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "A",
		Body:  strings.Repeat("x", 100),
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "B",
		Body:  strings.Repeat("y", 100),
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if err := service.BuildIndex(context.Background()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	facts, err := service.loadActiveFacts(context.Background(), 150)
	if err != nil {
		t.Fatalf("loadActiveFacts: %v", err)
	}
	// With 150 char limit, only the first fact (title + 100 char body) fits;
	// the second exceeds the remaining budget and is skipped (not truncated).
	if len(facts) != 1 {
		t.Fatalf("expected 1 active fact with char limit 150, got %d: %+v", len(facts), facts)
	}
}

func TestLoadActiveFactsCharLimitBelowSingleFact(t *testing.T) {
	// When the char limit is smaller than a single fact, the fact is dropped
	// entirely — never truncated mid-fact.
	service := newTestService(t)
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "Big",
		Body:  strings.Repeat("z", 200),
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if err := service.BuildIndex(context.Background()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	facts, err := service.loadActiveFacts(context.Background(), 50)
	if err != nil {
		t.Fatalf("loadActiveFacts: %v", err)
	}
	if len(facts) != 0 {
		t.Fatalf("expected 0 facts when limit < single fact size, got %d: %+v", len(facts), facts)
	}
}

func TestRenderActiveFactsEmpty(t *testing.T) {
	if got := RenderActiveFacts(nil); got != "" {
		t.Errorf("RenderActiveFacts(nil) = %q, want empty", got)
	}
	if got := RenderActiveFacts([]Entry{}); got != "" {
		t.Errorf("RenderActiveFacts([]) = %q, want empty", got)
	}
}

func TestRenderActiveFactsNonEmpty(t *testing.T) {
	facts := []Entry{
		{Content: "Owner timezone: UTC+8"},
		{Content: "VPS location: Tokyo"},
	}
	got := RenderActiveFacts(facts)
	if !strings.Contains(got, "Active Memory") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, "Owner timezone: UTC+8") {
		t.Errorf("missing fact content: %q", got)
	}
	if !strings.Contains(got, "VPS location: Tokyo") {
		t.Errorf("missing fact content: %q", got)
	}
}

// TestPrepareDoesNotDuplicateActiveFactsInEntries verifies that a fact
// appearing in ActiveFacts does not also appear in search-matched Entries.
func TestPrepareDoesNotDuplicateActiveFactsInEntries(t *testing.T) {
	service := newTestService(t)
	if _, err := service.CreateFact(context.Background(), CreateFactRequest{
		Title: "Owner timezone",
		Body:  "UTC+8",
	}); err != nil {
		t.Fatalf("CreateFact: %v", err)
	}
	if err := service.BuildIndex(context.Background()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	result, err := service.Prepare(context.Background(), PrepareRequest{
		UserInput:       "timezone",
		ActiveCharLimit: 2200,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// The fact should be in ActiveFacts.
	found := false
	for _, f := range result.ActiveFacts {
		if strings.Contains(f.Content, "timezone") || strings.Contains(f.Content, "Owner timezone") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected fact in ActiveFacts, got: %+v", result.ActiveFacts)
	}

	// The same fact should NOT be in Entries (deduped).
	for _, e := range result.Entries {
		if strings.Contains(e.Content, "timezone") || strings.Contains(e.Title, "timezone") {
			t.Errorf("fact duplicated in Entries: %+v", e)
		}
	}
}
