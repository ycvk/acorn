package api

import (
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

// PersonaService lets the owner read, edit and restore the persona.
type PersonaService struct {
	store core.PersonaStore
	clock func() time.Time
}

func NewPersonaService(store core.PersonaStore, clock func() time.Time) (*PersonaService, error) {
	if store == nil || clock == nil {
		return nil, errors.New("persona service: store and clock are required")
	}
	return &PersonaService{store: store, clock: clock}, nil
}

// PersonaDTO is one persona revision with its content.
type PersonaDTO struct {
	Revision int64     `json:"revision"`
	Content  string    `json:"content"`
	SHA256   string    `json:"sha256"`
	Author   string    `json:"author"`
	DeviceID string    `json:"device_id,omitempty"`
	SavedAt  time.Time `json:"saved_at"`
}

// PersonaRevisionSummaryDTO is one persona revision in the history.
type PersonaRevisionSummaryDTO struct {
	Revision int64     `json:"revision"`
	SHA256   string    `json:"sha256"`
	Author   string    `json:"author"`
	DeviceID string    `json:"device_id,omitempty"`
	SavedAt  time.Time `json:"saved_at"`
}

// PersonaRevisionListResponse is GET /v1/persona/revisions.
type PersonaRevisionListResponse struct {
	Revisions []PersonaRevisionSummaryDTO `json:"revisions"`
}

// SavePersonaRequest is PUT /v1/persona.
type SavePersonaRequest struct {
	Content      string `json:"content"`
	BaseRevision int64  `json:"base_revision"`
}

func (s *PersonaService) Current(ctx context.Context) (PersonaDTO, error) {
	current, err := s.store.Persona(ctx)
	if err != nil {
		return PersonaDTO{}, err
	}
	return personaDTO(current), nil
}

// Save writes content as the owner's revision on top of base. deviceID is
// empty when the owner saves from the server.
func (s *PersonaService) Save(ctx context.Context, deviceID, content string, base int64) (PersonaDTO, error) {
	saved, err := presence.SavePersona(ctx, s.store, core.PersonaWrite{Content: content, Author: core.PersonaAuthorOwner, DeviceID: deviceID, At: s.clock(), BaseRevision: base})
	if err != nil {
		return PersonaDTO{}, err
	}
	return personaDTO(saved), nil
}

func (s *PersonaService) Revisions(ctx context.Context) (PersonaRevisionListResponse, error) {
	revisions, err := s.store.PersonaRevisions(ctx)
	if err != nil {
		return PersonaRevisionListResponse{}, err
	}
	out := PersonaRevisionListResponse{Revisions: make([]PersonaRevisionSummaryDTO, 0, len(revisions))}
	for _, r := range revisions {
		out.Revisions = append(out.Revisions, PersonaRevisionSummaryDTO{Revision: r.Revision, SHA256: r.SHA256, Author: string(r.Author), DeviceID: r.DeviceID, SavedAt: r.At})
	}
	return out, nil
}

func (s *PersonaService) Revision(ctx context.Context, revision int64) (PersonaDTO, error) {
	r, err := s.store.PersonaRevision(ctx, revision)
	if err != nil {
		return PersonaDTO{}, err
	}
	return personaDTO(r), nil
}

func personaDTO(r core.PersonaRevision) PersonaDTO {
	return PersonaDTO{Revision: r.Revision, Content: r.Content, SHA256: r.SHA256, Author: string(r.Author), DeviceID: r.DeviceID, SavedAt: r.At}
}
