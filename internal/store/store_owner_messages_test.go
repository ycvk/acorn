package store

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// An owner message starts a run when its conversation has none going and
// waits while one runs or waits for the owner. The running run takes text
// messages in order, up to one that sends files; a waiting message is no
// memory source until a run takes it. The run's end starts the next run on
// the rest, and names it on its terminal event.
func TestOwnerMessagesWaitForTheRunOfTheirConversation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.CreateSession(ctx, "chat", "chat"); err != nil {
		t.Fatal(err)
	}
	send := func(content, runID string, files ...string) core.SentOwnerMessage {
		t.Helper()
		sent, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "chat", Content: content, FileIDs: files, RunID: runID})
		if err != nil {
			t.Fatalf("send %q: %v", content, err)
		}
		return sent
	}

	first := send("查一下天气", "run_1")
	if first.Started == nil || first.Queued != nil || first.RunID != "run_1" || first.Started.Input != "查一下天气" || len(first.Started.Messages) != 1 || first.Started.Messages[0].RunID != "run_1" {
		t.Fatalf("first = %+v", first)
	}
	waiting := send("只要气温", "run_unused")
	if waiting.Queued == nil || waiting.Started != nil || waiting.RunID != "run_1" {
		t.Fatalf("waiting = %+v", waiting)
	}
	if n := sourceCount(t, s, "chat"); n != 1 {
		t.Fatalf("memory sources while a message waits = %d, want only the first message", n)
	}
	photo := saveFile(t, s, core.ThreadFileWrite{SessionID: "chat", Origin: core.ThreadFileFromOwner, Name: "sky.png", Content: strings.NewReader("\x89PNG"), MaxBytes: 64})
	send("看看这张", "run_unused", photo.ID)
	send("还有明天", "run_unused")
	if _, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "chat", Content: "再看一次", FileIDs: []string{photo.ID}, RunID: "run_unused"}); err == nil {
		t.Fatal("a second queued message sends the same file")
	}

	taken, err := s.TakeQueuedMessages(ctx, "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(taken) != 1 || taken[0].Content != "只要气温" || taken[0].RunID != "run_1" || taken[0].TurnIndex != first.Started.TurnIndex {
		t.Fatalf("taken = %+v, want the text message before the one with files", taken)
	}
	if again, err := s.TakeQueuedMessages(ctx, "run_1"); err != nil || len(again) != 0 {
		t.Fatalf("second take = %+v, %v", again, err)
	}
	events, err := s.LoadEvents(ctx, "run_1")
	if err != nil || len(events) != 1 || events[0].Kind != "run.input" {
		t.Fatalf("events = %+v, %v", events, err)
	}
	if payload := events[0].Payload.(map[string]any); payload["text"] != "只要气温" || payload["queued_id"] == "" {
		t.Fatalf("run.input = %v", payload)
	}
	sources, err := s.RunMemorySources(ctx, "run_1")
	if err != nil || len(sources) != 2 {
		t.Fatalf("run sources = %+v, %v", sources, err)
	}

	if _, err := s.AppendSessionMessage(ctx, "chat", first.Started.TurnIndex, "assistant", "晴，20 度。", "run_1"); err != nil {
		t.Fatal(err)
	}
	_, next, err := s.FinishRun(ctx, core.RunEnd{RunID: "run_1", Status: core.RunStatusSucceeded, Output: "晴，20 度。", Event: "run.completed", Payload: map[string]any{"message": "晴"}, Next: "run_2"})
	if err != nil {
		t.Fatal(err)
	}
	if next == nil || next.RunID != "run_2" || next.Input != "看看这张\n\n还有明天" || len(next.Messages) != 2 || next.TurnIndex <= first.Started.TurnIndex {
		t.Fatalf("next = %+v", next)
	}
	events, err = s.LoadEvents(ctx, "run_1")
	if err != nil {
		t.Fatal(err)
	}
	if last := events[len(events)-1]; last.Kind != "run.completed" || last.Payload.(map[string]any)["next_run_id"] != "run_2" || last.Payload.(map[string]any)["message"] != "晴" {
		t.Fatalf("terminal event = %+v", last)
	}
	files, err := s.ThreadFiles(ctx, "chat")
	if err != nil || len(files) != 1 || files[0].MessageID != next.Messages[0].ID {
		t.Fatalf("files = %+v, %v", files, err)
	}
	if got := boundRuns(t, s, "chat"); got != "查一下天气:run_1 只要气温:run_1 晴，20 度。:run_1 看看这张:run_2 还有明天:run_2" {
		t.Fatalf("conversation = %q", got)
	}
	if queued, err := s.ListQueuedMessages(ctx, "chat"); err != nil || len(queued) != 0 {
		t.Fatalf("queued = %+v, %v", queued, err)
	}

	// A run that waits for the owner keeps messages waiting, and so does a
	// run of the conversation still going when another one ends.
	if err := s.MarkInterrupted(ctx, "run_2", ""); err != nil {
		t.Fatal(err)
	}
	if sent := send("算了不用了", "run_unused"); sent.Queued == nil || sent.RunID != "run_2" {
		t.Fatalf("message to a run waiting for the owner = %+v", sent)
	}
	if start, err := s.StartQueuedRun(ctx, "chat", "run_3"); err != nil || start != nil {
		t.Fatalf("start beside a waiting run = %+v, %v", start, err)
	}
	if err := s.DeleteThread(ctx, "chat"); err != nil {
		t.Fatal(err)
	}
	if sessions, err := s.ListQueuedSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("queued sessions after delete = %v, %v", sessions, err)
	}
}

// Messages left waiting by a run that ended without starting the next one
// go first in the run that starts on the conversation, from the sweep or with
// the owner's next message.
func TestWaitingMessagesStartTheNextRun(t *testing.T) {
	ctx := context.Background()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []string{"a", "b"} {
		if _, err := s.CreateSession(ctx, id, id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: id, Content: "开始", RunID: "run_" + id}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: id, Content: "还在吗", RunID: "run_unused"}); err != nil {
			t.Fatal(err)
		}
		if _, next, err := s.FinishRun(ctx, core.RunEnd{RunID: "run_" + id, Status: core.RunStatusFailed, Error: "boom", Event: "run.failed"}); err != nil || next != nil {
			t.Fatalf("finish without next = %+v, %v", next, err)
		}
	}
	sessions, err := s.ListQueuedSessions(ctx)
	if err != nil || !slices.Equal(sessions, []string{"a", "b"}) {
		t.Fatalf("queued sessions = %v, %v", sessions, err)
	}
	start, err := s.StartQueuedRun(ctx, "a", "run_a2")
	if err != nil || start == nil || start.Input != "还在吗" {
		t.Fatalf("swept start = %+v, %v", start, err)
	}
	sent, err := s.SendOwnerMessage(ctx, core.OwnerMessage{SessionID: "b", Content: "回我一下", RunID: "run_b2"})
	if err != nil || sent.Started == nil || sent.Started.Input != "还在吗\n\n回我一下" {
		t.Fatalf("next message = %+v, %v", sent, err)
	}
	if got := boundRuns(t, s, "b"); got != "开始:run_b 还在吗:run_b2 回我一下:run_b2" {
		t.Fatalf("conversation b = %q", got)
	}
	if sessions, err := s.ListQueuedSessions(ctx); err != nil || len(sessions) != 0 {
		t.Fatalf("queued sessions = %v, %v", sessions, err)
	}
}

func sourceCount(t *testing.T, s *Store, sessionID string) int {
	t.Helper()
	ids, err := s.ThreadMemorySourceIDs(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	return len(ids)
}
