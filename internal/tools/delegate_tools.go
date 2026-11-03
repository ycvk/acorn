package tools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
)

// Delegator hands work of one run to sub-runs and takes their results. The
// runtime binds one to each run that may delegate.
type Delegator interface {
	Delegate(ctx context.Context, brief core.SubRunBrief) (core.SubRun, error)
	Accept(ctx context.Context, task int) (core.SubRun, error)
	Redo(ctx context.Context, task int, feedback string) (core.SubRun, error)
	// SubRuns lists the run's attempts by task and attempt, and how many
	// more it may delegate.
	SubRuns(ctx context.Context) (attempts []core.SubRun, remaining int, err error)
}

// DelegateToolDeps configure delegate.
type DelegateToolDeps struct {
	Delegator Delegator
}

type DelegateInput struct {
	Action       string   `json:"action" jsonschema:"required,enum=start,enum=accept,enum=redo,enum=status" jsonschema_description:"start hands a new task to a sub-agent; accept takes the result of a reported task; redo sends a reported task back with feedback for another attempt; status lists this run's tasks."`
	Objective    string   `json:"objective,omitempty" jsonschema_description:"For start: what the sub-agent is to achieve, with every fact, link and path it needs. It sees nothing else of this conversation."`
	Deliverables []string `json:"deliverables,omitempty" jsonschema_description:"For start: what it hands back, such as a summary of each article or the path of a file on the workstation."`
	Constraints  []string `json:"constraints,omitempty" jsonschema_description:"For start: limits it keeps to, such as the sources to use or the files not to touch."`
	Acceptance   []string `json:"acceptance,omitempty" jsonschema_description:"For start: how you will tell the result is right; the sub-agent submits evidence against each."`
	Block        []string `json:"block,omitempty" jsonschema_description:"For start: tool name patterns the sub-agent may not call, on top of the owner's rules, such as files for a reviewer that only reads."`
	Task         int      `json:"task,omitempty" jsonschema_description:"For accept and redo: the task number."`
	Feedback     string   `json:"feedback,omitempty" jsonschema_description:"For redo: what is wrong or missing, against the acceptance criteria."`
}

type DelegateOutput struct {
	Tasks []DelegateTask `json:"tasks"`
	// Remaining is how many more attempts this run may delegate.
	Remaining int    `json:"remaining"`
	Note      string `json:"note,omitempty"`
}

// DelegateTask is the latest attempt at a task. State is running, reported,
// failed or accepted.
type DelegateTask struct {
	Task      int    `json:"task"`
	Attempt   int    `json:"attempt"`
	State     string `json:"state"`
	Objective string `json:"objective"`
}

const delegateStartedNote = "It works in the background. Its report arrives in this run as a message when it ends, and this run goes on until every task has reported: do other work meanwhile, or end your turn."

func buildDelegateTool(deps DelegateToolDeps) (einotool.BaseTool, error) {
	if deps.Delegator == nil {
		return nil, errors.New("delegate requires a delegator")
	}
	return inferLocalTool("delegate",
		"Hand a self-contained piece of work to a sub-agent of your own, such as reading one of several articles, or checking a result before the owner sees it. The sub-agent sees only the brief: objective, deliverables, constraints and acceptance. It can search and fetch the web, use the workstation and read the knowledge base, but cannot talk to the owner or wait for an approval. Several tasks run in parallel. When a task reports, accept its result or send it back with redo and feedback.",
		func(ctx context.Context, input DelegateInput) (DelegateOutput, error) {
			switch input.Action {
			case "start":
				brief, err := delegateBrief(input)
				if err != nil {
					return DelegateOutput{}, err
				}
				sub, err := deps.Delegator.Delegate(ctx, brief)
				if err != nil {
					return DelegateOutput{}, fmt.Errorf("delegate: %w", err)
				}
				return delegateResult(ctx, deps, sub, delegateStartedNote)
			case "accept":
				if input.Task < 1 {
					return DelegateOutput{}, errors.New("delegate: task is required for accept")
				}
				sub, err := deps.Delegator.Accept(ctx, input.Task)
				if err != nil {
					return DelegateOutput{}, fmt.Errorf("delegate: %w", err)
				}
				return delegateResult(ctx, deps, sub, "")
			case "redo":
				feedback := strings.TrimSpace(input.Feedback)
				if input.Task < 1 || feedback == "" {
					return DelegateOutput{}, errors.New("delegate: task and feedback are required for redo")
				}
				sub, err := deps.Delegator.Redo(ctx, input.Task, feedback)
				if err != nil {
					return DelegateOutput{}, fmt.Errorf("delegate: %w", err)
				}
				return delegateResult(ctx, deps, sub, delegateStartedNote)
			case "status":
				attempts, remaining, err := deps.Delegator.SubRuns(ctx)
				if err != nil {
					return DelegateOutput{}, fmt.Errorf("delegate: %w", err)
				}
				return DelegateOutput{Tasks: latestTasks(attempts), Remaining: remaining}, nil
			default:
				return DelegateOutput{}, fmt.Errorf("delegate: action must be start, accept, redo or status, got %q", input.Action)
			}
		})
}

