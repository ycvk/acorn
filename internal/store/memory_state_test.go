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
	forgotten, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{records[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	fresh := seedMemorySource(t, s, "owner:fresh", "owner", "我喜欢红色")
	next, err := s.CommitMemory(ctx, core.MemoryMutation{Changes: []core.MemoryChange{memoryFact(fresh, "owner 喜欢红色")}, Now: memoryTestNow.Add(time.Hour), Epoch: forgotten.Exclusion.Epoch})
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

func TestMemoryThreadSummaryKeepsItsSourcesAndForgetDropsOnlyCoveringSummaries(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	red := seedMemorySource(t, s, "owner:red", "owner", "我喜欢红色")
	tea := seedMemorySource(t, s, "owner:tea", "owner", "我早上喝茶")
	walk := seedMemorySource(t, s, "owner:walk", "owner", "我晚上散步")
	first := core.ThreadSummary{SessionID: "a", ThroughMessageID: 1, Content: "颜色和茶", SourceIDs: []string{tea.ID, red.ID, tea.ID}, UpdatedAt: memoryTestNow}
	if err := s.SaveThreadSummary(ctx, first); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadThreadSummary(ctx, "a")
	if err != nil || got == nil || !slices.Equal(got.SourceIDs, []string{red.ID, tea.ID}) {
		t.Fatalf("summary=%+v err=%v", got, err)
	}
	next := core.ThreadSummary{SessionID: "a", ThroughMessageID: 2, Content: "茶", SourceIDs: []string{tea.ID}, UpdatedAt: memoryTestNow.Add(time.Minute)}
	if err := s.SaveThreadSummary(ctx, next); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadThreadSummary(ctx, "a"); err != nil || got == nil || !slices.Equal(got.SourceIDs, []string{tea.ID}) || got.ThroughMessageID != 2 {
		t.Fatalf("replaced summary=%+v err=%v", got, err)
	}
	other := core.ThreadSummary{SessionID: "b", ThroughMessageID: 1, Content: "散步", SourceIDs: []string{walk.ID}, UpdatedAt: memoryTestNow}
	if err := s.SaveThreadSummary(ctx, other); err != nil {
		t.Fatal(err)
	}
	request := seedMemorySource(t, s, "owner:forget-tea", "owner", "忘记喝茶")
	if _, err := s.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{tea.ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadThreadSummary(ctx, "a"); err != nil || got != nil {
		t.Fatalf("forgotten summary=%+v err=%v", got, err)
	}
	if got, err := s.LoadThreadSummary(ctx, "b"); err != nil || got == nil || !slices.Equal(got.SourceIDs, []string{walk.ID}) {
		t.Fatalf("unrelated summary=%+v err=%v", got, err)
	}
	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM thread_summary_sources WHERE session_id='a'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("forgotten summary sources=%d err=%v", rows, err)
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
	if _, err := s.CreateSession(ctx, "thread_goal", "goal"); err != nil {
		t.Fatal(err)
	}
	c, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "准备 Go 面试", State: "active", Reason: "owner goal", SourceID: source.ID, ThreadID: "thread_goal", CreatedAt: memoryTestNow, UpdatedAt: memoryTestNow}, 0)
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
	if got := listed[0]; got.Reason != next.Reason || got.State != "waiting" || got.ThreadID != "thread_goal" || !slices.Equal(got.RecordIDs, next.RecordIDs) || !got.ReviewAt.Equal(next.ReviewAt) || !got.CreatedAt.Equal(memoryTestNow) || !got.UpdatedAt.Equal(next.UpdatedAt) {
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

func TestConcernBelongsToTheConversationItWasCreatedIn(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"thread_a", "thread_b"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	source := seedMemorySource(t, s, "owner:release", "owner", "帮我跟进 A 项目的发布")
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进 A 项目", State: "active", Reason: "owner goal", SourceID: source.ID, UpdatedAt: memoryTestNow}, 0); err == nil {
		t.Fatal("a concern without a conversation was saved")
	}
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进 A 项目", State: "active", Reason: "owner goal", SourceID: source.ID, ThreadID: "thread_gone", UpdatedAt: memoryTestNow}, 0); !errors.Is(err, core.ErrSessionNotFound) {
		t.Fatalf("concern in a missing conversation: %v", err)
	}
	c, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进 A 项目", State: "active", Reason: "owner goal", SourceID: source.ID, ThreadID: "thread_a", UpdatedAt: memoryTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	progress := seedMemorySource(t, s, "night:progress", "assistant", "A 项目发布了 v1.5")
	next := c
	next.Reason, next.SourceID, next.ThreadID, next.UpdatedAt = "v1.5 已发布", progress.ID, "thread_b", memoryTestNow.Add(time.Hour)
	if c, err = s.SaveConcern(ctx, next, c.Revision); err != nil || c.ThreadID != "thread_a" {
		t.Fatalf("revision moved the concern: %+v %v", c, err)
	}
	if _, err := s.AddCommitment(ctx, core.Commitment{Content: "明天看发布说明", SessionID: "thread_a", ConcernID: c.ID, WakeAt: memoryTestNow.Add(24 * time.Hour), CreatedAt: memoryTestNow}); err != nil {
		t.Fatal(err)
	}
	other, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进 B 项目", State: "active", Reason: "owner goal", SourceID: progress.ID, ThreadID: "thread_b", UpdatedAt: memoryTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ids, err := s.DeletedThreadConcernIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("concerns of deleted conversations before the delete = %v, %v", ids, err)
	}
	if err := s.DeleteThread(ctx, "thread_a"); err != nil {
		t.Fatal(err)
	}
	for _, active := range []bool{true, false} {
		if listed, err := s.ListConcerns(ctx, active); err != nil || len(listed) != 1 || listed[0].ID != other.ID {
			t.Fatalf("concerns after deleting a conversation (active=%t) = %+v, %v", active, listed, err)
		}
	}
	if ids, err := s.DeletedThreadConcernIDs(ctx); err != nil || !slices.Equal(ids, []string{c.ID}) {
		t.Fatalf("concerns of deleted conversations = %v, %v, want %s", ids, err, c.ID)
	}
	next = c
	next.Reason, next.UpdatedAt = "继续跟进", memoryTestNow.Add(2*time.Hour)
	if _, err := s.SaveConcern(ctx, next, c.Revision); !errors.Is(err, core.ErrMemoryExcluded) {
		t.Fatalf("revising a concern of a deleted conversation: %v", err)
	}
	if _, err := s.AddCommitment(ctx, core.Commitment{Content: "再看一次", SessionID: "thread_b", ConcernID: c.ID, WakeAt: memoryTestNow.Add(48 * time.Hour), CreatedAt: memoryTestNow}); !errors.Is(err, core.ErrMemoryNotFound) {
		t.Fatalf("commitment linked to a concern of a deleted conversation: %v", err)
	}
}

