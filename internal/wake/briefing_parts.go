package wake

import (
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// BriefingPart is a part of a briefing reply: the lines under one heading,
// or before the first one.
type BriefingPart struct {
	Heading string
	Body    string
}

// BriefingParts are the parts of a briefing reply a preview shows, in order,
// each with lines under it. Once the briefing's picked stories are known,
// their sections and today's focus are left out: the stories stand for them.
func BriefingParts(body string, stories []core.BriefingStory) []BriefingPart {
	var (
		parts   []BriefingPart
		heading string
		lines   []string
	)
	flush := func() {
		if len(lines) > 0 {
			parts = append(parts, BriefingPart{Heading: heading, Body: strings.Join(lines, "\n")})
		}
		lines = nil
	}
	for line := range strings.Lines(body) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "#"):
			flush()
			heading = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		case trimmed != "":
			lines = append(lines, trimmed)
		}
	}
	flush()
	if len(stories) == 0 {
		return parts
	}
	return slices.DeleteFunc(parts, func(p BriefingPart) bool {
		return p.Heading == focusHeading || slices.ContainsFunc(stories, func(s core.BriefingStory) bool { return s.Section == p.Heading })
	})
}
