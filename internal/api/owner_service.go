package api

import (
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/presence"
)

// OwnerService lets the owner read and set what Acorn calls them.
type OwnerService struct {
	store core.OwnerStore
	clock func() time.Time
}

func NewOwnerService(store core.OwnerStore, clock func() time.Time) (*OwnerService, error) {
	if store == nil || clock == nil {
		return nil, errors.New("owner service: store and clock are required")
	}
	return &OwnerService{store: store, clock: clock}, nil
}

// OwnerDTO is GET and PUT /v1/owner. UpdatedAt is nil when no name was ever
// saved.
type OwnerDTO struct {
	Name      string     `json:"name"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// SetOwnerRequest is PUT /v1/owner.
type SetOwnerRequest struct {
	Name string `json:"name"`
}

func (s *OwnerService) Current(ctx context.Context) (OwnerDTO, error) {
	owner, err := s.store.OwnerName(ctx)
	if err != nil {
		return OwnerDTO{}, err
	}
	return ownerDTO(owner), nil
}

// SetName saves name as what Acorn calls the owner; an empty name leaves the
// owner unnamed.
func (s *OwnerService) SetName(ctx context.Context, name string) (OwnerDTO, error) {
	owner, err := presence.SaveOwnerName(ctx, s.store, name, s.clock())
	if err != nil {
		return OwnerDTO{}, err
	}
	return ownerDTO(owner), nil
}

func ownerDTO(owner core.OwnerName) OwnerDTO {
	out := OwnerDTO{Name: owner.Name}
	if !owner.UpdatedAt.IsZero() {
		at := owner.UpdatedAt
		out.UpdatedAt = &at
	}
	return out
}
