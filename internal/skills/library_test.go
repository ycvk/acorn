package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type storedRevision struct {
	skill core.Skill
	files []core.SkillFile
}

type memorySkillStore struct {
	revisions map[string][]storedRevision
	staged    map[string]core.SkillPackage
	writes    []core.SkillWrite
	secrets   map[string]map[string]string
}

func newMemorySkillStore() *memorySkillStore {
	return &memorySkillStore{revisions: map[string][]storedRevision{}, staged: map[string]core.SkillPackage{}, secrets: map[string]map[string]string{}}
}

func (m *memorySkillStore) current(id string) (storedRevision, bool) {
	revs := m.revisions[id]
	if len(revs) == 0 {
		return storedRevision{}, false
	}
	return revs[len(revs)-1], true
}

func (m *memorySkillStore) WriteSkill(_ context.Context, w core.SkillWrite) (core.Skill, error) {
	current, _ := m.current(w.ID)
	if current.skill.Revision != w.BaseRevision {
		return core.Skill{}, fmt.Errorf("%w: %s is at revision %d", core.ErrSkillConflict, w.ID, current.skill.Revision)
	}
	next := core.Skill{ID: w.ID, Markdown: w.Markdown, Revision: current.skill.Revision + 1, Retired: w.Retired, Author: w.Author, SeedSHA256: current.skill.SeedSHA256, UpdatedAt: w.At}
	if w.SeedSHA256 != "" {
		next.SeedSHA256 = w.SeedSHA256
	}
	for _, f := range w.Files {
		sum := sha256.Sum256(f.Content)
		next.Files = append(next.Files, core.SkillFileInfo{Path: f.Path, Size: int64(len(f.Content)), SHA256: hex.EncodeToString(sum[:])})
	}
	m.revisions[w.ID] = append(m.revisions[w.ID], storedRevision{skill: next, files: slices.Clone(w.Files)})
	m.writes = append(m.writes, w)
	return next, nil
}

func (m *memorySkillStore) Skill(_ context.Context, id string) (core.Skill, error) {
	current, ok := m.current(id)
	if !ok {
		return core.Skill{}, fmt.Errorf("%w: %s", core.ErrSkillNotFound, id)
	}
	return current.skill, nil
}

func (m *memorySkillStore) Skills(context.Context) ([]core.Skill, error) {
	var out []core.Skill
	for id := range m.revisions {
		current, _ := m.current(id)
		out = append(out, current.skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memorySkillStore) SkillFiles(_ context.Context, id string, revision int64) ([]core.SkillFile, error) {
	for _, rev := range m.revisions[id] {
		if rev.skill.Revision == revision {
			return slices.Clone(rev.files), nil
		}
	}
	return nil, fmt.Errorf("%w: %s revision %d", core.ErrSkillNotFound, id, revision)
}

func (m *memorySkillStore) SkillFile(ctx context.Context, id string, revision int64, path string) ([]byte, error) {
	files, err := m.SkillFiles(ctx, id, revision)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Path == path {
			return f.Content, nil
		}
	}
	return nil, fmt.Errorf("skill %s revision %d has no file %s", id, revision, path)
}

func (m *memorySkillStore) SetSkillSecret(_ context.Context, skill, name, value string, _ time.Time) error {
	if _, ok := m.current(skill); !ok {
		return fmt.Errorf("%w: %s", core.ErrSkillNotFound, skill)
	}
	if m.secrets[skill] == nil {
		m.secrets[skill] = map[string]string{}
	}
	m.secrets[skill][name] = value
	return nil
}

func (m *memorySkillStore) SkillSecrets(_ context.Context, skill string) (map[string]string, error) {
	out := map[string]string{}
	for name, value := range m.secrets[skill] {
		out[name] = value
	}
	return out, nil
}

func (m *memorySkillStore) SkillLoadsSince(context.Context, time.Time) (map[string]int, error) {
	return map[string]int{}, nil
}

func (m *memorySkillStore) StageSkillPackage(_ context.Context, pkg core.SkillPackage, _ time.Time) error {
	m.staged[pkg.SHA256] = pkg
	return nil
}

func (m *memorySkillStore) StagedSkillPackage(_ context.Context, sha string) (core.SkillPackage, error) {
	pkg, ok := m.staged[sha]
	if !ok {
		return core.SkillPackage{}, fmt.Errorf("%w: %s", core.ErrSkillPackageNotStaged, sha)
	}
	return pkg, nil
}

func (m *memorySkillStore) DeleteStagedSkillPackage(_ context.Context, sha string) error {
	delete(m.staged, sha)
	return nil
}

func newTestLibrary(t *testing.T, providers ...string) (*Library, *memorySkillStore) {
	t.Helper()
	store := newMemorySkillStore()
	library, err := NewLibrary(store, func() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }, providers)
	if err != nil {
		t.Fatal(err)
	}
	return library, store
}

