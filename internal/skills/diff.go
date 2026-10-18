package skills

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"

	"github.com/ycvk/acorn/internal/core"
)

// LineKind says whether a diff line is in both versions, only the newer or
// only the older.
type LineKind string

const (
	LineSame    LineKind = "same"
	LineAdded   LineKind = "added"
	LineRemoved LineKind = "removed"
)

// DiffLine is one line of a diff, without its line break.
type DiffLine struct {
	Kind LineKind
	Text string
}

// Hunk is a run of changed lines with up to diffContext unchanged lines
// around them. OldStart and NewStart number its first line in each version,
// from 1.
type Hunk struct {
	OldStart int
	NewStart int
	Lines    []DiffLine
}

// FileChange says how a file differs between two versions.
type FileChange string

const (
	FileAdded    FileChange = "added"
	FileRemoved  FileChange = "removed"
	FileModified FileChange = "modified"
)

// FileDiff is how one file of a package differs between two versions. A
// file that is not text on either side has no hunks.
type FileDiff struct {
	Path   string
	Change FileChange
	Binary bool
	Hunks  []Hunk
}

const diffContext = 3

// Diff compares two versions of a skill's package file by file: SKILL.md
// first, then the other files by path. Files that are the same in both are
// left out.
func Diff(from, to Version) []FileDiff {
	var out []FileDiff
	if d, ok := diffFile(SkillFileName, []byte(from.Revision.Markdown), true, []byte(to.Revision.Markdown), true); ok {
		out = append(out, d)
	}
	older, newer := contents(from.Files), contents(to.Files)
	paths := slices.Sorted(maps.Keys(older))
	for p := range newer {
		if _, ok := older[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	for _, p := range paths {
		a, inOlder := older[p]
		b, inNewer := newer[p]
		if d, ok := diffFile(p, a, inOlder, b, inNewer); ok {
			out = append(out, d)
		}
	}
	return out
}

func contents(files []core.SkillFile) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		out[f.Path] = f.Content
	}
	return out
}

func diffFile(path string, a []byte, inOlder bool, b []byte, inNewer bool) (FileDiff, bool) {
	if inOlder && inNewer && bytes.Equal(a, b) {
		return FileDiff{}, false
	}
	d := FileDiff{Path: path, Change: FileModified}
	switch {
	case !inOlder:
		d.Change = FileAdded
	case !inNewer:
		d.Change = FileRemoved
	}
	if !IsText(a) || !IsText(b) {
		d.Binary = true
		return d, true
	}
	d.Hunks = hunks(diffLines(string(a), string(b)))
	return d, true
}

// IsText reports whether content is UTF-8 text without NUL bytes.
func IsText(content []byte) bool {
	return utf8.Valid(content) && !bytes.ContainsRune(content, 0)
}

// diffLines turns a into b line by line.
func diffLines(a, b string) []DiffLine {
	dmp := diffmatchpatch.New()
	older, newer, lines := dmp.DiffLinesToRunes(a, b)
	var out []DiffLine
	for _, d := range dmp.DiffCharsToLines(dmp.DiffMainRunes(older, newer, false), lines) {
		kind := LineSame
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			kind = LineAdded
		case diffmatchpatch.DiffDelete:
			kind = LineRemoved
		}
		for _, line := range splitLines(d.Text) {
			out = append(out, DiffLine{Kind: kind, Text: line})
		}
	}
	return out
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// hunks keeps the changed lines with the unchanged lines near them, split
// where more than twice diffContext unchanged lines lie between changes.
func hunks(lines []DiffLine) []Hunk {
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if l.Kind == LineSame {
			continue
		}
		for j := max(0, i-diffContext); j <= min(len(lines)-1, i+diffContext); j++ {
			keep[j] = true
		}
	}
	var out []Hunk
	open := false
	older, newer := 1, 1
	for i, l := range lines {
		switch {
		case !keep[i]:
			open = false
		case !open:
			out = append(out, Hunk{OldStart: older, NewStart: newer})
			open = true
			fallthrough
		default:
			out[len(out)-1].Lines = append(out[len(out)-1].Lines, l)
		}
		if l.Kind != LineAdded {
			older++
		}
		if l.Kind != LineRemoved {
			newer++
		}
	}
	return out
}
