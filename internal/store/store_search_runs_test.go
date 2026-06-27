package store

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestSearchRunsByInputTextKeyword(t *testing.T) {
	store := openTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	for _, r := range []struct {
		id    string
		input string
	}{
		{"run_deploy", "deploy the staging environment"},
		{"run_email", "send weekly summary email"},
		{"run_debug", "debug the deploy failure in logs"},
	} {
		if err := store.CreateRun(ctx, core.RunCreateParams{RunID: r.id, Input: r.input}); err != nil {
			t.Fatalf("create %s: %v", r.id, err)
		}
		if err := store.FinishRun(ctx, r.id, core.RunStatusSucceeded, "done", ""); err != nil {
			t.Fatalf("finish %s: %v", r.id, err)
		}
	}

	results, err := store.SearchRuns(ctx, "deploy", 10)
	if err != nil {
		t.Fatalf("SearchRuns: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 (deploy + debug deploy)", len(results))
	}

	ids := make(map[string]bool, len(results))
	for _, r := range results {
		ids[r.RunID] = true
	}
	if !ids["run_deploy"] || !ids["run_debug"] {
		t.Fatalf("expected run_deploy and run_debug, got %v", ids)
	}
}

func TestSearchRunsRespectsLimit(t *testing.T) {
	store := openTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if err := store.CreateRun(ctx, core.RunCreateParams{
			RunID: "run_" + string(rune('a'+i)),
			Input: "common keyword task",
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	results, err := store.SearchRuns(ctx, "common", 2)
	if err != nil {
		t.Fatalf("SearchRuns: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2 (limit)", len(results))
	}
}

func TestSearchRunsReturnsEmptyWhenNoMatch(t *testing.T) {
	store := openTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	store.CreateRun(ctx, core.RunCreateParams{RunID: "run_1", Input: "deploy"})

	results, err := store.SearchRuns(ctx, "nonexistent_keyword_xyz", 10)
	if err != nil {
		t.Fatalf("SearchRuns: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0 for no match", len(results))
	}
}

func TestSearchRunsEscapesLikeWildcards(t *testing.T) {
	store := openTestStore(t)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	// Create runs: one with a literal % in the input, one with _ .
	store.CreateRun(ctx, core.RunCreateParams{RunID: "run_pct", Input: "progress at 100%"})
	store.CreateRun(ctx, core.RunCreateParams{RunID: "run_underscore", Input: "file_name_test"})
	store.CreateRun(ctx, core.RunCreateParams{RunID: "run_plain", Input: "progress at 100 percent"})

	// Searching for "100%" should match only run_pct, not run_plain.
	results, err := store.SearchRuns(ctx, "100%", 10)
	if err != nil {
		t.Fatalf("SearchRuns: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 (only run_pct has literal %%); got: %+v", len(results), results)
	}
	if results[0].RunID != "run_pct" {
		t.Fatalf("matched run = %q, want run_pct", results[0].RunID)
	}

	// Searching for "file_name" should match only run_underscore.
	results, err = store.SearchRuns(ctx, "file_name", 10)
	if err != nil {
		t.Fatalf("SearchRuns underscore: %v", err)
	}
	if len(results) != 1 || results[0].RunID != "run_underscore" {
		t.Fatalf("underscore results = %+v, want only run_underscore", results)
	}
}
