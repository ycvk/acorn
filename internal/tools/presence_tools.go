package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

const localTimeLayout = "2006-01-02 Mon 15:04"

// ScheduleWakeInput is the input of schedule_wake.
type ScheduleWakeInput struct {
	ConcernID  string `json:"concern_id,omitempty" jsonschema_description:"Optional continuing concern this appointment advances."`
	Task       string `json:"task" jsonschema_description:"What to do when you wake, written so that you understand it later without other context."`
	At         string `json:"at,omitempty" jsonschema_description:"Wake time: RFC3339, or YYYY-MM-DD HH:MM in the owner's timezone."`
	In         string `json:"in,omitempty" jsonschema_description:"Wake after this long, e.g. 90m, 2h, 3d."`
	Recurrence string `json:"recurrence,omitempty" jsonschema_description:"Optional 5-field cron in the owner's timezone (minute hour day month weekday) to repeat the wake."`
}

// ScheduleWakeOutput reports the commitment.
type ScheduleWakeOutput struct {
	ID         int64  `json:"id"`
	WakeAt     string `json:"wake_at"`
	Recurrence string `json:"recurrence,omitempty"`
}

func buildScheduleWakeTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("schedule_wake",
		"Make an appointment with yourself: at the given time you wake up in this conversation with the task as your input. Use it for reminders, follow-ups and anything to do later.",
		func(ctx context.Context, input ScheduleWakeInput, _ ToolProgressEmitter) (ScheduleWakeOutput, error) {
			task := strings.TrimSpace(input.Task)
			if task == "" {
				return ScheduleWakeOutput{}, errors.New("schedule_wake: task is required")
			}
			sessionID := deps.Context.CurrentSessionID(ctx)
			if sessionID == "" {
				return ScheduleWakeOutput{}, errors.New("schedule_wake: no current conversation to wake in")
			}
			now := deps.Clock()
			wakeAt, err := resolveWakeTime(input, now, deps.Location)
			if err != nil {
				return ScheduleWakeOutput{}, fmt.Errorf("schedule_wake: %w", err)
			}
			item, err := deps.Store.AddCommitment(ctx, core.Commitment{
				Content:     task,
				State:       "scheduled",
				SessionID:   sessionID,
				SourceRunID: deps.Context.CurrentRunID(ctx),
				WakeAt:      wakeAt,
				Recurrence:  strings.TrimSpace(input.Recurrence),
				ConcernID:   input.ConcernID,
				CreatedAt:   now,
			})
			if err != nil {
				return ScheduleWakeOutput{}, fmt.Errorf("schedule_wake: %w", err)
			}
			return ScheduleWakeOutput{ID: item.ID, WakeAt: wakeAt.In(deps.Location).Format(localTimeLayout), Recurrence: item.Recurrence}, nil
		})
}

// resolveWakeTime picks the first wake: at or in when given, otherwise the
// next occurrence of the recurrence.
func resolveWakeTime(input ScheduleWakeInput, now time.Time, loc *time.Location) (time.Time, error) {
	at, in, recurrence := strings.TrimSpace(input.At), strings.TrimSpace(input.In), strings.TrimSpace(input.Recurrence)
	if at != "" && in != "" {
		return time.Time{}, errors.New("give either at or in, not both")
	}
	var schedule *presence.CronSchedule
	if recurrence != "" {
		var err error
		if schedule, err = presence.ParseCron(recurrence); err != nil {
			return time.Time{}, fmt.Errorf("recurrence: %w", err)
		}
	}
	var wakeAt time.Time
	switch {
	case at != "":
		t, err := parseWakeAt(at, loc)
		if err != nil {
			return time.Time{}, err
		}
		wakeAt = t
	case in != "":
		d, err := parseWakeIn(in)
		if err != nil {
			return time.Time{}, err
		}
		wakeAt = now.Add(d)
	case schedule != nil:
		next, err := schedule.Next(now.In(loc))
		if err != nil {
			return time.Time{}, fmt.Errorf("recurrence: %w", err)
		}
		wakeAt = next
	default:
		return time.Time{}, errors.New("give at, in or recurrence")
	}
	if !wakeAt.After(now) {
		return time.Time{}, fmt.Errorf("wake time %s is not in the future", wakeAt.In(loc).Format(localTimeLayout))
	}
	return wakeAt.UTC(), nil
}

func parseWakeAt(value string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", value, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("at %q must be RFC3339 or YYYY-MM-DD HH:MM", value)
	}
	return t, nil
}

// parseWakeIn accepts Go durations plus whole days ("3d").
func parseWakeIn(value string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("in %q: days must be a positive whole number", value)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("in %q must be a positive duration such as 90m, 2h or 3d", value)
	}
	return d, nil
}

// SettleInput completes one occurrence or cancels a commitment.
type SettleInput struct {
	ID           int64  `json:"id" jsonschema_description:"Commitment id."`
	OccurrenceID int64  `json:"occurrence_id,omitempty" jsonschema_description:"The occurrence shown in presence; required for done."`
	Action       string `json:"action" jsonschema:"enum=done,enum=cancel"`
	SourceID     string `json:"source_id,omitempty" jsonschema_description:"For done, an owner confirmation or tool execution source."`
}
type SettleOutput struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

func buildSettleTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("settle", "Complete one due appointment with outcome evidence, or cancel a commitment and its future occurrences.", func(ctx context.Context, in SettleInput, _ ToolProgressEmitter) (SettleOutput, error) {
		err := deps.Store.SettleCommitment(ctx, core.CommitmentSettlement{CommitmentID: in.ID, OccurrenceID: in.OccurrenceID, Action: in.Action, SourceID: in.SourceID, RunID: deps.Context.CurrentRunID(ctx), Now: deps.Clock()})
		if err != nil {
			return SettleOutput{}, err
		}
		state := "completed"
		if in.Action == "cancel" {
			state = "cancelled"
		}
		return SettleOutput{ID: in.ID, Status: state}, nil
	})
}
