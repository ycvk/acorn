package review

import (
	"regexp"
	"strings"
)

// Page is what the agent last saw of a browser session: where it was and
// what its @eN refs named.
type Page struct {
	URL   string
	Title string
	Refs  map[string]string
}

var (
	urlLine   = regexp.MustCompile(`(?m)^url=(\S+)`)
	titleLine = regexp.MustCompile(`(?m)^\s*RootWebArea "(.*)"\s*$`)
	refLine   = regexp.MustCompile(`^(\s*)(@e\d+) (.*)$`)
	textLine  = regexp.MustCompile(`^\s*\S+ "(.*)"\s*$`)
)

// ObservePage reads bsk navigate, observe and snapshot output. A ref without
// a name takes the text of the first line nested under it, so a link reads
// as its visible text.
func ObservePage(output string) Page {
	page := Page{Refs: map[string]string{}}
	if m := urlLine.FindAllStringSubmatch(output, -1); len(m) > 0 {
		page.URL = m[len(m)-1][1]
	}
	if m := titleLine.FindStringSubmatch(output); m != nil {
		page.Title = m[1]
	}
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		m := refLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent, ref, label := m[1], m[2], strings.TrimSpace(m[3])
		if !strings.Contains(label, `"`) && i+1 < len(lines) && len(leadingSpace(lines[i+1])) > len(indent) {
			if t := textLine.FindStringSubmatch(lines[i+1]); t != nil {
				label += ` "` + t[1] + `"`
			}
		}
		page.Refs[ref] = label
	}
	return page
}

// Merge returns the page after a newer observation. Navigating to another
// URL drops the old refs; otherwise refs, URL and title are replaced when the
// newer output has them.
func (p Page) Merge(newer Page) Page {
	out := p
	if newer.URL != "" && newer.URL != p.URL {
		out.URL, out.Title, out.Refs = newer.URL, "", map[string]string{}
	}
	if newer.Title != "" {
		out.Title = newer.Title
	}
	if len(newer.Refs) > 0 {
		out.Refs = newer.Refs
	}
	return out
}

func leadingSpace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