const ordersSkill = `---
name: shop-orders
description: Look up the owner's recent orders on the shop site.
metadata:
  acorn-requires-tools: shell
---
# Shop orders

1. Open the orders page.
`

func TestParseFollowsTheAgentSkillsFormat(t *testing.T) {
	spec, err := Parse(ordersSkill)
	if err != nil || spec.Name != "shop-orders" || spec.Description == "" || !slices.Equal(spec.RequiresTools, []string{"shell"}) || !strings.HasPrefix(spec.Instruction, "# Shop orders") {
		t.Fatalf("spec = %+v, %v", spec, err)
	}
	withAll := "---\nname: pdf\ndescription: Fill PDF forms.\nlicense: Apache-2.0\ncompatibility: Needs Python 3\nallowed-tools: Bash(python:*)\nmetadata:\n  version: \"1.0\"\n---\nsteps\n"
	if spec, err := Parse(withAll); err != nil || spec.License != "Apache-2.0" || spec.AllowedTools != "Bash(python:*)" || spec.Metadata["version"] != "1.0" {
		t.Fatalf("all fields = %+v, %v", spec, err)
	}
	for name, markdown := range map[string]string{
		"no frontmatter":     "# Orders\n\nsteps",
		"unknown field":      strings.Replace(ordersSkill, "name: shop-orders", "name: shop-orders\nid: skill.orders", 1),
		"uppercase name":     strings.Replace(ordersSkill, "shop-orders", "Shop-Orders", 1),
		"leading hyphen":     strings.Replace(ordersSkill, "shop-orders", "-orders", 1),
		"double hyphen":      strings.Replace(ordersSkill, "shop-orders", "shop--orders", 1),
		"long name":          strings.Replace(ordersSkill, "shop-orders", strings.Repeat("a", 65), 1),
		"no description":     strings.Replace(ordersSkill, "description: Look up the owner's recent orders on the shop site.\n", "", 1),
		"long description":   strings.Replace(ordersSkill, "Look up", strings.Repeat("长", 1025), 1),
		"long compatibility": strings.Replace(ordersSkill, "metadata:", "compatibility: "+strings.Repeat("x", 501)+"\nmetadata:", 1),
	} {
		if _, err := Parse(markdown); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestBuiltInSkillsShipWithTheirFiles(t *testing.T) {
	builtIn, err := BuiltIn()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Package{}
	for _, pkg := range builtIn {
		byName[pkg.Spec.Name] = pkg
	}
	for _, name := range []string{"browser", "browser-skill", "capability-recall", "capture-to-note", "morning-briefing", "night-reflection", "skill-creator", "wander", "web-browser-research"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("built-in skill %s is missing", name)
		}
	}
	paths := func(pkg Package) []string {
		var out []string
		for _, f := range pkg.Files {
			out = append(out, f.Path)
		}
		return out
	}
	if got := paths(byName["browser-skill"]); !slices.Contains(got, "LICENSE") || !slices.Contains(got, "references/environment.md") {
		t.Fatalf("browser-skill files = %v", got)
	}
	if got := paths(byName["skill-creator"]); !slices.Equal(got, []string{"LICENSE.txt", "references/patterns.md"}) || byName["skill-creator"].Spec.License == "" {
		t.Fatalf("skill-creator files = %v", got)
	}
}

func TestFromEntriesFindsTheSkillInADirectoryOrZip(t *testing.T) {
	file := func(p, content string) core.SkillFile { return core.SkillFile{Path: p, Content: []byte(content)} }
	markdown, files, err := FromEntries([]core.SkillFile{
		file("skill-creator_prod/SKILL.md", ordersSkill),
		file("skill-creator_prod/scripts/run.py", "print(1)"),
		file("skill-creator_prod/.DS_Store", "x"),
		file("__MACOSX/skill-creator_prod/._SKILL.md", "x"),
		file("skill-creator_prod/references/._notes.md", "x"),
		file("skill-creator_prod/examples/inner/SKILL.md", "nested skill text"),
	})
	if err != nil || markdown != ordersSkill || len(files) != 2 || files[0].Path != "scripts/run.py" || files[1].Path != "examples/inner/SKILL.md" {
		t.Fatalf("package = %q %v, %v", markdown, files, err)
	}
	for name, entries := range map[string][]core.SkillFile{
		"no skill":      {file("README.md", "x")},
		"two skills":    {file("a/SKILL.md", ordersSkill), file("b/SKILL.md", ordersSkill)},
		"stray file":    {file("skill/SKILL.md", ordersSkill), file("notes.txt", "x")},
		"skill at root": nil,
	} {
		if name == "skill at root" {
			if md, _, err := FromEntries([]core.SkillFile{file("SKILL.md", ordersSkill), file("scripts/a.sh", "echo")}); err != nil || md != ordersSkill {
				t.Errorf("root skill: %v", err)
			}
			continue
		}
		if _, _, err := FromEntries(entries); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, bad := range []string{"../x", "/etc/passwd", "a//b", "a/./b", `a\b`, "SKILL.md"} {
		if _, err := cleanPath(bad); err == nil {
			t.Errorf("path %q accepted", bad)
		}
	}
}

func TestWriteChangesFilesOnTopOfTheBaseRevision(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	first, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill, Put: map[string]string{"scripts/fetch.sh": "curl x", "references/fields.md": "a"}, RunID: "run-1"})
	if err != nil || first.Revision != 1 || len(first.Files) != 2 {
		t.Fatalf("create = %+v, %v", first, err)
	}
	second, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill, Put: map[string]string{"references/fields.md": "b"}, Delete: []string{"scripts/fetch.sh"}, BaseRevision: 1, RunID: "run-2"})
	if err != nil || second.Revision != 2 {
		t.Fatalf("edit = %+v, %v", second, err)
	}
	files, err := store.SkillFiles(ctx, "shop-orders", 2)
	if err != nil || len(files) != 1 || files[0].Path != "references/fields.md" || string(files[0].Content) != "b" {
		t.Fatalf("files = %+v, %v", files, err)
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill, Delete: []string{"missing.md"}, BaseRevision: 2}); err == nil {
		t.Fatal("deleting a missing file must fail")
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill, BaseRevision: 0}); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("blind overwrite err = %v", err)
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill, Put: map[string]string{"../escape": "x"}, BaseRevision: 2}); err == nil {
		t.Fatal("a path out of the skill must fail")
	}
	retired, err := library.Retire(ctx, "shop-orders", 2, "run-3")
	if err != nil || !retired.Retired || len(retired.Files) != 1 {
		t.Fatalf("retire = %+v, %v", retired, err)
	}
	if scan, err := library.Scan(ctx); err != nil || len(scan.Skills) != 0 {
		t.Fatalf("scan after retire = %+v, %v", scan, err)
	}
}

