package core

import (
	"context"
	"encoding/json"
	"time"
)

// RunCreateParams holds the parameters for creating a run record.
type RunCreateParams struct {
	RunID          string
	SessionID      string
	TurnIndex      int
	Input          string
	BoundMessageID int64
}

// PendingActionInput holds the parameters for creating a pending action.
type PendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        PendingActionKind
	Subject     string
	PayloadJSON string
	Status      PendingActionStatus
	Reason      string
}

// --- Session messages ---

type SessionMessageRecord struct {
	ID           int64           `json:"id"`
	SessionID    string          `json:"session_id"`
	TurnIndex    int             `json:"turn_index"`
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	ContentParts json.RawMessage `json:"content_parts,omitempty"`
	RunID        string          `json:"run_id,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// --- Pairing / devices / OAuth ---

type PairingCode struct {
	CodeHash  string
	ExpiresAt time.Time
	UsedAt    *time.Time
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

type OAuthToken struct {
	ProviderName string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	UpdatedAt    time.Time
}

// --- Artifacts ---

type ArtifactWriteRequest struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	Content             []byte
	CreatedAt           time.Time
}

type ArtifactRecord struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                string
	Title               string
	MIMEType            string
	RelativePath        string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}

type ArtifactReadRangeRequest struct {
	ArtifactID string
	Offset     int64
	Limit      int64
}

type ArtifactReadRangeResult struct {
	Record  ArtifactRecord
	Offset  int64
	Content []byte
	EOF     bool
}

// --- Session summary ---

type SessionSummary struct {
	SessionID   string    `json:"session_id"`
	SourceRunID string    `json:"source_run_id"`
	RunStatus   string    `json:"run_status"`
	Summary     string    `json:"summary"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SessionSummaryStore interface {
	GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error)
	UpsertSessionSummary(ctx context.Context, summary SessionSummary) error
}

