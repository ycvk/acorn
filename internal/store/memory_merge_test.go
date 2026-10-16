package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// mergeFixture commits two current facts about the same claim from distinct
// owner sources, the second recorded an hour after the first with a validity
// window, and returns them in that order.
func mergeFixture(t *testing.T, s *Store) (core.MemoryRecord, core.MemoryRecord) {
	t.Helper()
	ctx := context.Background()
	first := seedMemorySource(t, s, "owner:keep", "owner", "记住，我不吃香菜")
	second := seedMemorySource(t, s, "owner:chat", "owner", "我不吃香菜")
	survivor, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(first, "owner 不吃香菜")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	duplicate := memoryFact(second, "owner 不吃香菜（偏好）")
	duplicate.Draft.ValidFrom = memoryTestNow.Format(time.RFC3339)
	duplicate.Draft.ValidTo = memoryTestNow.AddDate(1, 0, 0).Format(time.RFC3339)
	merged, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{duplicate}, Now: memoryTestNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return survivor[0], merged[0]
}

// mergeChange revises survivor with the union of both records' evidence and
// retires duplicate.
func mergeChange(survivor, duplicate core.MemoryRecord) core.MemoryChange {
	evidence := append(slices.Clone(survivor.Evidence), duplicate.Evidence...)
	entities := append(slices.Clone(survivor.Entities), duplicate.Entities...)
	return core.MemoryChange{
		Draft:            core.MemoryDraft{ID: survivor.ID, Kind: "fact", Content: survivor.Content, Scope: survivor.Scope, State: "current", Pinned: survivor.Pinned, Entities: entities, Evidence: evidence},
		ExpectedRevision: survivor.Revision,
		Merges:           duplicate.ID,
		MergesRevision:   duplicate.Revision,
		Reason:           "same claim from two statements",
	}
}

func TestMemoryMergeRetiresDuplicateIntoSurvivor(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	survivor, duplicate := mergeFixture(t, s)
	at := memoryTestNow.Add(2 * time.Hour)
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{mergeChange(survivor, duplicate)}, Now: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != survivor.ID || records[0].Revision != 2 || len(records[0].Evidence) != 2 || records[0].State != "current" {
		t.Fatalf("survivor revision=%+v", records)
	}
	retired, err := s.ReadMemory(ctx, duplicate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Record.State != "merged" || retired.Record.Revision != 2 || !retired.Record.ValidTo.Equal(duplicate.ValidTo) || !retired.Record.ValidFrom.Equal(duplicate.ValidFrom) || len(retired.Record.Evidence) != 1 {
		t.Fatalf("merged record=%+v", retired.Record)
	}
	if !slices.Contains(retired.Links, core.MemoryLink{FromID: duplicate.ID, ToID: survivor.ID, Relation: "merged_into"}) {
		t.Fatalf("merged_into link missing: %+v", retired.Links)
	}
	jobs, err := s.ReadyMemoryJobs(ctx, at, "consolidate", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(jobs, func(j core.MemoryJob) bool { return j.ObjectID == survivor.ID && j.Version == "2" }) {
		t.Fatalf("survivor not queued for consolidation: %+v", jobs)
	}
	if slices.ContainsFunc(jobs, func(j core.MemoryJob) bool { return j.ObjectID == duplicate.ID && j.Version == "2" }) {
		t.Fatalf("merged record queued for consolidation: %+v", jobs)
	}
}

func TestMemoryMergedRecordLeavesCurrentReadsAndStaysInHistory(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	survivor, duplicate := mergeFixture(t, s)
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{mergeChange(survivor, duplicate)}, Now: memoryTestNow.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	ids := func(records []core.MemoryRecord) []string {
		out := make([]string, 0, len(records))
		for _, r := range records {
			out = append(out, r.ID)
		}
		slices.Sort(out)
		return out
	}
	both := []string{survivor.ID, duplicate.ID}
	slices.Sort(both)
	for _, tc := range []struct {
		name string
		read func(core.MemoryQuery) ([]core.MemoryRecord, error)
	}{
		{"list", func(q core.MemoryQuery) ([]core.MemoryRecord, error) { return s.ListMemoryRecords(ctx, q) }},
		{"search", func(q core.MemoryQuery) ([]core.MemoryRecord, error) {
			q.Query = "香菜"
			return s.SearchMemoryText(ctx, q)
		}},
		{"by ids", func(q core.MemoryQuery) ([]core.MemoryRecord, error) { return s.MemoryRecordsByIDs(ctx, both, q) }},
	} {
		current, err := tc.read(core.MemoryQuery{Mode: "current", Limit: 10})
		if err != nil || !slices.Equal(ids(current), []string{survivor.ID}) {
			t.Fatalf("%s current=%v err=%v", tc.name, ids(current), err)
		}
		history, err := tc.read(core.MemoryQuery{Mode: "history", Limit: 10})
		if err != nil || !slices.Equal(ids(history), both) {
			t.Fatalf("%s history=%v err=%v", tc.name, ids(history), err)
		}
	}
	if facts, err := s.MemorySourceFacts(ctx, "owner:chat"); err != nil || !slices.Equal(ids(facts), []string{survivor.ID}) {
		t.Fatalf("source facts after merge=%v err=%v", ids(facts), err)
	}
}

func TestMemoryMergeRejectsStaleOrRetiredDuplicates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	survivor, duplicate := mergeFixture(t, s)
	stale := mergeChange(survivor, duplicate)
	stale.MergesRevision = 7
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{stale}, Now: memoryTestNow.Add(2 * time.Hour)}); !errors.Is(err, core.ErrMemoryConflict) {
		t.Fatalf("stale merge revision: %v", err)
	}
	direct := memoryFact(core.MemorySource{ID: "owner:chat", Content: "我不吃香菜"}, "owner 讨厌香菜")
	direct.Draft.State = "merged"
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{direct}, Now: memoryTestNow.Add(2 * time.Hour)}); err == nil {
		t.Fatal("a draft wrote the merged state directly")
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{mergeChange(survivor, duplicate)}, Now: memoryTestNow.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	revised, err := s.ReadMemory(ctx, survivor.ID)
	if err != nil {
		t.Fatal(err)
	}
	retired, err := s.ReadMemory(ctx, duplicate.ID)
	if err != nil {
		t.Fatal(err)
	}
	again := mergeChange(revised.Record, retired.Record)
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{again}, Now: memoryTestNow.Add(3 * time.Hour)}); !errors.Is(err, core.ErrMemoryConflict) {
		t.Fatalf("merging a merged record: %v", err)
	}
}

// Forgetting the duplicate's source follows the revision evidence that merge
// copied into the survivor: the survivor's current revision cites that source,
// so the exclusion takes the survivor with it, as it does any fact whose
// current revision cites a forgotten source.
func TestMemoryForgetMergedSourceExcludesSurvivorCitingIt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	survivor, duplicate := mergeFixture(t, s)
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{mergeChange(survivor, duplicate)}, Now: memoryTestNow.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记那次聊天")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{"owner:chat"}, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: memoryTestNow.Add(3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{survivor.ID, duplicate.ID} {
		if !slices.Contains(exclusion.RecordIDs, id) {
			t.Fatalf("%s not excluded: %+v", id, exclusion.RecordIDs)
		}
		if _, err := s.ReadMemory(ctx, id); !errors.Is(err, core.ErrMemoryExcluded) {
			t.Fatalf("read %s after forget: %v", id, err)
		}
	}
	if _, err := s.LoadMemorySource(ctx, "owner:keep"); err != nil {
		t.Fatalf("the survivor's own source is excluded: %v", err)
	}
}
