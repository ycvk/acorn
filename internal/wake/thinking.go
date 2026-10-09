package wake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Scheduler) nightInput(ctx context.Context, day string, now time.Time) (routineInput, error) {
	items, err := s.cfg.Store.ListMemoryItems(ctx, []core.MemoryStatus{core.MemoryActive, core.MemoryResting, core.MemoryWoken})
	if err != nil {
		return routineInput{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[night %s] night reflection\nReview this working memory:\n", day)
	n := 0
	for _, item := range items {
		review := item.Status == core.MemoryResting ||
			(item.Status == core.MemoryActive && item.Kind == core.MemoryThought && !item.CreatedAt.After(now.Add(-24*time.Hour))) ||
			(item.Status == core.MemoryWoken && !item.UpdatedAt.After(now.Add(-24*time.Hour))) ||
			(item.Status == core.MemoryActive && item.Kind == core.MemorySaid && !item.ExpiresAt.IsZero() && !item.ExpiresAt.After(now.Add(48*time.Hour)))
		if !review {
			continue
		}
		n++
		fmt.Fprintf(&b, "- #%d %s %s %s %s\n", item.ID, item.Kind, item.Status, item.CreatedAt.In(s.cfg.Location).Format("2006-01-02"), previewText(item.Content, 200))
	}
	return routineInput{wake: "night reflection " + day, input: b.String(), skip: n == 0}, nil
}

func previewText(text string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