// An outcome stays with every revision until the owner changes it, and only
// the owner's confirmation resolves a concern that has one; a concern without
// an outcome is resolved by the owner or by a tool result.
func TestConcernWithAnOutcomeIsResolvedOnlyByTheOwner(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_doc", "doc"); err != nil {
		t.Fatal(err)
	}
	asked := seedMemorySource(t, s, "owner:ask", "owner", "把 X 近几个版本的变化整理成一份 Markdown")
	c, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "整理 X 的版本变化", Outcome: "一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更", State: "active", Reason: "owner 交代", SourceID: asked.ID, ThreadID: "thread_doc", UpdatedAt: memoryTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	revise := func(c core.MemoryConcern, state, reason string, source core.MemorySource, at time.Duration) (core.MemoryConcern, error) {
		next := c
		next.Outcome, next.State, next.Reason, next.SourceID, next.UpdatedAt = "", state, reason, source.ID, memoryTestNow.Add(at)
		return s.SaveConcern(ctx, next, c.Revision)
	}
	sent := seedMemorySource(t, s, "tool:send", "tool", "sent changes.md")
	if c, err = revise(c, "waiting", "changes.md 已发在对话里", sent, time.Hour); err != nil || c.Outcome != "一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更" {
		t.Fatalf("revision without an outcome = %+v, %v", c, err)
	}
	for _, source := range []core.MemorySource{sent, seedMemorySource(t, s, "assistant:done", "assistant", "做完了")} {
		if _, err := revise(c, "resolved", "已交付", source, 2*time.Hour); err == nil || !strings.Contains(err.Error(), "only by the owner's confirmation") {
			t.Fatalf("%s resolved a concern with an outcome: %v", source.Speaker, err)
		}
	}
	changed := seedMemorySource(t, s, "owner:change", "owner", "再加上 v1.8")
	next := c
	next.Outcome, next.State, next.Reason, next.SourceID, next.UpdatedAt = "一份 Markdown，列出 v1.4 到 v1.8 的不兼容变更", "active", "owner 要加上 v1.8", changed.ID, memoryTestNow.Add(3*time.Hour)
	if c, err = s.SaveConcern(ctx, next, c.Revision); err != nil {
		t.Fatal(err)
	}
	confirmed := seedMemorySource(t, s, "owner:ok", "owner", "看过了，可以")
	if c, err = revise(c, "resolved", "owner 确认了成品", confirmed, 4*time.Hour); err != nil || c.State != "resolved" || c.Revision != 4 {
		t.Fatalf("owner confirmation = %+v, %v", c, err)
	}
	rows, err := s.db.Query(`SELECT outcome FROM memory_concern_revisions WHERE concern_id=? ORDER BY revision`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := memoryStringRows(rows)
	if err != nil || !slices.Equal(outcomes, []string{"一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更", "一份 Markdown，列出 v1.4 到 v1.7 的不兼容变更", "一份 Markdown，列出 v1.4 到 v1.8 的不兼容变更", "一份 Markdown，列出 v1.4 到 v1.8 的不兼容变更"}) {
		t.Fatalf("revision outcomes = %q, %v", outcomes, err)
	}

	followed, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "等快递", State: "active", Reason: "owner 在等快递", SourceID: asked.ID, ThreadID: "thread_doc", UpdatedAt: memoryTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revise(followed, "resolved", "快递到了", seedMemorySource(t, s, "assistant:arrived", "assistant", "快递应该到了"), time.Hour); err == nil || !strings.Contains(err.Error(), "requires owner confirmation or execution evidence") {
		t.Fatalf("the agent's own words resolved a concern: %v", err)
	}
	if followed, err = revise(followed, "resolved", "物流显示已签收", seedMemorySource(t, s, "tool:track", "tool", "已签收"), time.Hour); err != nil || followed.State != "resolved" || followed.Outcome != "" {
		t.Fatalf("a tool result resolving a concern without an outcome = %+v, %v", followed, err)
	}
}

