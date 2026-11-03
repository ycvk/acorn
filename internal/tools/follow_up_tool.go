package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// A follow-up wakes the agent at its deadline at the latest.
const (
	defaultFollowUpWithin = 24 * time.Hour
	maxFollowUpWithin     = 7 * 24 * time.Hour
)

type FollowUpInput struct {
	Task           string `json:"task,omitempty" jsonschema_description:"What to do when you wake, written so that you understand it later without other context."`
	JobID          string `json:"job_id,omitempty" jsonschema_description:"Wake when this job ends, as shell returned it."`
	OutputContains string `json:"output_contains,omitempty" jsonschema_description:"With job_id: wake as soon as the job prints this text, or when it ends without printing it."`
	Path           string `json:"path,omitempty" jsonschema_description:"Wake when a file here exists and has stopped growing, or a directory here exists; absolute or relative to the home directory."`
	Within         string `json:"within,omitempty" jsonschema_description:"Wake after this long regardless, e.g. 90m, 6h or 3d; defaults to 24h, at most 7d."`
	Cancel         int64  `json:"cancel,omitempty" jsonschema_description:"Give only this to cancel the waiting follow-up with this id."`
}

type FollowUpOutput struct {
	ID       int64  `json:"id"`
	State    string `json:"state"`
	Deadline string `json:"deadline,omitempty"`
}

func buildFollowUpTool(cfg WorkstationToolsConfig) (einotool.BaseTool, error) {
	return inferLocalTool("follow_up",
		"Be woken in this conversation when something on your workstation happens: a job ends or prints a text, or a file is ready. Set it and end the run; you wake up in a new run with the task and what happened, and at its deadline in any case. Conditions are checked every half minute, once the conversation is idle.",
		func(ctx context.Context, input FollowUpInput) (FollowUpOutput, error) {
			sessionID := cfg.Context.CurrentSessionID(ctx)
			if sessionID == "" {
				return FollowUpOutput{}, errors.New("follow_up: no current conversation to wake in")
			}
			now := cfg.Clock()
			if input.Cancel != 0 {
				if input != (FollowUpInput{Cancel: input.Cancel}) {
					return FollowUpOutput{}, errors.New("follow_up: give cancel alone")
				}
				if err := cfg.FollowUps.CancelFollowUp(ctx, sessionID, input.Cancel, now); err != nil {
					return FollowUpOutput{}, fmt.Errorf("follow_up: %w", err)
				}
				return FollowUpOutput{ID: input.Cancel, State: core.FollowUpCancelled}, nil
			}
			task, jobID, path := strings.TrimSpace(input.Task), strings.TrimSpace(input.JobID), strings.TrimSpace(input.Path)
			switch {
			case task == "":
				return FollowUpOutput{}, errors.New("follow_up: task is required")
			case (jobID == "") == (path == ""):
				return FollowUpOutput{}, errors.New("follow_up: give either job_id or path")
			case input.OutputContains != "" && jobID == "":
				return FollowUpOutput{}, errors.New("follow_up: output_contains needs job_id")
			case len(input.OutputContains) > workstation.MaxJobFindText:
				return FollowUpOutput{}, fmt.Errorf("follow_up: output_contains must be at most %d bytes", workstation.MaxJobFindText)
			}
			within := defaultFollowUpWithin
			if input.Within != "" {
				d, err := parseWakeIn(strings.TrimSpace(input.Within))
				if err != nil {
					return FollowUpOutput{}, fmt.Errorf("follow_up: within: %w", err)
				}
				if d > maxFollowUpWithin {
					return FollowUpOutput{}, fmt.Errorf("follow_up: within must be at most %s", maxFollowUpWithin)
				}
				within = d
			}
			if jobID != "" {
				job, err := cfg.Service.Job(ctx, jobID)
				if err != nil {
					return FollowUpOutput{}, fmt.Errorf("follow_up: %w", err)
				}
				if job.State != workstation.JobRunning {
					return FollowUpOutput{}, fmt.Errorf("follow_up: job %s already ended (%s); read it with job", jobID, job.State)
				}
			}
			loc, err := ownerLocation(ctx, cfg.Settings)
			if err != nil {
				return FollowUpOutput{}, fmt.Errorf("follow_up: %w", err)
			}
			f, err := cfg.FollowUps.AddFollowUp(ctx, core.FollowUp{
				SessionID:      sessionID,
				SourceRunID:    cfg.Context.CurrentRunID(ctx),
				Task:           task,
				JobID:          jobID,
				OutputContains: input.OutputContains,
				Path:           path,
				DeadlineAt:     now.Add(within).UTC(),
				CreatedAt:      now,
			})
			if err != nil {
				return FollowUpOutput{}, fmt.Errorf("follow_up: %w", err)
			}
			return FollowUpOutput{ID: f.ID, State: f.State, Deadline: f.DeadlineAt.In(loc).Format(localTimeLayout)}, nil
		})
}
