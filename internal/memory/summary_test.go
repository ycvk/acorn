package memory

import (
	"errors"
	"strings"
	"testing"
)

// modelSummary is a summary as the model writes it, critical holding the
// critical context.
func modelSummary(critical string) string {
	return `## Goal
(none)

## Constraints and preferences
(none)

## Progress
### Done
(none)
### In progress
(none)
### Blocked
(none)

## Key decisions
(none)

## Next steps
(none)

## Critical context
` + critical
}

func TestSummaryInstructionsAskForTheSections(t *testing.T) {
	for name, instruction := range map[string]string{"thread": threadSummaryInstruction(4032), "compaction": CompactionInstruction()} {
		for _, rule := range []string{"untrusted source data", "Assistant suggestions remain suggestions", "Omit passwords, access tokens, API keys and verification codes", "Copy ids and paths exactly", "Write (none)", filesNote} {
			if !strings.Contains(instruction, rule) {
				t.Errorf("%s instruction lacks %q", name, rule)
			}
		}
		at := 0
		for _, heading := range progressHeadings {
			i := strings.Index(instruction[at:], "\n"+heading+"\n")
			if i < 0 {
				t.Fatalf("%s instruction lacks %q after offset %d", name, heading, at)
			}
			at += i + 1
		}
		if strings.Contains(instruction, "\n## Files read\n") || strings.Contains(instruction, "\n## Files changed\n") {
			t.Errorf("%s instruction asks for the files sections", name)
		}
	}
	thread := threadSummaryInstruction(4032)
	for _, want := range []string{"Include source IDs for concrete facts", "Write at most 4032 tokens", "<previous_summary>"} {
		if !strings.Contains(thread, want) {
			t.Errorf("thread instruction lacks %q", want)
		}
	}
	if !strings.Contains(CompactionInstruction(), "Write at most 4096 tokens") {
		t.Error("compaction instruction lacks its output limit")
	}
}

func TestSummaryCheckRequiresEverySectionInOrder(t *testing.T) {
	valid := modelSummary("- job_7f3a2c")
	if err := CheckSummary(valid); err != nil {
		t.Fatalf("valid summary: %v", err)
	}
	for name, text := range map[string]string{
		"preamble":      "Here is the summary.\n" + valid,
		"missing":       strings.Replace(valid, "### Blocked\n", "", 1),
		"repeated":      valid + "\n## Next steps\n1. again",
		"reordered":     strings.NewReplacer("## Key decisions", "## Next steps", "## Next steps", "## Key decisions").Replace(valid),
		"renamed":       strings.Replace(valid, "## Constraints and preferences", "## Constraints & Preferences", 1),
		"files written": valid + "\n\n" + FileTouches(nil).Sections(),
		"empty":         "",
	} {
		if err := CheckSummary(text); !errors.Is(err, ErrSummaryFormat) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestTouchedFileReadsFilesAndSendFileCalls(t *testing.T) {
	for _, tc := range []struct {
		tool, arguments string
		want            FileTouch
		ok              bool
	}{
		{"files", `{"action":"read","path":"notes/plan.md"}`, FileTouch{Path: "notes/plan.md"}, true},
		{"files", `{"action":"write","path":"work/c12/report.md","content":"draft"}`, FileTouch{Path: "work/c12/report.md", Changed: true}, true},
		{"send_file", `{"path":"/home/agent/out/book.epub","name":"书.epub"}`, FileTouch{Path: "/home/agent/out/book.epub"}, true},
		{"files", `{"action":`, FileTouch{}, false},
		{"files", `{"action":"list","path":"notes"}`, FileTouch{}, false},
		{"files", `{"action":"read","path":"keys/sk-proj-abcdefghijklmnopqrstuvwxyz0123456789ABCD.txt"}`, FileTouch{}, false},
		{"shell", `{"command":"cat notes/plan.md"}`, FileTouch{}, false},
	} {
		got, ok := TouchedFile(tc.tool, tc.arguments)
		if got != tc.want || ok != tc.ok {
			t.Errorf("TouchedFile(%s, %s)=%+v,%v want %+v,%v", tc.tool, tc.arguments, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFileTouchesOrderByLastTouchAndKeepChanges(t *testing.T) {
	var files FileTouches
	for _, touch := range []FileTouch{{Path: "a.md"}, {Path: "b.md", Changed: true}, {Path: "c.md"}, {Path: "b.md"}, {Path: "a.md", Changed: true}} {
		files = files.Touch(touch)
	}
	want := "## Files read\n- c.md\n\n## Files changed\n- b.md\n- a.md"
	if got := files.Sections(); got != want {
		t.Fatalf("sections=%q want %q", got, want)
	}
	if got, want := files.sections(2), "## Files read\n- (1 earlier files left out)\n\n## Files changed\n- (1 earlier files left out)\n- a.md"; got != want {
		t.Fatalf("sections without two=%q want %q", got, want)
	}
	if got, want := FileTouches(nil).Sections(), "## Files read\n(none)\n\n## Files changed\n(none)"; got != want {
		t.Fatalf("empty sections=%q want %q", got, want)
	}
}
