package redact

import (
	"slices"
	"strings"
)

// ExcludedPlaceholder stands where the owner excluded a quote from memory.
const ExcludedPlaceholder = "[owner excluded this memory]"

// Render replaces the excluded quotes in raw and, when redacted, its
// credentials. A quote may come from either text: one of the redacted text is
// located there and mapped back onto raw, so forgetting hides the same words
// in raw text. Overlapping quotes are excluded as one range, and a credential
// that an excluded range touches is excluded with it, so no part of it
// survives at the range's edge.
func Render(raw string, quotes []string, redacted bool) string {
	if !redacted && len(quotes) == 0 {
		return raw
	}
	secrets := Find(raw)
	if len(secrets) == 0 && len(quotes) == 0 {
		return raw
	}
	var excluded []textRange
	if len(quotes) > 0 {
		view := newRedactedView(raw, secrets)
		for _, quote := range quotes {
			excluded = append(excluded, occurrences(raw, quote)...)
			for _, r := range occurrences(view.text, quote) {
				excluded = append(excluded, view.raw(r))
			}
		}
	}
	if redacted {
		for _, s := range secrets {
			for i, r := range excluded {
				if s.Start < r.end && r.start < s.End {
					excluded[i] = textRange{min(r.start, s.Start), max(r.end, s.End)}
				}
			}
		}
	}
	excluded = mergeRanges(excluded)
	var b strings.Builder
	at, next := 0, 0
	for _, r := range excluded {
		if redacted {
			next = writeRedacted(&b, raw, at, r.start, secrets, next)
		} else {
			b.WriteString(raw[at:r.start])
		}
		b.WriteString(ExcludedPlaceholder)
		at = r.end
	}
	if redacted {
		writeRedacted(&b, raw, at, len(raw), secrets, next)
	} else {
		b.WriteString(raw[at:])
	}
	return b.String()
}

type textRange struct{ start, end int }

// occurrences lists the non-overlapping positions of quote in text.
func occurrences(text, quote string) []textRange {
	var out []textRange
	for at := 0; quote != ""; {
		i := strings.Index(text[at:], quote)
		if i < 0 {
			break
		}
		out = append(out, textRange{at + i, at + i + len(quote)})
		at += i + len(quote)
	}
	return out
}

func mergeRanges(ranges []textRange) []textRange {
	slices.SortFunc(ranges, func(a, b textRange) int { return a.start - b.start })
	var out []textRange
	for _, r := range ranges {
		if n := len(out); n > 0 && r.start < out[n-1].end {
			out[n-1].end = max(out[n-1].end, r.end)
			continue
		}
		out = append(out, r)
	}
	return out
}

// writeRedacted writes raw[from:to] with the credentials wholly inside it
// replaced, starting the search at secrets[next], and returns the next unused
// secret. Excluded ranges absorb the credentials they touch.
func writeRedacted(b *strings.Builder, raw string, from, to int, secrets []Span, next int) int {
	for next < len(secrets) && secrets[next].End <= from {
		next++
	}
	for ; next < len(secrets) && secrets[next].End <= to; next++ {
		s := secrets[next]
		b.WriteString(raw[from:s.Start])
		b.WriteString(Placeholder(s.Kind))
		from = s.End
	}
	b.WriteString(raw[from:to])
	return next
}

// redactedView is raw with every credential replaced, plus the segments that
// map its offsets back onto raw.
type redactedView struct {
	text     string
	segments []viewSegment
}

type viewSegment struct {
	start, end, rawStart, rawEnd int
	secret                       bool
}

func newRedactedView(raw string, secrets []Span) redactedView {
	var v redactedView
	var b strings.Builder
	at := 0
	add := func(text string, rawEnd int, secret bool) {
		start := b.Len()
		b.WriteString(text)
		v.segments = append(v.segments, viewSegment{start, b.Len(), at, rawEnd, secret})
		at = rawEnd
	}
	for _, s := range secrets {
		add(raw[at:s.Start], s.Start, false)
		add(Placeholder(s.Kind), s.End, true)
	}
	add(raw[at:], len(raw), false)
	v.text = b.String()
	return v
}

// raw maps a range of the view onto raw; a range that touches a placeholder
// covers the whole credential behind it.
func (v redactedView) raw(r textRange) textRange {
	out := textRange{v.segments[len(v.segments)-1].rawEnd, 0}
	for _, s := range v.segments {
		if s.end <= r.start || s.start >= r.end {
			continue
		}
		if s.secret {
			out.start, out.end = min(out.start, s.rawStart), max(out.end, s.rawEnd)
			continue
		}
		out.start = min(out.start, s.rawStart+max(r.start, s.start)-s.start)
		out.end = max(out.end, s.rawStart+min(r.end, s.end)-s.start)
	}
	return out
}
