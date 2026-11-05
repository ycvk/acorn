package core

import (
	"errors"
	"time"
)

// ErrPortraitLineNotFound is a portrait line that is not shown.
var ErrPortraitLineNotFound = errors.New("portrait line not found")

const (
	PortraitLineShown   = "shown"
	PortraitLineRemoved = "removed"
)

// PortraitOperation is the memory job that refreshes the portrait. Its
// object is always PortraitObject; the version names the refresh.
const (
	PortraitOperation = "portrait"
	PortraitObject    = "owner"
)

// PortraitTokens bounds the portrait every run carries.
const PortraitTokens = 1024

// PortraitLine is one thing Acorn has gathered about the owner, drawn from
// the sources it cites. A line the owner removed stays, so a refresh does not
// write it again; forgetting any of its sources deletes it.
type PortraitLine struct {
	ID        int64
	Content   string
	State     string
	SourceIDs []string
	CreatedAt time.Time
	RemovedAt time.Time
}

// PortraitDraft is a line a refresh writes.
type PortraitDraft struct {
	Content   string
	SourceIDs []string
}

// PortraitMutation replaces the shown lines with Lines and finishes the
// refresh that leased Lease.
type PortraitMutation struct {
	Lease MemoryJob
	Lines []PortraitDraft
	Now   time.Time
}
