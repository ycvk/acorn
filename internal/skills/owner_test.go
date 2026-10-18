package skills

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// TestOwnerChangesAreOwnerRevisionsOnTheReadRevision rolls back, retires and
// restores as the owner: each change is a new owner revision written only on
// the revision the owner read, and content goes through the checks every
// write passes.
func TestOwnerChangesAreOwnerRevisionsOnTheReadRevision(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t, "github")
	if _, err := library.Import(ctx, []core.SkillFile{{Path: SkillFileName, Content: []byte(ordersSkill)}, {Path: "references/sites.md", Content: []byte("shop.example\n")}}, false); err != nil {
		t.Fatal(err)
	}
	agent := strings.Replace(ordersSkill, "1. Open the orders page.", "1. Open the orders page.\n2. Export them.", 1)
	if _, err := library.Write(ctx, WriteRequest{Markdown: agent, Delete: []string{"references/sites.md"}, BaseRevision: 1, RunID: "run-1"}); err != nil {
		t.Fatal(err)
	}

	if _, err := library.Rollback(ctx, "shop-orders", 1, 1); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("rollback on a stale base: err = %v", err)
	}
	if _, err := library.Rollback(ctx, "shop-orders", 7, 2); !errors.Is(err, core.ErrSkillNotFound) {
		t.Fatalf("rollback to a missing revision: err = %v", err)
	}
	rolled, err := library.Rollback(ctx, "shop-orders", 1, 2)
	if err != nil || rolled.Revision != 3 || rolled.Author != core.SkillAuthorOwner || rolled.Retired || len(rolled.Files) != 1 {
		t.Fatalf("rollback = %+v, %v", rolled, err)
	}
	if rolled.Markdown != normalizeMarkdown(ordersSkill) {
		t.Fatalf("rolled back markdown = %q", rolled.Markdown)
	}
	if w := store.writes[len(store.writes)-1]; w.RunID != "" || w.BaseRevision != 2 {
		t.Fatalf("rollback write = %+v", w)
	}

	if _, err := library.Restore(ctx, "shop-orders", 3); !errors.Is(err, core.ErrSkillInvalid) {
		t.Fatalf("restore a skill in use: err = %v", err)
	}
	if _, err := library.RetireForOwner(ctx, "shop-orders", 2); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("retire on a stale base: err = %v", err)
	}
	retired, err := library.RetireForOwner(ctx, "shop-orders", 3)
	if err != nil || !retired.Retired || retired.Author != core.SkillAuthorOwner || retired.Revision != 4 {
		t.Fatalf("retire = %+v, %v", retired, err)
	}
	if _, err := library.RetireForOwner(ctx, "shop-orders", 4); !errors.Is(err, core.ErrSkillInvalid) {
		t.Fatalf("retire a retired skill: err = %v", err)
	}
	if _, err := library.Restore(ctx, "shop-orders", 3); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("restore on a stale base: err = %v", err)
	}
	restored, err := library.Restore(ctx, "shop-orders", 4)
	if err != nil || restored.Retired || restored.Author != core.SkillAuthorOwner || restored.Revision != 5 || restored.Markdown != rolled.Markdown || len(restored.Files) != 1 {
		t.Fatalf("restore = %+v, %v", restored, err)
	}

	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	old := core.SkillWrite{ID: "github", Markdown: "---\nname: github\ndescription: Read issues.\n---\nUse the tools.\n", Author: core.SkillAuthorAgent, At: at,
		Files: []core.SkillFile{{Path: ManifestFileName, Content: []byte("mcp:\n  command: uvx\n  secrets: [GITHUB_TOKEN]\n")}}}
	if _, err := store.WriteSkill(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Rollback(ctx, "github", 1, 1); !errors.Is(err, core.ErrSkillInvalid) || !strings.Contains(err.Error(), "secrets") {
		t.Fatalf("rollback to a manifest that does not parse: err = %v", err)
	}
	old.Files, old.BaseRevision = []core.SkillFile{{Path: ManifestFileName, Content: []byte("mcp:\n  command: uvx\n")}}, 1
	if _, err := store.WriteSkill(ctx, old); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Rollback(ctx, "github", 2, 2); !errors.Is(err, core.ErrSkillInvalid) || !strings.Contains(err.Error(), "mcp.providers") {
		t.Fatalf("rollback to a server named like a provider: err = %v", err)
	}
}

// TestEntriesReadEveryStoredSkill keeps retired skills and skills whose
// manifest no longer parses: runs see neither, the owner sees both.
func TestEntriesReadEveryStoredSkill(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	if _, err := store.WriteSkill(ctx, core.SkillWrite{ID: "fetch", Markdown: fetchSkill, Author: core.SkillAuthorAgent, At: at,
		Files: []core.SkillFile{{Path: ManifestFileName, Content: []byte("mcp:\n  command: uvx\n  secrets: [FETCH_TOKEN]\n")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Import(ctx, []core.SkillFile{{Path: SkillFileName, Content: []byte(ordersSkill)}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := library.RetireForOwner(ctx, "shop-orders", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteSkill(ctx, core.SkillWrite{ID: "broken", Markdown: "no frontmatter", Author: core.SkillAuthorAgent, At: at}); err != nil {
		t.Fatal(err)
	}

	entries, err := library.Entries(ctx)
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	broken, fetch, orders := entries[0], entries[1], entries[2]
	if broken.Spec.Name != "" || !strings.HasPrefix(broken.Problem, "parse skill:") {
		t.Fatalf("broken = %+v", broken)
	}
	if fetch.Spec.Name != "fetch" || fetch.Spec.Description == "" || fetch.Spec.MCP != nil || !strings.Contains(fetch.Problem, "field secrets not found") {
		t.Fatalf("fetch = %+v", fetch)
	}
	if !orders.Skill.Retired || orders.Problem != "" || orders.Spec.Name != "shop-orders" {
		t.Fatalf("orders = %+v", orders)
	}
	scan, err := library.Scan(ctx)
	if err != nil || len(scan.Skills) != 0 || len(scan.Problems) != 2 {
		t.Fatalf("scan = %+v, %v", scan, err)
	}

	version, err := library.Version(ctx, "fetch", 1)
	if err != nil || version.Problem != fetch.Problem || len(version.Files) != 1 || version.Revision.Author != core.SkillAuthorAgent {
		t.Fatalf("version = %+v, %v", version, err)
	}
	history, err := library.Revisions(ctx, "shop-orders")
	if err != nil || len(history) != 2 || !history[0].Retired || RevisionHash(history[0]) != RevisionHash(history[1]) {
		t.Fatalf("history = %+v, %v", history, err)
	}
}
