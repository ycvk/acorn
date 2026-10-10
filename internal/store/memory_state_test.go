package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestMemoryUnchangedRevisionDoesNotScheduleAnotherPass(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:stable", "owner", "我喜欢清淡的食物")
	change := memoryFact(source, "owner 喜欢清淡的食物")
	first, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.MemoryProcessingStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	change.Draft.ID = first[0].ID
	change.ExpectedRevision = first[0].Revision
	change.Reason = "review found the same supported state"
	for i := range 3 {
		again, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow.Add(time.Duration(i+1) * time.Hour)})
		if err != nil || len(again) != 1 || again[0].Revision != first[0].Revision || !again[0].UpdatedAt.Equal(first[0].UpdatedAt) {
			t.Fatalf("unchanged review advanced revision: %+v %v", again, err)
		}
	}
	after, err := s.MemoryProcessingStatus(ctx)
	if err != nil || after.Pending != before.Pending {
		t.Fatalf("unchanged review enqueued work: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestMemoryPartialExclusionKeepsUnrelatedSourceText(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:two", "owner", "我不吃香菜。我喜欢游泳。")
	fact := memoryFact(source, "owner 不吃香菜")
	fact.Draft.Evidence[0].Quote = "我不吃香菜"
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{fact}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget1", "owner", "忘记香菜偏好")
	if _, err = s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{records[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	other := seedMemorySource(t, s, "owner:other", "owner", "我喜欢红色")
	request2 := seedMemorySource(t, s, "owner:forget2", "owner", "忘记颜色偏好")
	if _, err = s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{other.ID}, RequestSourceID: request2.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMemorySource(ctx, source.ID)
	if err != nil || !strings.Contains(got.Content, "我喜欢游泳") || strings.Contains(got.Content, "我不吃香菜") {
		t.Fatalf("remaining source=%+v err=%v", got, err)
	}
}

func TestMemoryNewOwnerSourceCanReintroduceForgottenFact(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:first", "owner", "我喜欢红色")
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 喜欢红色")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记颜色偏好")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{records[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	fresh := seedMemorySource(t, s, "owner:fresh", "owner", "我喜欢红色")
	next, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(fresh, "owner 喜欢红色")}, Now: memoryTestNow.Add(time.Hour), Epoch: exclusion.Epoch})
	if err != nil || len(next) != 1 || next[0].ID == records[0].ID {
		t.Fatalf("fresh=%+v err=%v", next, err)
	}
}

func TestMemoryThreadSummaryCannotCrossExclusionEpoch(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:summary", "owner", "我喜欢红色")
	summary := core.ThreadSummary{SessionID: "thread", ThroughMessageID: 1, Content: "owner 喜欢红色", SourceIDs: []string{source.ID}, UpdatedAt: memoryTestNow}
	if err := s.SaveThreadSummary(ctx, summary); err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记颜色偏好")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{source.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadThreadSummary(ctx, "thread"); err != nil || got != nil {
		t.Fatalf("summary=%+v err=%v", got, err)
	}
	if err := s.SaveThreadSummary(ctx, summary); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("stale summary: %v", err)
	}
}

func TestMemoryConcernUsesRevisionAndVisibleEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:goal", "owner", "我要准备 Go 面试")
	facts, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 要准备 Go 面试")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "准备 Go 面试", State: "active", Reason: "owner goal", SourceID: source.ID, CreatedAt: memoryTestNow, UpdatedAt: memoryTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	next := c
	next.State = "waiting"
	next.Reason = "等待面试安排"
	next.RecordIDs = []string{facts[0].ID}
	next.ReviewAt = memoryTestNow.Add(48 * time.Hour)
	next.UpdatedAt = memoryTestNow.Add(time.Hour)
	saved, err := s.SaveConcern(ctx, next, c.Revision)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("save=%+v %v", saved, err)
	}
	listed, err := s.ListConcerns(ctx, true)
	if err != nil || len(listed) != 1 {
		t.Fatalf("concerns=%+v err=%v", listed, err)
	}
	if got := listed[0]; got.Reason != next.Reason || got.State != "waiting" || !slices.Equal(got.RecordIDs, next.RecordIDs) || !got.ReviewAt.Equal(next.ReviewAt) || !got.CreatedAt.Equal(memoryTestNow) || !got.UpdatedAt.Equal(next.UpdatedAt) {
		t.Fatalf("listed concern = %+v", got)
	}
	if _, err := s.SaveConcern(ctx, next, c.Revision); !errors.Is(err, core.ErrMemoryConflict) {
		t.Fatalf("stale update: %v", err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记面试计划")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{source.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListConcerns(ctx, true); err != nil || len(got) != 0 {
		t.Fatalf("concerns=%+v err=%v", got, err)
	}
}

func TestMemoryForgetInferencePreservesOwnerEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:sport", "owner", "我周六去过游泳馆")
	facts, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 周六去过游泳馆")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	insights, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "insight", Content: "owner 可能喜欢游泳", Parents: []string{facts[0].ID}}, Reason: "tentative interpretation"}}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记喜欢游泳这个猜想")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{insights[0].ID}, RequestSourceID: request.ID, Reason: "withdraw inference", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadMemory(ctx, facts[0].ID); err != nil {
		t.Fatalf("source fact lost: %v", err)
	}
	if _, err := s.LoadMemorySource(ctx, source.ID); err != nil {
		t.Fatalf("owner evidence lost: %v", err)
	}
	if _, err := s.ReadMemory(ctx, insights[0].ID); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("inference survived: %v", err)
	}
}

func TestMemoryKnownAtSearchUsesHistoricalRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:wording", "owner", "我喜欢游泳，也喜欢跑步")
	first, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 喜欢游泳")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	revised := memoryFact(source, "owner 喜欢跑步")
	revised.Draft.ID = first[0].ID
	revised.ExpectedRevision = 1
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{revised}, Now: memoryTestNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	before, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "游泳", KnownAt: memoryTestNow.Add(time.Minute), Mode: "history"})
	if err != nil || len(before) != 1 {
		t.Fatalf("historical search=%+v %v", before, err)
	}
	after, err := s.SearchMemoryText(ctx, core.MemoryQuery{Query: "游泳", KnownAt: memoryTestNow.Add(2 * time.Hour), Mode: "history"})
	if err != nil || len(after) != 0 {
		t.Fatalf("later search=%+v %v", after, err)
	}
}

func TestScheduledInputInheritsSourceExclusions(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, db, "owner:schedule-source", "owner", "记忆中的事项")
	if _, err := db.CreateSession(ctx, "scheduled", "schedule"); err != nil {
		t.Fatal(err)
	}
	input, err := db.AppendSessionMessage(ctx, "scheduled", 1, "wake", "夜思引用记忆中的事项", "night-run")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.LinkRunInputSources(ctx, "night-run", []string{source.ID}); err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, db, "owner:forget-schedule", "owner", "忘记这个事项")
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{source.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoadMemorySource(ctx, fmt.Sprintf("message:%d", input.ID)); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("scheduled input remained visible: %v", err)
	}
}

func TestMemoryForgetOldEvidenceHidesHistoricalRevision(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	old := seedMemorySource(t, db, "old-evidence", "owner", "原先房间叫月光小站")
	first, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(old, old.Content)}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	current := seedMemorySource(t, db, "new-evidence", "owner", "现在房间叫书房")
	change := memoryFact(current, current.Content)
	change.Draft.ID = first[0].ID
	change.ExpectedRevision = 1
	if _, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, db, "forget-old-evidence", "owner", "忘记原来的私密称呼")
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{old.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	read, err := db.ReadMemory(ctx, first[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Record.Content != current.Content {
		t.Fatalf("independent current revision lost: %+v", read.Record)
	}
	for _, version := range read.Versions {
		if strings.Contains(version.Content, "月光小站") {
			t.Fatal("forgotten historical revision leaked")
		}
	}
	for _, query := range []core.MemoryQuery{{Query: "月光小站", Mode: "history", KnownAt: memoryTestNow.Add(time.Minute)}, {Mode: "history", KnownAt: memoryTestNow.Add(time.Minute)}} {
		got, err := db.ListMemoryRecords(ctx, query)
		if err != nil || len(got) != 0 {
			t.Fatalf("historical visibility %+v %v", got, err)
		}
		got, err = db.SearchMemoryText(ctx, query)
		if err != nil || len(got) != 0 {
			t.Fatalf("historical search %+v %v", got, err)
		}
	}
}

func TestForgetEvidenceRequeuesSurvivingParentsForConsolidation(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	first := seedMemorySource(t, db, "evidence-a", "owner", "我周末在家做饭")
	second := seedMemorySource(t, db, "evidence-b", "owner", "我每周购买新鲜蔬菜")
	facts, err := db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(first, first.Content), memoryFact(second, second.Content)}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "insight", Content: "周末饮食安排偏向自己准备", Parents: []string{facts[0].ID, facts[1].ID}}, Reason: "related evidence"}}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	for {
		job, err := db.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "")
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if err := db.FinishMemoryJob(ctx, *job, "processed", memoryTestNow); err != nil {
			t.Fatal(err)
		}
	}
	request := seedMemorySource(t, db, "forget-a", "owner", "忘记周末做饭这件事")
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{facts[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	job, err := db.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "consolidate")
	if err != nil || job == nil || job.ObjectID != facts[1].ID {
		t.Fatalf("surviving evidence review %+v %v", job, err)
	}
	if _, err := db.ReadMemory(ctx, facts[1].ID); err != nil {
		t.Fatalf("surviving fact lost: %v", err)
	}
}