// A v16 database has no outcomes; its concerns, revisions and linked records
// survive the upgrade without one.
func TestSchemaSeventeenKeepsConcernsWithoutAnOutcome(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	at := "2026-10-01T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 16)+`PRAGMA user_version = 16;
INSERT INTO memory_state (id, epoch) VALUES (1, 0);
INSERT INTO sessions (session_id, title, created_at, updated_at) VALUES ('thread_a', 'a', ?1, ?1);
INSERT INTO memory_sources (id, kind, object_id, version, speaker, session_id, run_id, recorded_at) VALUES ('message:1', 'message', '1', '1', 'owner', 'thread_a', '', ?1);
INSERT INTO memory_records (id, kind, content, scope, state, basis, revision, recorded_at, updated_at, valid_from) VALUES ('m_fact', 'fact', 'owner 在跟进 A', 'owner', 'active', 'stated', 1, ?1, ?1, ?1);
INSERT INTO memory_concerns (id, title, state, reason, source_id, revision, review_at, created_at, updated_at, thread_id) VALUES
 ('concern:a', 'A', 'waiting', 'progress', 'message:1', 2, ?1, ?1, ?1, 'thread_a');
INSERT INTO memory_concern_revisions (concern_id, revision, title, state, reason, source_id, at) VALUES
 ('concern:a', 1, 'A', 'active', 'goal', 'message:1', ?1),
 ('concern:a', 2, 'A', 'waiting', 'progress', 'message:1', ?1);
INSERT INTO memory_concern_records (concern_id, revision, record_id) VALUES ('concern:a', 2, 'm_fact');`, at); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	listed, err := s.ListConcerns(context.Background(), true)
	if err != nil || len(listed) != 1 {
		t.Fatalf("concerns = %+v, %v", listed, err)
	}
	if c := listed[0]; c.ID != "concern:a" || c.State != "waiting" || c.Revision != 2 || c.ThreadID != "thread_a" || c.Outcome != "" || !slices.Equal(c.RecordIDs, []string{"m_fact"}) {
		t.Fatalf("upgraded concern = %+v", c)
	}
	var revisions int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_concern_revisions WHERE concern_id='concern:a' AND outcome=''`).Scan(&revisions); err != nil || revisions != 2 {
		t.Fatalf("upgraded revisions = %d, %v", revisions, err)
	}
}