func delegateBrief(input DelegateInput) (core.SubRunBrief, error) {
	brief := core.SubRunBrief{
		Objective:    strings.TrimSpace(input.Objective),
		Deliverables: trimmedItems(input.Deliverables),
		Constraints:  trimmedItems(input.Constraints),
		Acceptance:   trimmedItems(input.Acceptance),
		Block:        trimmedItems(input.Block),
	}
	if brief.Objective == "" {
		return core.SubRunBrief{}, errors.New("delegate: objective is required for start")
	}
	for _, pattern := range brief.Block {
		if _, err := path.Match(pattern, ""); err != nil {
			return core.SubRunBrief{}, fmt.Errorf("delegate: block pattern %q: %w", pattern, err)
		}
	}
	return brief, nil
}

func trimmedItems(items []string) []string {
	var out []string
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func delegateResult(ctx context.Context, deps DelegateToolDeps, sub core.SubRun, note string) (DelegateOutput, error) {
	_, remaining, err := deps.Delegator.SubRuns(ctx)
	if err != nil {
		return DelegateOutput{}, fmt.Errorf("delegate: %w", err)
	}
	return DelegateOutput{Tasks: []DelegateTask{delegateTask(sub)}, Remaining: remaining, Note: note}, nil
}

// latestTasks keeps the latest attempt of each task, in task order.
func latestTasks(attempts []core.SubRun) []DelegateTask {
	tasks := []DelegateTask{}
	for _, sub := range attempts {
		if n := len(tasks); n > 0 && tasks[n-1].Task == sub.Task {
			tasks[n-1] = delegateTask(sub)
			continue
		}
		tasks = append(tasks, delegateTask(sub))
	}
	return tasks
}

func delegateTask(sub core.SubRun) DelegateTask {
	state := "running"
	switch {
	case sub.Verdict == core.SubRunAccepted:
		state = "accepted"
	case sub.Status == core.RunStatusFailed:
		state = "failed"
	case sub.Reported():
		state = "reported"
	}
	objective := []rune(sub.Brief.Objective)
	if len(objective) > 120 {
		objective = append(objective[:120], '…')
	}
	return DelegateTask{Task: sub.Task, Attempt: sub.Attempt, State: state, Objective: string(objective)}
}

// SubmitToolDeps configure submit.
type SubmitToolDeps struct {
	Store   SubRunSubmitter
	Context core.ToolCallContextBridge
}

// SubRunSubmitter records what a sub-run hands back.
type SubRunSubmitter interface {
	SubmitSubRun(ctx context.Context, runID string, submission core.SubRunSubmission) error
}

type SubmitInput struct {
	Summary     string   `json:"summary" jsonschema:"required" jsonschema_description:"The result itself, as the deliverables ask: findings, answers, or what you changed and where."`
	Evidence    []string `json:"evidence,omitempty" jsonschema_description:"What shows the result meets each acceptance criterion: commands you ran and their outcome, pages and files you read, with their URLs and paths."`
	Limitations []string `json:"limitations,omitempty" jsonschema_description:"What you could not do or check, calls that were refused, and what the brief left unclear."`
}

type SubmitOutput struct {
	Submitted bool   `json:"submitted"`
	Note      string `json:"note"`
}

func buildSubmitTool(deps SubmitToolDeps) (einotool.BaseTool, error) {
	if deps.Store == nil || deps.Context == nil {
		return nil, errors.New("submit requires a store and the tool call context")
	}
	return inferLocalTool("submit",
		"Hand your result back to the agent that delegated the brief to you: the summary, the evidence for each acceptance criterion, and the limitations. A later submit replaces an earlier one.",
		func(ctx context.Context, input SubmitInput) (SubmitOutput, error) {
			runID := deps.Context.CurrentRunID(ctx)
			if runID == "" {
				return SubmitOutput{}, errors.New("submit: no current run")
			}
			submission := core.SubRunSubmission{
				Summary:     strings.TrimSpace(input.Summary),
				Evidence:    trimmedItems(input.Evidence),
				Limitations: trimmedItems(input.Limitations),
			}
			if submission.Summary == "" {
				return SubmitOutput{}, errors.New("submit: summary is required")
			}
			if err := deps.Store.SubmitSubRun(ctx, runID, submission); err != nil {
				return SubmitOutput{}, fmt.Errorf("submit: %w", err)
			}
			return SubmitOutput{Submitted: true, Note: "Submitted. End now with a one-line reply."}, nil
		})
}

// BuildDelegateToolSpec builds delegate for one run.
func BuildDelegateToolSpec(deps DelegateToolDeps) (core.ToolSpec, error) {
	tool, err := buildDelegateTool(deps)
	if err != nil {
		return core.ToolSpec{}, err
	}
	return enabledLocalSpec("delegate", tool), nil
}

// BuildSubmitToolSpec builds submit for sub-runs.
func BuildSubmitToolSpec(deps SubmitToolDeps) (core.ToolSpec, error) {
	tool, err := buildSubmitTool(deps)
	if err != nil {
		return core.ToolSpec{}, err
	}
	return enabledLocalSpec("submit", tool), nil
}
