package store

import (
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

// Sentinel errors
var (
	ErrRunNotFound              = errors.New("run not found")
	ErrSessionNotFound          = errors.New("session not found")
	ErrSessionMessageNotFound   = errors.New("session message not found")
	ErrFactNotFound             = errors.New("fact not found")
	ErrPendingActionNotFound    = errors.New("pending action not found")
	ErrPendingActionExists      = errors.New("pending action already exists")
	ErrPendingActionDecided     = errors.New("pending action already decided")
	ErrUnsupportedStorageSchema = errors.New("unsupported storage schema")
	ErrOAuthTokenNotFound       = errors.New("oauth token not found")
	ErrDeviceNotFound           = errors.New("device not found")
	ErrPairingCodeNotFound      = errors.New("pairing code not found")
	ErrPairingCodeUsed          = errors.New("pairing code already used")
	ErrPairingCodeExpired       = errors.New("pairing code expired")
)

// Types
type RunCreateParams struct {
	RunID     string
	SessionID string
	TurnIndex int
	Input     string
	// BoundMessageID, when > 0, binds the run to that exact user message id
	// (race-free). When 0, binding falls back to the latest unbound user message
	// for TurnIndex (used by fresh-session / subagent paths where the message is
	// the only one at that turn).
	BoundMessageID int64
}

type OAuthToken struct {
	ProviderName string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	UpdatedAt    time.Time
}

type OwnerProfile struct {
	OwnerID   string
	CreatedAt time.Time
}

type Device struct {
	DeviceID   string
	Name       string
	Platform   string
	TokenHash  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

type PairingCode struct {
	CodeHash  string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type CreatePendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        events.PendingActionKind
	Subject     string
	PayloadJSON string
	Status      events.PendingActionStatus
	Reason      string
}
