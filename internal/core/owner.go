package core

import (
	"context"
	"errors"
	"time"
)

// ErrOwnerNameInvalid means a name is too long or holds line breaks or other
// control characters.
var ErrOwnerNameInvalid = errors.New("invalid owner name")

// OwnerName is what Acorn calls the owner. An empty Name leaves the owner
// unnamed; UpdatedAt is zero when no name was ever saved.
type OwnerName struct {
	Name      string
	UpdatedAt time.Time
}

// OwnerStore holds what Acorn calls the owner. Only the owner sets it, from
// the App or the CLI.
type OwnerStore interface {
	SetOwnerName(ctx context.Context, name string, at time.Time) (OwnerName, error)
	OwnerName(ctx context.Context) (OwnerName, error)
}
