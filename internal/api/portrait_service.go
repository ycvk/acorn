package api

import (
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// portraitExcerptRunes clips each piece of evidence the app shows.
const portraitExcerptRunes = 120

// portraitStore is the part of the memory store the portrait page uses.
type portraitStore interface {
	Portrait(ctx context.Context) ([]core.PortraitLine, error)
	RemovePortraitLine(ctx context.Context, id int64, now time.Time) error
	LoadMemorySource(ctx context.Context, id string) (core.MemorySource, error)
}

// PortraitService lets the owner see what Acorn has gathered about them, with
// the words each line rests on, and remove lines that are wrong.
type PortraitService struct {
	store portraitStore
	clock func() time.Time
}

func NewPortraitService(store portraitStore, clock func() time.Time) (*PortraitService, error) {
	if store == nil || clock == nil {
		return nil, errors.New("portrait service: store and clock are required")
	}
	return &PortraitService{store: store, clock: clock}, nil
}

// PortraitDTO is GET /v1/portrait.
type PortraitDTO struct {
	Lines     []PortraitLineDTO `json:"lines"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
}

type PortraitLineDTO struct {
	ID       int64                 `json:"id"`
	Content  string                `json:"content"`
	Evidence []PortraitEvidenceDTO `json:"evidence"`
}

// PortraitEvidenceDTO is one source a line rests on, as memory reads it.
type PortraitEvidenceDTO struct {
	Excerpt    string    `json:"excerpt"`
	SourceKind string    `json:"source_kind"`
	ThreadID   string    `json:"thread_id,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// Get lists the shown lines in order.
func (s *PortraitService) Get(ctx context.Context) (PortraitDTO, error) {
	lines, err := s.store.Portrait(ctx)
	if err != nil {
		return PortraitDTO{}, err
	}
	out := PortraitDTO{Lines: []PortraitLineDTO{}}
	for _, line := range lines {
		if line.State != core.PortraitLineShown {
			continue
		}
		dto := PortraitLineDTO{ID: line.ID, Content: line.Content, Evidence: []PortraitEvidenceDTO{}}
		for _, id := range line.SourceIDs {
			source, err := s.store.LoadMemorySource(ctx, id)
			if errors.Is(err, core.ErrMemoryExcluded) {
				continue
			}
			if err != nil {
				return PortraitDTO{}, err
			}
			dto.Evidence = append(dto.Evidence, PortraitEvidenceDTO{
				Excerpt:    clip(compactWhitespace(source.Content), portraitExcerptRunes),
				SourceKind: source.Kind,
				ThreadID:   source.SessionID,
				RecordedAt: source.RecordedAt.UTC(),
			})
		}
		out.Lines = append(out.Lines, dto)
		if out.UpdatedAt == nil || line.CreatedAt.After(*out.UpdatedAt) {
			at := line.CreatedAt.UTC()
			out.UpdatedAt = &at
		}
	}
	return out, nil
}

// Remove takes a shown line off the portrait; later refreshes do not write
// it again.
func (s *PortraitService) Remove(ctx context.Context, id int64) error {
	return s.store.RemovePortraitLine(ctx, id, s.clock())
}
