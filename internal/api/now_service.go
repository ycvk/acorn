package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// briefingNotesPrefix is where the morning briefing skill writes its notes.
const briefingNotesPrefix = "briefings/"

// nowStore is the part of the store the now page reads and changes.
type nowStore interface {
	ListCommitments(ctx context.Context, activeOnly bool) ([]core.Commitment, error)
	ListDueOccurrences(ctx context.Context) ([]core.CommitmentOccurrence, error)
	SettleCommitment(ctx context.Context, change core.CommitmentSettlement) error
	ListConcerns(ctx context.Context, activeOnly bool) ([]core.MemoryConcern, error)
	ListWatches(ctx context.Context) ([]core.Watch, error)
	LoadWatch(ctx context.Context, id int64) (*core.Watch, error)
	UpdateWatch(ctx context.Context, w core.Watch) error
}

// briefingReader finds the latest briefing note.
type briefingReader interface {
	Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error)
}

// NowService serves the now page: the latest briefing, what is coming up,
// what the agent is working on and the watches.
type NowService struct {
	store nowStore
	notes briefingReader
	clock func() time.Time
}

func NewNowService(store nowStore, notes briefingReader, clock func() time.Time) (*NowService, error) {
	if store == nil || notes == nil || clock == nil {
		return nil, errors.New("now service: store, notes and clock are required")
	}
	return &NowService{store: store, notes: notes, clock: clock}, nil
}

// NowResponse is GET /v1/now.
type NowResponse struct {
	Briefing    *NowBriefingDTO    `json:"briefing,omitempty"`
	Commitments []NowCommitmentDTO `json:"commitments"`
	Concerns    []NowConcernDTO    `json:"concerns"`
	Watches     []NowWatchDTO      `json:"watches"`
}

// NowBriefingDTO is the latest briefing note.
type NowBriefingDTO struct {
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	Snippet   string    `json:"snippet"`
	UpdatedAt time.Time `json:"updated_at"`
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
	Title     string     `json:"title"`
	State     string     `json:"state"`
	Reason    string     `json:"reason"`
	ReviewAt  *time.Time `json:"review_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// NowWatchDTO is one watch.
type NowWatchDTO struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	Target          string     `json:"target"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	IntervalSeconds int64      `json:"interval_seconds"`
	NextCheckAt     time.Time  `json:"next_check_at"`
	LastCheckedAt   *time.Time `json:"last_checked_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	Failures        int        `json:"failures"`
}

// Load reads every section; any failing read fails the whole page.
func (s *NowService) Load(ctx context.Context) (NowResponse, error) {
	hits, err := s.notes.Recent(ctx, briefingNotesPrefix, 1)
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
	watches, err := s.store.ListWatches(ctx)
	if err != nil {
		return NowResponse{}, fmt.Errorf("list watches: %w", err)
	}
	out := NowResponse{Concerns: nowConcerns(concerns), Watches: nowWatches(watches)}
	if len(hits) > 0 {
		out.Briefing = &NowBriefingDTO{Path: hits[0].Path, Title: hits[0].Title, Snippet: hits[0].Snippet, UpdatedAt: hits[0].UpdatedAt.UTC()}
	}
	if out.Commitments, err = nowCommitments(commitments, occurrences); err != nil {
		return NowResponse{}, err
	}
	return out, nil
}

// CancelCommitment cancels a commitment and its unsettled occurrences, the
// same way the agent's settle tool does.
func (s *NowService) CancelCommitment(ctx context.Context, id int64) error {
	return s.store.SettleCommitment(ctx, core.CommitmentSettlement{CommitmentID: id, Action: "cancel", Now: s.clock()})
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

func nowConcerns(concerns []core.MemoryConcern) []NowConcernDTO {
	out := make([]NowConcernDTO, 0, len(concerns))
	for _, c := range concerns {
		out = append(out, NowConcernDTO{
			ID: c.ID, Title: c.Title, State: c.State, Reason: c.Reason,
			ReviewAt: optionalUTC(c.ReviewAt), UpdatedAt: c.UpdatedAt.UTC(),
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
				ID: w.ID, Name: w.Name, Kind: string(w.Kind), Target: w.Target, Mode: string(w.Mode), Status: string(w.Status),
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