func TestStagedPackagesImportExactlyWhatWasStaged(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	entries := []core.SkillFile{{Path: "pdf/SKILL.md", Content: []byte(strings.Replace(ordersSkill, "shop-orders", "pdf", 1))}, {Path: "pdf/scripts/fill.py", Content: []byte("print('fill')")}}
	staged, err := library.Stage(ctx, entries)
	if err != nil || staged.Name != "pdf" || len(staged.SHA256) != 64 || len(staged.Files) != 1 {
		t.Fatalf("stage = %+v, %v", staged, err)
	}
	if _, err := library.ImportStaged(ctx, "pdf", strings.Repeat("0", 64), 0, "run-1"); !errors.Is(err, core.ErrSkillPackageNotStaged) {
		t.Fatalf("unknown hash err = %v", err)
	}
	if _, err := library.ImportStaged(ctx, "docx", staged.SHA256, 0, "run-1"); err == nil || !strings.Contains(err.Error(), "holds skill pdf") {
		t.Fatalf("wrong name err = %v", err)
	}
	skill, err := library.ImportStaged(ctx, "pdf", staged.SHA256, 0, "run-1")
	if err != nil || skill.ID != "pdf" || skill.Author != core.SkillAuthorAgent || len(skill.Files) != 1 {
		t.Fatalf("import = %+v, %v", skill, err)
	}
	if _, ok := store.staged[staged.SHA256]; ok {
		t.Fatal("an imported package must leave the stage")
	}

	if _, err := library.Import(ctx, entries, false); err == nil {
		t.Fatal("installing over an existing skill without replace must fail")
	}
	replaced, err := library.Import(ctx, entries, true)
	if err != nil || replaced.Revision != 2 || replaced.Author != core.SkillAuthorOwner {
		t.Fatalf("owner replace = %+v, %v", replaced, err)
	}
}

