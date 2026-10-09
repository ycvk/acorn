package wake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// Thinking holds owner-local times for night reflection and idle thought.
type Thinking struct {
	Night  Briefing
	Wander []time.Duration
}

type routineInput struct {
	wake  string
	input string
	skip  bool
	after func(context.Context, string) error
}

func (s *Scheduler) runRoutines(ctx context.Context, now time.Time) error {
	local := now.In(s.cfg.Location)
	day := local.Format("2006-01-02")
	due := func(at time.Duration) bool {
		scheduled := time.Date(local.Year(), local.Month(), local.Day(), int(at/time.Hour), int(at%time.Hour/time.Minute), 0, 0, s.cfg.Location)
		return !local.Before(scheduled)
	}
	var errs []error
	if s.cfg.Briefing.Enabled && due(s.cfg.Briefing.At) {
		errs = append(errs, s.runRoutine(ctx, "briefing", day, "Briefings", false, now, func() (routineInput, error) { return s.prepareBriefing(ctx, day, now) }))
	}
	if s.cfg.Thinking.Night.Enabled && due(s.cfg.Thinking.Night.At) {
		errs = append(errs, s.runRoutine(ctx, "night", day, "Thoughts", true, now, func() (routineInput, error) { return s.nightInput(ctx, day, now) }))
	}
	for _, at := range s.cfg.Thinking.Wander {
		if !due(at) {
			continue
		}
		slot := fmt.Sprintf("%s %02d:%02d", day, int(at/time.Hour), int(at%time.Hour/time.Minute))
		errs = append(errs, s.runRoutine(ctx, "wander", slot, "Thoughts", true, now, func() (routineInput, error) {
			return routineInput{wake: "idle thought " + slot, input: "[wander " + slot + "] idle time"}, nil
		}))
	}
	return errors.Join(errs...)
}

// A slot is kept after a run starts, including when later bookkeeping fails.
// Preparation/start failures release it; budget skips and empty inputs keep it.
func (s *Scheduler) runRoutine(ctx context.Context, name, slot, title string, autonomous bool, now time.Time, prepare func() (routineInput, error)) (err error) {
	if err := s.cfg.Routines.ClaimRoutine(ctx, name, slot, now); err != nil {
		if errors.Is(err, core.ErrRoutineTaken) {
			return nil
		}
		return err
	}
	keep := false
	defer func() {
		if !keep {
			if releaseErr := s.cfg.Routines.ReleaseRoutine(ctx, name, slot); releaseErr != nil {
				err = errors.Join(err, fmt.Errorf("release routine %s: %w", name, releaseErr))
			}
		}
	}()
	if autonomous {
		allowed, err := s.withinBudget(ctx, name+" "+slot, now)
		if err != nil {
			return err
		}
		if !allowed {
			keep = true
			return nil
		}
	}
	prepared, err := prepare()
	if err != nil {
		return fmt.Errorf("prepare %s: %w", name, err)
	}
	if prepared.skip {
		keep = true
		return nil
	}
	names := []string{"briefing"}
	if autonomous {
		names = []string{"night", "wander"}
	}
	thread, err := s.cfg.Routines.LatestRoutineThread(ctx, names...)
	if err != nil {
		return err
	}
	thread, runID, err := s.cfg.Runs.StartRoutineRun(ctx, thread, title, prepared.wake, prepared.input)
	if err != nil {
		return fmt.Errorf("start %s run: %w", name, err)
	}
	keep = true
	if err := s.cfg.Routines.SetRoutineRun(ctx, name, slot, thread, runID); err != nil {
		return err
	}
	kind, payload := EventBriefingFired, map[string]any{"day": slot}
	if autonomous {
		kind, payload = core.EventWakeFired, map[string]any{"routine": name, "slot": slot}
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, kind, payload); err != nil {
		return fmt.Errorf("record %s run: %w", name, err)
	}
	if prepared.after != nil {
		return prepared.after(ctx, runID)
	}
	return nil
}
