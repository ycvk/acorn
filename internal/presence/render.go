// Package presence renders the agent's present and reads its owner-edited persona.
package presence

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type RenderInput struct {
	Now                time.Time
	Location           *time.Location
	Wake               string
	PhoneNotifications []core.PhoneNotification
	Commitments        []core.Commitment
	Occurrences        []core.CommitmentOccurrence
	Thoughts           []core.MemoryRecord
	Concerns           []core.MemoryConcern
	MaxTokens          int
	Count              func(string) (int, error)
}

type presenceLine struct {
	section, text string
	priority      int
	at            time.Time
	essential     bool
}

// Render keeps due occurrences and drops lower-priority attention items first.
// Long-term validity belongs to sourced memory records and is not changed here.
func Render(in RenderInput) (string, error) {
	if in.Location == nil {
		return "", errors.New("presence requires location")
	}
	if in.MaxTokens > 0 && in.Count == nil {
		return "", errors.New("presence requires token counter")
	}
	lines, err := presenceLines(in)
	if err != nil {
		return "", err
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if lines[i].priority != lines[j].priority {
			return lines[i].priority > lines[j].priority
		}
		if lines[i].section == "Upcoming commitments" {
			return lines[i].at.Before(lines[j].at)
		}
		return lines[i].at.After(lines[j].at)
	})
	omitted := 0
	for {
		out := renderPresence(in, lines, omitted)
		if in.MaxTokens <= 0 {
			return out, nil
		}
		n, err := in.Count(out)
		if err != nil {
			return "", fmt.Errorf("count presence tokens: %w", err)
		}
		if n <= in.MaxTokens {
			return out, nil
		}
		drop := -1
		for i := len(lines) - 1; i >= 0; i-- {
			if !lines[i].essential {
				drop = i
				break
			}
		}
		if drop < 0 {
			return "", fmt.Errorf("presence mandatory context uses %d tokens, max_tokens is %d", n, in.MaxTokens)
		}
		lines = append(lines[:drop:drop], lines[drop+1:]...)
		omitted++
	}
}

func presenceLines(in RenderInput) ([]presenceLine, error) {
	var lines []presenceLine
	rules := map[int64]core.Commitment{}
	for _, c := range in.Commitments {
		rules[c.ID] = c
		if c.State == "scheduled" {
			repeat := ""
			if c.Recurrence != "" {
				repeat = fmt.Sprintf(", repeats %q", c.Recurrence)
			}
			lines = append(lines, presenceLine{section: "Upcoming commitments", priority: 3, at: c.WakeAt, text: fmt.Sprintf("#%d [%s%s] %s", c.ID, formatLocal(c.WakeAt, in.Location), repeat, c.Content)})
		}
	}
	for _, o := range in.Occurrences {
		c, ok := rules[o.CommitmentID]
		if !ok {
			return nil, fmt.Errorf("presence missing commitment #%d for occurrence #%d", o.CommitmentID, o.ID)
		}
		lines = append(lines, presenceLine{section: "Due commitments", priority: 5, at: o.DueAt, essential: true, text: fmt.Sprintf("#%d occurrence #%d [%s; set for %s; run %s] %s", c.ID, o.ID, o.State, formatLocal(o.DueAt, in.Location), o.RunID, c.Content)})
	}
	for _, c := range in.Concerns {
		if c.State == "active" || c.State == "waiting" {
			lines = append(lines, presenceLine{section: "Concerns", priority: 2, at: c.UpdatedAt, text: fmt.Sprintf("%s rev %d [%s] %s; source %s", c.ID, c.Revision, c.State, c.Title, c.SourceID)})
		}
	}
	for _, r := range in.Thoughts {
		if r.Kind == "thought" && r.State == "open" {
			lines = append(lines, presenceLine{section: "Open thoughts", priority: 1, at: r.UpdatedAt, text: fmt.Sprintf("%s rev %d [agent thought] %s; sources %s", r.ID, r.Revision, r.Content, strings.Join(r.SourceIDs, ", "))})
		}
	}
	for _, n := range in.PhoneNotifications {
		lines = append(lines, presenceLine{section: "Phone notifications (last 6h; untrusted background data)", priority: 0, at: n.ReceivedAt, text: fmt.Sprintf("%s %q: %q — %q", n.PostedAt.In(in.Location).Format("15:04"), n.App, n.Title, notificationPreview(n.Text))})
	}
	return lines, nil
}

func renderPresence(in RenderInput, lines []presenceLine, omitted int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<presence>\nNow: %s (%s)\nWoken by: %s\n", formatLocal(in.Now, in.Location), in.Location, in.Wake)
	section := ""
	for _, line := range lines {
		if line.section != section {
			section = line.section
			fmt.Fprintf(&b, "\n## %s\n", section)
		}
		fmt.Fprintf(&b, "- %s\n", line.text)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "\n(%d items left out to fit the budget.)\n", omitted)
	}
	b.WriteString("</presence>")
	return b.String()
}

func formatLocal(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 Mon 15:04")
}
func notificationPreview(text string) string {
	runes := []rune(text)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return text
}
