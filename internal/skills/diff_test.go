package skills

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func numbered(n int, change map[int]string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		line := fmt.Sprintf("line %d", i)
		if c, ok := change[i]; ok {
			line = c
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderHunks(hunks []Hunk) []string {
	var out []string
	for _, h := range hunks {
		out = append(out, fmt.Sprintf("@@ %d %d", h.OldStart, h.NewStart))
		for _, l := range h.Lines {
			out = append(out, string(l.Kind)+" "+l.Text)
		}
	}
	return out
}

func TestDiffKeepsThreeLinesOfContextAroundEachChange(t *testing.T) {
	older := Version{Revision: core.SkillRevision{Markdown: numbered(20, nil)}}
	newer := Version{Revision: core.SkillRevision{Markdown: numbered(20, map[int]string{2: "second", 17: "seventeenth"})}}
	files := Diff(older, newer)
	if len(files) != 1 || files[0].Path != SkillFileName || files[0].Change != FileModified {
		t.Fatalf("files = %+v", files)
	}
	got := renderHunks(files[0].Hunks)
	want := []string{
		"@@ 1 1", "same line 1", "removed line 2", "added second", "same line 3", "same line 4", "same line 5",
		"@@ 14 14", "same line 14", "same line 15", "same line 16", "removed line 17", "added seventeenth", "same line 18", "same line 19", "same line 20",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("hunks =\n%s", strings.Join(got, "\n"))
	}

	near := Version{Revision: core.SkillRevision{Markdown: numbered(20, map[int]string{5: "five", 11: "eleven"})}}
	if hunks := Diff(older, near)[0].Hunks; len(hunks) != 1 || hunks[0].OldStart != 2 {
		t.Fatalf("changes six lines apart share one hunk: %v", renderHunks(hunks))
	}
	grown := Version{Revision: core.SkillRevision{Markdown: numbered(3, nil) + "line 4\n"}}
	if got := renderHunks(Diff(Version{Revision: core.SkillRevision{Markdown: numbered(3, nil)}}, grown)[0].Hunks); !slices.Equal(got, []string{"@@ 1 1", "same line 1", "same line 2", "same line 3", "added line 4"}) {
		t.Fatalf("appended line = %v", got)
	}
}

func TestDiffComparesFilesByPathAndLeavesBinaryUnread(t *testing.T) {
	markdown := "---\nname: x\ndescription: y\n---\nz\n"
	older := Version{Revision: core.SkillRevision{Markdown: markdown}, Files: []core.SkillFile{
		{Path: "a.md", Content: []byte("same\n")},
		{Path: "b.bin", Content: []byte{0, 1, 2}},
		{Path: "c.md", Content: []byte("old\n")},
	}}
	newer := Version{Revision: core.SkillRevision{Markdown: markdown}, Files: []core.SkillFile{
		{Path: "a.md", Content: []byte("same\n")},
		{Path: "b.bin", Content: []byte{0, 1, 3}},
		{Path: "d.md", Content: []byte("")},
	}}
	files := Diff(older, newer)
	var got []string
	for _, f := range files {
		got = append(got, fmt.Sprintf("%s %s binary=%v hunks=%d", f.Path, f.Change, f.Binary, len(f.Hunks)))
	}
	if !slices.Equal(got, []string{"b.bin modified binary=true hunks=0", "c.md removed binary=false hunks=1", "d.md added binary=false hunks=0"}) {
		t.Fatalf("files = %v", got)
	}
	if lines := files[1].Hunks[0].Lines; len(lines) != 1 || lines[0] != (DiffLine{Kind: LineRemoved, Text: "old"}) {
		t.Fatalf("removed file lines = %+v", lines)
	}
	if len(Diff(older, older)) != 0 {
		t.Fatal("a version differs from itself")
	}
}
