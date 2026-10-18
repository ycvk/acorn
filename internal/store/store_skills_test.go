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
	first, err := s.WriteSkill(ctx, core.SkillWrite{ID: "orders", Markdown: "v1", Author: core.SkillAuthorRelease, SeedSHA256: "seed-1", At: day})
	if err != nil || first.Revision != 1 || first.SeedSHA256 != "seed-1" {
		t.Fatalf("create = %+v, %v", first, err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "orders", Markdown: "blind", Author: core.SkillAuthorAgent, At: day}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("write over an existing skill without its revision: err = %v", err)
	}
	second, err := s.WriteSkill(ctx, core.SkillWrite{ID: "orders", Markdown: "v2", Retired: true, Author: core.SkillAuthorAgent, RunID: "run-1", At: day.Add(time.Hour), BaseRevision: 1})
	if err != nil || second.Revision != 2 || !second.Retired || second.SeedSHA256 != "seed-1" || !second.CreatedAt.Equal(day) || !second.UpdatedAt.Equal(day.Add(time.Hour)) {
		t.Fatalf("agent revision = %+v, %v", second, err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "orders", Markdown: "stale", Author: core.SkillAuthorAgent, At: day, BaseRevision: 1}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("stale write err = %v", err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "new", Markdown: "x", Author: core.SkillAuthorAgent, At: day, BaseRevision: 3}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("create with a base revision err = %v", err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "new", Markdown: "x", Author: "someone", At: day}); err == nil {
		t.Fatal("an unknown author must be rejected")
	}
	got, err := s.Skill(ctx, "orders")
	if err != nil || got.Markdown != "v2" || got.Revision != 2 || !got.Retired || got.Author != core.SkillAuthorAgent {
		t.Fatalf("read = %+v, %v", got, err)
	}
	var revisions int
	var author, runID string
	if err := s.db.QueryRow(`SELECT COUNT(*), MAX(author), MAX(run_id) FROM skill_revisions WHERE id = 'orders'`).Scan(&revisions, &author, &runID); err != nil || revisions != 2 || author != "release" || runID != "run-1" {
		t.Fatalf("revisions = %d %q %q, %v", revisions, author, runID, err)
	}
	if _, err := s.Skill(ctx, "missing"); !errors.Is(err, core.ErrSkillNotFound) {
		t.Fatalf("missing skill err = %v", err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "a", Markdown: "a", Author: core.SkillAuthorAgent, At: day}); err != nil {
		t.Fatal(err)
	}
	all, err := s.Skills(ctx)
	if err != nil || len(all) != 2 || all[0].ID != "a" || all[1].ID != "orders" {
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
		{"tool_name": "skill", "arguments": `{"skill":"orders"}`},
		{"tool_name": "skill", "arguments": `{"skill":"orders"}`},
		{"tool_name": "skill", "arguments": `{"skill":"capture-to-note"}`},
		{"tool_name": "skill", "arguments": `not json`},
		{"tool_name": "shell", "arguments": `{"skill":"orders"}`},
	} {
		if _, err := s.AppendEvent(ctx, "run_s", "tool.call.started", payload); err != nil {
			t.Fatal(err)
		}
	}
	loads, err := s.SkillLoadsSince(ctx, before)
	if err != nil || len(loads) != 2 || loads["orders"] != 2 || loads["capture-to-note"] != 1 {
		t.Fatalf("loads = %v, %v", loads, err)
	}
	if later, err := s.SkillLoadsSince(ctx, time.Now().UTC().Add(time.Second)); err != nil || len(later) != 0 {
		t.Fatalf("loads after = %v, %v", later, err)
	}
}

