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

// twoSourceFact commits one current fact the owner stated twice, in sources
// "owner:a" and "owner:b".
func twoSourceFact(t *testing.T, s *Store) (core.MemoryRecord, core.MemorySource, core.MemorySource) {
	t.Helper()
	a := seedMemorySource(t, s, "owner:a", "owner", "我不吃香菜")
	b := seedMemorySource(t, s, "owner:b", "owner", "记住，我不吃香菜")
	change := memoryFact(a, "owner 不吃香菜")
	change.Draft.Evidence = append(change.Draft.Evidence, core.MemoryEvidence{SourceID: b.ID, Quote: b.Content, Relation: "supports"})
	records, err := s.CommitMemory(context.Background(), core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	return records[0], a, b
}

func revisionReason(t *testing.T, s *Store, id string, revision int64) string {
	t.Helper()
	var reason string
	if err := s.db.QueryRow(`SELECT reason FROM memory_revisions WHERE record_id=? AND revision=?`, id, revision).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	return reason
}

func TestMemoryForgetOneSourceKeepsFactOnIndependentEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	fact, a, b := twoSourceFact(t, s)
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记第一次聊天")
	at := memoryTestNow.Add(time.Hour)
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(exclusion.RecordIDs) != 0 {
		t.Fatalf("fact with independent evidence excluded: %v", exclusion.RecordIDs)
	}
	read, err := s.ReadMemory(ctx, fact.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := read.Record
	if got.State != "current" || got.Revision != 2 || got.Content != fact.Content || got.NeedsReview || !got.UpdatedAt.Equal(at) {
		t.Fatalf("stripped fact=%+v", got)
	}
	if !slices.Equal(got.Evidence, []core.MemoryEvidence{{SourceID: b.ID, Quote: b.Content, Relation: "supports"}}) || !slices.Equal(got.SourceIDs, []string{b.ID}) {
		t.Fatalf("evidence after forget=%+v sources=%v", got.Evidence, got.SourceIDs)
	}
	if reason := revisionReason(t, s, fact.ID, 2); reason != "evidence excluded by owner" {
		t.Fatalf("revision reason=%q", reason)
	}
	if len(read.Versions) != 1 || read.Versions[0].Revision != 2 {
		t.Fatalf("visible versions=%+v", read.Versions)
	}
	jobs, err := s.ReadyMemoryJobs(ctx, at, "embed", 10)
	if err != nil || !slices.ContainsFunc(jobs, func(j core.MemoryJob) bool { return j.ObjectID == fact.ID && j.Version == "2" }) {
		t.Fatalf("new revision not queued for embedding: %+v %v", jobs, err)
	}
	before, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", KnownAt: memoryTestNow.Add(time.Minute), Limit: 10})
	if err != nil || len(before) != 0 {
		t.Fatalf("stripped revision visible in history: %+v %v", before, err)
	}
	after, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "history", KnownAt: at.Add(time.Minute), Limit: 10})
	if err != nil || len(after) != 1 || after[0].Revision != 2 {
		t.Fatalf("new revision missing from history: %+v %v", after, err)
	}
	current, err := s.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", Limit: 10})
	if err != nil || len(current) != 1 || current[0].ID != fact.ID {
		t.Fatalf("current=%+v %v", current, err)
	}
}

