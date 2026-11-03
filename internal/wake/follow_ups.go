package wake

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

// Workstation is what follow-up checks read on the agent's workstation.
type Workstation interface {
	Job(ctx context.Context, id string) (workstation.Job, error)
	FindJobOutput(ctx context.Context, id, text string, offset int64) (workstation.JobFind, error)
	Stat(ctx context.Context, path string) (workstation.StatResult, error)
}

// checkFollowUps wakes the agent for each waiting follow-up whose condition
// holds or whose deadline passed, once its conversation is idle and the wake
// fits the day's budget.
func (s *Scheduler) checkFollowUps(ctx context.Context, now time.Time, settings core.Settings) error {
	if err := s.cfg.FollowUps.RecoverFollowUpClaims(ctx, now); err != nil {
		return fmt.Errorf("recover follow-up claims: %w", err)
	}
	waiting, err := s.cfg.FollowUps.WaitingFollowUps(ctx, "")
	if err != nil {
		return fmt.Errorf("list follow-ups: %w", err)
	}
	var errs []error
	for _, f := range waiting {
		if err := s.followUp(ctx, f, now, settings); err != nil {
			errs = append(errs, fmt.Errorf("follow-up #%d: %w", f.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) followUp(ctx context.Context, f core.FollowUp, now time.Time, settings core.Settings) error {
	reason, err := s.followUpReason(ctx, f, now, settings)
	if err != nil || reason == "" {
		return err
	}
	idle, err := s.threadIdle(ctx, f.SessionID)
	if err != nil || !idle {
		return err
	}
	allowed, err := s.withinBudget(ctx, fmt.Sprintf("follow-up #%d", f.ID), now, settings)
	if err != nil || !allowed {
		return err
	}
	if err := s.cfg.FollowUps.ClaimFollowUp(ctx, f.ID, reason, now); errors.Is(err, core.ErrFollowUpNotWaiting) {
		return nil
	} else if err != nil {
		return err
	}
	input := fmt.Sprintf("[follow_up #%d, set %s] %s\nWhat happened: %s",
		f.ID, f.CreatedAt.In(settings.Location).Format("2006-01-02 15:04"), f.Task, reason)
	runID, err := s.cfg.Runs.StartWakeRun(ctx, f.SessionID, input, core.ScheduledWake{Reason: fmt.Sprintf("follow-up #%d: %s", f.ID, reason), Autonomous: true})
	if err != nil {
		return errors.Join(fmt.Errorf("start wake run: %w", err), s.cfg.FollowUps.ReleaseFollowUp(ctx, f.ID, now))
	}
	if err := s.cfg.FollowUps.StartFollowUp(ctx, f.ID, runID, now); err != nil {
		return fmt.Errorf("record run %s: %w", runID, err)
	}
	if _, err := s.cfg.Events.AppendEvent(ctx, runID, core.EventWakeFired, map[string]any{"follow_up_id": f.ID}); err != nil {
		return fmt.Errorf("record wake of run %s: %w", runID, err)
	}
	return nil
}

// followUpReason says what happened once the follow-up should wake the
// agent, and is empty while it waits. Past the deadline it wakes the agent
// with how things stand, also when checking failed.
func (s *Scheduler) followUpReason(ctx context.Context, f core.FollowUp, now time.Time, settings core.Settings) (string, error) {
	if s.cfg.Workstation == nil {
		if now.Before(f.DeadlineAt) {
			return "", nil
		}
		return "its deadline passed; no workstation is configured to check it", nil
	}
	var reason, status string
	var err error
	switch {
	case f.OutputContains != "":
		reason, status, err = s.jobPrinted(ctx, f, now)
	case f.JobID != "":
		reason, status, err = s.jobEnded(ctx, f)
	default:
		reason, status, err = s.pathReady(ctx, f, now)
	}
	if reason != "" || now.Before(f.DeadlineAt) {
		return reason, err
	}
	deadline := f.DeadlineAt.In(settings.Location).Format("2006-01-02 15:04")
	if err != nil {
		return fmt.Sprintf("its deadline %s passed; checking failed: %v", deadline, err), nil
	}
	return fmt.Sprintf("its deadline %s passed; %s", deadline, status), nil
}

// jobEnded holds once the job is no longer running.
func (s *Scheduler) jobEnded(ctx context.Context, f core.FollowUp) (reason, status string, err error) {
	job, err := s.cfg.Workstation.Job(ctx, f.JobID)
	if gone(err) {
		return fmt.Sprintf("job %s is no longer on the workstation", f.JobID), "", nil
	}
	if err != nil {
		return "", "", err
	}
	if job.State == workstation.JobRunning {
		return "", fmt.Sprintf("job %s is still running", f.JobID), nil
	}
	return jobEnd(job), "", nil
}

// jobPrinted holds once the job's output holds the text, or the job ended
// without printing it. The search continues where the last one stopped.
func (s *Scheduler) jobPrinted(ctx context.Context, f core.FollowUp, now time.Time) (reason, status string, err error) {
	found, err := s.cfg.Workstation.FindJobOutput(ctx, f.JobID, f.OutputContains, f.ScannedOffset)
	if gone(err) {
		return fmt.Sprintf("job %s is no longer on the workstation", f.JobID), "", nil
	}
	if err != nil {
		return "", "", err
	}
	switch {
	case found.Found:
		return fmt.Sprintf("job %s printed %q", f.JobID, f.OutputContains), "", nil
	case found.Job.State != workstation.JobRunning:
		return fmt.Sprintf("%s without printing %q", jobEnd(found.Job), f.OutputContains), "", nil
	}
	if err := s.recordCheck(ctx, f, found.NextOffset, nil, now); err != nil {
		return "", "", err
	}
	return "", fmt.Sprintf("job %s is still running and has not printed %q", f.JobID, f.OutputContains), nil
}

// pathReady holds once a directory exists at the path, or a file whose size
// did not change since the previous check.
func (s *Scheduler) pathReady(ctx context.Context, f core.FollowUp, now time.Time) (reason, status string, err error) {
	st, err := s.cfg.Workstation.Stat(ctx, f.Path)
	if err != nil {
		return "", "", err
	}
	switch {
	case !st.Exists:
		if f.PathSize != nil {
			if err := s.recordCheck(ctx, f, 0, nil, now); err != nil {
				return "", "", err
			}
		}
		return "", fmt.Sprintf("%s does not exist yet", st.Path), nil
	case st.Dir:
		return fmt.Sprintf("directory %s exists", st.Path), "", nil
	case f.PathSize != nil && *f.PathSize == st.Size:
		return fmt.Sprintf("%s is ready: %d bytes, unchanged since the previous check", st.Path, st.Size), "", nil
	}
	if err := s.recordCheck(ctx, f, 0, &st.Size, now); err != nil {
		return "", "", err
	}
	return "", fmt.Sprintf("%s is still being written (%d bytes)", st.Path, st.Size), nil
}

// recordCheck keeps what a check saw; a follow-up cancelled meanwhile needs
// nothing kept.
func (s *Scheduler) recordCheck(ctx context.Context, f core.FollowUp, scanned int64, size *int64, now time.Time) error {
	err := s.cfg.FollowUps.RecordFollowUpCheck(ctx, f.ID, scanned, size, now)
	if errors.Is(err, core.ErrFollowUpNotWaiting) {
		return nil
	}
	return err
}

// threadIdle reports whether nothing is going in a conversation: no run
// running or waiting for the owner, and no owner message waiting for a run.
func (s *Scheduler) threadIdle(ctx context.Context, sessionID string) (bool, error) {
	latest, err := s.cfg.RunRecords.LoadLatestRunForSession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	if latest != nil && (latest.Status == core.RunStatusRunning || latest.Status == core.RunStatusInterrupted) {
		return false, nil
	}
	queued, err := s.cfg.RunRecords.ListQueuedMessages(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return len(queued) == 0, nil
}

func jobEnd(job workstation.Job) string {
	switch {
	case job.State == workstation.JobExited && job.ExitCode != nil:
		return fmt.Sprintf("job %s exited with code %d", job.ID, *job.ExitCode)
	case job.Note != "":
		return fmt.Sprintf("job %s is %s: %s", job.ID, job.State, job.Note)
	default:
		return fmt.Sprintf("job %s is %s", job.ID, job.State)
	}
}

// gone reports whether the workstation no longer has the job.
func gone(err error) bool {
	var httpErr *workstation.HTTPError
	return errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound
}