func TestSeedWritesBuiltInsOnceAndReleaseChangesOnTop(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	builtIn, err := BuiltIn()
	if err != nil {
		t.Fatal(err)
	}
	if err := library.Seed(ctx); err != nil || len(store.writes) != len(builtIn) {
		t.Fatalf("first seed wrote %d of %d, %v", len(store.writes), len(builtIn), err)
	}
	if err := library.Seed(ctx); err != nil || len(store.writes) != len(builtIn) {
		t.Fatalf("unchanged seed wrote again: %d, %v", len(store.writes), err)
	}
	creator, _ := store.current("skill-creator")
	if len(creator.files) != 2 || creator.skill.SeedSHA256 != PackageHash(creator.skill.Markdown, creator.files) {
		t.Fatalf("seeded skill-creator = %+v", creator.skill)
	}
	if _, err := library.Retire(ctx, "skill-creator", 1, "run-1"); err != nil {
		t.Fatal(err)
	}
	stale := store.revisions["skill-creator"]
	stale[len(stale)-1].skill.SeedSHA256 = "an older release"
	if err := library.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := store.current("skill-creator")
	if after.skill.Revision != 3 || !after.skill.Retired || len(after.files) != 2 || after.skill.Author != core.SkillAuthorRelease {
		t.Fatalf("release update = %+v", after.skill)
	}
}

type fakeHost struct {
	held    map[string]string
	put     map[string]int
	deleted []string
}

func (h *fakeHost) SkillPackages(context.Context) (string, map[string]string, error) {
	return "/home/agent/skills", h.held, nil
}

func (h *fakeHost) PutSkillPackage(_ context.Context, name, hash, _ string, files []core.SkillFile) error {
	h.held[name] = hash
	h.put[name] = len(files)
	return nil
}

func (h *fakeHost) DeleteSkillPackage(_ context.Context, name string) error {
	delete(h.held, name)
	h.deleted = append(h.deleted, name)
	return nil
}

func TestSyncKeepsExactlyTheEligiblePackagesOnTheHost(t *testing.T) {
	ctx := context.Background()
	library, _ := newTestLibrary(t)
	if _, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill, Put: map[string]string{"scripts/fetch.sh": "curl x"}}); err != nil {
		t.Fatal(err)
	}
	plain := strings.Replace(strings.Replace(ordersSkill, "shop-orders", "plain", 1), "  acorn-requires-tools: shell\n", "  note: none\n", 1)
	if _, err := library.Write(ctx, WriteRequest{Markdown: plain}); err != nil {
		t.Fatal(err)
	}
	scan, err := library.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := BuildSnapshot(*scan, EligibilityContext{AvailableTools: []string{"shell", "files"}})
	host := &fakeHost{held: map[string]string{"gone": "x"}, put: map[string]int{}}
	dir, err := library.Sync(ctx, host, snapshot)
	if err != nil || dir != "/home/agent/skills" {
		t.Fatalf("sync = %q, %v", dir, err)
	}
	if host.put["shop-orders"] != 1 || len(host.put) != 1 || !slices.Equal(host.deleted, []string{"gone"}) {
		t.Fatalf("put %v deleted %v", host.put, host.deleted)
	}
	host.put = map[string]int{}
	if _, err := library.Sync(ctx, host, snapshot); err != nil || len(host.put) != 0 {
		t.Fatalf("an unchanged package was put again: %v, %v", host.put, err)
	}
	withoutWorkstation := BuildSnapshot(*scan, EligibilityContext{AvailableTools: []string{"shell"}})
	for _, view := range withoutWorkstation.Skills {
		if view.Name == "shop-orders" && (view.Eligible || !slices.Contains(view.DisabledReasons, "files_need_workstation")) {
			t.Fatalf("a skill with files needs the workstation: %+v", view)
		}
	}
}
