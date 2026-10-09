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

// PresenceToolDeps are what the working-memory tools need.
type PresenceToolDeps struct {
	Store    core.PresenceStore
	Context  core.ToolCallContextBridge
	Clock    func() time.Time
	Location *time.Location
}

const localTimeLayout = "2006-01-02 Mon 15:04"

// MemoryWriteInput is the input of keep and think.
type MemoryWriteInput struct {
	Content string `json:"content" jsonschema_description:"The text to keep. For keep, the owner's own words verbatim."`
}

// MemoryWriteOutput reports the stored item.
type MemoryWriteOutput struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	ExpiresAt string `json:"expires_at"`
}

func buildMemoryWriteTool(name, description string, kind core.MemoryKind, deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool(name, description, func(ctx context.Context, input MemoryWriteInput, _ ToolProgressEmitter) (MemoryWriteOutput, error) {
		content := strings.TrimSpace(input.Content)
		if content == "" {
			return MemoryWriteOutput{}, fmt.Errorf("%s: content is required", name)
		}
		now := deps.Clock()
		item, err := deps.Store.AddMemoryItem(ctx, core.MemoryItem{
			Kind:        kind,
			Content:     content,
			Status:      core.MemoryActive,
			SessionID:   deps.Context.CurrentSessionID(ctx),
			SourceRunID: deps.Context.CurrentRunID(ctx),
			ExpiresAt:   presence.NewExpiry(kind, now),
			CreatedAt:   now,
		})
		if err != nil {
			return MemoryWriteOutput{}, fmt.Errorf("%s: %w", name, err)
		}
		return MemoryWriteOutput{ID: item.ID, Kind: string(kind), ExpiresAt: item.ExpiresAt.In(deps.Location).Format(localTimeLayout)}, nil
	})
}

