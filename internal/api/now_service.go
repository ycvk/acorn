package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/wake"
)

// nowStore is the part of the store the now page reads and changes.
type nowStore interface {
	ListCommitments(ctx context.Context, activeOnly bool) ([]core.Commitment, error)
	ListDueOccurrences(ctx context.Context) ([]core.CommitmentOccurrence, error)
	SettleCommitment(ctx context.Context, change core.CommitmentSettlement) error
	ListConcerns(ctx context.Context, activeOnly bool) ([]core.MemoryConcern, error)
	ActiveGoals(ctx context.Context) ([]core.Goal, error)
	EndGoal(ctx context.Context, id int64, state, result, endRunID string, now time.Time) (core.Goal, error)
	WaitingFollowUps(ctx context.Context, sessionID string) ([]core.FollowUp, error)
	CancelFollowUp(ctx context.Context, sessionID string, id int64, now time.Time) error
	LoadSession(ctx context.Context, sessionID string) (*core.SessionRecord, error)
	ListWatches(ctx context.Context) ([]core.Watch, error)
	LoadWatch(ctx context.Context, id int64) (*core.Watch, error)
	UpdateWatch(ctx context.Context, w core.Watch) error
	LatestRoutineOutput(ctx context.Context, routine string) (core.RoutineOutput, error)
	Panels(ctx context.Context) ([]core.Panel, error)
	BriefingStories(ctx context.Context, runID string) ([]core.BriefingStory, error)
	ReactToNewsStory(ctx context.Context, storyID int64, reaction core.NewsReaction, deviceID string, at time.Time) error
	ClearNewsReaction(ctx context.Context, storyID int64) error
}

// NowService serves the now page: the latest briefing, the views the owner
// pinned, what is coming up, what the agent is working on, what it waits
// for and the watches.
type NowService struct {
	store nowStore
	clock func() time.Time
}

func NewNowService(store nowStore, clock func() time.Time) (*NowService, error) {
	if store == nil || clock == nil {
		return nil, errors.New("now service: store and clock are required")
	}
	return &NowService{store: store, clock: clock}, nil
}

// NowResponse is GET /v1/now.
type NowResponse struct {
	Briefing    *NowBriefingDTO    `json:"briefing,omitempty"`
	Panels      []NowPanelDTO      `json:"panels"`
	Commitments []NowCommitmentDTO `json:"commitments"`
	Concerns    []NowConcernDTO    `json:"concerns"`
	Goals       []NowGoalDTO       `json:"goals"`
	FollowUps   []NowFollowUpDTO   `json:"follow_ups"`
	Watches     []NowWatchDTO      `json:"watches"`
}

// NowBriefingDTO is the reply of the newest succeeded briefing run with the
// stories it picked.
type NowBriefingDTO struct {
	ThreadID   string                `json:"thread_id"`
	RunID      string                `json:"run_id"`
	Date       string                `json:"date"`
	Body       string                `json:"body"`
	FinishedAt time.Time             `json:"finished_at"`
	Stories    []NowBriefingStoryDTO `json:"stories"`
	Parts      []NowBriefingPartDTO  `json:"parts"`
}

// NowBriefingPartDTO is a part of the briefing the card previews.
type NowBriefingPartDTO struct {
	Heading string `json:"heading,omitempty"`
	Body    string `json:"body"`
}

