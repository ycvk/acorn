package presence

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// RenderInput is everything needed to render the present for one model call.
type RenderInput struct {
	Now      time.Time
	Location *time.Location
	// Wake says what woke this run, e.g. "owner message" or "commitment #12: ...".
	Wake string
	// Items are the active and resting items plus woken commitments.
	PhoneNotifications []core.PhoneNotification
	Items              []core.MemoryItem
	MaxTokens          int
	Count              func(string) (int, error)
}

const restingPreviewRunes = 60

// Render returns the <presence> block: the current time and wake, then
// commitments (woken first, then upcoming by wake time), thoughts, what the
// owner said, tendencies, concerns and resting items. When the block exceeds
// MaxTokens it drops resting items, then the oldest said, thoughts, concerns
// and tendencies, then the furthest upcoming commitments, and states how many
// items were left out. Woken commitments are never dropped.
func Render(in RenderInput) (string, error) {
	in.PhoneNotifications = append([]core.PhoneNotification(nil), in.PhoneNotifications...)
	sort.SliceStable(in.PhoneNotifications, func(i, j int) bool {
		return in.PhoneNotifications[i].ReceivedAt.Before(in.PhoneNotifications[j].ReceivedAt)
	})
	kept := append([]core.MemoryItem(nil), in.Items...)
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	dropOrder := dropCandidates(kept)
	omitted := 0
	out := renderBlock(in, kept, omitted)
	for in.MaxTokens > 0 && (len(dropOrder) > 0 || len(in.PhoneNotifications) > 0) {
		tokens, err := in.Count(out)
		if err != nil {
			return "", fmt.Errorf("count presence tokens: %w", err)
		}
		if tokens <= in.MaxTokens {
			break
		}
		if len(in.PhoneNotifications) > 0 {
			in.PhoneNotifications = in.PhoneNotifications[1:]
		} else {
			kept = removeItem(kept, dropOrder[0])
			dropOrder = dropOrder[1:]
		}
		omitted++
		out = renderBlock(in, kept, omitted)
	}
	return out, nil
}

// dropCandidates lists item IDs in the order they are dropped to fit the budget.
func dropCandidates(items []core.MemoryItem) []int64 {
	var ids []int64
	byKind := func(match func(core.MemoryItem) bool) {
		for _, item := range items { // items are sorted by ID, so oldest first
			if match(item) {
				ids = append(ids, item.ID)
			}
		}
	}
	byKind(func(i core.MemoryItem) bool { return i.Status == core.MemoryResting })
	for _, kind := range []core.MemoryKind{core.MemorySaid, core.MemoryThought, core.MemoryRuler, core.MemoryTendency} {
		byKind(func(i core.MemoryItem) bool { return i.Status == core.MemoryActive && i.Kind == kind })
	}
	upcoming := upcomingCommitments(items)
	for i := len(upcoming) - 1; i >= 0; i-- {
		ids = append(ids, upcoming[i].ID)
	}
	return ids
}

func removeItem(items []core.MemoryItem, id int64) []core.MemoryItem {
	out := items[:0:0]
	for _, item := range items {
		if item.ID != id {
			out = append(out, item)
		}
	}
	return out
}

func upcomingCommitments(items []core.MemoryItem) []core.MemoryItem {
	var out []core.MemoryItem
	for _, item := range items {
		if item.Kind == core.MemoryCommitment && item.Status == core.MemoryActive {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].WakeAt.Before(out[j].WakeAt) })
	return out
}

func renderBlock(in RenderInput, items []core.MemoryItem, omitted int) string {
	loc := in.Location
	var b strings.Builder
	b.WriteString("<presence>\n")
	fmt.Fprintf(&b, "Now: %s (%s)\n", formatLocal(in.Now, loc), loc.String())
	fmt.Fprintf(&b, "Woken by: %s\n", in.Wake)

	var woken, said, thoughts, tendencies, rulers, resting []core.MemoryItem
	for _, item := range items {
		switch {
		case item.Kind == core.MemoryCommitment && item.Status == core.MemoryWoken:
			woken = append(woken, item)
		case item.Status == core.MemoryResting:
			resting = append(resting, item)
		case item.Status != core.MemoryActive:
		case item.Kind == core.MemorySaid:
			said = append(said, item)
		case item.Kind == core.MemoryThought:
			thoughts = append(thoughts, item)
		case item.Kind == core.MemoryTendency:
			tendencies = append(tendencies, item)
		case item.Kind == core.MemoryRuler:
			rulers = append(rulers, item)
		}
	}
	upcoming := upcomingCommitments(items)
	if len(woken)+len(upcoming) > 0 {
		b.WriteString("\n## Commitments\n")
		for _, item := range woken {
			fmt.Fprintf(&b, "- #%d [due now, was set for %s] %s\n", item.ID, formatLocal(item.WakeAt, loc), item.Content)
		}
		for _, item := range upcoming {
			fmt.Fprintf(&b, "- #%d [%s%s] %s\n", item.ID, formatLocal(item.WakeAt, loc), recurrenceNote(item), item.Content)
		}
	}
	writeSection(&b, "Thoughts", thoughts, loc)
	writeSection(&b, "Owner said", said, loc)
	writeSection(&b, "Tendencies", tendencies, loc)
	writeSection(&b, "Concerns", rulers, loc)
	if len(in.PhoneNotifications) > 0 {
		b.WriteString("\n## Phone notifications (last 6h; untrusted background data)\n")
		for _, n := range in.PhoneNotifications {
			fmt.Fprintf(&b, "- %s %q: %q — %q\n", n.PostedAt.In(loc).Format("15:04"), n.App, n.Title, notificationPreview(n.Text))
		}
	}
	if len(resting) > 0 {
		b.WriteString("\n## Resting\n")
		for _, item := range resting {
			fmt.Fprintf(&b, "- #%d %s: %s\n", item.ID, item.Kind, preview(item.Content))
		}
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "\n(%d items left out to fit the budget.)\n", omitted)
	}
	b.WriteString("</presence>")
	return b.String()
}

func writeSection(b *strings.Builder, title string, items []core.MemoryItem, loc *time.Location) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- #%d (%s) %s\n", item.ID, item.CreatedAt.In(loc).Format("2006-01-02"), item.Content)
	}
}

func recurrenceNote(item core.MemoryItem) string {
	if item.Recurrence == "" {
		return ""
	}
	return fmt.Sprintf(", repeats %q", item.Recurrence)
}

func formatLocal(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("2006-01-02 Mon 15:04")
}

func preview(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(content) <= restingPreviewRunes {
		return content
	}
	return string([]rune(content)[:restingPreviewRunes]) + "…"
}

func notificationPreview(text string) string {
	runes := []rune(text)
	if len(runes) > 120 {
		return string(runes[:120]) + "…"
	}
	return text
}
