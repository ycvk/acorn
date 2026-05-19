package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/providerusage"
)

func TestAppendAndListProviderUsagesByRun(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	ctx := context.Background()
	first := providerusage.Record{
		UsageID:          "provider_usage:run_1:000001",
		RunID:            "run_1",
		SessionID:        "session_1",
		CallSite:         providerusage.CallSitePlan,
		ProviderName:     "openai",
		ModelName:        "gpt-test",
		PromptTokens:     100,
		CompletionTokens: 20,
		TotalTokens:      120,
		CachedTokens:     60,
		ReasoningTokens:  5,
		CreatedAt:        time.Date(2026, 5, 9, 1, 2, 3, 4, time.UTC),
	}
	second := providerusage.Record{
		UsageID:          "provider_usage:run_1:000002",
		RunID:            "run_1",
		SessionID:        "session_1",
		CallSite:         providerusage.CallSiteAct,
		ProviderName:     "openai",
		ModelName:        "gpt-test",
		PromptTokens:     40,
		CompletionTokens: 10,
		TotalTokens:      50,
		CachedTokens:     0,
		ReasoningTokens:  1,
		CreatedAt:        first.CreatedAt.Add(time.Second),
	}
	if err := store.AppendProviderUsage(ctx, first); err != nil {
		t.Fatalf("AppendProviderUsage first: %v", err)
	}
	if err := store.AppendProviderUsage(ctx, second); err != nil {
		t.Fatalf("AppendProviderUsage second: %v", err)
	}
	if err := store.AppendProviderUsage(ctx, providerusage.Record{
		UsageID:      "provider_usage:other:000001",
		RunID:        "other",
		CallSite:     providerusage.CallSiteRuntime,
		ProviderName: "openai",
		ModelName:    "gpt-other",
		CreatedAt:    first.CreatedAt,
	}); err != nil {
		t.Fatalf("AppendProviderUsage other: %v", err)
	}

	items, err := store.ListProviderUsagesByRun(ctx, "run_1")
	if err != nil {
		t.Fatalf("ListProviderUsagesByRun: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].UsageID != first.UsageID || items[1].UsageID != second.UsageID {
		t.Fatalf("unexpected order: %+v", items)
	}
	if got := items[0]; got.PromptTokens != 100 || got.CachedTokens != 60 || got.ReasoningTokens != 5 {
		t.Fatalf("unexpected first usage: %+v", got)
	}
}

func TestAppendProviderUsageRequiresRunID(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	err = store.AppendProviderUsage(context.Background(), providerusage.Record{
		UsageID:      "provider_usage::000001",
		CallSite:     providerusage.CallSiteRuntime,
		ProviderName: "openai",
		ModelName:    "gpt-test",
	})
	if err == nil {
		t.Fatal("expected run_id validation error")
	}
}
