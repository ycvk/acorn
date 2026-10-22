package wake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type routineInput struct {
	// thread is the conversation the run belongs in; empty runs it in the
	// routine's own thread.
	thread     string
	sources    []string
	wake       string
	input      string
	skip       bool
	standalone bool
	after      func(context.Context, string) error
}

func (s *Scheduler) runRoutines(ctx context.Context, now time.Time, settings core.Settings) error {
	local := now.In(settings.Location)
	day := local.Format("2006-01-02")
	due := func(at time.Duration) bool {
		scheduled := time.Date(local.Year(), local.Month(), local.Day(), int(at/time.Hour), int(at%time.Hour/time.Minute), 0, 0, settings.Location)
		return !local.Before(scheduled)
	}
	var errs []error
	if settings.Briefing.Enabled && due(settings.Briefing.At) {
		errs = append(errs, s.runRoutine(ctx, "briefing", day, "Briefings", false, now, settings, func() (routineInput, error) { return s.prepareBriefing(ctx, day, now, settings.Location) }))
	}
	if settings.Night.Enabled && due(settings.Night.At) {
		errs = append(errs, s.runRoutine(ctx, "night", day, "Thoughts", true, now, settings, func() (routineInput, error) { return s.nightInput(ctx, day, now, settings.Location) }))
	}
	for _, at := range settings.Wander {
		if !due(at) {
			continue
		}
		slot := fmt.Sprintf("%s %02d:%02d", day, int(at/time.Hour), int(at%time.Hour/time.Minute))
		errs = append(errs, s.runRoutine(ctx, "wander", slot, "", true, now, settings, func() (routineInput, error) {
			return s.wanderInput(ctx, slot, now, settings.Location)
		}))
	}
	return errors.Join(errs...)
}

// A slot is kept after a run starts, including when later bookkeeping fails.
// Preparation/start failures release it; budget skips and empty inputs keep it.
// A run without a prepared thread goes to the routine's latest thread, created
// with title when missing.
func (s *Scheduler) runRoutine(ctx context.Context, name, slot, title string, autonomous bool, now time.Time, settings core.Settings, prepare func() (routineInput, error)) (err error) {
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
		allowed, err := s.withinBudget(ctx, name+" "+slot, now, settings)
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
	wake := core.ScheduledWake{Reason: prepared.wake, Autonomous: autonomous, Standalone: prepared.standalone, SourceIDs: prepared.sources}
	thread, runID := prepared.thread, ""
	if thread != "" {
		runID, err = s.cfg.Runs.StartThreadRun(ctx, thread, prepared.input, wake)
	} else {
		if thread, err = s.cfg.Routines.LatestRoutineThread(ctx, name); err != nil {
			return err
		}
		thread, runID, err = s.cfg.Runs.StartRoutineRun(ctx, thread, title, prepared.input, wake)
	}
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
