package runtime

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

type unchangedRecall struct{ MemoryContextService }

func (unchangedRecall) RefreshRecall(_ context.Context, prior core.MemoryRecall) (core.MemoryRecall, error) {
	return prior, nil
}

func renderedPortrait(t *testing.T, rendered string) []string {
	t.Helper()
	var value struct {
		Portrait []string `json:"owner_portrait"`
	}
	body := strings.TrimSuffix(strings.TrimPrefix(rendered, "<memory_context>\n"), "\n</memory_context>")
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		t.Fatalf("memory context %q: %v", rendered, err)
	}
	return value.Portrait
}

func TestEveryModelCallCarriesTheShownPortrait(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now()
	source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "owner:a", Kind: "fixture", ObjectID: "owner:a", Version: "1", Speaker: "owner", Body: "我在做 Acorn，平时用 Go", RecordedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueuePortrait(ctx, "day:1", now); err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimMemoryJob(ctx, now, time.Minute, core.PortraitOperation)
	if err != nil || job == nil {
		t.Fatalf("claim = %+v, %v", job, err)
	}
	if err := db.CommitPortrait(ctx, core.PortraitMutation{Lease: *job, Now: now, Lines: []core.PortraitDraft{
		{Content: "在做个人 agent Acorn", SourceIDs: []string{source.ID}},
		{Content: "平时写 Go", SourceIDs: []string{source.ID}},
	}}); err != nil {
		t.Fatal(err)
	}
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	mem := &runMemory{store: db, activity: db, service: unchangedRecall{}, counter: counter, budget: 4096, runID: "run_portrait"}
	rendered, err := mem.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedPortrait(t, rendered); !slices.Equal(got, []string{"在做个人 agent Acorn", "平时写 Go"}) {
		t.Fatalf("portrait in memory context = %v", got)
	}
	// A tight budget gives up the portrait's last lines before failing.
	full, err := counter.CountText(ctx, strings.TrimSuffix(strings.TrimPrefix(rendered, "<memory_context>\n"), "\n</memory_context>"))
	if err != nil {
		t.Fatal(err)
	}
	mem.budget = full - 1
	trimmed, err := mem.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedPortrait(t, trimmed); !slices.Equal(got, []string{"在做个人 agent Acorn"}) {
		t.Fatalf("portrait within a tight budget = %v", got)
	}
	mem.budget = 4096
	lines, err := db.Portrait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RemovePortraitLine(ctx, lines[0].ID, now); err != nil {
		t.Fatal(err)
	}
	rendered, err = mem.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := renderedPortrait(t, rendered); !slices.Equal(got, []string{"平时写 Go"}) {
		t.Fatalf("portrait after removing a line = %v", got)
	}
}
