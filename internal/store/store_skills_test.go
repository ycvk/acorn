package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestSkillWritesAreRevisionsBasedOnTheCurrentOne(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	first, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.orders", Markdown: "v1", Author: core.SkillAuthorRelease, SeedSHA256: "seed-1", At: day})
	if err != nil || first.Revision != 1 || first.SeedSHA256 != "seed-1" {
		t.Fatalf("create = %+v, %v", first, err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.orders", Markdown: "blind", Author: core.SkillAuthorAgent, At: day}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("write over an existing skill without its revision: err = %v", err)
	}
	second, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.orders", Markdown: "v2", Retired: true, Author: core.SkillAuthorAgent, RunID: "run-1", At: day.Add(time.Hour), BaseRevision: 1})
	if err != nil || second.Revision != 2 || !second.Retired || second.SeedSHA256 != "seed-1" || !second.CreatedAt.Equal(day) || !second.UpdatedAt.Equal(day.Add(time.Hour)) {
		t.Fatalf("agent revision = %+v, %v", second, err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.orders", Markdown: "stale", Author: core.SkillAuthorAgent, At: day, BaseRevision: 1}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("stale write err = %v", err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.new", Markdown: "x", Author: core.SkillAuthorAgent, At: day, BaseRevision: 3}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("create with a base revision err = %v", err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.new", Markdown: "x", Author: "owner", At: day}); err == nil {
		t.Fatal("an unknown author must be rejected")
	}
	got, err := s.Skill(ctx, "skill.orders")
	if err != nil || got.Markdown != "v2" || got.Revision != 2 || !got.Retired || got.Author != core.SkillAuthorAgent {
		t.Fatalf("read = %+v, %v", got, err)
	}
	var revisions int
	var author, runID string
	if err := s.db.QueryRow(`SELECT COUNT(*), MAX(author), MAX(run_id) FROM skill_revisions WHERE id = 'skill.orders'`).Scan(&revisions, &author, &runID); err != nil || revisions != 2 || author != "release" || runID != "run-1" {
		t.Fatalf("revisions = %d %q %q, %v", revisions, author, runID, err)
	}
	if _, err := s.Skill(ctx, "skill.missing"); !errors.Is(err, core.ErrSkillNotFound) {
		t.Fatalf("missing skill err = %v", err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "skill.a", Markdown: "a", Author: core.SkillAuthorAgent, At: day}); err != nil {
		t.Fatal(err)
	}
	all, err := s.Skills(ctx)
	if err != nil || len(all) != 2 || all[0].ID != "skill.a" || all[1].ID != "skill.orders" {
		t.Fatalf("list = %+v, %v", all, err)
	}
}

func TestSkillLoadsSinceCountsSkillToolCalls(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	if err := s.CreateRun(ctx, core.RunCreateParams{RunID: "run_s", Input: "orders"}); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Second)
	for _, payload := range []map[string]any{
		{"tool_name": "skill", "arguments": `{"skill":"skill.orders"}`},
		{"tool_name": "skill", "arguments": `{"skill":"skill.orders"}`},
		{"tool_name": "skill", "arguments": `{"skill":"skill.capture.to.note"}`},
		{"tool_name": "skill", "arguments": `not json`},
		{"tool_name": "shell", "arguments": `{"skill":"skill.orders"}`},
	} {
		if _, err := s.AppendEvent(ctx, "run_s", "tool.call.started", payload); err != nil {
			t.Fatal(err)
		}
	}
	loads, err := s.SkillLoadsSince(ctx, before)
	if err != nil || len(loads) != 2 || loads["skill.orders"] != 2 || loads["skill.capture.to.note"] != 1 {
		t.Fatalf("loads = %v, %v", loads, err)
	}
	if later, err := s.SkillLoadsSince(ctx, time.Now().UTC().Add(time.Second)); err != nil || len(later) != 0 {
		t.Fatalf("loads after = %v, %v", later, err)
	}
}
