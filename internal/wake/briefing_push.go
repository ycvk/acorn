package wake

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

const (
	// briefingPushTitle titles the push that follows a briefing.
	briefingPushTitle = "早安"
	// actionHeading is the briefing section of what the owner has to act on.
	actionHeading = "需要你处理"
	// briefingPushLines bounds the section lines a briefing without focus
	// pushes.
	briefingPushLines = 3
	// briefingPushRunes bounds one line of the push.
	briefingPushRunes = 60
)

// pushBriefing sends the owner the push for a finished briefing. The
// briefing run pushes nothing itself, so its last reply is the whole
// briefing. A reply without sections is an error; a push that fails, such as
// with no device registered, leaves the briefing on the now page and is
// logged, as pending action pushes are.
func (s *Scheduler) pushBriefing(ctx context.Context, briefing core.RoutineOutput) error {
	if s.notifier == nil {
		return nil
	}
	body := briefingPush(briefing.Text)
	if body == "" {
		return fmt.Errorf("push briefing %s: its reply has no section to push", briefing.RunID)
	}
	if _, err := s.notifier.Notify(ctx, core.Notification{Title: briefingPushTitle, Body: body, ThreadID: briefing.ThreadID, RunID: briefing.RunID}); err != nil {
		slog.Error("push briefing", "run_id", briefing.RunID, "error", err)
	}
	return nil
}

// briefingPush is the body of the push that follows a briefing: the titles
// of today's focus in its order or, without focus, the first line of each of
// the first sections; then the first line of what the owner has to act on.
// It is empty for a reply without `## ` sections.
func briefingPush(body string) string {
	var (
		heading   string
		pastFocus bool
		titles    = map[int]string{}
		started   = map[string]bool{}
		focus     []string
		firsts    []string
		action    string
	)
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if h, ok := strings.CutPrefix(trimmed, "## "); ok {
			heading = strings.TrimSpace(h)
			pastFocus = pastFocus || heading == focusHeading
			continue
		}
		if heading == "" || trimmed == "" {
			continue
		}
		switch heading {
		case focusHeading:
			if m := focusLine.FindStringSubmatch(trimmed); m != nil {
				if n, err := strconv.Atoi(m[1]); err == nil && titles[n] != "" {
					focus = append(focus, titles[n])
				}
			}
		case actionHeading:
			if action == "" {
				action = pushLine(trimmed)
			}
		default:
			// News lines come before today's focus and are numbered across
			// their sections; focus lines name them by that number.
			if m := newsLine.FindStringSubmatch(trimmed); m != nil && !pastFocus {
				if n, err := strconv.Atoi(m[1]); err == nil {
					title, _ := newsLineText(m[2])
					titles[n] = pushLine(title)
				}
			}
			if !started[heading] {
				started[heading] = true
				firsts = append(firsts, pushLine(trimmed))
			}
		}
	}
	lines := focus
	if len(lines) == 0 {
		lines = firsts[:min(len(firsts), briefingPushLines)]
	}
	if action != "" {
		lines = append(lines, action)
	}
	return strings.Join(lines, "\n")
}

// pushMarks are the Markdown marks a push shows as they are: bold and inline
// code.
var pushMarks = strings.NewReplacer("**", "", "`", "")

// pushLine is a briefing line as plain text: no number or list marker, bold,
// inline code marks or links, at most briefingPushRunes.
func pushLine(line string) string {
	if m := newsLine.FindStringSubmatch(line); m != nil {
		line = m[2]
	}
	line = strings.TrimLeft(strings.TrimSpace(line), "-*• ")
	line = markdownLink.ReplaceAllStringFunc(line, func(link string) string {
		text, _, _ := strings.Cut(strings.TrimPrefix(link, "["), "](")
		return text
	})
	return truncateRunes(strings.TrimSpace(pushMarks.Replace(line)), briefingPushRunes)
}
