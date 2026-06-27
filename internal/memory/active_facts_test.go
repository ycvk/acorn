package memory

import (
	"context"
	"strings"
	"testing"
)

func TestLoadActiveFactsEmpty(t *testing.T) {
	service := newTestService(t)
	facts := service.loadActiveFacts(context.Background(), 2200)
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

	facts := service.loadActiveFacts(context.Background(), 2200)
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

	facts := service.loadActiveFacts(context.Background(), 150)
	// With 150 char limit, only the first fact (title + 100 char body) fits;
	// the second exceeds the remaining budget.
	if len(facts) != 1 {
		t.Fatalf("expected 1 active fact with char limit 150, got %d: %+v", len(facts), facts)
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
