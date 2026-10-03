package knowledge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// memoryIndex is an in-memory core.KnowledgeStore: substring search, newest first.
type memoryIndex struct {
	mu    sync.Mutex
	notes map[string]core.KnowledgeNote
}

func newMemoryIndex() *memoryIndex { return &memoryIndex{notes: map[string]core.KnowledgeNote{}} }

func (m *memoryIndex) UpsertKnowledgeNote(_ context.Context, note core.KnowledgeNote) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes[note.Path] = note
	return nil
}

func (m *memoryIndex) DeleteKnowledgeNote(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.notes, path)
	return nil
}

func (m *memoryIndex) ListKnowledgeFileStats(context.Context) ([]core.KnowledgeFileStat, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.KnowledgeFileStat
	for _, n := range m.notes {
		out = append(out, core.KnowledgeFileStat{Path: n.Path, MTimeNS: n.MTimeNS, Size: n.Size})
	}
	return out, nil
}

func (m *memoryIndex) SearchKnowledge(_ context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	return m.filter(func(n core.KnowledgeNote) bool {
		return strings.Contains(n.Title+" "+n.Body, query)
	}, limit), nil
}

func (m *memoryIndex) RecentKnowledge(_ context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	return m.filter(func(n core.KnowledgeNote) bool { return strings.HasPrefix(n.Path, prefix) }, limit), nil
}

func (m *memoryIndex) filter(keep func(core.KnowledgeNote) bool, limit int) []core.KnowledgeHit {
	m.mu.Lock()
	defer m.mu.Unlock()
	var hits []core.KnowledgeHit
	for _, n := range m.notes {
		if keep(n) {
			hits = append(hits, core.KnowledgeHit{Path: n.Path, Title: n.Title, Tags: n.Tags, Snippet: n.Body, UpdatedAt: n.UpdatedAt})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].UpdatedAt.After(hits[j].UpdatedAt) })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

type testVault struct {
	*Vault
	index *memoryIndex
	now   time.Time
}

var shanghai = time.FixedZone("CST", 8*3600)

func openTestVault(t *testing.T) *testVault {
	t.Helper()
	git, err := LookupGit()
	if err != nil {
		t.Fatal(err)
	}
	tv := &testVault{index: newMemoryIndex(), now: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)}
	v, err := Open(context.Background(), VaultConfig{
		Dir:      filepath.Join(t.TempDir(), "knowledge"),
		Git:      git,
		Index:    tv.index,
		Clock:    func() time.Time { return tv.now },
		Location: shanghai,
	})
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	tv.Vault = v
	return tv
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCleanNotePath(t *testing.T) {
	for _, bad := range []string{"", "../x.md", "/abs.md", ".git/config", "a.txt", "notes/../x.md", "./x.md", `a\b.md`, ".obsidian/x.md", "attachments/x.md", "a//b.md"} {
		if _, err := CleanNotePath(bad); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("CleanNotePath(%q) err = %v, want ErrInvalidPath", bad, err)
		}
	}
	if got, err := CleanNotePath(" inbox/读书.md "); err != nil || got != "inbox/读书.md" {
		t.Fatalf("CleanNotePath = %q, %v", got, err)
	}
}

