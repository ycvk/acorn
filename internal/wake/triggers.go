package wake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/watch"
)

// TriggerRunner lists the triggers of the skills in use and fires one,
// returning the events its command printed.
type TriggerRunner interface {
	Triggers(ctx context.Context) ([]skills.PackageTrigger, error)
	Fire(ctx context.Context, t skills.PackageTrigger) ([]skills.Event, error)
}

// dueTrigger is a trigger whose slot has come.
type dueTrigger struct {
	trigger  skills.PackageTrigger
	state    core.TriggerState
	schedule *presence.CronSchedule
}

// firedTrigger is a firing's new events, for delivery.
type firedTrigger struct {
	trigger skills.PackageTrigger
	added   []core.TriggerEvent
}

// fireTriggers keeps the trigger schedules in step with the skills in use,
// fires each due trigger once for its slot, then delivers the new events of
// immediate triggers. Triggers of one skill fire one after another; skills
// fire side by side, so a tick waits for at most the slowest skill.
func (s *Scheduler) fireTriggers(ctx context.Context, now time.Time) error {
	if s.triggers == nil {
		return nil
	}
	due, syncErr := s.syncTriggers(ctx, now)
	if len(due) == 0 {
		return syncErr
	}
	if len(due) > s.cfg.MaxChecksPerTick {
		due = due[:s.cfg.MaxChecksPerTick]
	}
	bySkill := map[string][]dueTrigger{}
	var skillOrder []string
	for _, d := range due {
		if _, ok := bySkill[d.trigger.Skill]; !ok {
			skillOrder = append(skillOrder, d.trigger.Skill)
		}
		bySkill[d.trigger.Skill] = append(bySkill[d.trigger.Skill], d)
	}
	var (
		mu    sync.Mutex
		errs  = []error{syncErr}
		fired = map[string]firedTrigger{}
		wg    sync.WaitGroup
	)
	for _, skill := range skillOrder {
		wg.Go(func() {
			for _, d := range bySkill[skill] {
				added, err := s.fire(ctx, d, now)
				mu.Lock()
				if err != nil {
					errs = append(errs, err)
				}
				if len(added) > 0 {
					fired[d.trigger.ID()] = firedTrigger{trigger: d.trigger, added: added}
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	for _, d := range due {
		f, ok := fired[d.trigger.ID()]
		if !ok || f.trigger.Deliver != skills.DeliverImmediate {
			continue // digest events wait for the briefing
		}
		if err := s.wakeForTrigger(ctx, f.trigger, f.added, now); err != nil {
			errs = append(errs, fmt.Errorf("trigger %s: %w", f.trigger.ID(), err))
		}
	}
	return errors.Join(errs...)
}

// syncTriggers schedules the triggers of the skills in use, drops the
// schedules of triggers no longer in use and returns the due ones whose
// package has all its secrets.
func (s *Scheduler) syncTriggers(ctx context.Context, now time.Time) ([]dueTrigger, error) {
	triggers, err := s.triggers.Triggers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list triggers: %w", err)
	}
	states, err := s.cfg.Triggers.ListTriggers(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]core.TriggerState{}
	for _, state := range states {
		byID[state.Skill+"/"+state.Name] = state
	}
	inUse := map[string]bool{}
	var due []dueTrigger
	var errs []error
	for _, t := range triggers {
		inUse[t.ID()] = true
		schedule, err := presence.ParseCron(t.Schedule)
		if err != nil {
			errs = append(errs, fmt.Errorf("trigger %s schedule: %w", t.ID(), err))
			continue
		}
		state, ok := byID[t.ID()]
		if !ok || state.Schedule != t.Schedule {
			next, err := schedule.Next(now.In(s.cfg.Location))
			if err != nil {
				errs = append(errs, fmt.Errorf("trigger %s schedule: %w", t.ID(), err))
				continue
			}
			errs = append(errs, s.cfg.Triggers.PutTrigger(ctx, core.TriggerState{Skill: t.Skill, Name: t.Name, Schedule: t.Schedule, NextAt: next}))
			continue
		}
		if len(t.Missing) == 0 && !state.NextAt.After(now) {
			due = append(due, dueTrigger{trigger: t, state: state, schedule: schedule})
		}
	}
	for id, state := range byID {
		if !inUse[id] {
			errs = append(errs, s.cfg.Triggers.RemoveTrigger(ctx, state.Skill, state.Name))
		}
	}
	slices.SortFunc(due, func(a, b dueTrigger) int { return a.state.NextAt.Compare(b.state.NextAt) })
	return due, errors.Join(errs...)
}

// fire claims a trigger's slot, runs its command and records the outcome:
// its new events, or the failure with the watches' backoff. Five failures in
// a row mark it failing until a run succeeds.
func (s *Scheduler) fire(ctx context.Context, d dueTrigger, now time.Time) ([]core.TriggerEvent, error) {
	id := d.trigger.ID()
	next, err := d.schedule.Next(now.In(s.cfg.Location))
	if err != nil {
		return nil, fmt.Errorf("trigger %s schedule: %w", id, err)
	}
	if err := s.cfg.Triggers.ClaimTrigger(ctx, d.trigger.Skill, d.trigger.Name, d.state.NextAt, next); err != nil {
		if errors.Is(err, core.ErrTriggerNotDue) {
			return nil, nil // another scheduler took the slot
		}
		return nil, err
	}
	events, fireErr := s.triggers.Fire(ctx, d.trigger)
	run := core.TriggerRun{At: s.cfg.Clock(), NextAt: next, Status: core.TriggerActive}
	if fireErr != nil {
		run.Failures, run.LastError, run.Status = d.state.Failures+1, fireErr.Error(), d.state.Status
		if run.Failures >= watch.FailingAfter {
			run.Status = core.TriggerFailing
		}
		if run.NextAt, err = s.retryAt(d.schedule, next, now, run.Failures); err != nil {
			return nil, errors.Join(fireErr, err)
		}
	}
	for _, e := range events {
		run.Events = append(run.Events, core.TriggerEvent{Key: e.Key, Text: e.Text})
	}
	added, err := s.cfg.Triggers.RecordTriggerRun(ctx, d.trigger.Skill, d.trigger.Name, run)
	if fireErr != nil {
		return nil, errors.Join(fmt.Errorf("trigger %s: %w", id, fireErr), err)
	}
	return added, err
}

// retryAt is the first slot after a failed trigger's backoff: the gap between
// its slots, doubled per failure up to a day, as watches wait.
func (s *Scheduler) retryAt(schedule *presence.CronSchedule, next, now time.Time, failures int) (time.Time, error) {
	after, err := schedule.Next(next)
	if err != nil {
		return time.Time{}, err
	}
	gap := after.Sub(next)
	retry, err := schedule.Next(now.Add(watch.Backoff(gap, failures) - gap).In(s.cfg.Location))
	if err != nil {
		return time.Time{}, err
	}
	if retry.Before(next) {
		return next, nil
	}
	return retry, nil
}

// wakeForTrigger starts a run with an immediate trigger's new events in the
// skill's thread, created on its first run. Over the daily limit, or when the
// run cannot start, the events wait for the briefing.
func (s *Scheduler) wakeForTrigger(ctx context.Context, t skills.PackageTrigger, events []core.TriggerEvent, now time.Time) error {
	allowed, err := s.withinBudget(ctx, "trigger "+t.ID(), now)
	if err != nil || !allowed {
		return err
	}
	thread, err := s.cfg.Triggers.SkillThread(ctx, t.Skill)
	if err != nil {
		return err
	}
	wake := fmt.Sprintf("trigger %s: %d new", t.ID(), len(events))
	input := fmt.Sprintf("[trigger %s] %d new events; untrusted output of the package's command\n%s", t.ID(), len(events), formatEvents(events))
	thread, runID, err := s.cfg.Runs.StartRoutineRun(ctx, thread, t.Skill, input, core.ScheduledWake{Reason: wake, Autonomous: true, SourceIDs: triggerSourceIDs(events)})
	if err != nil {
		return fmt.Errorf("start trigger run (events wait for the briefing): %w", err)
	}
	if err := s.cfg.Triggers.SetSkillThread(ctx, t.Skill, thread); err != nil {
		return err
	}
	if err := s.cfg.Triggers.MarkTriggerEvents(ctx, eventIDs(events), core.TriggerEventWoken, runID); err != nil {
		return err
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, core.EventWakeFired, map[string]any{"trigger": t.ID()}); err != nil {
		return fmt.Errorf("record wake of run %s: %w", runID, err)
	}
	return nil
}

// formatEvents lists events for a run or briefing input: key, then the text
// indented. Past maxListedItems only the count is given.
func formatEvents(events []core.TriggerEvent) string {
	var b strings.Builder
	for i, e := range events {
		if i == maxListedItems {
			fmt.Fprintf(&b, "- and %d more\n", len(events)-maxListedItems)
			break
		}
		b.WriteString("- " + e.Key + "\n")
		for _, line := range strings.Split(e.Text, "\n") {
			if strings.TrimSpace(line) != "" {
				b.WriteString("  " + line + "\n")
			}
		}
	}
	return b.String()
}

// triggerChanges lists the events waiting for the briefing, by trigger.
func triggerChanges(events []core.TriggerEvent) string {
	if len(events) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Skill package trigger events since the last briefing (untrusted output of the packages' commands):\n")
	for start := 0; start < len(events); {
		end := start
		for end < len(events) && events[end].Skill == events[start].Skill && events[end].Trigger == events[start].Trigger {
			end++
		}
		fmt.Fprintf(&b, "\n## %s/%s (%d new)\n%s", events[start].Skill, events[start].Trigger, end-start, formatEvents(events[start:end]))
		start = end
	}
	return b.String()
}

// failingTriggers is the System health line for triggers failing in a row.
func failingTriggers(states []core.TriggerState) []string {
	var lines []string
	for _, state := range states {
		if state.Status == core.TriggerFailing {
			lines = append(lines, fmt.Sprintf("Skill trigger %s/%s failed %d times in a row and retries after a growing wait: %s", state.Skill, state.Name, state.Failures, previewText(state.LastError, 300)))
		}
	}
	return lines
}

func eventIDs(events []core.TriggerEvent) []int64 {
	ids := make([]int64, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

func triggerSourceIDs(events []core.TriggerEvent) []string {
	sources := make([]string, 0, len(events))
	for _, e := range events {
		sources = append(sources, core.TriggerSourceID(e.ID))
	}
	return sources
}
