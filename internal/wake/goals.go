package wake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// Titles of the pushes that announce an ended goal.
const (
	goalDonePushTitle    = "目标完成"
	goalBlockedPushTitle = "目标需要你"
)

// continueGoals starts the next run of each active goal whose conversation
// is idle, and ends the goals whose runs ran out or whose last run failed.
func (s *Scheduler) continueGoals(ctx context.Context, now time.Time, settings core.Settings) error {
	goals, err := s.cfg.Goals.ActiveGoals(ctx)
	if err != nil {
		return fmt.Errorf("list goals: %w", err)
	}
	var errs []error
	for _, g := range goals {
		if err := s.continueGoal(ctx, g, now, settings); err != nil {
			errs = append(errs, fmt.Errorf("goal #%d: %w", g.ID, err))
		}
	}
	return errors.Join(errs...)
}

// continueGoal waits while the conversation has a run going, an owner
// message waiting or a follow-up waiting. A run that Acorn stopped by
// restarting is no failure of the goal.
func (s *Scheduler) continueGoal(ctx context.Context, g core.Goal, now time.Time, settings core.Settings) error {
	latest, err := s.cfg.RunRecords.LoadLatestRunForSession(ctx, g.SessionID)
	if err != nil {
		return err
	}
	if latest == nil {
		return nil
	}
	restarted := latest.Status == core.RunStatusFailed && (latest.Error == core.AbandonedRunError || latest.Error == core.ErrShuttingDown.Error())
	if latest.Status == core.RunStatusFailed && !restarted {
		return s.endGoal(ctx, g, core.GoalBlocked, fmt.Sprintf("run %s failed: %s", latest.RunID, latest.Error), now)
	}
	idle, err := s.threadIdle(ctx, g.SessionID)
	if err != nil || !idle {
		return err
	}
	waiting, err := s.cfg.FollowUps.WaitingFollowUps(ctx, g.SessionID)
	if err != nil || len(waiting) > 0 {
		return err
	}
	if g.Runs >= g.MaxRuns {
		return s.endGoal(ctx, g, core.GoalBlocked, fmt.Sprintf("used all %d runs without reaching it", g.MaxRuns), now)
	}
	allowed, err := s.withinBudget(ctx, fmt.Sprintf("goal #%d", g.ID), now, settings)
	if err != nil || !allowed {
		return err
	}
	if err := s.cfg.Goals.CountGoalRun(ctx, g.ID, g.Runs, now); errors.Is(err, core.ErrGoalChanged) {
		return nil
	} else if err != nil {
		return err
	}
	input := fmt.Sprintf("[goal #%d, run %d of %d] Objective: %s\n", g.ID, g.Runs+1, g.MaxRuns, g.Objective)
	if restarted {
		input += "The last run stopped when Acorn restarted; check what it left unfinished.\n"
	}
	input += "Keep working toward the objective from where this conversation left off. When it is reached, call goal with done and what was achieved; when you cannot go on without the owner, call goal with blocked and what you need. To wait for a job or a file, set a follow_up and end the run; you continue here when it fires."
	runID, err := s.cfg.Runs.StartThreadRun(ctx, g.SessionID, input, core.ScheduledWake{Reason: fmt.Sprintf("goal #%d: %s", g.ID, previewText(g.Objective, 80)), Autonomous: true})
	if err != nil {
		return fmt.Errorf("start goal run: %w", err)
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, core.EventWakeFired, map[string]any{"goal_id": g.ID}); err != nil {
		return fmt.Errorf("record wake of run %s: %w", runID, err)
	}
	return nil
}

func (s *Scheduler) endGoal(ctx context.Context, g core.Goal, state, result string, now time.Time) error {
	if _, err := s.cfg.Goals.EndGoal(ctx, g.ID, state, result, "", now); err != nil && !errors.Is(err, core.ErrGoalNotFound) && !errors.Is(err, core.ErrGoalEnded) {
		return err
	}
	return nil
}

// announceGoals pushes each goal that ended done or blocked, once the run
// that ended it has finished, so its reply is in the conversation the push
// opens. A goal the agent ended in a run that already pushed to the owner
// needs no second push.
func (s *Scheduler) announceGoals(ctx context.Context, now time.Time) error {
	goals, err := s.cfg.Goals.UnannouncedGoals(ctx)
	if err != nil {
		return fmt.Errorf("list ended goals: %w", err)
	}
	var errs []error
	for _, g := range goals {
		latest, err := s.cfg.RunRecords.LoadLatestRunForSession(ctx, g.SessionID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if latest != nil && (latest.Status == core.RunStatusRunning || latest.Status == core.RunStatusInterrupted) {
			continue
		}
		notified := false
		if g.EndRunID != "" {
			if notified, err = s.cfg.RunRecords.RunNotifiedOwner(ctx, g.EndRunID); err != nil {
				errs = append(errs, fmt.Errorf("goal #%d: %w", g.ID, err))
				continue
			}
		}
		if s.notifier != nil && !notified {
			title := goalDonePushTitle
			if g.State == core.GoalBlocked {
				title = goalBlockedPushTitle
			}
			n := core.Notification{Title: title, Body: previewText(g.Result, 120), ThreadID: g.SessionID}
			if latest != nil {
				n.RunID = latest.RunID
			}
			if _, err := s.notifier.Notify(ctx, n); err != nil {
				slog.Error("push goal end", "goal", g.ID, "error", err)
			}
		}
		if err := s.cfg.Goals.MarkGoalAnnounced(ctx, g.ID, now); err != nil && !errors.Is(err, core.ErrGoalChanged) {
			errs = append(errs, fmt.Errorf("goal #%d: %w", g.ID, err))
		}
	}
	return errors.Join(errs...)
}
