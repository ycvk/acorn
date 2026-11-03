package memory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

func TestHistoryUsesBudgetAndStableSourceIDs(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "history", "history"); err != nil {
		t.Fatal(err)
	}
	var last int64
	for i := range 20 {
		m, err := db.AppendSessionMessage(ctx, "history", i+1, "user", fmt.Sprintf("第%d条：请保留完整上下文。", i), "")
		if err != nil {
			t.Fatal(err)
		}
		last = m.ID
	}
	got, err := e.History(ctx, "history", 0, last, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 20 || len(got.SourceIDs) != 20 {
		t.Fatalf("history=%+v", got)
	}
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "forget-history", Kind: "request", ObjectID: "forget-history", Version: "1", Speaker: "owner", Body: "忘记第0条", RecordedAt: e.cfg.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: got.SourceIDs[:1], RequestSourceID: request.ID, Reason: "owner request", Now: e.cfg.Clock()}); err != nil {
		t.Fatal(err)
	}
	visible, err := e.History(ctx, "history", 0, last, 8192)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible.Messages) != 19 || strings.Contains(fmt.Sprint(visible.Messages), "第0条") {
		t.Fatalf("excluded history=%+v", visible)
	}
}

func TestHistoryChunksLongOlderMessageAndKeepsCurrentInput(t *testing.T) {
	model := &protocolModel{}
	e, db := testEngine(t, model, 0)
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "long-history", "history"); err != nil {
		t.Fatal(err)
	}
	long := "START_MARKER" + strings.Repeat("长期历史材料", 5000) + "END_MARKER"
	if _, err := db.AppendSessionMessage(ctx, "long-history", 1, "user", long, ""); err != nil {
		t.Fatal(err)
	}
	latest, err := db.AppendSessionMessage(ctx, "long-history", 2, "user", "继续上面的事情", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.History(ctx, "long-history", 0, latest.ID, 3000)
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary == nil || got.Summary.ThroughMessageID != latest.ID-1 || len(got.Messages) != 1 {
		t.Fatalf("history %+v", got)
	}
	if model.calls < 2 || !strings.Contains(strings.Join(model.seen, "\n"), "START_MARKER") || !strings.Contains(strings.Join(model.seen, "\n"), "END_MARKER") {
		t.Fatal("long source was not fully processed")
	}
	current := strings.Repeat("当前输入", 3000)
	if _, err := db.CreateSession(ctx, "large-current", "current"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AppendSessionMessage(ctx, "large-current", 1, "user", current, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err = e.History(ctx, "large-current", 0, msg.ID, 12000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content != current {
		t.Fatal("current input was clipped")
	}
}

func TestThreadSummaryBudgetScalesWithHistoryBudget(t *testing.T) {
	for budget, want := range map[int]int{2048: 512, 8191: 1024, 8192: 1024, 32768: 4096, 65536: 8192, 131072: 8192} {
		if got := threadSummaryBudget(budget); got != want {
			t.Errorf("threadSummaryBudget(%d)=%d want %d", budget, got, want)
		}
	}
}

func TestHistorySummaryUsesScaledBudgetAndRejectsOverflow(t *testing.T) {
	ctx := context.Background()
	older := strings.Repeat("更早的讨论", 12000)
	files := "\n\n" + FileTouches(nil).Sections()
	for _, tc := range []struct {
		name     string
		summary  string
		overflow bool
	}{
		{name: "within budget", summary: modelSummary(strings.Repeat("摘要", 2000))},
		{name: "over budget", summary: modelSummary(strings.Repeat("摘要", 4100)), overflow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &protocolModel{summary: tc.summary}
			e, db := testEngine(t, model, 0)
			filesTokens, err := e.cfg.Count(ctx, files)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.CreateSession(ctx, "scaled", "history"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendSessionMessage(ctx, "scaled", 1, "user", older, ""); err != nil {
				t.Fatal(err)
			}
			latest, err := db.AppendSessionMessage(ctx, "scaled", 2, "user", "接着说", "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.History(ctx, "scaled", 0, latest.ID, 32768)
			if tc.overflow {
				if want := fmt.Sprintf("thread summary exceeds %d-token budget", 4096-filesTokens); err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("err=%v want %q", err, want)
				}
			} else if err != nil || got.Summary == nil || got.Summary.Content != tc.summary+files {
				t.Fatalf("history=%+v err=%v", got, err)
			}
			if len(model.maxOutputs) == 0 {
				t.Fatal("summary was not generated")
			}
			for _, n := range model.maxOutputs {
				if want := 4096 - filesTokens - 64; n != want {
					t.Fatalf("max output %d, want %d", n, want)
				}
			}
		})
	}
}

func TestHistoryAfterLeavesOutEarlierMessagesAndTheSummary(t *testing.T) {
	e, db := testEngine(t, &protocolModel{}, 0)
	ctx := context.Background()
	if _, err := db.CreateSession(ctx, "standalone", "briefings"); err != nil {
		t.Fatal(err)
	}
	var last int64
	for i := range 3 {
		m, err := db.AppendSessionMessage(ctx, "standalone", i+1, "wake", fmt.Sprintf("[briefing 2026-10-0%d] morning briefing", i+1), "")
		if err != nil {
			t.Fatal(err)
		}
		last = m.ID
	}
	got, err := e.History(ctx, "standalone", last-1, last, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].ID != last || got.Summary != nil || len(got.SourceIDs) != 1 {
		t.Fatalf("standalone history=%+v", got)
	}
}

// toolCall is a call a test run made and the output it returned.
type toolCall struct{ tool, arguments, output string }

// recordRun records a run of a turn with its tool calls and ends it with
// status unless it is running.
func recordRun(t *testing.T, db *store.Store, session, runID string, turn int, status core.RunStatus, calls ...toolCall) {
	t.Helper()
	ctx := context.Background()
	if err := db.CreateRun(ctx, core.RunCreateParams{RunID: runID, SessionID: session, TurnIndex: turn, Input: "input"}); err != nil {
		t.Fatal(err)
	}
	for i, call := range calls {
		id := fmt.Sprintf("%s-call-%d", runID, i)
		if _, err := db.AppendEvent(ctx, runID, "tool.call.started", map[string]any{"tool_call_id": id, "tool_name": call.tool, "arguments": call.arguments}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AppendEvent(ctx, runID, "tool.call.succeeded", map[string]any{"tool_call_id": id, "tool_name": call.tool, "output": call.output}); err != nil {
			t.Fatal(err)
		}
	}
	if status != core.RunStatusRunning {
		if _, _, err := db.FinishRun(ctx, core.RunEnd{RunID: runID, Status: status, Event: "run.completed"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestThreadSummaryKeepsIdsPathsAndFilesAcrossUpdates(t *testing.T) {
	ctx := context.Background()
	padding := strings.Repeat("背景材料", 1250)
	first := modelSummary("- goal #9 runs the digest (message:1, message:2)")
	second := modelSummary("- goal #9; job_7f3a2c writes ~/.jobs/job_7f3a2c/; follow_up #4 waits for it (message:3, message:4)")
	model := &protocolModel{summaries: []string{first, second}}
	e, db := testEngine(t, model, 0)
	if _, err := db.CreateSession(ctx, "goal-thread", "goal"); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, m := range []struct {
		turn          int
		role, content string
	}{
		{1, "user", "把每天的 RSS 汇总写进 notes/rss-digest.md，不用付费 API。" + padding},
		{1, "assistant", "读了 notes/rss-digest.md，按 goal #9 来做。" + padding},
		{2, "wake", "[goal #9, run 2 of 50] Objective: 每天的 RSS 汇总\n" + padding},
		{2, "assistant", "开始了 job_7f3a2c，输出在 ~/.jobs/job_7f3a2c/，follow_up #4 等它结束。" + padding},
		{3, "wake", "[goal #9, run 3 of 50] Objective: 每天的 RSS 汇总"},
	} {
		msg, err := db.AppendSessionMessage(ctx, "goal-thread", m.turn, m.role, m.content, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, msg.ID)
	}
	recordRun(t, db, "goal-thread", "run-1", 1, core.RunStatusSucceeded,
		toolCall{"files", `{"action":"read","path":"notes/rss-digest.md"}`, `{"path":"/home/agent/notes/rss-digest.md","content":"digest"}`})
	recordRun(t, db, "goal-thread", "run-2", 2, core.RunStatusFailed,
		toolCall{"files", `{"action":"write","path":"work/c12/report.md","content":"draft"}`, `{"path":"/home/agent/work/c12/report.md","bytes":5}`},
		toolCall{"shell", `{"command":"cat notes/feeds.opml"}`, `{"job_id":"job_7f3a2c","state":"done"}`},
		toolCall{"files", `{"action":"write","path":"notes/declined.md","content":"x"}`, "The owner declined this files call. Do not retry it; continue without it or ask the owner what to do instead."},
		toolCall{"send_file", `{"path":"/home/agent/out/digest.epub"}`, `{"file_id":"f1","name":"digest.epub"}`},
		toolCall{"files", `{"action":"read","path":"notes/rss-digest.md"}`, `{"path":"/home/agent/notes/rss-digest.md","content":"digest"}`})
	recordRun(t, db, "goal-thread", "run-3", 3, core.RunStatusRunning,
		toolCall{"files", `{"action":"write","path":"notes/in-progress.md","content":"x"}`, `{"path":"/home/agent/notes/in-progress.md","bytes":1}`})
	got, err := e.History(ctx, "goal-thread", 0, ids[4], 3000)
	if err != nil {
		t.Fatal(err)
	}
	files := "\n\n## Files read\n- /home/agent/out/digest.epub\n- notes/rss-digest.md\n\n## Files changed\n- work/c12/report.md"
	if len(got.Messages) != 1 || got.Summary == nil || got.Summary.ThroughMessageID != ids[3] || got.Summary.Content != second+files {
		t.Fatalf("history=%+v summary=%+v", got, got.Summary)
	}
	if len(model.seen) != 2 || len(model.instructions) != 2 {
		t.Fatalf("summary calls=%d", len(model.seen))
	}
	for _, want := range []string{fmt.Sprintf("source=message:%d speaker=user", ids[0]), "notes/rss-digest.md", fmt.Sprintf("source=message:%d speaker=assistant", ids[1])} {
		if !strings.Contains(model.seen[0], want) {
			t.Errorf("first summary input lacks %q", want)
		}
	}
	if !strings.HasPrefix(model.seen[1], "<previous_summary>\n"+first+"\n\n## Files read\n- notes/rss-digest.md\n\n## Files changed\n(none)\n</previous_summary>\n") {
		t.Errorf("update does not start from the previous summary: %.400q", model.seen[1])
	}
	for _, want := range []string{fmt.Sprintf("source=message:%d speaker=wake", ids[2]), "[goal #9, run 2 of 50]", "job_7f3a2c", "~/.jobs/job_7f3a2c/", "follow_up #4"} {
		if !strings.Contains(model.seen[1], want) {
			t.Errorf("update input lacks %q", want)
		}
	}
	saved, err := db.LoadThreadSummary(ctx, "goal-thread")
	if err != nil || saved == nil || saved.Content != second+files {
		t.Fatalf("saved summary=%+v err=%v", saved, err)
	}
	calls, err := db.ThreadToolCalls(ctx, "goal-thread", ids[3], fileTools)
	if err != nil || len(calls) != 5 {
		t.Fatalf("calls=%+v err=%v", calls, err)
	}
	for _, i := range []int{0, 1, 3, 4} {
		if !slices.Contains(saved.SourceIDs, calls[i].SourceID) {
			t.Errorf("summary sources %v lack %s", saved.SourceIDs, calls[i].SourceID)
		}
	}
	if slices.Contains(saved.SourceIDs, calls[2].SourceID) {
		t.Errorf("summary depends on the declined call %s", calls[2].SourceID)
	}

	// Forgetting a call's result takes its file out of the rebuilt summary.
	request, err := db.RegisterMemorySource(ctx, core.MemorySource{ID: "forget-epub", Kind: "request", ObjectID: "forget-epub", Version: "1", Speaker: "owner", Body: "忘掉发过的 epub", RecordedAt: e.cfg.Clock()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ForgetMemory(ctx, core.MemoryForget{SourceIDs: []string{calls[3].SourceID}, RequestSourceID: request.ID, Reason: "owner request", Now: e.cfg.Clock()}); err != nil {
		t.Fatal(err)
	}
	if hidden, err := db.LoadThreadSummary(ctx, "goal-thread"); err != nil || hidden != nil {
		t.Fatalf("summary after forget=%+v err=%v", hidden, err)
	}
	rebuilt, err := e.History(ctx, "goal-thread", 0, ids[4], 3000)
	if err != nil || rebuilt.Summary == nil {
		t.Fatalf("rebuilt=%+v err=%v", rebuilt, err)
	}
	if want := "\n\n## Files read\n- notes/rss-digest.md\n\n## Files changed\n- work/c12/report.md"; !strings.HasSuffix(rebuilt.Summary.Content, want) || slices.Contains(rebuilt.Summary.SourceIDs, calls[3].SourceID) {
		t.Fatalf("rebuilt summary=%+v", rebuilt.Summary)
	}
}

func TestThreadSummaryFilesLeaveOutTheOldestPastTheirBudget(t *testing.T) {
	ctx := context.Background()
	e, db := testEngine(t, &protocolModel{}, 0)
	if _, err := db.CreateSession(ctx, "many-files", "files"); err != nil {
		t.Fatal(err)
	}
	msg, err := db.AppendSessionMessage(ctx, "many-files", 1, "user", "整理下载目录", "")
	if err != nil {
		t.Fatal(err)
	}
	var calls []toolCall
	for i := range 80 {
		calls = append(calls, toolCall{"files", fmt.Sprintf(`{"action":"read","path":"downloads/batch/file-%02d.txt"}`, i), `{"path":"x","content":"x"}`})
	}
	recordRun(t, db, "many-files", "run-1", 1, core.RunStatusSucceeded, calls...)
	text, sources, tokens, err := e.summaryFiles(ctx, "many-files", msg.ID, 750)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := e.cfg.Count(ctx, text); err != nil || n != tokens || n > 750/4 {
		t.Fatalf("files sections use %d tokens (reported %d) err=%v", n, tokens, err)
	}
	if !strings.Contains(text, "earlier files left out)") || !strings.Contains(text, "downloads/batch/file-79.txt") || strings.Contains(text, "downloads/batch/file-00.txt") || len(sources) != 80 {
		t.Fatalf("files sections=%s sources=%d", text, len(sources))
	}
}

func TestThreadSummaryAsksAgainForMissingSections(t *testing.T) {
	ctx := context.Background()
	older := strings.Repeat("更早的讨论", 2000)
	for _, tc := range []struct {
		name    string
		replies []string
		wantErr bool
	}{
		{name: "second reply has them", replies: []string{"只是一段话", modelSummary("- job_7f3a2c")}},
		{name: "no reply has them", replies: []string{"只是一段话", strings.Replace(modelSummary("- job_7f3a2c"), "### Blocked\n", "", 1)}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &protocolModel{summaries: tc.replies}
			e, db := testEngine(t, model, 0)
			if _, err := db.CreateSession(ctx, "format", "history"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.AppendSessionMessage(ctx, "format", 1, "user", older, ""); err != nil {
				t.Fatal(err)
			}
			latest, err := db.AppendSessionMessage(ctx, "format", 2, "user", "接着说", "")
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.History(ctx, "format", 0, latest.ID, 3000)
			if len(model.instructions) != 2 {
				t.Fatalf("summary attempts=%d", len(model.instructions))
			}
			saved, loadErr := db.LoadThreadSummary(ctx, "format")
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if tc.wantErr {
				if !errors.Is(err, ErrSummaryFormat) || saved != nil {
					t.Fatalf("err=%v saved=%+v", err, saved)
				}
				return
			}
			if err != nil || got.Summary == nil || got.Summary.Content != tc.replies[1]+"\n\n"+FileTouches(nil).Sections() || saved == nil {
				t.Fatalf("history=%+v err=%v", got, err)
			}
		})
	}
}