func TestWriteCommitsAndKeepsCreated(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	if got := gitOutput(t, v.Dir(), "config", "receive.denyCurrentBranch"); got != "updateInstead" {
		t.Fatalf("receive.denyCurrentBranch = %q", got)
	}
	note, err := v.Write(ctx, WriteNote{Path: "inbox/tokio.md", Title: "Tokio", Tags: []string{"rust", "#async", "rust"}, Source: "https://tokio.rs", Body: "Async runtime.", RunID: "run_1"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if note.Commit == "" || gitOutput(t, v.Dir(), "rev-parse", "HEAD") != note.Commit {
		t.Fatalf("commit = %q", note.Commit)
	}
	if msg := gitOutput(t, v.Dir(), "log", "-1", "--format=%B"); msg != "knowledge: write inbox/tokio.md\n\nAcorn-Run: run_1" {
		t.Fatalf("commit message = %q", msg)
	}
	if author := gitOutput(t, v.Dir(), "log", "-1", "--format=%an <%ae>"); author != "Acorn <acorn@localhost>" {
		t.Fatalf("author = %q", author)
	}
	raw, err := os.ReadFile(filepath.Join(v.Dir(), "inbox", "tokio.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntitle: Tokio\ntags: [rust, async]\nsource: https://tokio.rs\ncreated: 2026-10-03T09:00:00+08:00\nupdated: 2026-10-03T09:00:00+08:00\n---\n\nAsync runtime.\n"
	if string(raw) != want {
		t.Fatalf("file =\n%s\nwant\n%s", raw, want)
	}

	v.now = v.now.Add(2 * time.Hour)
	again, err := v.Write(ctx, WriteNote{Path: "inbox/tokio.md", Title: "Tokio runtime", Body: "Work stealing."})
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if !again.Frontmatter.Created.Equal(time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)) || !again.Frontmatter.Updated.Equal(v.now) {
		t.Fatalf("times = %v / %v", again.Frontmatter.Created, again.Frontmatter.Updated)
	}
	if hits, _ := v.Search(ctx, "Work stealing", 5); len(hits) != 1 || hits[0].Title != "Tokio runtime" {
		t.Fatalf("search after rewrite = %+v", hits)
	}
	if _, err := v.Write(ctx, WriteNote{Path: "x.md", Title: "big", Body: strings.Repeat("a", MaxNoteBodyBytes+1)}); err == nil {
		t.Fatal("an oversized body must be rejected")
	}
	if _, err := v.Write(ctx, WriteNote{Path: "x.md", Body: "no title"}); err == nil {
		t.Fatal("a note without a title must be rejected")
	}
}

func TestEditNeedsUniqueOldText(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	if _, err := v.Write(ctx, WriteNote{Path: "a.md", Title: "A", Body: "one two two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Edit(ctx, "a.md", "three", "x", ""); err == nil {
		t.Fatal("missing old text must fail")
	}
	if _, err := v.Edit(ctx, "a.md", "two", "x", ""); err == nil || !strings.Contains(err.Error(), "2 times") {
		t.Fatalf("ambiguous old text err = %v", err)
	}
	note, err := v.Edit(ctx, "a.md", "one", "uno", "run_2")
	if err != nil || note.Body != "uno two two" || note.Commit == "" {
		t.Fatalf("edit = %+v, %v", note, err)
	}
	if msg := gitOutput(t, v.Dir(), "log", "-1", "--format=%s"); msg != "knowledge: edit a.md" {
		t.Fatalf("subject = %q", msg)
	}
	if _, err := v.Edit(ctx, "missing.md", "a", "b", ""); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("edit missing err = %v", err)
	}
}

func TestOwnerNotesAreParsedAndPreserved(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	plain := "# Reading list\n\n- SICP\n"
	if err := os.WriteFile(filepath.Join(v.Dir(), "reading.md"), []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	withExtra := "---\naliases: [todo]\ntags: a, b\ncreated: 2024-01-02\ncssclass: wide\n---\nBody here.\n"
	if err := os.WriteFile(filepath.Join(v.Dir(), "todo.md"), []byte(withExtra), 0o644); err != nil {
		t.Fatal(err)
	}
	recent, err := v.Recent(ctx, "", 10)
	if err != nil || len(recent) != 2 {
		t.Fatalf("recent = %+v, %v", recent, err)
	}
	read, err := v.Read(ctx, "reading.md")
	if err != nil || read.Frontmatter.Title != "Reading list" {
		t.Fatalf("read = %+v, %v", read, err)
	}
	todo, err := v.Read(ctx, "todo.md")
	if err != nil || todo.Frontmatter.Title != "todo" || strings.Join(todo.Frontmatter.Tags, ",") != "a,b" {
		t.Fatalf("todo = %+v, %v", todo, err)
	}
	if _, err := v.Edit(ctx, "todo.md", "Body here.", "Body changed.", ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(v.Dir(), "todo.md"))
	for _, want := range []string{"aliases: [todo]", "cssclass: wide", "created: 2024-01-02T00:00:00+08:00", "Body changed."} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("rewritten note lacks %q:\n%s", want, raw)
		}
	}

	if err := os.WriteFile(filepath.Join(v.Dir(), "broken.md"), []byte("---\ncreated: someday\n---\nx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Recent(ctx, "", 10); err == nil || !strings.Contains(err.Error(), "broken.md") {
		t.Fatalf("broken note err = %v", err)
	}
}

func TestSyncFollowsExternalChanges(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	if _, err := v.Write(ctx, WriteNote{Path: "a.md", Title: "A", Body: "alpha"}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".obsidian/workspace.md", "attachments/x.md"} {
		abs := filepath.Join(v.Dir(), filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte("ignored"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(v.Dir(), "b.md"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.Dir(), "a.md"), []byte("---\ntitle: A\n---\ngamma, edited outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hits, err := v.Search(ctx, "gamma", 5); err != nil || len(hits) != 1 || hits[0].Path != "a.md" {
		t.Fatalf("changed note search = %+v, %v", hits, err)
	}
	if hits, _ := v.Search(ctx, "ignored", 5); len(hits) != 0 {
		t.Fatalf("hidden and attachment files were indexed: %+v", hits)
	}
	if err := os.Remove(filepath.Join(v.Dir(), "b.md")); err != nil {
		t.Fatal(err)
	}
	if recent, _ := v.Recent(ctx, "", 10); len(recent) != 1 {
		t.Fatalf("removed note still listed: %+v", recent)
	}
}

func TestCommitLeavesOtherEditsAlone(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	if err := os.WriteFile(filepath.Join(v.Dir(), "draft.md"), []byte("owner draft"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Write(ctx, WriteNote{Path: "a.md", Title: "A", Body: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if status := gitOutput(t, v.Dir(), "status", "--porcelain"); status != "?? draft.md" {
		t.Fatalf("status = %q", status)
	}
}

func TestSaveAttachment(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	rel, err := v.SaveAttachment(ctx, "image/png", []byte("\x89PNG fake"), "knowledge: capture image")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rel, "attachments/2026/10/") || !strings.HasSuffix(rel, ".png") {
		t.Fatalf("path = %q", rel)
	}
	if files := gitOutput(t, v.Dir(), "show", "--name-only", "--format=%s", "HEAD"); files != "knowledge: capture image\n\n"+rel {
		t.Fatalf("commit = %q", files)
	}
	if _, err := v.SaveAttachment(ctx, "application/pdf", []byte("x"), "m"); err == nil {
		t.Fatal("unsupported type must be rejected")
	}
}
