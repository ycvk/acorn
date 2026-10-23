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
	job, err := s.ClaimMemoryJob(ctx, message.CreatedAt.Add(time.Minute), time.Minute, "")
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
	forgotten, err := s.ForgetMemory(ctx, core.MemoryForget{RecordIDs: []string{facts[0].ID}, RequestSourceID: request.ID, Reason: "owner request", Now: memoryTestNow.Add(time.Second)})
	if err != nil || forgotten.Exclusion.Epoch != 1 {
		t.Fatalf("forget=%+v %v", forgotten, err)
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

// Only the owner's own words queue extraction. Tool results and watch items
// are registered as sources through their triggers, external and tool sources
// through RegisterMemorySource, and none of them gets an extract job.
func TestMemoryExtractionQueuesOnlyOwnerSources(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_1", "memory"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_1", SessionID: "thread_1", TurnIndex: 1, Input: "查书"}); err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, "run_1", "tool.call.succeeded", map[string]any{"tool_name": "web_fetch", "output": "豆瓣短评：这本书一般"})
	if err != nil {
		t.Fatal(err)
	}
	w := addTestWatch(t, s, "feed", core.WatchActive, watchTestNow.Add(time.Hour))
	item := core.WatchItem{WatchID: w.ID, Key: "a", Title: "新文章", URL: "https://example.com/a", Status: core.WatchItemPending, SeenAt: watchTestNow}
	added, err := s.RecordWatchCheck(ctx, w, core.WatchCheck{At: watchTestNow, NextCheckAt: watchTestNow.Add(time.Hour), Status: core.WatchActive, Items: []core.WatchItem{item}})
	if err != nil || len(added) != 1 {
		t.Fatalf("watch items=%+v err=%v", added, err)
	}
	seedMemorySource(t, s, "note:1", "external", "一篇剪藏的文章")
	seedMemorySource(t, s, "shell:1", "tool", "$ ls")
	for _, id := range []string{fmt.Sprintf("event:%d", event.Sequence), fmt.Sprintf("watch:%d", added[0].ID), "note:1", "shell:1"} {
		source, err := s.LoadMemorySource(ctx, id)
		if err != nil || source.Content == "" || source.Speaker == "owner" {
			t.Fatalf("source %s=%+v err=%v", id, source, err)
		}
	}
	if job, err := s.ClaimMemoryJob(ctx, memoryTestNow.Add(time.Hour), time.Minute, "extract"); err != nil || job != nil {
		t.Fatalf("extraction queued for a source the owner did not speak: %+v err=%v", job, err)
	}
	message, err := s.AppendSessionMessage(ctx, "thread_1", 1, "user", "我不吃香菜", "run_1")
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimMemoryJob(ctx, message.CreatedAt.Add(time.Minute), time.Minute, "extract")
	if err != nil || job == nil || job.ObjectID != fmt.Sprintf("message:%d", message.ID) {
		t.Fatalf("owner message job=%+v err=%v", job, err)
	}
}

// A v7 database queued extraction for every tool result and watch item. The
// upgrade finishes that queued work and stops the triggers from queueing more.
func TestSchemaEightFinishesQueuedToolExtraction(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	at := "2026-10-01T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 7)+`PRAGMA user_version = 7;
INSERT INTO memory_state (id, epoch) VALUES (1, 0);
INSERT INTO memory_sources (id, kind, object_id, version, speaker, body, recorded_at) VALUES ('owner:1', 'import', '1', '1', 'owner', '我不吃香菜', ?1), ('shell:1', 'import', '2', '1', 'tool', '$ ls', ?1), ('note:1', 'import', '3', '1', 'external', '文章', ?1);
INSERT INTO memory_jobs (operation, object_id, version, state, available_at, created_at) VALUES ('extract', 'owner:1', '1', 'pending', ?1, ?1), ('extract', 'shell:1', '1', 'pending', ?1, ?1), ('extract', 'note:1', '1', 'failed', ?1, ?1);`, at); err != nil {
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
	ctx := context.Background()
	job, err := s.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "extract")
	if err != nil || job == nil || job.ObjectID != "owner:1" {
		t.Fatalf("owner job=%+v err=%v", job, err)
	}
	if next, err := s.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "extract"); err != nil || next != nil {
		t.Fatalf("tool extraction survived the upgrade: %+v err=%v", next, err)
	}
	var done int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_jobs WHERE state='done' AND completed_at<>'' AND error='source retained with its original attribution' AND object_id IN ('shell:1','note:1')`).Scan(&done); err != nil || done != 2 {
		t.Fatalf("finished jobs=%d err=%v", done, err)
	}
	if _, err := s.CreateSession(ctx, "thread_1", "memory"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_1", SessionID: "thread_1", TurnIndex: 1, Input: "查书"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(ctx, "run_1", "tool.call.succeeded", map[string]any{"tool_name": "shell", "output": "ok"}); err != nil {
		t.Fatal(err)
	}
	if next, err := s.ClaimMemoryJob(ctx, memoryTestNow, time.Minute, "extract"); err != nil || next != nil {
		t.Fatalf("upgraded trigger still queues tool extraction: %+v err=%v", next, err)
	}
}