// A v15 database has no conversation on its concerns. The upgrade takes it
// from the first revision's source, or from that source's run; a concern whose
// conversation is gone is no longer listed.
func TestSchemaSixteenRecordsConcernThreads(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	at := "2026-10-01T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 15)+`PRAGMA user_version = 15;
INSERT INTO memory_state (id, epoch) VALUES (1, 0);
INSERT INTO sessions (session_id, title, created_at, updated_at) VALUES ('thread_a', 'a', ?1, ?1), ('thread_b', 'b', ?1, ?1), ('thread_thoughts', 'Thoughts', ?1, ?1);
INSERT INTO runs (run_id, session_id, status, input_text, created_at) VALUES ('run_b', 'thread_b', 'succeeded', '整理笔记', ?1);
INSERT INTO memory_sources (id, kind, object_id, version, speaker, session_id, run_id, recorded_at) VALUES
 ('message:1', 'message', '1', '1', 'owner', 'thread_a', '', ?1),
 ('message:2', 'message', '2', '1', 'assistant', 'thread_thoughts', '', ?1),
 ('knowledge:n@1', 'knowledge', 'n.md', '1', 'assistant', '', 'run_b', ?1),
 ('message:9', 'message', '9', '1', 'owner', 'thread_gone', '', ?1);
INSERT INTO memory_concerns (id, title, state, reason, source_id, revision, created_at, updated_at) VALUES
 ('concern:a', 'A', 'active', 'progress', 'message:2', 2, ?1, ?1),
 ('concern:b', 'B', 'active', 'goal', 'knowledge:n@1', 1, ?1, ?1),
 ('concern:gone', 'gone', 'active', 'goal', 'message:9', 1, ?1, ?1);
INSERT INTO memory_concern_revisions (concern_id, revision, title, state, reason, source_id, at) VALUES
 ('concern:a', 1, 'A', 'active', 'goal', 'message:1', ?1),
 ('concern:a', 2, 'A', 'active', 'progress', 'message:2', ?1),
 ('concern:b', 1, 'B', 'active', 'goal', 'knowledge:n@1', ?1),
 ('concern:gone', 1, 'gone', 'active', 'goal', 'message:9', ?1);`, at); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	listed, err := s.ListConcerns(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	threads := map[string]string{}
	for _, c := range listed {
		threads[c.ID] = c.ThreadID
	}
	if len(threads) != 2 || threads["concern:a"] != "thread_a" || threads["concern:b"] != "thread_b" {
		t.Fatalf("concern threads = %v", threads)
	}
	var gone string
	if err := s.db.QueryRow(`SELECT thread_id FROM memory_concerns WHERE id='concern:gone'`).Scan(&gone); err != nil || gone != "thread_gone" {
		t.Fatalf("concern of a deleted conversation: thread %q, %v", gone, err)
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
