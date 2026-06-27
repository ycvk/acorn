package runtime

import (
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// TestFallbackSummaryRuneSafe verifies that the fallback summary truncates
// by rune count, not byte count, so CJK text is not split mid-character.
func TestFallbackSummaryRuneSafe(t *testing.T) {
	// 1000 Chinese characters = 3000 bytes. Truncation at 500 runes should
	// produce a valid string, not a byte-cut mid-character.
	long := strings.Repeat("中", 1000)
	got := fallbackSummary(long, core.RunStatusSucceeded)
	if !strings.HasPrefix(got, "succeeded: ") {
		t.Fatalf("expected status prefix, got %q", got[:20])
	}
	runes := []rune(got)
	// "succeeded: " (11 runes) + 500 runes + "..." (3 runes) = 514
	if len(runes) > 514 {
		t.Fatalf("fallback summary too long: %d runes", len(runes))
	}
	// Verify no replacement char (broken UTF-8 indicator)
	if strings.Contains(got, "\ufffd") {
		t.Fatal("fallback summary contains replacement character — byte-cut broke UTF-8")
	}
}

// TestFallbackSummaryShortText verifies that short input is not truncated.
func TestFallbackSummaryShortText(t *testing.T) {
	got := fallbackSummary("short task", core.RunStatusFailed)
	want := "failed: short task"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestFallbackSummaryEmptyInput verifies that empty combined input returns
// just the status.
func TestFallbackSummaryEmptyInput(t *testing.T) {
	got := fallbackSummary("", core.RunStatusSucceeded)
	if got != "succeeded" {
		t.Fatalf("got %q, want %q", got, "succeeded")
	}
}
