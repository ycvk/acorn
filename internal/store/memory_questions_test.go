package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// askedSource appends the owner's message to a conversation and registers
// it as a source of the run it started.
func askedSource(t *testing.T, s *Store, session, run, text string) core.MemorySource {
	t.Helper()
	ctx := context.Background()
	message, err := s.AppendSessionMessage(ctx, session, 1, "user", text, run)
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.RegisterMemorySource(ctx, core.MemorySource{
		ID: core.MessageSourceID(message.ID), Kind: "message", ObjectID: strconv.FormatInt(message.ID, 10), Version: "1",
		Speaker: "owner", SessionID: session, RunID: run, RecordedAt: memoryTestNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// foundSource records a tool result of a run in a conversation and registers
// it as a source; sourceSession is the conversation the source itself names.
func foundSource(t *testing.T, s *Store, session, sourceSession, run string) core.MemorySource {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: run, SessionID: session, Input: "check"}); err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, run, "tool.call.succeeded", map[string]any{"tool_name": "web_search", "output": "result"})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(event.Sequence, 10)
	source, err := s.RegisterMemorySource(ctx, core.MemorySource{
		ID: "event:" + id, Kind: "event", ObjectID: id, Version: "1", Speaker: "tool", SessionID: sourceSession, RunID: run, RecordedAt: memoryTestNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func watchQuestion(t *testing.T, s *Store, title string, source core.MemorySource, thread string, at time.Time) (core.MemoryConcern, error) {
	t.Helper()
	return s.SaveConcern(context.Background(), core.MemoryConcern{Title: title, Question: true, State: "active", Reason: "截至今天还没有", SourceID: source.ID, ThreadID: thread, ReviewAt: at.Add(48 * time.Hour), UpdatedAt: at}, 0)
}

// A watched question is opened from the owner's own message, has no outcome,
// and at most core.MaxWatchedQuestions are open at once; a revision keeps
// the mark whatever it says.
func TestAWatchedQuestionIsOpenedFromTheOwnersMessageUpToTheLimit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_q", "q"); err != nil {
		t.Fatal(err)
	}
	asked := askedSource(t, s, "thread_q", "run_ask", "Qwen 4 还没出吗")
	found := foundSource(t, s, "thread_q", "thread_q", "run_ask")
	if _, err := watchQuestion(t, s, "Qwen 4 发布了吗", found, "thread_q", memoryTestNow); err == nil || !strings.Contains(err.Error(), "opened from the owner's own message") {
		t.Fatalf("a question opened from a tool result: %v", err)
	}
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "整理清单", Question: true, Outcome: "一份清单", State: "active", Reason: "r", SourceID: asked.ID, ThreadID: "thread_q", UpdatedAt: memoryTestNow}, 0); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("a question with an outcome: %v", err)
	}
	first, err := watchQuestion(t, s, "Qwen 4 发布了吗", asked, "thread_q", memoryTestNow)
	if err != nil || !first.Question {
		t.Fatalf("first = %+v, %v", first, err)
	}
	for i := 2; i <= core.MaxWatchedQuestions; i++ {
		if _, err := watchQuestion(t, s, fmt.Sprintf("问题 %d", i), asked, "thread_q", memoryTestNow); err != nil {
			t.Fatalf("question %d: %v", i, err)
		}
	}
	if _, err := watchQuestion(t, s, "第六个问题", asked, "thread_q", memoryTestNow); err == nil || !strings.Contains(err.Error(), "5 questions are already watched") {
		t.Fatalf("a question past the limit: %v", err)
	}
	// A plain concern is not counted, and a revision cannot drop the mark.
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进 A", State: "active", Reason: "r", SourceID: asked.ID, ThreadID: "thread_q", UpdatedAt: memoryTestNow}, 0); err != nil {
		t.Fatal(err)
	}
	revised, err := s.SaveConcern(ctx, core.MemoryConcern{ID: first.ID, State: "active", Reason: "查过，仍未发布", SourceID: found.ID, UpdatedAt: memoryTestNow.Add(time.Hour)}, first.Revision)
	if err != nil || !revised.Question || revised.Title != "Qwen 4 发布了吗" {
		t.Fatalf("revised = %+v, %v", revised, err)
	}
	// Releasing one makes room for another.
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{ID: first.ID, State: "released", Reason: "owner 说不用了", SourceID: asked.ID, UpdatedAt: memoryTestNow.Add(2 * time.Hour)}, revised.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := watchQuestion(t, s, "第六个问题", asked, "thread_q", memoryTestNow); err != nil {
		t.Fatalf("a question after one was released: %v", err)
	}
	listed, err := s.ListConcerns(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	watched := 0
	for _, c := range listed {
		if c.Question {
			watched++
		}
	}
	if watched != core.MaxWatchedQuestions || len(listed) != core.MaxWatchedQuestions+1 {
		t.Fatalf("%d watched of %d listed", watched, len(listed))
	}
}

