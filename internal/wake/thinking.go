package wake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Scheduler) nightInput(ctx context.Context, day string, now time.Time) (routineInput, error) {
	last, err := s.cfg.Routines.LastRoutineAt(ctx, "night", day)
	if err != nil {
		return routineInput{}, err
	}
	if last.IsZero() {
		last = now.Add(-24 * time.Hour)
	}
	records, err := s.cfg.Memory.ListMemoryRecords(ctx, core.MemoryQuery{Mode: "current", ChangedSince: last, Limit: 100})
	if err != nil {
		return routineInput{}, err
	}
	concerns, err := s.cfg.Memory.ListConcerns(ctx, true)
	if err != nil {
		return routineInput{}, err
	}
	occurrences, err := s.cfg.Commitments.ListDueOccurrences(ctx)
	if err != nil {
		return routineInput{}, err
	}
	status, err := s.cfg.Memory.MemoryProcessingStatus(ctx)
	if err != nil {
		return routineInput{}, err
	}
	skills, err := s.cfg.Skills.Skills(ctx)
	if err != nil {
		return routineInput{}, err
	}
	loads, err := s.cfg.Skills.SkillLoadsSince(ctx, last)
	if err != nil {
		return routineInput{}, err
	}
	var sources []string
	var b strings.Builder
	fmt.Fprintf(&b, "[night %s] night reflection\nReview changed evidence, understandings that need review, open concerns and unfinished actions. Use recall and memory_read to inspect evidence before changing anything.\nMemory processing: pending=%d failed=%d\n", day, status.Pending, status.Failed)
	n := 0
	for _, r := range records {
		if !r.UpdatedAt.After(last) && !r.NeedsReview && r.Kind != "thought" {
			continue
		}
		n++
		sources = append(sources, r.SourceIDs...)
		fmt.Fprintf(&b, "- memory %s rev %d %s %s needs_review=%t: %s\n", r.ID, r.Revision, r.Kind, r.State, r.NeedsReview, previewText(r.Content, 200))
	}
	for _, c := range concerns {
		n++
		sources = append(sources, c.SourceID)
		fmt.Fprintf(&b, "- concern %s rev %d [%s]: %s (review %s)\n", c.ID, c.Revision, c.State, c.Title, c.ReviewAt.In(s.cfg.Location).Format(time.RFC3339))
	}
	for _, o := range occurrences {
		n++
		fmt.Fprintf(&b, "- commitment #%d occurrence #%d [%s] run %s\n", o.CommitmentID, o.ID, o.State, o.RunID)
	}
	// Skills the agent changed or loaded since the last night; a release's
	// rewrite of a built-in skill is not the agent's work to review.
	for _, skill := range skills {
		written := skill.Author == core.SkillAuthorAgent && skill.UpdatedAt.After(last)
		if !written && loads[skill.ID] == 0 {
			continue
		}
		n++
		state := "in use"
		if skill.Retired {
			state = "retired"
		}
		fmt.Fprintf(&b, "- skill %s rev %d [%s] written=%t loads=%d\n", skill.ID, skill.Revision, state, written, loads[skill.ID])
	}
	return routineInput{sources: sources, wake: "night reflection " + day, input: b.String(), skip: n == 0}, nil
}

// wanderInput advances the due concern revised longest ago, in the
// conversation it was created in. A concern whose conversation is busy with a
// running run or one waiting for the owner is left for a later slot.
func (s *Scheduler) wanderInput(ctx context.Context, slot string, now time.Time) (routineInput, error) {
	concerns, err := s.cfg.Memory.ListConcerns(ctx, true)
	if err != nil {
		return routineInput{}, err
	}
	slices.SortStableFunc(concerns, func(a, b core.MemoryConcern) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
	for _, c := range concerns {
		if c.ReviewAt.After(now) {
			continue
		}
		latest, err := s.cfg.RunRecords.LoadLatestRunForSession(ctx, c.ThreadID)
		if err != nil {
			return routineInput{}, err
		}
		if latest != nil && (latest.Status == core.RunStatusRunning || latest.Status == core.RunStatusInterrupted) {
			continue
		}
		return routineInput{thread: c.ThreadID, sources: []string{c.SourceID}, wake: "idle thought " + slot, input: fmt.Sprintf("[wander %s] Advance concern %s rev %d [%s]: %s. Latest reason: %s\nThis is the conversation the concern was raised in; continue from the progress made here before. Inspect evidence with recall; capture a sourced thought, useful progress, or a concrete next appointment, and record what this advance did with concern. A lack of owner response remains unknown feedback.", slot, c.ID, c.Revision, c.State, c.Title, c.Reason)}, nil
	}
	return routineInput{skip: true}, nil
}

func previewText(text string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