func TestSkillRevisionsKeepTheirFiles(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	first, err := s.WriteSkill(ctx, core.SkillWrite{ID: "pdf", Markdown: "v1", Author: core.SkillAuthorOwner, At: day,
		Files: []core.SkillFile{{Path: "references/forms.md", Content: []byte("forms")}, {Path: "scripts/fill.py", Content: []byte("print(1)")}}})
	if err != nil || len(first.Files) != 2 || first.Files[1].Path != "scripts/fill.py" || first.Files[1].Size != 8 || len(first.Files[1].SHA256) != 64 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "pdf", Markdown: "v2", Author: core.SkillAuthorAgent, At: day, BaseRevision: 1,
		Files: []core.SkillFile{{Path: "scripts/fill.py", Content: []byte("print(2)")}}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Skill(ctx, "pdf")
	if err != nil || len(got.Files) != 1 || got.Author != core.SkillAuthorAgent {
		t.Fatalf("current = %+v, %v", got, err)
	}
	old, err := s.SkillFiles(ctx, "pdf", 1)
	if err != nil || len(old) != 2 || string(old[1].Content) != "print(1)" {
		t.Fatalf("revision 1 files = %+v, %v", old, err)
	}
	current, err := s.SkillFiles(ctx, "pdf", 2)
	if err != nil || len(current) != 1 || string(current[0].Content) != "print(2)" {
		t.Fatalf("revision 2 files = %+v, %v", current, err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "pdf", Markdown: "v2", Retired: true, Author: core.SkillAuthorOwner, At: day.Add(time.Hour), BaseRevision: 2,
		Files: []core.SkillFile{{Path: "scripts/fill.py", Content: []byte("print(2)")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "other", Markdown: "x", Author: core.SkillAuthorOwner, At: day}); err != nil {
		t.Fatal(err)
	}
	history, err := s.SkillRevisions(ctx, "pdf")
	if err != nil || len(history) != 3 {
		t.Fatalf("history = %+v, %v", history, err)
	}
	if h := history[0]; h.Revision != 3 || !h.Retired || h.Author != core.SkillAuthorOwner || !h.At.Equal(day.Add(time.Hour)) || h.Markdown != "v2" || len(h.Files) != 1 {
		t.Fatalf("newest revision = %+v", h)
	}
	if h := history[2]; h.Revision != 1 || h.Retired || h.Author != core.SkillAuthorOwner || h.Markdown != "v1" || len(h.Files) != 2 || h.Files[0].Path != "references/forms.md" {
		t.Fatalf("first revision = %+v", h)
	}
	second, err := s.SkillRevision(ctx, "pdf", 2)
	if err != nil || second.Author != core.SkillAuthorAgent || second.Markdown != "v2" || len(second.Files) != 1 || second.Files[0].SHA256 != got.Files[0].SHA256 {
		t.Fatalf("revision 2 = %+v, %v", second, err)
	}
	if _, err := s.SkillRevision(ctx, "pdf", 4); !errors.Is(err, core.ErrSkillNotFound) {
		t.Fatalf("revision 4 err = %v", err)
	}
	if _, err := s.SkillRevisions(ctx, "missing"); !errors.Is(err, core.ErrSkillNotFound) {
		t.Fatalf("history of a missing skill err = %v", err)
	}
}

func TestStagedSkillPackagesAreReadByHashAndExpire(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	pkg := core.SkillPackage{SHA256: "aaa", Name: "pdf", Markdown: "md", StagedAt: day, Files: []core.SkillFile{{Path: "scripts/fill.py", Content: []byte("x")}}}
	if err := s.StageSkillPackage(ctx, pkg, day.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.StageSkillPackage(ctx, pkg, day.Add(-time.Hour)); err != nil {
		t.Fatalf("staging the same package again: %v", err)
	}
	got, err := s.StagedSkillPackage(ctx, "aaa")
	if err != nil || got.Name != "pdf" || len(got.Files) != 1 || string(got.Files[0].Content) != "x" || !got.StagedAt.Equal(day) {
		t.Fatalf("staged = %+v, %v", got, err)
	}
	later := core.SkillPackage{SHA256: "bbb", Name: "docx", Markdown: "md", StagedAt: day.Add(48 * time.Hour)}
	if err := s.StageSkillPackage(ctx, later, day.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StagedSkillPackage(ctx, "aaa"); !errors.Is(err, core.ErrSkillPackageNotStaged) {
		t.Fatalf("an expired stage must go: %v", err)
	}
	if err := s.DeleteStagedSkillPackage(ctx, "bbb"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StagedSkillPackage(ctx, "bbb"); !errors.Is(err, core.ErrSkillPackageNotStaged) {
		t.Fatalf("a deleted stage must go: %v", err)
	}
}

func TestSchemaFiveSkillsAdoptTheAgentSkillsFormat(t *testing.T) {
	dir := t.TempDir()
	raw := openRaw(t, dir)
	old := "---\nid: skill.shop_orders\nsummary: Look up recent orders.\ntrigger_hints: [订单, orders]\nrequires:\n  tools: [shell]\n---\n\n# Shop orders\n\n1. Open the page.\n"
	at := "2026-10-01T00:00:00.000000000Z"
	if _, err := raw.Exec(readLayout(t, 5)+`PRAGMA user_version = 5;
INSERT INTO skills (id, markdown, revision, retired, seed_sha256, created_at, updated_at) VALUES ('skill.shop_orders', ?, 2, 1, '', ?, ?);
INSERT INTO skill_revisions (id, revision, markdown, markdown_sha256, retired, author, run_id, at) VALUES ('skill.shop_orders', 1, ?, 'x', 0, 'agent', 'run_1', ?), ('skill.shop_orders', 2, ?, 'x', 1, 'agent', 'run_2', ?);`,
		old, at, at, old, at, old, at); err != nil {
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
	got, err := s.Skill(context.Background(), "shop-orders")
	if err != nil || got.Revision != 3 || !got.Retired || got.Author != core.SkillAuthorRelease {
		t.Fatalf("adopted = %+v, %v", got, err)
	}
	want := "---\nname: shop-orders\ndescription: 'Look up recent orders. Use for: 订单; orders.'\nmetadata:\n    acorn-requires-tools: shell\n---\n# Shop orders\n\n1. Open the page.\n"
	if got.Markdown != want {
		t.Fatalf("markdown = %q", got.Markdown)
	}
	var revisions int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM skill_revisions WHERE id = 'shop-orders'`).Scan(&revisions); err != nil || revisions != 3 {
		t.Fatalf("revisions = %d, %v", revisions, err)
	}
}

func TestSkillSecretsAreSetPerSkillAndReplaced(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	day := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	if err := s.SetSkillSecret(ctx, "github", "GITHUB_TOKEN", "ghp_1", day); err == nil {
		t.Fatal("set a secret of a skill that does not exist")
	}
	if _, err := s.WriteSkill(ctx, core.SkillWrite{ID: "github", Markdown: "v1", Author: core.SkillAuthorOwner, At: day,
		Files: []core.SkillFile{{Path: "acorn-package.yaml", Content: []byte("mcp:\n  command: uvx\n")}}}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"ghp_1", "ghp_2"} {
		if err := s.SetSkillSecret(ctx, "github", "GITHUB_TOKEN", value, day); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetSkillSecret(ctx, "github", "OTHER", "", day); err == nil {
		t.Fatal("stored an empty secret")
	}
	got, err := s.SkillSecrets(ctx, "github")
	if err != nil || len(got) != 1 || got["GITHUB_TOKEN"] != "ghp_2" {
		t.Fatalf("secrets = %v, %v", got, err)
	}
	if none, err := s.SkillSecrets(ctx, "pdf"); err != nil || len(none) != 0 {
		t.Fatalf("secrets of another skill = %v, %v", none, err)
	}
	for range 2 {
		if err := s.DeleteSkillSecret(ctx, "github", "GITHUB_TOKEN"); err != nil {
			t.Fatal(err)
		}
	}
	if cleared, err := s.SkillSecrets(ctx, "github"); err != nil || len(cleared) != 0 {
		t.Fatalf("secrets after clearing = %v, %v", cleared, err)
	}
	manifest, err := s.SkillFile(ctx, "github", 1, "acorn-package.yaml")
	if err != nil || string(manifest) != "mcp:\n  command: uvx\n" {
		t.Fatalf("manifest = %q, %v", manifest, err)
	}
	if _, err := s.SkillFile(ctx, "github", 1, "SKILL.md"); err == nil {
		t.Fatal("read a file the revision does not hold")
	}
}

func TestTriggerSlotsEventsAndThreads(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 17, 9, 30, 0, 0, time.UTC)
	if err := s.PutTrigger(ctx, core.TriggerState{Skill: "price-watch", Name: "price", Schedule: "*/30 * * * *", NextAt: at}); err != nil {
		t.Fatal(err)
	}
	next := at.Add(30 * time.Minute)
	if err := s.ClaimTrigger(ctx, "price-watch", "price", at, next); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimTrigger(ctx, "price-watch", "price", at, next); !errors.Is(err, core.ErrTriggerNotDue) {
		t.Fatalf("a slot claimed twice: %v", err)
	}
	run := core.TriggerRun{At: at, NextAt: next, Status: core.TriggerActive, Events: []core.TriggerEvent{{Key: "a", Text: "first"}, {Key: "b", Text: "second"}}}
	added, err := s.RecordTriggerRun(ctx, "price-watch", "price", run)
	if err != nil || len(added) != 2 || added[0].ID == 0 || added[0].Status != core.TriggerEventPending {
		t.Fatalf("added = %+v, %v", added, err)
	}
	run.Events = append(run.Events, core.TriggerEvent{Key: "c", Text: "third"})
	run.Failures, run.LastError, run.Status = 5, "boom", core.TriggerFailing
	again, err := s.RecordTriggerRun(ctx, "price-watch", "price", run)
	if err != nil || len(again) != 1 || again[0].Key != "c" {
		t.Fatalf("a repeated key was added again: %+v, %v", again, err)
	}
	if _, err := s.RecordTriggerRun(ctx, "other", "price", core.TriggerRun{At: at, NextAt: next, Status: core.TriggerActive, Events: []core.TriggerEvent{{Key: "a", Text: "x"}}}); err != nil {
		t.Fatalf("keys are per skill and trigger: %v", err)
	}
	states, err := s.ListTriggers(ctx)
	if err != nil || len(states) != 1 || states[0].Status != core.TriggerFailing || states[0].Failures != 5 || !states[0].LastRunAt.Equal(at) || !states[0].NextAt.Equal(next) {
		t.Fatalf("states = %+v, %v", states, err)
	}
	source, err := s.LoadMemorySource(ctx, core.TriggerSourceID(added[0].ID))
	if err != nil || source.Kind != "trigger" || source.Speaker != "external" || source.Content != "price-watch/price a\nfirst" {
		t.Fatalf("source = %+v, %v", source, err)
	}
	if err := s.MarkTriggerEvents(ctx, []int64{added[0].ID}, core.TriggerEventWoken, "run_1"); err != nil {
		t.Fatal(err)
	}
	pending, err := s.ListTriggerEvents(ctx, core.TriggerEventPending, 10)
	if err != nil || len(pending) != 3 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if err := s.PutTrigger(ctx, core.TriggerState{Skill: "price-watch", Name: "price", Schedule: "0 * * * *", NextAt: next}); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.ListTriggers(ctx); states[0].Status != core.TriggerActive || states[0].Failures != 0 || states[0].Schedule != "0 * * * *" {
		t.Fatalf("a new schedule keeps old failures: %+v", states)
	}
	if err := s.RemoveTrigger(ctx, "price-watch", "price"); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.ListTriggers(ctx); len(states) != 0 {
		t.Fatalf("states after remove = %+v", states)
	}
	if thread, err := s.SkillThread(ctx, "price-watch"); err != nil || thread != "" {
		t.Fatalf("thread before the first run = %q, %v", thread, err)
	}
	if err := s.SetSkillThread(ctx, "price-watch", "thread_1"); err != nil {
		t.Fatal(err)
	}
	if thread, err := s.SkillThread(ctx, "price-watch"); err != nil || thread != "thread_1" {
		t.Fatalf("thread = %q, %v", thread, err)
	}
}
