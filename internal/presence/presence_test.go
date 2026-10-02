package presence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

var t0 = time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC) // 09:00 Monday in Shanghai

func TestDecayMovesExpiredItemsOneStep(t *testing.T) {
	items := []core.MemoryItem{
		{ID: 1, Kind: core.MemoryThought, Status: core.MemoryActive, ExpiresAt: t0.Add(-time.Second)},
		{ID: 2, Kind: core.MemoryThought, Status: core.MemoryActive, ExpiresAt: t0},
		{ID: 3, Kind: core.MemorySaid, Status: core.MemoryResting, ExpiresAt: t0.Add(-time.Hour)},
		{ID: 4, Kind: core.MemoryCommitment, Status: core.MemoryActive, ExpiresAt: t0.Add(-time.Hour)},
		{ID: 5, Kind: core.MemoryTendency, Status: core.MemoryReleased, ExpiresAt: t0.Add(-time.Hour)},
	}
	before := append([]core.MemoryItem(nil), items...)
	changed := Decay(items, t0)
	if len(changed) != 2 {
		t.Fatalf("changed = %+v, want items 1 and 3", changed)
	}
	if changed[0].ID != 1 || changed[0].Status != core.MemoryResting || !changed[0].ExpiresAt.Equal(t0.Add(restingTTL)) {
		t.Fatalf("item 1 = %+v", changed[0])
	}
	if changed[1].ID != 3 || changed[1].Status != core.MemorySunk || !changed[1].ExpiresAt.IsZero() {
		t.Fatalf("item 3 = %+v", changed[1])
	}
	for i := range items {
		if items[i] != before[i] {
			t.Fatalf("Decay mutated its input at %d", i)
		}
	}
}

func TestNewExpiryByKind(t *testing.T) {
	if got := NewExpiry(core.MemoryThought, t0); !got.Equal(t0.Add(48 * time.Hour)) {
		t.Fatalf("thought expiry = %v", got)
	}
	if got := NewExpiry(core.MemoryCommitment, t0); !got.IsZero() {
		t.Fatalf("commitment expiry = %v", got)
	}
}

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func TestRenderOrdersSectionsInOwnerTime(t *testing.T) {
	items := []core.MemoryItem{
		{ID: 1, Kind: core.MemorySaid, Status: core.MemoryActive, Content: "我周末不想被打扰", CreatedAt: t0},
		{ID: 2, Kind: core.MemoryCommitment, Status: core.MemoryActive, Content: "later", WakeAt: t0.Add(48 * time.Hour), Recurrence: "0 9 * * *"},
		{ID: 3, Kind: core.MemoryCommitment, Status: core.MemoryWoken, Content: "提醒 owner 看 X", WakeAt: t0},
		{ID: 4, Kind: core.MemoryThought, Status: core.MemoryActive, Content: "owner 在读论文", CreatedAt: t0},
		{ID: 5, Kind: core.MemoryCommitment, Status: core.MemoryActive, Content: "sooner", WakeAt: t0.Add(time.Hour)},
		{ID: 6, Kind: core.MemoryRuler, Status: core.MemoryActive, Content: "推送只写摘要", CreatedAt: t0},
		{ID: 7, Kind: core.MemoryThought, Status: core.MemoryResting, Content: strings.Repeat("长", 100), CreatedAt: t0},
	}
	out := Render(RenderInput{Now: t0, Location: shanghai(t), Wake: "commitment #3: 提醒 owner 看 X", Items: items, MaxTokens: 10000, Count: runeCount})
	for _, want := range []string{
		"Now: 2026-10-05 Mon 09:00 (Asia/Shanghai)",
		"Woken by: commitment #3",
		"- #3 [due now, was set for 2026-10-05 Mon 09:00] 提醒 owner 看 X",
		"- #4 (2026-10-05) owner 在读论文",
		"- #1 (2026-10-05) 我周末不想被打扰",
		"## Concerns\n- #6",
		`repeats "0 9 * * *"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	order := []string{"#3 [due now", "#5 [", "#2 [", "## Thoughts", "## Owner said", "## Concerns", "## Resting"}
	last := -1
	for _, marker := range order {
		idx := strings.Index(out, marker)
		if idx <= last {
			t.Fatalf("%q out of order:\n%s", marker, out)
		}
		last = idx
	}
	if strings.Contains(out, strings.Repeat("长", 61)) {
		t.Fatalf("resting item not shortened:\n%s", out)
	}
}

func TestRenderDropsLowPriorityItemsToFitBudget(t *testing.T) {
	items := []core.MemoryItem{
		{ID: 1, Kind: core.MemoryCommitment, Status: core.MemoryWoken, Content: "must stay", WakeAt: t0},
		{ID: 2, Kind: core.MemorySaid, Status: core.MemoryActive, Content: strings.Repeat("a", 200), CreatedAt: t0},
		{ID: 3, Kind: core.MemorySaid, Status: core.MemoryActive, Content: "newest said", CreatedAt: t0},
		{ID: 4, Kind: core.MemoryThought, Status: core.MemoryResting, Content: strings.Repeat("r", 50), CreatedAt: t0},
	}
	in := RenderInput{Now: t0, Location: time.UTC, Wake: "owner message", Items: items, Count: runeCount}
	full := Render(in)
	in.MaxTokens = runeCount(full) - 100
	out := Render(in)
	if runeCount(out) > in.MaxTokens {
		t.Fatalf("render exceeds budget: %d > %d", runeCount(out), in.MaxTokens)
	}
	if strings.Contains(out, "#4 ") || strings.Contains(out, "#2 ") {
		t.Fatalf("resting and oldest said should be dropped first:\n%s", out)
	}
	if !strings.Contains(out, "#3 ") || !strings.Contains(out, "#1 [due now") {
		t.Fatalf("newest said and woken commitment must stay:\n%s", out)
	}
	if !strings.Contains(out, "2 items left out") {
		t.Fatalf("missing omission note:\n%s", out)
	}
	in.MaxTokens = 1
	if out := Render(in); !strings.Contains(out, "#1 [due now") {
		t.Fatalf("woken commitment dropped under a tiny budget:\n%s", out)
	}
}

func TestLoadPersona(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persona.md")
	if _, err := LoadPersona(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("missing persona error = %v", err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPersona(path); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty persona error = %v", err)
	}
	if err := os.WriteFile(path, []byte(DefaultPersona), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPersona(path)
	if err != nil || !strings.HasPrefix(got, "You are Acorn") {
		t.Fatalf("persona = %q err=%v", got, err)
	}
}
