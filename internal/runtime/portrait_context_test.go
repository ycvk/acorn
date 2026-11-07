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

func TestForgettingReachesTheRepliesOfRunsThatReadTheSource(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now()
	register := func(id, speaker, runID, body string) core.MemorySource {
		t.Helper()
		source, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: id, Kind: "fixture", ObjectID: id, Version: "1", Speaker: speaker, RunID: runID, Body: body, RecordedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	portrayed := register("owner:portrayed", "owner", "", "我在做 Acorn")
	recalled := register("owner:recalled", "owner", "", "早安卡要中新网要闻")
	trimmed := register("owner:trimmed", "owner", "", "以后经常丢 CSV 链接给你，按某一列统计每组多少行")
	if err := db.QueuePortrait(ctx, "day:1", now); err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimMemoryJob(ctx, now, time.Minute, core.PortraitOperation)
	if err != nil || job == nil {
		t.Fatalf("claim = %+v, %v", job, err)
	}
	if err := db.CommitPortrait(ctx, core.PortraitMutation{Lease: *job, Now: now, Lines: []core.PortraitDraft{{Content: "在做个人 agent Acorn", SourceIDs: []string{portrayed.ID}}}}); err != nil {
		t.Fatal(err)
	}
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	recall := core.MemoryRecall{Sources: []core.MemorySource{recalled, trimmed}}
	measure := &runMemory{store: db, activity: db, service: unchangedRecall{}, counter: counter, budget: 4096, runID: "run_measure", recall: recall}
	rendered, err := measure.render(ctx)
	if err != nil {
		t.Fatal(err)
	}
	full, err := counter.CountText(ctx, strings.TrimSuffix(strings.TrimPrefix(rendered, "<memory_context>\n"), "\n</memory_context>"))
	if err != nil {
		t.Fatal(err)
	}
	// Within this budget the model reads the portrait and the first recalled
	// source, without the second.
	mem := &runMemory{store: db, activity: db, service: unchangedRecall{}, counter: counter, budget: full - 1, runID: "run_read", recall: recall}
	if rendered, err = mem.render(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered, trimmed.ID) || !strings.Contains(rendered, recalled.ID) || len(renderedPortrait(t, rendered)) != 1 {
		t.Fatalf("memory context within budget = %s", rendered)
	}
	reply := register("reply:read", "assistant", "run_read", "按你的要求排好了")
	request := register("request:forget", "owner", "", "忘掉这些")
	forget := func(id string) core.MemoryExclusion {
		t.Helper()
		forgotten, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{id}, RequestSourceID: request.ID, Reason: "owner asked", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		return forgotten.Exclusion
	}
	// The run saw one source only through the portrait and never read the
	// other; forgetting them leaves its reply, and the portrait line goes.
	for _, id := range []string{portrayed.ID, trimmed.ID} {
		if exclusion := forget(id); slices.Contains(exclusion.SourceIDs, reply.ID) || slices.Contains(exclusion.RunIDs, "run_read") {
			t.Fatalf("forgetting %s reached the run: %+v", id, exclusion)
		}
	}
	if lines, err := db.Portrait(ctx); err != nil || len(lines) != 0 {
		t.Fatalf("portrait after forgetting its source = %+v, %v", lines, err)
	}
	if exclusion := forget(recalled.ID); !slices.Contains(exclusion.SourceIDs, reply.ID) || !slices.Contains(exclusion.RunIDs, "run_read") {
		t.Fatalf("forgetting the recalled source = %+v", exclusion)
	}
}