// A v8 database kept memory jobs that retries gave up on the daily token
// budget as failed. The upgrade requeues them and leaves other failures alone.
func TestSchemaNineRequeuesBudgetFailedJobs(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	at := "2026-10-01T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 8)+`PRAGMA user_version = 8;
INSERT INTO memory_state (id, epoch) VALUES (1, 0);
INSERT INTO memory_jobs (operation, object_id, version, state, attempts, available_at, created_at, error) VALUES
 ('consolidate', 'fact:budget', '1', 'failed', 3, ?1, ?1, 'memory job 7 consolidate fact:budget version 1: `+core.ErrMemoryBudget.Error()+`'),
 ('consolidate', 'fact:broken', '1', 'failed', 3, ?1, ?1, 'memory model JSON: unexpected end of JSON input');`, at); err != nil {
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
	ctx := context.Background()
	now := time.Now().Add(time.Minute)
	job, err := s.ClaimMemoryJob(ctx, now, time.Minute, "consolidate")
	if err != nil || job == nil || job.ObjectID != "fact:budget" || job.Attempts != 1 {
		t.Fatalf("requeued job=%+v err=%v", job, err)
	}
	if next, err := s.ClaimMemoryJob(ctx, now, time.Minute, "consolidate"); err != nil || next != nil {
		t.Fatalf("a job failed for another reason was requeued: %+v err=%v", next, err)
	}
	var state string
	var attempts int
	if err := s.db.QueryRow(`SELECT state, attempts FROM memory_jobs WHERE object_id='fact:broken'`).Scan(&state, &attempts); err != nil || state != "failed" || attempts != 3 {
		t.Fatalf("other failure state=%s attempts=%d err=%v", state, attempts, err)
	}
}

// A v25 database kept memory jobs and news items given up on because Acorn
// stopped during their model calls. The upgrade requeues them and leaves
// other failures alone.
func TestSchemaTwentySixRequeuesWorkAStopCancelled(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	at := "2026-10-22T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 25)+`PRAGMA user_version = 25;
INSERT INTO memory_state (id, epoch) VALUES (1, 0);
INSERT INTO memory_jobs (operation, object_id, version, state, attempts, available_at, created_at, error) VALUES
 ('consolidate', 'fact:stopped', '1', 'failed', 3, ?1, ?1, 'memory job 9 consolidate fact:stopped version 1: memory consolidate: failed to create response: context canceled'),
 ('consolidate', 'fact:broken', '1', 'failed', 3, ?1, ?1, 'memory model JSON: unexpected end of JSON input');
INSERT INTO news_sections (name, position, created_at, updated_at) VALUES ('国际', 1, ?1, ?1);
INSERT INTO watches (id, name, kind, target, mode, section, interval_seconds, status, next_check_at, created_at, updated_at)
 VALUES (1, 'bbc', 'rss', 'https://bbc.test/feed', 'news', '国际', 3600, 'active', ?1, ?1, ?1);
INSERT INTO watch_items (id, watch_id, item_key, title, status, seen_at) VALUES (1, 1, 'stopped', 'stopped', 'pending', ?1), (2, 1, 'broken', 'broken', 'pending', ?1);
INSERT INTO news_scores (item_id, score, error, sorted_at) VALUES (1, 0, 'news sorting model call: context canceled', ?1), (2, 0, 'news sorting reply is not the JSON asked for', ?1);`, at); err != nil {
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
	ctx := context.Background()
	now := time.Now().Add(time.Minute)
	job, err := s.ClaimMemoryJob(ctx, now, time.Minute, "consolidate")
	if err != nil || job == nil || job.ObjectID != "fact:stopped" || job.Attempts != 1 {
		t.Fatalf("requeued job=%+v err=%v", job, err)
	}
	if next, err := s.ClaimMemoryJob(ctx, now, time.Minute, "consolidate"); err != nil || next != nil {
		t.Fatalf("a job failed for another reason was requeued: %+v err=%v", next, err)
	}
	rows, err := s.db.Query(`SELECT item_id FROM news_scores ORDER BY item_id`)
	if err != nil {
		t.Fatal(err)
	}
	var left []int64
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id int64
		if err := scan(&id); err != nil {
			return err
		}
		left = append(left, id)
		return nil
	}); err != nil || len(left) != 1 || left[0] != 2 {
		t.Fatalf("news scores left = %v, %v; only the stopped item is put back", left, err)
	}
}