// A new answer found by a run in another conversation makes one commitment
// that wakes the agent where the owner asked, due at once. A run in that
// conversation that resolves the question is telling the owner already.
func TestANewAnswerFoundElsewhereIsToldWhereTheOwnerAsked(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"thread_q", "thread_checks"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
	}
	asked := askedSource(t, s, "thread_q", "run_ask", "Qwen 4 还没出吗")
	// The check's result names its run and leaves the conversation to it.
	found := foundSource(t, s, "thread_checks", "", "run_check")
	here := foundSource(t, s, "thread_q", "thread_q", "run_again")

	elsewhere, err := watchQuestion(t, s, "Qwen 4 发布了吗", asked, "thread_q", memoryTestNow)
	if err != nil {
		t.Fatal(err)
	}
	at := memoryTestNow.Add(24 * time.Hour)
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{ID: elsewhere.ID, State: "active", Reason: "查过，仍未发布", SourceID: found.ID, UpdatedAt: at.Add(-time.Hour)}, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListCommitments(ctx, true); err != nil || len(got) != 0 {
		t.Fatalf("commitments after an unchanged check = %+v, %v", got, err)
	}
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{ID: elsewhere.ID, State: "resolved", Reason: "今天发布了 https://example.com/qwen4", SourceID: found.ID, UpdatedAt: at}, 2); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListCommitments(ctx, true)
	if err != nil || len(got) != 1 {
		t.Fatalf("commitments = %+v, %v", got, err)
	}
	if c := got[0]; c.SessionID != "thread_q" || c.ConcernID != elsewhere.ID || c.Content != "Qwen 4 发布了吗" || c.SourceRunID != "run_check" || !c.WakeAt.Equal(at) || c.Recurrence != "" {
		t.Fatalf("commitment = %+v", c)
	}
	due, err := s.DueCommitments(ctx, at)
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %+v, %v", due, err)
	}

	sameThread, err := watchQuestion(t, s, "这个套餐还免费吗", asked, "thread_q", memoryTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConcern(ctx, core.MemoryConcern{ID: sameThread.ID, State: "resolved", Reason: "已经收费 https://example.com/price", SourceID: here.ID, UpdatedAt: at}, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListCommitments(ctx, true); err != nil || len(got) != 1 {
		t.Fatalf("commitments after an answer given where it was asked = %+v, %v", got, err)
	}
}

// Questions asked before the cut-off and still unanswered are released with
// a revision that keeps their source; newer ones and other concerns stay.
func TestStaleWatchedQuestionsAreReleased(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if _, err := s.CreateSession(ctx, "thread_q", "q"); err != nil {
		t.Fatal(err)
	}
	asked := askedSource(t, s, "thread_q", "run_ask", "这个还免费吗")
	old, err := watchQuestion(t, s, "旧问题", asked, "thread_q", memoryTestNow)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := watchQuestion(t, s, "新问题", asked, "thread_q", memoryTestNow.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.SaveConcern(ctx, core.MemoryConcern{Title: "跟进 A", State: "active", Reason: "r", SourceID: asked.ID, ThreadID: "thread_q", UpdatedAt: memoryTestNow}, 0)
	if err != nil {
		t.Fatal(err)
	}
	now := memoryTestNow.Add(15 * 24 * time.Hour)
	released, err := s.ReleaseStaleQuestions(ctx, now.Add(-14*24*time.Hour), now)
	if err != nil || len(released) != 1 {
		t.Fatalf("released = %+v, %v", released, err)
	}
	if r := released[0]; r.ID != old.ID || r.State != "released" || r.Revision != 2 || r.SourceID != asked.ID || !strings.Contains(r.Reason, "without a new answer") || !r.UpdatedAt.Equal(now) {
		t.Fatalf("released question = %+v", r)
	}
	listed, err := s.ListConcerns(ctx, true)
	if err != nil || len(listed) != 2 {
		t.Fatalf("still open = %+v, %v", listed, err)
	}
	for _, c := range listed {
		if c.ID != recent.ID && c.ID != plain.ID {
			t.Fatalf("still open = %+v", listed)
		}
	}
	if again, err := s.ReleaseStaleQuestions(ctx, now.Add(-14*24*time.Hour), now); err != nil || len(again) != 0 {
		t.Fatalf("released twice = %+v, %v", again, err)
	}
}

// A v39 database has no watched questions; its concerns, revisions and
// linked records survive the upgrade as concerns that watch none.
func TestSchemaFortyKeepsConcernsThatWatchNoQuestion(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	at := "2026-11-01T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 39)+`PRAGMA user_version = 39;
INSERT INTO memory_state (id, epoch) VALUES (1, 0);
INSERT INTO sessions (session_id, title, created_at, updated_at) VALUES ('thread_a', 'a', ?1, ?1);
INSERT INTO memory_sources (id, kind, object_id, version, speaker, session_id, run_id, recorded_at) VALUES ('message:1', 'message', '1', '1', 'owner', 'thread_a', '', ?1);
INSERT INTO memory_records (id, kind, content, scope, state, basis, revision, recorded_at, updated_at, valid_from) VALUES ('m_fact', 'fact', 'owner 在跟进 A', 'owner', 'active', 'stated', 1, ?1, ?1, ?1);
INSERT INTO memory_concerns (id, title, state, reason, source_id, revision, review_at, created_at, updated_at, thread_id, outcome) VALUES
 ('concern:a', 'A', 'waiting', 'progress', 'message:1', 2, ?1, ?1, ?1, 'thread_a', '一份清单');
INSERT INTO memory_concern_revisions (concern_id, revision, title, state, reason, source_id, at, outcome) VALUES
 ('concern:a', 1, 'A', 'active', 'goal', 'message:1', ?1, '一份清单'),
 ('concern:a', 2, 'A', 'waiting', 'progress', 'message:1', ?1, '一份清单');
INSERT INTO memory_concern_records (concern_id, revision, record_id) VALUES ('concern:a', 2, 'm_fact');
INSERT INTO commitments (content, state, session_id, source_run_id, concern_id, wake_at, recurrence, created_at, updated_at) VALUES ('看 A', 'scheduled', 'thread_a', '', 'concern:a', ?1, '', ?1, ?1);`, at); err != nil {
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
	if c := listed[0]; c.ID != "concern:a" || c.State != "waiting" || c.Revision != 2 || c.ThreadID != "thread_a" || c.Outcome != "一份清单" || c.Question || len(c.RecordIDs) != 1 || c.RecordIDs[0] != "m_fact" {
		t.Fatalf("upgraded concern = %+v", c)
	}
	var revisions, commitments int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memory_concern_revisions WHERE concern_id='concern:a' AND outcome='一份清单'`).Scan(&revisions); err != nil || revisions != 2 {
		t.Fatalf("upgraded revisions = %d, %v", revisions, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM commitments WHERE concern_id='concern:a'`).Scan(&commitments); err != nil || commitments != 1 {
		t.Fatalf("upgraded commitments = %d, %v", commitments, err)
	}
}
