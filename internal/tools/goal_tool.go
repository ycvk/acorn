package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// A goal takes defaultGoalRuns runs of the agent's own unless it says, and
// at most maxGoalRuns.
const (
	defaultGoalRuns = 10
	maxGoalRuns     = 50
)

// GoalToolDeps configure goal.
type GoalToolDeps struct {
	Store   core.GoalStore
	Context core.ToolCallContextBridge
	Clock   func() time.Time
}

type GoalInput struct {
	Action    string `json:"action" jsonschema:"required,enum=start,enum=done,enum=blocked,enum=stop" jsonschema_description:"start sets this conversation's goal; done ends it reached; blocked ends it when you cannot go on without the owner; stop ends it quietly when the owner asks."`
	Objective string `json:"objective,omitempty" jsonschema_description:"For start: the outcome and how to tell it is reached, written so that you understand it later without other context."`
	MaxRuns   int    `json:"max_runs,omitempty" jsonschema_description:"For start: how many runs of your own you may take toward it, 1 to 50; defaults to 10."`
	Result    string `json:"result,omitempty" jsonschema_description:"For done, blocked and stop: what was achieved, what you need from the owner, or why it stopped, written to the owner; it is the text of the push they get."`
}

type GoalOutput struct {
	ID      int64  `json:"id"`
	State   string `json:"state"`
	Runs    int    `json:"runs"`
	MaxRuns int    `json:"max_runs"`
}

func buildGoalTool(deps GoalToolDeps) (einotool.BaseTool, error) {
	return inferLocalTool("goal",
		"Work toward an outcome over several runs of your own in this conversation. After each run here ends you wake up here again to continue, until you end the goal with done or blocked or its runs run out; the owner then gets a push with the result, unless you already notified them in that run. While a follow_up of this conversation waits, the goal waits with it. Use it when the owner hands you work that takes more than one sitting.",
		func(ctx context.Context, input GoalInput) (GoalOutput, error) {
			sessionID := deps.Context.CurrentSessionID(ctx)
			if sessionID == "" {
				return GoalOutput{}, errors.New("goal: no current conversation")
			}
			now := deps.Clock()
			if input.Action == "start" {
				return startGoal(ctx, deps, sessionID, input, now)
			}
			state := map[string]string{"done": core.GoalDone, "blocked": core.GoalBlocked, "stop": core.GoalStopped}[input.Action]
			if state == "" {
				return GoalOutput{}, fmt.Errorf("goal: action must be start, done, blocked or stop, got %q", input.Action)
			}
			result := strings.TrimSpace(input.Result)
			if result == "" {
				return GoalOutput{}, fmt.Errorf("goal: result is required for %s", input.Action)
			}
			active, err := deps.Store.ActiveGoal(ctx, sessionID)
			if err != nil {
				return GoalOutput{}, fmt.Errorf("goal: %w", err)
			}
			ended, err := deps.Store.EndGoal(ctx, active.ID, state, result, deps.Context.CurrentRunID(ctx), now)
			if err != nil {
				return GoalOutput{}, fmt.Errorf("goal: %w", err)
			}
			return GoalOutput{ID: ended.ID, State: ended.State, Runs: ended.Runs, MaxRuns: ended.MaxRuns}, nil
		})
}

func startGoal(ctx context.Context, deps GoalToolDeps, sessionID string, input GoalInput, now time.Time) (GoalOutput, error) {
	objective := strings.TrimSpace(input.Objective)
	if objective == "" {
		return GoalOutput{}, errors.New("goal: objective is required for start")
	}
	runs := input.MaxRuns
	if runs == 0 {
		runs = defaultGoalRuns
	}
	if runs < 1 || runs > maxGoalRuns {
		return GoalOutput{}, fmt.Errorf("goal: max_runs must be between 1 and %d", maxGoalRuns)
	}
	g, err := deps.Store.StartGoal(ctx, core.Goal{SessionID: sessionID, SourceRunID: deps.Context.CurrentRunID(ctx), Objective: objective, MaxRuns: runs, CreatedAt: now})
	if errors.Is(err, core.ErrGoalActive) {
		active, loadErr := deps.Store.ActiveGoal(ctx, sessionID)
		if loadErr != nil {
			return GoalOutput{}, fmt.Errorf("goal: %w", errors.Join(err, loadErr))
		}
		return GoalOutput{}, fmt.Errorf("goal: this conversation already has goal #%d (%s); end it first", active.ID, active.Objective)
	}
	if err != nil {
		return GoalOutput{}, fmt.Errorf("goal: %w", err)
	}
	return GoalOutput{ID: g.ID, State: g.State, Runs: g.Runs, MaxRuns: g.MaxRuns}, nil
}
