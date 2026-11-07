package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// A memory the owner takes back in the app goes alone. A run that read it
// keeps its reply and tool results, the concern citing one of those stays in
// view, and nothing is excluded; only the portrait lines citing the memory's
// source go, and what was drawn from it is reviewed.
func TestWithdrawingAMemoryLeavesWhatRunsWroteAfterReadingIt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_1", "电动车"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_1", SessionID: "thread_1", TurnIndex: 1, Input: "这是哈啰电动车"}); err != nil {
		t.Fatal(err)
	}
	said, err := s.AppendSessionMessage(ctx, "thread_1", 1, "user", "这是哈啰电动车", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.LoadMemorySource(ctx, fmt.Sprintf("message:%d", said.ID))
	if err != nil {
		t.Fatal(err)
	}
	facts, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(owner, "owner 表示这是哈啰电动车")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	fact := facts[0]
	insights, err := s.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow.Add(time.Second), Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "insight", Content: "owner 骑共享电动车出行", Parents: []string{fact.ID}}, Reason: "drawn from the bike"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemorySnapshotRefs(ctx, "night", "run_1", []string{owner.ID}, []string{fact.ID}, 0); err != nil {
		t.Fatal(err)
	}
	reply, err := s.AppendSessionMessage(ctx, "thread_1", 1, "assistant", "停火还没有新消息", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.AppendEvent(ctx, "run_1", "tool.call.succeeded", map[string]any{"tool_name": "news_search", "output": "两条"})
	if err != nil {
		t.Fatal(err)
	}
	derived := []string{fmt.Sprintf("message:%d", reply.ID), fmt.Sprintf("event:%d", result.Sequence)}
	concern, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "持续跟进停火", Reason: "近 7 天两条", SourceID: derived[1], State: "active", ThreadID: "thread_1", UpdatedAt: memoryTestNow.Add(time.Minute)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	other := seedMemorySource(t, s, "owner:run", "owner", "我每天早上跑步")
	at := memoryTestNow.Add(time.Hour)
	job := claimPortrait(t, s, "day:2026-10-09", at)
	if err := s.CommitPortrait(ctx, core.PortraitMutation{Lease: job, Now: at, Lines: []core.PortraitDraft{
		{Content: "骑哈啰电动车", SourceIDs: []string{owner.ID}},
		{Content: "每天早上跑步", SourceIDs: []string{other.ID}},
	}}); err != nil {
		t.Fatal(err)
	}

	at = at.Add(time.Hour)
	withdrawn, err := s.WithdrawMemory(ctx, fact.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.State != "retracted" || withdrawn.Revision != fact.Revision+1 || revisionReason(t, s, fact.ID, withdrawn.Revision) != "owner withdrew it in the app" {
		t.Fatalf("withdrawn = %+v", withdrawn)
	}
	current, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Limit: 10})
	if err != nil || slices.ContainsFunc(current, func(r core.MemoryRecord) bool { return r.ID == fact.ID }) {
		t.Fatalf("current memories = %+v, %v", current, err)
	}
	insight, err := s.ReadMemory(ctx, insights[0].ID)
	if err != nil || !insight.Record.NeedsReview {
		t.Fatalf("insight drawn from it = %+v, %v", insight.Record, err)
	}
	jobs, err := s.ReadyMemoryJobs(ctx, at, "consolidate", 10)
	if err != nil || !slices.ContainsFunc(jobs, func(j core.MemoryJob) bool { return j.ObjectID == insights[0].ID }) {
		t.Fatalf("insight not queued for review: %+v %v", jobs, err)
	}

	for _, id := range append([]string{owner.ID}, derived...) {
		if _, err := s.LoadThreadSource(ctx, id); err != nil {
			t.Fatalf("source %s after the withdrawal: %v", id, err)
		}
	}
	exclusion, err := s.MemoryExclusions(ctx)
	if err != nil || exclusion.Epoch != 0 || len(exclusion.SourceIDs) != 0 || len(exclusion.RunIDs) != 0 {
		t.Fatalf("withdrawal excluded %+v, %v", exclusion, err)
	}
	concerns, err := s.ListConcerns(ctx, true)
	if err != nil || len(concerns) != 1 || concerns[0].ID != concern.ID {
		t.Fatalf("concerns = %+v, %v", concerns, err)
	}

	lines, err := s.Portrait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if shown := portraitContents(lines, core.PortraitLineShown); !slices.Equal(shown, []string{"每天早上跑步"}) || len(lines) != 1 {
		t.Fatalf("portrait after the withdrawal = %+v", lines)
	}
	refreshes, err := s.ReadyMemoryJobs(ctx, at, core.PortraitOperation, 10)
	if err != nil || !slices.ContainsFunc(refreshes, func(j core.MemoryJob) bool {
		return j.Version == fmt.Sprintf("withdraw:%s:%d", fact.ID, withdrawn.Revision)
	}) {
		t.Fatalf("portrait refresh = %+v, %v", refreshes, err)
	}

	if _, err := s.WithdrawMemory(ctx, fact.ID, at.Add(time.Minute)); !errors.Is(err, core.ErrMemoryNotFound) {
		t.Fatalf("second withdrawal = %v", err)
	}
}

func TestWithdrawingAThoughtReleasesIt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "event:1", "tool", "GitHub Trending 近 7 天选用 0")
	thoughts, err := s.CommitMemory(ctx, core.MemoryMutation{Now: memoryTestNow, Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "thought", Content: "到 11-08 仍是 0 再提议停用", Evidence: []core.MemoryEvidence{{SourceID: source.ID, Quote: source.Content, Relation: "supports"}}}, Reason: "night reflection"}}})
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, err := s.WithdrawMemory(ctx, thoughts[0].ID, memoryTestNow.Add(time.Hour))
	if err != nil || withdrawn.State != "released" {
		t.Fatalf("withdrawn thought = %+v, %v", withdrawn, err)
	}
}