func TestMemoryForgetOnlySourceExcludesWholeFact(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	source := seedMemorySource(t, s, "owner:only", "owner", "我不吃香菜")
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(source, "owner 不吃香菜")}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记那次聊天")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{source.ID}, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: memoryTestNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(exclusion.RecordIDs, []string{records[0].ID}) {
		t.Fatalf("excluded records=%v", exclusion.RecordIDs)
	}
	if _, err := s.ReadMemory(ctx, records[0].ID); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("read after forget: %v", err)
	}
	var revisions int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_revisions WHERE record_id=?`, records[0].ID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("excluded fact gained a revision: %d %v", revisions, err)
	}
}

func TestMemoryForgetStrippedParentMarksDerivedInsightForReview(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	fact, a, b := twoSourceFact(t, s)
	insights, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{{Draft: core.MemoryDraft{Kind: "insight", Content: "为 owner 点餐要避开香菜", Parents: []string{fact.ID}}, Reason: "meal preference"}}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	for {
		job, err := s.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "")
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		if err := s.FinishMemoryJob(ctx, *job, "processed", memoryTestNow); err != nil {
			t.Fatal(err)
		}
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记第一次聊天")
	at := memoryTestNow.Add(time.Hour)
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: at})
	if err != nil {
		t.Fatal(err)
	}
	if len(exclusion.RecordIDs) != 0 {
		t.Fatalf("stripped parent cascaded: %v", exclusion.RecordIDs)
	}
	parent, err := s.ReadMemory(ctx, fact.ID)
	if err != nil || parent.Record.Revision != 2 || parent.Record.NeedsReview {
		t.Fatalf("parent=%+v %v", parent.Record, err)
	}
	read, err := s.ReadMemory(ctx, insights[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := read.Record; !got.NeedsReview || got.Revision != 2 || got.State != "current" || !slices.Equal(got.Parents, []string{fact.ID}) || !slices.Equal(got.SourceIDs, []string{b.ID}) {
		t.Fatalf("derived insight=%+v", got)
	}
	jobs, err := s.ReadyMemoryJobs(ctx, at, "consolidate", 10)
	if err != nil || !slices.ContainsFunc(jobs, func(j core.MemoryJob) bool { return j.ObjectID == insights[0].ID && j.Version == "2" }) {
		t.Fatalf("derived insight not queued for consolidation: %+v %v", jobs, err)
	}
	if slices.ContainsFunc(jobs, func(j core.MemoryJob) bool { return j.ObjectID == fact.ID }) {
		t.Fatalf("stripped fact queued for consolidation: %+v", jobs)
	}
}

// Forgetting a fact excludes its quotes as fragments. Another fact citing the
// same words loses only that evidence, and a fact citing other words of the
// same source is untouched.
func TestMemoryForgetFragmentStripsOnlyMatchingEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	shared := seedMemorySource(t, s, "owner:shared", "owner", "我不吃香菜。我喜欢游泳。")
	other := seedMemorySource(t, s, "owner:other", "owner", "记住，我不吃香菜")
	target := memoryFact(shared, "owner 不吃香菜")
	target.Draft.Evidence[0].Quote = "我不吃香菜"
	both := memoryFact(shared, "owner 点菜时不要香菜")
	both.Draft.Evidence = []core.MemoryEvidence{{SourceID: shared.ID, Quote: "我不吃香菜", Relation: "supports"}, {SourceID: other.ID, Quote: other.Content, Relation: "supports"}}
	swim := memoryFact(shared, "owner 喜欢游泳")
	swim.Draft.Evidence[0].Quote = "我喜欢游泳"
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{target, both, swim}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记香菜偏好")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{records[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(exclusion.RecordIDs, []string{records[0].ID}) {
		t.Fatalf("excluded records=%v", exclusion.RecordIDs)
	}
	stripped, err := s.ReadMemory(ctx, records[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := stripped.Record; got.Revision != 2 || !slices.Equal(got.Evidence, []core.MemoryEvidence{{SourceID: other.ID, Quote: other.Content, Relation: "supports"}}) || !slices.Equal(got.SourceIDs, []string{other.ID}) {
		t.Fatalf("fact citing the forgotten words=%+v", got)
	}
	untouched, err := s.ReadMemory(ctx, records[2].ID)
	if err != nil || untouched.Record.Revision != 1 || len(untouched.Record.Evidence) != 1 {
		t.Fatalf("fact citing other words of the source=%+v %v", untouched.Record, err)
	}
}

func TestMemoryForgetDropsEntitiesAndAliasesOnlyTheExcludedEvidenceNames(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	a := seedMemorySource(t, s, "owner:alias", "owner", "小明也就是我同事张明")
	b := seedMemorySource(t, s, "owner:visit", "owner", "张明下周来访")
	change := core.MemoryChange{Reason: "owner statement", Draft: core.MemoryDraft{Kind: "fact", Content: "张明是 owner 的同事", Entities: []string{"张明", "小明", "owner"},
		Evidence: []core.MemoryEvidence{{SourceID: a.ID, Quote: a.Content, Relation: "supports"}, {SourceID: b.ID, Quote: b.Content, Relation: "supports"}},
		Aliases:  []core.MemoryEntityAlias{{Canonical: "张明", Alias: "小明", SourceID: a.ID, Quote: a.Content}}}}
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil || len(records[0].Aliases) != 1 {
		t.Fatalf("fact=%+v %v", records, err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记小明那次聊天")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: memoryTestNow.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadMemory(ctx, records[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := read.Record; got.Revision != 2 || !slices.Equal(got.Entities, []string{"owner", "张明"}) || len(got.Aliases) != 0 {
		t.Fatalf("stripped fact=%+v", got)
	}
	var mentions int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_mentions m JOIN memory_entities e ON e.id=m.entity_id WHERE m.record_id=? AND e.name='小明'`, records[0].ID).Scan(&mentions); err != nil || mentions != 0 {
		t.Fatalf("dropped entity still mentioned: %d %v", mentions, err)
	}
}

