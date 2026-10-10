package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var memoryTestNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func seedMemorySource(t *testing.T, s *Store, id, speaker, content string) core.MemorySource {
	t.Helper()
	source, err := s.RegisterMemorySource(context.Background(), core.MemorySource{
		ID: id, Kind: "import", ObjectID: id, Version: "1", Speaker: speaker,
		Body: content, RecordedAt: memoryTestNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func memoryFact(source core.MemorySource, content string) core.MemoryChange {
	return core.MemoryChange{Draft: core.MemoryDraft{Kind: "fact", Content: content,
		Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: source.Content, Relation: "supports"}},
		Entities: []string{"owner"}}, Reason: "owner statement"}
}

func TestMemorySourceIsRegisteredWithMessage(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "memory-thread", "memory"); err != nil {
		t.Fatal(err)
	}
	message, err := s.AppendSessionMessage(ctx, "memory-thread", 1, "user", "我不吃香菜", "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.LoadMemorySource(ctx, fmt.Sprintf("message:%d", message.ID))
	if err != nil || source.Content != message.Content || source.Speaker != "owner" {
		t.Fatalf("source=%+v err=%v", source, err)
	}
	job, err := s.ClaimMemoryJob(ctx, memoryTestNow.Add(24*time.Hour), time.Minute, "")
	if err != nil || job == nil || job.ObjectID != source.ID || job.Operation != "extract" {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestMemoryEvidenceAndReplayAreAtomic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:1", "owner", "我不吃香菜")
	change := memoryFact(source, "owner 不吃香菜")
	bad := memoryFact(source, "owner 喜欢辣椒")
	bad.Draft.Evidence[0].Quote = "我喜欢辣椒"
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change, bad}, Now: memoryTestNow}); err == nil {
		t.Fatal("unquoted evidence accepted")
	}
	items, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", Limit: 10})
	if err != nil || len(items) != 0 {
		t.Fatalf("partial mutation: %+v %v", items, err)
	}
	first, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow.Add(time.Hour)})
	if err != nil || len(again) != 1 || first[0].ID != again[0].ID || again[0].Revision != 1 || len(again[0].Evidence) != 1 {
		t.Fatalf("replay=%+v err=%v", again, err)
	}
}

func TestMemoryCorrectionPreservesValidAndKnownTimes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	oldSource := seedMemorySource(t, s, "owner:old", "owner", "我不吃香菜")
	change := memoryFact(oldSource, "owner 不吃香菜")
	change.Draft.ValidFrom = memoryTestNow.Add(-24 * time.Hour).Format(time.RFC3339Nano)
	first, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	newSource := seedMemorySource(t, s, "owner:new", "owner", "我从今天开始可以接受少量香菜")
	next := memoryFact(newSource, "owner 可以接受少量香菜")
	next.Draft.ValidFrom = memoryTestNow.Add(time.Hour).Format(time.RFC3339Nano)
	next.Supersedes, next.SupersedesRevision = first[0].ID, 1
	second, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{next}, Now: memoryTestNow.Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Limit: 10})
	if err != nil || len(current) != 1 || current[0].ID != second[0].ID {
		t.Fatalf("current=%+v %v", current, err)
	}
	past, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", AsOf: memoryTestNow, KnownAt: memoryTestNow.Add(30 * time.Minute), Limit: 10})
	if err != nil || len(past) != 1 || past[0].ID != first[0].ID || past[0].State != "current" {
		t.Fatalf("past=%+v %v", past, err)
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{next}, Now: memoryTestNow.Add(3 * time.Hour)}); !errors.Is(err, core.ErrMemoryConflict) {
		t.Fatalf("stale supersession: %v", err)
	}
}

func TestMemoryAssistantInferenceKeepsItsAttribution(t *testing.T) {
	s := openTestStore(t)
	source := seedMemorySource(t, s, "assistant:1", "assistant", "你可能喜欢辛辣食物")
	if _, err := s.CommitMemory(context.Background(), core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 喜欢辛辣食物")}, Now: memoryTestNow}); err == nil {
		t.Fatal("assistant guess became a fact")
	}
}

func TestMemoryForgetInvalidatesDerivationsAndInFlightWork(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:preference", "owner", "我不吃香菜")
	lease, err := s.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "")
	if err != nil || lease == nil {
		t.Fatalf("claim=%+v %v", lease, err)
	}
	facts, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 不吃香菜")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	insights, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "insight", Content: "为 owner 点餐需要考虑香菜偏好", Parents: []string{facts[0].ID}}, Reason: "meal preference"}}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘掉我的香菜偏好")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{facts[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow.Add(time.Second)})
	if err != nil || exclusion.Epoch != 1 {
		t.Fatalf("forget=%+v %v", exclusion, err)
	}
	for _, id := range []string{facts[0].ID, insights[0].ID} {
		if _, err := s.ReadMemory(ctx, id); !errors.Is(err, core.ErrMemoryExcluded) {
			t.Fatalf("read forgotten %s: %v", id, err)
		}
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Lease: lease, Changes: []core.MemoryChange{memoryFact(source, "owner 不吃香菜")}, Now: memoryTestNow.Add(2 * time.Second)}); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("forgotten task committed: %v", err)
	}
	for _, mode := range []string{"current", "history"} {
		items, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "香菜", Mode: mode, Limit: 10})
		if err != nil || len(items) != 0 {
			t.Fatalf("forgotten search %s = %+v %v", mode, items, err)
		}
	}
}

func TestMemoryExpiredWorkerCannotCommit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:lease", "owner", "我喜欢清淡食物")
	first, err := s.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "")
	if err != nil || first == nil {
		t.Fatalf("first=%+v %v", first, err)
	}
	second, err := s.ClaimMemoryJob(ctx, memoryTestNow.Add(2*time.Minute), time.Minute, "")
	if err != nil || second == nil || second.ID != first.ID || second.Token <= first.Token {
		t.Fatalf("second=%+v %v", second, err)
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Lease: first, Changes: []core.MemoryChange{memoryFact(source, "owner 喜欢清淡食物")}, Now: memoryTestNow.Add(2 * time.Minute)}); !errors.Is(err, core.ErrMemoryLeaseLost) {
		t.Fatalf("stale lease: %v", err)
	}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Lease: second, Changes: []core.MemoryChange{memoryFact(source, "owner 喜欢清淡食物")}, Now: memoryTestNow.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryIndexRequiresExplicitRebuild(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first, err := s.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "voyage-4", Dimensions: 1024}, false)
	if err != nil || first.Generation != 1 || first.State != "ready" {
		t.Fatalf("index=%+v %v", first, err)
	}
	if _, err := s.ConfigureMemoryIndex(ctx, core.MemoryIndex{Model: "voyage-4", Dimensions: 512}, false); !errors.Is(err, core.ErrMemoryIndexMismatch) {
		t.Fatalf("dimension change: %v", err)
	}
}

func TestMemoryValidityAllowsUnknownAndRejectsRelativeDates(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, db, "optional-time", "owner", "我住在杭州")
	change := memoryFact(source, source.Content)
	data, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"content":"我住在杭州"`), []byte(`"content":"我住在杭州","valid_from":"","valid_to":""`), 1)
	if err := json.Unmarshal(data, &change); err != nil {
		t.Fatal(err)
	}
	saved, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil || !saved[0].ValidFrom.IsZero() || !saved[0].ValidTo.IsZero() {
		t.Fatalf("unknown time %+v %v", saved, err)
	}
	change.Draft.Content = "相对时间必须有明确日期"
	change.Draft.ValidFrom = "yesterday"
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow}); err == nil {
		t.Fatal("relative date accepted")
	}
}
