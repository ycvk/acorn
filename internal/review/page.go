package review

import (
	"regexp"
	"strings"
)

// Page is what bsk last showed of a browser session: where it was and what
// its @eN refs named.
type Page struct {
	URL   string
	Title string
	Refs  map[string]string
	// continued is set when the refs are a later part of a long observation,
	// which prints no title.
	continued bool
}

// Shown is what is known of a session once bsk printed next about it, p
// being what was known before. An observation prints no address, and a
// later part of a long one no title either: they are of the page the session
// was on, unless the command had a page action that may have left it.
func (p Page) Shown(next Page, left bool) Page {
	if left || next.URL != "" {
		return next
	}
	next.URL = p.URL
	if next.continued {
		next.Title = p.Title
	}
	return next
}

var (
	// urlLine is where navigate, reload and the history commands report the
	// address they reached.
	urlLine = regexp.MustCompile(`^(?:final_)?url=(\S+)$`)
	// nodeLine is one element of an observation: its indent, ref, role, name
	// and attributes. bsk prints an element on one line and escapes the
	// quotes and backslashes inside a name or an attribute's value, so page
	// text can neither end a name nor start a line.
	nodeLine = regexp.MustCompile(`^( *)(?:(@e[0-9]+) )?([A-Za-z][A-Za-z0-9]*)(?: "((?:[^"\\]|\\.)*)")?(?: (?:\[[^\]"]*\]|[A-Za-z-]*="(?:[^"\\]|\\.)*"|[A-Za-z-]+=[^\s"]+))*$`)
	// quoted is a name or a value as bsk prints it.
	quoted = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
	// escaped is a character bsk put a backslash before.
	escaped = regexp.MustCompile(`\\(.)`)
)

// readPage reads what bsk printed about one session, in order: an address it
// reached opens a new page, and an observation, which starts with an @vom
// line, replaces the title and the refs, as bsk replaces the refs it knows
// with each observation and each part of one. The title is the name of the
// observation's first RootWebArea, the page itself; the ones under it are
// frames. A ref without a name takes the text of the first line nested
// under it, so a link reads as its visible text. Lines that are not bsk's
// are skipped. It also counts the addresses and the observations it read.
func readPage(output string) (page Page, addresses, observations int) {
	page = Page{Refs: map[string]string{}}
	observing, titled := false, false
	// unnamed is a ref still waiting for the text nested under it.
	unnamed, depth := "", 0
	for _, line := range strings.Split(output, "\n") {
		nested := unnamed
		unnamed = ""
		if m := urlLine.FindStringSubmatch(line); m != nil {
			page, observing = Page{URL: m[1], Refs: map[string]string{}}, false
			addresses++
			continue
		}
		if strings.HasPrefix(line, "@vom ") {
			page.Title, page.Refs, page.continued = "", map[string]string{}, false
			observing, titled = true, false
			observations++
			continue
		}
		if observing && strings.HasPrefix(line, "@continued ") {
			// The page's own root was in an earlier part; a root in this
			// one is a frame's.
			page.continued, titled = true, true
			continue
		}
		m := nodeLine.FindStringSubmatch(line)
		if !observing || m == nil {
			continue
		}
		indent, ref, role, name := len(m[1]), m[2], m[3], m[4]
		label := strings.TrimPrefix(line[indent:], ref+" ")
		named := strings.HasPrefix(label[len(role):], ` "`)
		switch {
		case ref != "":
			page.Refs[ref] = label
			if !named {
				unnamed, depth = ref, indent
			}
		case nested != "" && named && indent > depth:
			page.Refs[nested] += ` "` + name + `"`
		case !titled && role == "RootWebArea":
			page.Title, titled = unescape(name), true
		}
	}
	return page, addresses, observations
}

// unescape is the text bsk quoted.
func unescape(text string) string {
	return escaped.ReplaceAllString(text, "$1")
}
