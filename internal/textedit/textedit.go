// Package textedit changes parts of a text in place. Each edit replaces the
// one place its old text occurs; old text that occurs nowhere exactly is
// matched again with whitespace at line ends, typographic quotes and dashes
// and non-breaking spaces normalized. A byte order mark and CRLF line endings
// are kept.
package textedit

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Edit replaces the one place Old occurs with New.
type Edit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

const bom = "\uFEFF"

// Apply makes every edit against text as it is and returns the new text with
// the line, counted from 1, at which each edit's new text starts. Either all
// edits apply or none does.
func Apply(text string, edits []Edit) (string, []int, error) {
	if len(edits) == 0 {
		return "", nil, errors.New("give at least one edit")
	}
	body := strings.TrimPrefix(text, bom)
	crlf := strings.Contains(body, "\r\n")
	type span struct {
		edit, start, end int
		replacement      string
	}
	spans := make([]span, 0, len(edits))
	for i, e := range edits {
		old, replacement := lineEndings(e.Old, crlf), lineEndings(e.New, crlf)
		switch old {
		case "":
			return "", nil, fmt.Errorf("edit %d: old text is empty", i+1)
		case replacement:
			return "", nil, fmt.Errorf("edit %d: old and new text are the same", i+1)
		}
		start, end, err := locate(body, old)
		if err != nil {
			return "", nil, fmt.Errorf("edit %d: %w", i+1, err)
		}
		spans = append(spans, span{edit: i, start: start, end: end, replacement: replacement})
	}
	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })
	for k := 1; k < len(spans); k++ {
		if spans[k].start < spans[k-1].end {
			return "", nil, fmt.Errorf("edits %d and %d overlap; make them one edit", spans[k-1].edit+1, spans[k].edit+1)
		}
	}
	var b strings.Builder
	b.Grow(len(text))
	b.WriteString(text[:len(text)-len(body)])
	lines := make([]int, len(edits))
	at, line := 0, 1
	for _, s := range spans {
		line += strings.Count(body[at:s.start], "\n")
		b.WriteString(body[at:s.start])
		lines[s.edit] = line
		b.WriteString(s.replacement)
		line += strings.Count(s.replacement, "\n")
		at = s.end
	}
	b.WriteString(body[at:])
	return b.String(), lines, nil
}

// lineEndings writes s with the line endings of the text it edits.
func lineEndings(s string, crlf bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if crlf {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// locate finds the one place old occurs in body, exactly or else loosely.
func locate(body, old string) (int, int, error) {
	switch n := strings.Count(body, old); n {
	case 1:
		start := strings.Index(body, old)
		return start, start + len(old), nil
	case 0:
	default:
		return 0, 0, ambiguous(n)
	}
	text, starts, ends := normalize(body)
	want, _, _ := normalize(old)
	if want == "" {
		return 0, 0, errNotFound
	}
	switch n := strings.Count(text, want); n {
	case 1:
		i := strings.Index(text, want)
		return starts[i], ends[i+len(want)-1], nil
	case 0:
		return 0, 0, errNotFound
	default:
		return 0, 0, ambiguous(n)
	}
}

var errNotFound = errors.New("the old text is not in the file; read it again and copy the text exactly")

func ambiguous(n int) error {
	return fmt.Errorf("the old text occurs %d times; include more of the text around it so it occurs once", n)
}

// normalize drops whitespace at line ends and folds typographic quotes,
// dashes and non-breaking spaces. For each byte of the result, starts and
// ends hold where the character it came from begins and ends in s.
func normalize(s string) (string, []int, []int) {
	var b strings.Builder
	starts := make([]int, 0, len(s))
	ends := make([]int, 0, len(s))
	at := 0
	for _, line := range strings.SplitAfter(s, "\n") {
		content := strings.TrimRight(strings.TrimSuffix(line, "\n"), " \t\r\u00A0\u3000")
		for i, r := range content {
			folded := fold(r)
			for range utf8.RuneLen(folded) {
				starts = append(starts, at+i)
				ends = append(ends, at+i+utf8.RuneLen(r))
			}
			b.WriteRune(folded)
		}
		if strings.HasSuffix(line, "\n") {
			starts = append(starts, at+len(line)-1)
			ends = append(ends, at+len(line))
			b.WriteByte('\n')
		}
		at += len(line)
	}
	return b.String(), starts, ends
}

func fold(r rune) rune {
	switch r {
	case '‘', '’', '‚', '‛':
		return '\''
	case '“', '”', '„', '‟':
		return '"'
	case '‐', '‑', '‒', '–', '—', '―', '−':
		return '-'
	case '\u00A0', '\u2007', '\u202F':
		return ' '
	}
	return r
}
