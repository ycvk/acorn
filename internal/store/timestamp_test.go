package store

import (
	"strings"
	"testing"
	"time"
)

func TestParseTimestampReadsEveryStoredForm(t *testing.T) {
	want := time.Date(2026, 10, 31, 9, 30, 0, 120000000, time.UTC)
	for _, value := range []string{formatTimestamp(want), "2026-10-31T09:30:00.12Z", "2026-10-31T17:30:00.12+08:00"} {
		got, err := parseTimestamp(value, "runs.created_at")
		if err != nil || !got.Equal(want) {
			t.Fatalf("parseTimestamp(%q) = %v, %v", value, got, err)
		}
	}
	if _, err := parseTimestamp("yesterday", "runs.created_at"); err == nil || !strings.Contains(err.Error(), "runs.created_at") {
		t.Fatalf("bad timestamp err = %v", err)
	}
}