// ScheduleWakeInput is the input of schedule_wake.
type ScheduleWakeInput struct {
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
			item, err := deps.Store.AddMemoryItem(ctx, core.MemoryItem{
				Kind:        core.MemoryCommitment,
				Content:     task,
				Status:      core.MemoryActive,
				SessionID:   sessionID,
				SourceRunID: deps.Context.CurrentRunID(ctx),
				WakeAt:      wakeAt,
				Recurrence:  strings.TrimSpace(input.Recurrence),
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

// SettleInput is the input of settle.
type SettleInput struct {
	ID      int64  `json:"id" jsonschema_description:"The #id of the item."`
	Action  string `json:"action" jsonschema:"enum=renew,enum=internalize,enum=release,enum=done" jsonschema_description:"renew keeps an item active longer; internalize turns what the owner said or a thought into a tendency or concern; release lets an item go or cancels a commitment; done settles a commitment you woke up for."`
	As      string `json:"as,omitempty" jsonschema:"enum=tendency,enum=ruler" jsonschema_description:"For internalize: tendency for the owner's lasting preferences, ruler for your own concerns and assumptions."`
	Content string `json:"content,omitempty" jsonschema_description:"For internalize: the tendency or concern in your own words."`
}

// SettleOutput reports the change.
type SettleOutput struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	NewID  int64  `json:"new_id,omitempty"`
}

func buildSettleTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("settle",
		"Decide what happens to a working-memory item: renew, internalize, release, or mark a woken commitment done.",
		func(ctx context.Context, input SettleInput, _ ToolProgressEmitter) (SettleOutput, error) {
			item, err := deps.Store.LoadMemoryItem(ctx, input.ID)
			if err != nil {
				return SettleOutput{}, fmt.Errorf("settle: %w", err)
			}
			now := deps.Clock()
			out := SettleOutput{ID: item.ID}
			switch input.Action {
			case "renew":
				if item.Kind == core.MemoryCommitment {
					return SettleOutput{}, errors.New("settle: commitments are not renewed; use schedule_wake for a new time")
				}
				if item.Status != core.MemoryActive && item.Status != core.MemoryResting && item.Status != core.MemorySunk {
					return SettleOutput{}, fmt.Errorf("settle: #%d is %s and cannot be renewed", item.ID, item.Status)
				}
				item.Status, item.ExpiresAt = core.MemoryActive, presence.NewExpiry(item.Kind, now)
			case "internalize":
				newID, err := internalize(ctx, deps, *item, input, now)
				if err != nil {
					return SettleOutput{}, fmt.Errorf("settle: %w", err)
				}
				out.NewID = newID
				item.Status = core.MemoryInternalized
			case "release":
				switch item.Status {
				case core.MemoryActive, core.MemoryResting, core.MemorySunk, core.MemoryWoken:
				default:
					return SettleOutput{}, fmt.Errorf("settle: #%d is already %s", item.ID, item.Status)
				}
				item.Status = core.MemoryReleased
			case "done":
				if item.Kind != core.MemoryCommitment || item.Status != core.MemoryWoken {
					return SettleOutput{}, fmt.Errorf("settle: done applies to a woken commitment; #%d is a %s %s", item.ID, item.Status, item.Kind)
				}
				item.Status = core.MemorySettled
			default:
				return SettleOutput{}, fmt.Errorf("settle: unknown action %q", input.Action)
			}
			item.UpdatedAt = now
			if err := deps.Store.UpdateMemoryItem(ctx, *item); err != nil {
				return SettleOutput{}, fmt.Errorf("settle: %w", err)
			}
			out.Status = string(item.Status)
			return out, nil
		})
}

func internalize(ctx context.Context, deps PresenceToolDeps, item core.MemoryItem, input SettleInput, now time.Time) (int64, error) {
	if item.Kind != core.MemorySaid && item.Kind != core.MemoryThought {
		return 0, fmt.Errorf("only what the owner said or a thought can be internalized; #%d is a %s", item.ID, item.Kind)
	}
	if item.Status == core.MemoryInternalized || item.Status == core.MemoryReleased {
		return 0, fmt.Errorf("#%d is already %s", item.ID, item.Status)
	}
	kind := core.MemoryKind(input.As)
	if kind != core.MemoryTendency && kind != core.MemoryRuler {
		return 0, errors.New("internalize needs as: tendency or ruler")
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return 0, errors.New("internalize needs content")
	}
	added, err := deps.Store.AddMemoryItem(ctx, core.MemoryItem{
		Kind:        kind,
		Content:     content,
		Status:      core.MemoryActive,
		SessionID:   item.SessionID,
		SourceRunID: deps.Context.CurrentRunID(ctx),
		ExpiresAt:   presence.NewExpiry(kind, now),
		CreatedAt:   now,
	})
	if err != nil {
		return 0, err
	}
	return added.ID, nil
}

// RecallInput is the input of recall.
type RecallInput struct {
	Query string `json:"query" jsonschema_description:"Words to look for in past conversations and working memory."`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Maximum results (default 10, at most 50)."`
}

// RecallHit is one recall result.
type RecallHit struct {
	Source    string `json:"source"`
	RunID     string `json:"run_id,omitempty"`
	MemoryID  int64  `json:"memory_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Snippet   string `json:"snippet"`
	CreatedAt string `json:"created_at"`
}

// RecallOutput lists recall results.
type RecallOutput struct {
	Hits []RecallHit `json:"hits"`
}

func buildRecallTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("recall",
		"Search what happened before: past conversations and runs, and all working-memory items including resting, sunk and released ones.",
		func(ctx context.Context, input RecallInput, _ ToolProgressEmitter) (RecallOutput, error) {
			limit := input.Limit
			if limit <= 0 {
				limit = 10
			}
			limit = min(limit, 50)
			hits, err := deps.Store.SearchExperience(ctx, input.Query, limit)
			if err != nil {
				return RecallOutput{}, fmt.Errorf("recall: %w", err)
			}
			out := RecallOutput{Hits: make([]RecallHit, 0, len(hits))}
			for _, h := range hits {
				out.Hits = append(out.Hits, RecallHit{
					Source: h.Source, RunID: h.RunID, MemoryID: h.MemoryID, Kind: string(h.Kind),
					Snippet: h.Snippet, CreatedAt: h.CreatedAt.In(deps.Location).Format(localTimeLayout),
				})
			}
			return out, nil
		})
}
