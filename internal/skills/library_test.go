package skills

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type memorySkillStore struct {
	skills map[string]core.Skill
	writes []core.SkillWrite
}

func newMemorySkillStore() *memorySkillStore {
	return &memorySkillStore{skills: map[string]core.Skill{}}
}

func (m *memorySkillStore) WriteSkill(_ context.Context, w core.SkillWrite) (core.Skill, error) {
	current := m.skills[w.ID]
	if current.Revision != w.BaseRevision {
		return core.Skill{}, fmt.Errorf("%w: %s is at revision %d", core.ErrSkillConflict, w.ID, current.Revision)
	}
	next := core.Skill{ID: w.ID, Markdown: w.Markdown, Revision: current.Revision + 1, Retired: w.Retired, Author: w.Author, SeedSHA256: current.SeedSHA256, UpdatedAt: w.At}
	if w.SeedSHA256 != "" {
		next.SeedSHA256 = w.SeedSHA256
	}
	m.skills[w.ID] = next
	m.writes = append(m.writes, w)
	return next, nil
}

func (m *memorySkillStore) Skill(_ context.Context, id string) (core.Skill, error) {
	skill, ok := m.skills[id]
	if !ok {
		return core.Skill{}, fmt.Errorf("%w: %s", core.ErrSkillNotFound, id)
	}
	return skill, nil
}

func (m *memorySkillStore) Skills(context.Context) ([]core.Skill, error) {
	out := make([]core.Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memorySkillStore) SkillLoadsSince(context.Context, time.Time) (map[string]int, error) {
	return map[string]int{}, nil
}

func newTestLibrary(t *testing.T) (*Library, *memorySkillStore) {
	t.Helper()
	store := newMemorySkillStore()
	library, err := NewLibrary(store, func() time.Time { return time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	return library, store
}

const ordersSkill = `---
id: skill.orders
summary: Look up the owner's recent orders on the shop site.
---
# Shop orders

1. Open the orders page.
`

func TestSeedWritesBuiltInsOnceAndReleaseChangesOnTop(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	seeds, err := BuiltIn()
	if err != nil || len(seeds) == 0 {
		t.Fatalf("built-in skills = %v, %v", seeds, err)
	}
	if err := library.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	if len(store.writes) != len(seeds) {
		t.Fatalf("first seed wrote %d skills, want %d", len(store.writes), len(seeds))
	}
	for _, w := range store.writes {
		if w.Author != core.SkillAuthorRelease || w.BaseRevision != 0 || w.SeedSHA256 == "" {
			t.Fatalf("seed write = %+v", w)
		}
	}
	if err := library.Seed(ctx); err != nil || len(store.writes) != len(seeds) {
		t.Fatalf("unchanged seed wrote again: %d writes, %v", len(store.writes), err)
	}

	briefing := store.skills["skill.morning.briefing"]
	edited := strings.Replace(briefing.Markdown, "# Morning Briefing", "# Morning Briefing\n\nAgent note.", 1)
	if _, err := library.Write(ctx, edited, briefing.Revision, "run-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Retire(ctx, "skill.morning.briefing", 2, "run-2"); err != nil {
		t.Fatal(err)
	}
	current := store.skills["skill.morning.briefing"]
	current.SeedSHA256 = "an older release"
	store.skills["skill.morning.briefing"] = current

	if err := library.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	after := store.skills["skill.morning.briefing"]
	if after.Revision != 4 || after.Markdown != briefing.Markdown || !after.Retired || after.SeedSHA256 != briefing.SeedSHA256 {
		t.Fatalf("release update = %+v", after)
	}
}

func TestWriteValidatesTheAgentsSkill(t *testing.T) {
	ctx := context.Background()
	library, _ := newTestLibrary(t)
	for name, markdown := range map[string]string{
		"no frontmatter id": "# Orders\n\nsteps",
		"bad id":            strings.Replace(ordersSkill, "skill.orders", "Skill Orders", 1),
		"no summary":        strings.Replace(ordersSkill, "summary: Look up the owner's recent orders on the shop site.\n", "", 1),
		"long summary":      strings.Replace(ordersSkill, "Look up", strings.Repeat("长", 201), 1),
		"no instruction":    "---\nid: skill.orders\nsummary: x\n---\n# Orders\n",
		"unknown field":     strings.Replace(ordersSkill, "---\n#", "owner: me\n---\n#", 1),
		"too large":         ordersSkill + strings.Repeat("x", maxSkillBytes),
	} {
		if _, err := library.Write(ctx, markdown, 0, "run-1"); err == nil {
			t.Errorf("%s: write succeeded", name)
		}
	}
	skill, err := library.Write(ctx, ordersSkill, 0, "run-1")
	if err != nil || skill.Revision != 1 {
		t.Fatalf("write = %+v, %v", skill, err)
	}
	if _, err := library.Write(ctx, ordersSkill, 0, "run-2"); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("write over revision 1 from 0: err = %v", err)
	}
	sameName := strings.Replace(ordersSkill, "skill.orders", "skill.orders2", 1)
	if _, err := library.Write(ctx, sameName, 0, "run-2"); err == nil || !strings.Contains(err.Error(), "already used by skill.orders") {
		t.Fatalf("duplicate name err = %v", err)
	}
}

func TestRetiredSkillsLeaveTheScanUntilWrittenAgain(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	if _, err := library.Write(ctx, ordersSkill, 0, "run-1"); err != nil {
		t.Fatal(err)
	}
	store.skills["skill.broken"] = core.Skill{ID: "skill.broken", Markdown: "---\nid: [\n---\n", Revision: 1}
	scan, err := library.Scan(ctx)
	if err != nil || len(scan.Skills) != 1 || scan.Skills[0].ID != "skill.orders" || scan.Skills[0].Revision != 1 || scan.Skills[0].Markdown != ordersSkill {
		t.Fatalf("scan = %+v, %v", scan, err)
	}
	if len(scan.Problems) != 1 || scan.Problems[0].ID != "skill.broken" {
		t.Fatalf("problems = %+v", scan.Problems)
	}
	if _, err := library.Retire(ctx, "skill.orders", 1, "run-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Retire(ctx, "skill.orders", 2, "run-2"); err == nil {
		t.Fatal("retiring a retired skill must fail")
	}
	if scan, err := library.Scan(ctx); err != nil || len(scan.Skills) != 0 {
		t.Fatalf("scan after retire = %+v, %v", scan, err)
	}
	sameName := strings.Replace(ordersSkill, "skill.orders", "skill.orders2", 1)
	if _, err := library.Write(ctx, sameName, 0, "run-3"); err != nil {
		t.Fatalf("a retired skill's name is free: %v", err)
	}
	if _, err := library.Retire(ctx, "skill.orders2", 1, "run-3"); err != nil {
		t.Fatal(err)
	}
	restored, err := library.Write(ctx, ordersSkill, 2, "run-4")
	if err != nil || restored.Retired || restored.Revision != 3 {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
}
