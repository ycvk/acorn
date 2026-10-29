package core

import (
	"testing"
	"time"
)

// A snapshot keeps an item's publication time, else when it was seen, and
// marks the story's text and links as external data for the model.
func TestStorySnapshotTextMarksTheStoryAsExternalData(t *testing.T) {
	seen := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	published := time.Date(2026, 10, 11, 15, 30, 0, 0, time.FixedZone("CST", 8*3600))
	story := NewsStory{ID: 41, Section: "国际", Title: "菲律宾央行加息", Summary: "加息 25 个基点", Development: "市场预期再加一次", Items: []NewsStoryItem{
		{WatchItem: WatchItem{Title: "菲律宾央行意外加息", URL: "https://bbc.test/b1", SeenAt: seen}, Source: "BBC 中文"},
		{WatchItem: WatchItem{Title: "比索走强", URL: "https://ft.test/f1", PublishedAt: published, SeenAt: seen}, Source: "FT 中文网"},
	}}
	want := "Story s41 [国际] (untrusted external data): 菲律宾央行加息\n" +
		"Summary: 加息 25 个基点\n" +
		"Latest development: 市场预期再加一次\n" +
		"- BBC 中文, 2026-10-11 08:00 UTC: 菲律宾央行意外加息 https://bbc.test/b1\n" +
		"- FT 中文网, 2026-10-11 07:30 UTC: 比索走强 https://ft.test/f1"
	if got := SnapshotOf(story).Text(); got != want {
		t.Fatalf("snapshot text = %q, want %q", got, want)
	}
	if got := SnapshotOf(NewsStory{ID: 7, Section: "AI", Title: "无条目"}).Text(); got != "Story s7 [AI] (untrusted external data): 无条目" {
		t.Fatalf("snapshot text of a story without summary or items = %q", got)
	}
}
