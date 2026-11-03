package runtime

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memory"
)

// compactionReply is a compaction summary with every section, critical holding
// the critical context.
func compactionReply(critical string) *schema.AgenticMessage {
	return assistantMessage("## Goal\nDaily RSS digest under goal #9.\n\n## Constraints and preferences\n- No paid APIs.\n\n## Progress\n### Done\n- Read the digest note.\n### In progress\n- job_7f3a2c builds the digest.\n### Blocked\n(none)\n\n## Key decisions\n(none)\n\n## Next steps\n1. Check job_7f3a2c.\n\n## Critical context\n"+critical, nil)
}

func compact(t *testing.T, model *scriptedModel, messages []*schema.AgenticMessage) []*schema.AgenticMessage {
	t.Helper()
	counter, err := NewTokenCounter()
	if err != nil {
		t.Fatal(err)
	}
	mw, err := newCompactionMiddleware(context.Background(), model, 1, counter)
	if err != nil {
		t.Fatal(err)
	}
	_, state, err := mw.BeforeModelRewriteState(context.Background(), &adk.TypedChatModelAgentState[*schema.AgenticMessage]{Messages: messages}, &adk.TypedModelContext[*schema.AgenticMessage]{})
	if err != nil {
		t.Fatal(err)
	}
	return state.Messages
}

func TestCompactionKeepsTheFormatIdsAndFilesAcrossCompactions(t *testing.T) {
	first := compactionReply("- goal #9; job_7f3a2c writes ~/.jobs/job_7f3a2c/; follow_up #4 waits for it")
	second := compactionReply("- goal #9; job_7f3a2c; follow_up #4; job_99b1")
	model := &scriptedModel{replies: []*schema.AgenticMessage{first, second}}
	messages := []*schema.AgenticMessage{
		schema.SystemAgenticMessage("You are Acorn."),
		schema.UserAgenticMessage("[goal #9, run 3 of 50] Objective: daily RSS digest in notes/rss-digest.md"),
		toolCallReply("c1", "files", `{"action":"read","path":"notes/rss-digest.md"}`),
		toolResultMessage(`{"path":"/home/agent/notes/rss-digest.md","content":"digest"}`, "c1", "files"),
		toolCallReply("c2", "shell", `{"command":"make digest","wait_seconds":0}`),
		toolResultMessage(`{"job_id":"job_7f3a2c","state":"running"}`, "c2", "shell"),
		toolCallReply("c3", "files", `{"action":"write","path":"work/c12/report.md","content":"draft"}`),
		toolResultMessage(`{"path":"/home/agent/work/c12/report.md","bytes":5}`, "c3", "files"),
		toolCallReply("c4", "send_file", `{"path":"/home/agent/out/digest.epub"}`),
		toolResultMessage(`{"file_id":"f1","name":"digest.epub"}`, "c4", "send_file"),
		toolCallReply("c5", "files", `{"action":`),
		toolResultMessage("Tool files failed: bad arguments", "c5", "files"),
	}
	compacted := compact(t, model, messages)
	if len(compacted) != 2 || messageText(compacted[0]) != "You are Acorn." || compacted[1].Role != schema.AgenticRoleTypeUser {
		t.Fatalf("compacted=%v", compacted)
	}
	summary := messageText(compacted[1])
	wantFiles := "## Files read\n- notes/rss-digest.md\n- /home/agent/out/digest.epub\n\n## Files changed\n- work/c12/report.md"
	if !strings.Contains(summary, summaryText(first)+"\n\n"+wantFiles) {
		t.Fatalf("summary=%s", summary)
	}
	input := model.lastInput()
	if got := messageText(input[len(input)-1]); got != memory.CompactionInstruction() {
		t.Fatalf("compaction instruction=%s", got)
	}

	// The summary message reaches the next compaction through the run's
	// checkpoint.
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(compacted); err != nil {
		t.Fatal(err)
	}
	var resumed []*schema.AgenticMessage
	if err := gob.NewDecoder(&buf).Decode(&resumed); err != nil {
		t.Fatal(err)
	}
	more := append(resumed,
		toolCallReply("c6", "files", `{"action":"read","path":"work/c12/report.md"}`),
		toolResultMessage(`{"path":"/home/agent/work/c12/report.md","content":"draft"}`, "c6", "files"),
		toolCallReply("c7", "files", `{"action":"write","path":"notes/rss-digest.md","content":"digest v2"}`),
		toolResultMessage(`{"path":"/home/agent/notes/rss-digest.md","bytes":9}`, "c7", "files"),
		toolCallReply("c8", "files", `{"action":"read","path":"data/feeds.opml"}`),
		toolResultMessage(`{"path":"/home/agent/data/feeds.opml","content":"<opml/>"}`, "c8", "files"),
	)
	again := compact(t, model, more)
	if !strings.Contains(messageText(model.lastInput()[1]), "follow_up #4 waits for it") {
		t.Fatal("second compaction did not see the first summary")
	}
	summary = messageText(again[1])
	wantFiles = "## Files read\n- /home/agent/out/digest.epub\n- data/feeds.opml\n\n## Files changed\n- work/c12/report.md\n- notes/rss-digest.md"
	if len(again) != 2 || !strings.Contains(summary, summaryText(second)+"\n\n"+wantFiles) {
		t.Fatalf("second summary=%s", summary)
	}
}

func TestCompactionRejectsASummaryWithoutItsSections(t *testing.T) {
	for name, reply := range map[string]*schema.AgenticMessage{
		"prose":         assistantMessage("We read the note and started a job.", nil),
		"tool call":     toolCallReply("c1", "files", `{"action":"read","path":"notes/a.md"}`),
		"files written": assistantMessage(summaryText(compactionReply("- job_7f3a2c"))+"\n\n"+memory.FileTouches(nil).Sections(), nil),
	} {
		model := checkedCompactionModel{&scriptedModel{replies: []*schema.AgenticMessage{reply}}}
		if _, err := model.Generate(context.Background(), []*schema.AgenticMessage{schema.UserAgenticMessage("history")}); !errors.Is(err, memory.ErrSummaryFormat) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	model := checkedCompactionModel{&scriptedModel{replies: []*schema.AgenticMessage{compactionReply("- job_7f3a2c")}}}
	if _, err := model.Generate(context.Background(), []*schema.AgenticMessage{schema.UserAgenticMessage("history")}); err != nil {
		t.Fatalf("summary with its sections: %v", err)
	}
}