// NowBriefingStoryDTO is a story the briefing picked, with the owner's
// reaction to it, its conversation and whether Acorn is following it.
type NowBriefingStoryDTO struct {
	StoryID   int64  `json:"story_id"`
	Section   string `json:"section"`
	Title     string `json:"title"`
	Summary   string `json:"summary,omitempty"`
	URL       string `json:"url"`
	Focus     bool   `json:"focus"`
	Reaction  string `json:"reaction,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	Following bool   `json:"following"`
}

// NowPanelDTO is a view pinned to the now page, with its conversation and
// when its content last changed.
type NowPanelDTO struct {
	ThreadID    string        `json:"thread_id"`
	ThreadTitle string        `json:"thread_title"`
	View        core.ViewData `json:"view"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// NowCommitmentDTO is a scheduled or due commitment with the occurrences that
// still wait for an outcome.
type NowCommitmentDTO struct {
	ID          int64              `json:"id"`
	Content     string             `json:"content"`
	State       string             `json:"state"`
	WakeAt      time.Time          `json:"wake_at"`
	Recurrence  string             `json:"recurrence,omitempty"`
	Occurrences []NowOccurrenceDTO `json:"occurrences"`
}

// NowOccurrenceDTO is one wake of a commitment that has not been settled.
type NowOccurrenceDTO struct {
	ID    int64     `json:"id"`
	DueAt time.Time `json:"due_at"`
	State string    `json:"state"`
	RunID string    `json:"run_id,omitempty"`
}

// NowConcernDTO is an active or waiting concern.
type NowConcernDTO struct {
	ID        string     `json:"id"`
	ThreadID  string     `json:"thread_id"`
	Title     string     `json:"title"`
	Outcome   string     `json:"outcome,omitempty"`
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	ReviewAt  *time.Time `json:"review_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// NowGoalDTO is an active goal: what it works toward, the runs it took of
// the most it may take, and its conversation.
type NowGoalDTO struct {
	ID          int64     `json:"id"`
	ThreadID    string    `json:"thread_id"`
	ThreadTitle string    `json:"thread_title"`
	Objective   string    `json:"objective"`
	Runs        int       `json:"runs"`
	MaxRuns     int       `json:"max_runs"`
	CreatedAt   time.Time `json:"created_at"`
}

// What a waiting follow-up waits for.
const (
	followUpWaitsForJobEnd    = "job_end"
	followUpWaitsForJobOutput = "job_output"
	followUpWaitsForPath      = "path"
)

// NowFollowUpDTO is a waiting follow-up: what wakes the agent, the deadline
// at which it wakes regardless, and its conversation.
type NowFollowUpDTO struct {
	ID             int64     `json:"id"`
	ThreadID       string    `json:"thread_id"`
	ThreadTitle    string    `json:"thread_title"`
	Task           string    `json:"task"`
	WaitsFor       string    `json:"waits_for"`
	JobID          string    `json:"job_id,omitempty"`
	OutputContains string    `json:"output_contains,omitempty"`
	Path           string    `json:"path,omitempty"`
	DeadlineAt     time.Time `json:"deadline_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// NowWatchDTO is one watch.
type NowWatchDTO struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	Target          string     `json:"target"`
	Mode            string     `json:"mode"`
	Section         string     `json:"section,omitempty"`
	Issue           bool       `json:"issue,omitempty"`
	Trial           bool       `json:"trial,omitempty"`
	Status          string     `json:"status"`
	IntervalSeconds int64      `json:"interval_seconds"`
	NextCheckAt     time.Time  `json:"next_check_at"`
	LastCheckedAt   *time.Time `json:"last_checked_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	Failures        int        `json:"failures"`
}

// Load reads every section; any failing read fails the whole page.
func (s *NowService) Load(ctx context.Context) (NowResponse, error) {
	briefing, err := s.store.LatestRoutineOutput(ctx, "briefing")
	if err != nil {
		return NowResponse{}, fmt.Errorf("latest briefing: %w", err)
	}
	commitments, err := s.store.ListCommitments(ctx, true)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list commitments: %w", err)
	}
	occurrences, err := s.store.ListDueOccurrences(ctx)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list due occurrences: %w", err)
	}
	concerns, err := s.store.ListConcerns(ctx, true)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list concerns: %w", err)
	}
	goals, err := s.store.ActiveGoals(ctx)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list goals: %w", err)
	}
	followUps, err := s.store.WaitingFollowUps(ctx, "")
	if err != nil {
		return NowResponse{}, fmt.Errorf("list follow-ups: %w", err)
	}
	watches, err := s.store.ListWatches(ctx)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list watches: %w", err)
	}
	panels, err := s.store.Panels(ctx)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list panels: %w", err)
	}
	out := NowResponse{Panels: nowPanels(panels), Concerns: nowConcerns(concerns), Watches: nowWatches(watches)}
	if briefing.RunID != "" {
		stories, err := s.store.BriefingStories(ctx, briefing.RunID)
		if err != nil {
			return NowResponse{}, fmt.Errorf("briefing stories: %w", err)
		}
		out.Briefing = &NowBriefingDTO{ThreadID: briefing.ThreadID, RunID: briefing.RunID, Date: briefing.Slot, Body: briefing.Text, FinishedAt: briefing.FinishedAt.UTC(), Stories: nowBriefingStories(stories), Parts: nowBriefingParts(briefing.Text, stories)}
	}
	if out.Commitments, err = nowCommitments(commitments, occurrences); err != nil {
		return NowResponse{}, err
	}
	titles, err := s.threadTitles(ctx, goals, followUps)
	if err != nil {
		return NowResponse{}, err
	}
	out.Goals, out.FollowUps = nowGoals(goals, titles), nowFollowUps(followUps, titles)
	return out, nil
}

// threadTitles reads the title of each conversation a goal or follow-up
// belongs to.
func (s *NowService) threadTitles(ctx context.Context, goals []core.Goal, followUps []core.FollowUp) (map[string]string, error) {
	titles := map[string]string{}
	ids := make([]string, 0, len(goals)+len(followUps))
	for _, g := range goals {
		ids = append(ids, g.SessionID)
	}
	for _, f := range followUps {
		ids = append(ids, f.SessionID)
	}
	for _, id := range ids {
		if _, ok := titles[id]; ok {
			continue
		}
		session, err := s.store.LoadSession(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("conversation %s: %w", id, err)
		}
		titles[id] = session.Title
	}
	return titles, nil
}

// CancelCommitment cancels a commitment and its unsettled occurrences, the
// same way the agent's settle tool does.
func (s *NowService) CancelCommitment(ctx context.Context, id int64) error {
	return s.store.SettleCommitment(ctx, core.CommitmentSettlement{CommitmentID: id, Action: "cancel", Now: s.clock()})
}

// ownerStoppedGoal is the result of a goal the owner stopped.
const ownerStoppedGoal = "the owner stopped it"

// StopGoal ends an active goal as stopped: no further run starts for it and
// nothing is pushed.
func (s *NowService) StopGoal(ctx context.Context, id int64) error {
	_, err := s.store.EndGoal(ctx, id, core.GoalStopped, ownerStoppedGoal, "", s.clock())
	return err
}

// CancelFollowUp cancels a waiting follow-up of any conversation.
func (s *NowService) CancelFollowUp(ctx context.Context, id int64) error {
	return s.store.CancelFollowUp(ctx, "", id, s.clock())
}

// PauseWatch stops checking a watch.
func (s *NowService) PauseWatch(ctx context.Context, id int64) error {
	return s.changeWatch(ctx, id, core.Watch.Pause)
}

// ResumeWatch checks a watch right away and keeps checking it.
func (s *NowService) ResumeWatch(ctx context.Context, id int64) error {
	return s.changeWatch(ctx, id, core.Watch.Resume)
}

func (s *NowService) changeWatch(ctx context.Context, id int64, change func(core.Watch, time.Time) core.Watch) error {
	w, err := s.store.LoadWatch(ctx, id)
	if err != nil {
		return err
	}
	return s.store.UpdateWatch(ctx, change(*w, s.clock()))
}

// ReactToStory records the owner's reaction to a story a briefing picked.
func (s *NowService) ReactToStory(ctx context.Context, storyID int64, reaction core.NewsReaction, deviceID string) error {
	return s.store.ReactToNewsStory(ctx, storyID, reaction, deviceID, s.clock())
}

// ClearStoryReaction drops the owner's reaction to a story.
func (s *NowService) ClearStoryReaction(ctx context.Context, storyID int64) error {
	return s.store.ClearNewsReaction(ctx, storyID)
}

func nowBriefingStories(stories []core.BriefingStory) []NowBriefingStoryDTO {
	out := make([]NowBriefingStoryDTO, 0, len(stories))
	for _, story := range stories {
		out = append(out, NowBriefingStoryDTO{
			StoryID: story.StoryID, Section: story.Section, Title: story.Title, Summary: story.Summary,
			URL: story.URL, Focus: story.Focus, Reaction: string(story.Reaction), ThreadID: story.ThreadID, Following: story.Following,
		})
	}
	return out
}

func nowBriefingParts(body string, stories []core.BriefingStory) []NowBriefingPartDTO {
	parts := wake.BriefingParts(body, stories)
	out := make([]NowBriefingPartDTO, 0, len(parts))
	for _, p := range parts {
		out = append(out, NowBriefingPartDTO{Heading: p.Heading, Body: p.Body})
	}
	return out
}

func nowCommitments(commitments []core.Commitment, occurrences []core.CommitmentOccurrence) ([]NowCommitmentDTO, error) {
	out := make([]NowCommitmentDTO, 0, len(commitments))
	index := make(map[int64]int, len(commitments))
	for _, c := range commitments {
		index[c.ID] = len(out)
		out = append(out, NowCommitmentDTO{
			ID: c.ID, Content: c.Content, State: c.State, WakeAt: c.WakeAt.UTC(),
			Recurrence: c.Recurrence, Occurrences: []NowOccurrenceDTO{},
		})
	}
	for _, o := range occurrences {
		i, ok := index[o.CommitmentID]
		if !ok {
			return nil, fmt.Errorf("occurrence #%d belongs to commitment #%d, which is not active", o.ID, o.CommitmentID)
		}
		out[i].Occurrences = append(out[i].Occurrences, NowOccurrenceDTO{ID: o.ID, DueAt: o.DueAt.UTC(), State: o.State, RunID: o.RunID})
	}
	return out, nil
}

func nowPanels(panels []core.Panel) []NowPanelDTO {
	out := make([]NowPanelDTO, 0, len(panels))
	for _, p := range panels {
		out = append(out, NowPanelDTO{ThreadID: p.View.SessionID, ThreadTitle: p.ThreadTitle, View: core.ViewDataOf(p.View), UpdatedAt: p.View.UpdatedAt.UTC()})
	}
	return out
}

func nowConcerns(concerns []core.MemoryConcern) []NowConcernDTO {
	out := make([]NowConcernDTO, 0, len(concerns))
	for _, c := range concerns {
		out = append(out, NowConcernDTO{
			ID: c.ID, ThreadID: c.ThreadID, Title: c.Title, Outcome: c.Outcome, State: c.State, Reason: c.Reason,
			ReviewAt: optionalUTC(c.ReviewAt), UpdatedAt: c.UpdatedAt.UTC(),
		})
	}
	return out
}

func nowGoals(goals []core.Goal, titles map[string]string) []NowGoalDTO {
	out := make([]NowGoalDTO, 0, len(goals))
	for _, g := range goals {
		out = append(out, NowGoalDTO{
			ID: g.ID, ThreadID: g.SessionID, ThreadTitle: titles[g.SessionID], Objective: g.Objective,
			Runs: g.Runs, MaxRuns: g.MaxRuns, CreatedAt: g.CreatedAt.UTC(),
		})
	}
	return out
}

func nowFollowUps(followUps []core.FollowUp, titles map[string]string) []NowFollowUpDTO {
	out := make([]NowFollowUpDTO, 0, len(followUps))
	for _, f := range followUps {
		waits := followUpWaitsForPath
		switch {
		case f.OutputContains != "":
			waits = followUpWaitsForJobOutput
		case f.JobID != "":
			waits = followUpWaitsForJobEnd
		}
		out = append(out, NowFollowUpDTO{
			ID: f.ID, ThreadID: f.SessionID, ThreadTitle: titles[f.SessionID], Task: f.Task, WaitsFor: waits,
			JobID: f.JobID, OutputContains: f.OutputContains, Path: f.Path, DeadlineAt: f.DeadlineAt.UTC(), CreatedAt: f.CreatedAt.UTC(),
		})
	}
	return out
}

// nowWatches lists failing watches first, then the rest in creation order.
func nowWatches(watches []core.Watch) []NowWatchDTO {
	out := make([]NowWatchDTO, 0, len(watches))
	for _, failing := range []bool{true, false} {
		for _, w := range watches {
			if (w.Status == core.WatchFailing) != failing {
				continue
			}
			out = append(out, NowWatchDTO{
				ID: w.ID, Name: w.Name, Kind: string(w.Kind), Target: w.Target, Mode: string(w.Mode), Section: w.Section, Issue: w.Issue, Trial: w.Trial, Status: string(w.Status),
				IntervalSeconds: int64(w.Interval / time.Second), NextCheckAt: w.NextCheckAt.UTC(),
				LastCheckedAt: optionalUTC(w.LastCheckedAt), LastError: w.LastError, Failures: w.Failures,
			})
		}
	}
	return out
}

func optionalUTC(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	utc := t.UTC()
	return &utc
}