// A later consolidation reads sources through the memory view, so evidence
// the owner forgot cannot be written back onto the stripped record.
func TestMemoryConsolidationCannotRestoreStrippedEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	fact, a, b := twoSourceFact(t, s)
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记第一次聊天")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{a.ID}, RequestSourceID: request.ID, Reason: "owner deleted the conversation", Now: memoryTestNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	restore := core.MemoryChange{Reason: "consolidation", ExpectedRevision: 2, Draft: core.MemoryDraft{ID: fact.ID, Kind: "fact", Content: fact.Content, Entities: fact.Entities,
		Evidence: []core.MemoryEvidence{{SourceID: a.ID, Quote: a.Content, Relation: "supports"}, {SourceID: b.ID, Quote: b.Content, Relation: "supports"}}}}
	if _, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{restore}, Now: memoryTestNow.Add(2 * time.Hour), Epoch: exclusion.Epoch}); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("forgotten evidence restored: %v", err)
	}
	read, err := s.ReadMemory(ctx, fact.ID)
	if err != nil || read.Record.Revision != 2 || len(read.Record.Evidence) != 1 {
		t.Fatalf("record after rejected restore=%+v %v", read.Record, err)
	}
}

// Forgetting a fact excludes only the words it cites. A run that read the
// message before the forget read those words, so what it derives, before or
// after, is excluded with them. A run that reads the message afterwards sees
// it without them: what it derives stays readable, a summary covering the
// message reads back, and a later forget leaves both alone.
func TestMemoryFragmentExclusionSparesLaterDerivedSources(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_1", "memory"); err != nil {
		t.Fatal(err)
	}
	for i, run := range []string{"run_1", "run_2"} {
		if err := s.CreateRun(ctx, core.RunCreateParams{RunID: run, SessionID: "thread_1", TurnIndex: i + 1, Input: "聊聊"}); err != nil {
			t.Fatal(err)
		}
	}
	said, err := s.AppendSessionMessage(ctx, "thread_1", 1, "user", "我不吃香菜。我喜欢游泳。", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.LoadMemorySource(ctx, fmt.Sprintf("message:%d", said.ID))
	if err != nil {
		t.Fatal(err)
	}
	change := memoryFact(owner, "owner 不吃香菜")
	change.Draft.Evidence[0].Quote = "我不吃香菜"
	records, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{change}, Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMemorySnapshotRefs(ctx, "before", "run_1", []string{owner.ID}, nil, 0); err != nil {
		t.Fatal(err)
	}
	echoed, err := s.AppendSessionMessage(ctx, "thread_1", 1, "assistant", "记住了，你不吃香菜", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget", "owner", "忘记香菜偏好")
	exclusion, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{records[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.AppendEvent(ctx, "run_1", "tool.call.succeeded", map[string]any{"tool_name": "shell", "output": "已记下：不吃香菜"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{fmt.Sprintf("message:%d", echoed.ID), fmt.Sprintf("event:%d", late.Sequence)} {
		if _, err := s.LoadThreadSource(ctx, id); !errors.Is(err, core.ErrMemoryExcluded) {
			t.Fatalf("source %s of the run that read the forgotten words: %v", id, err)
		}
	}

	if err := s.SaveMemorySnapshotRefs(ctx, "after", "run_2", []string{owner.ID}, nil, exclusion.Epoch); err != nil {
		t.Fatal(err)
	}
	reply, err := s.AppendSessionMessage(ctx, "thread_1", 2, "assistant", "游泳很好", "run_2")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.AppendEvent(ctx, "run_2", "tool.call.succeeded", map[string]any{"tool_name": "shell", "output": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	derived := []string{fmt.Sprintf("message:%d", reply.ID), fmt.Sprintf("event:%d", result.Sequence)}
	summary := core.ThreadSummary{SessionID: "thread_1", ThroughMessageID: reply.ID, Content: "owner 喜欢游泳", SourceIDs: append([]string{owner.ID}, derived[0]), Epoch: exclusion.Epoch, UpdatedAt: memoryTestNow.Add(2 * time.Hour)}
	if err := s.SaveThreadSummary(ctx, summary); err != nil {
		t.Fatal(err)
	}
	readable := func(stage string) {
		t.Helper()
		for _, id := range derived {
			if _, err := s.LoadMemorySource(ctx, id); err != nil {
				t.Fatalf("%s: source %s derived from the remaining words: %v", stage, id, err)
			}
		}
		if got, err := s.LoadThreadSummary(ctx, "thread_1"); err != nil || got == nil {
			t.Fatalf("%s: summary=%+v err=%v", stage, got, err)
		}
	}
	readable("after the forget")

	tea := seedMemorySource(t, s, "owner:tea", "owner", "我早上喝茶")
	again := seedMemorySource(t, s, "owner:forget-tea", "owner", "忘记喝茶")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{tea.ID}, RequestSourceID: again.ID, Reason: "owner request", Now: memoryTestNow.Add(3 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	readable("after an unrelated forget")
}
