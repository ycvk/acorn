package domain

import "time"

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

// PairingCode represents a device pairing code.
type PairingCode struct {
	CodeHash  string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// Device represents a registered device.
type Device struct {
	DeviceID   string
	Name       string
	Platform   string
	TokenHash  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

// OAuthToken represents an MCP OAuth token.
type OAuthToken struct {
	ProviderName string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	UpdatedAt    time.Time
}

// OwnerProfile represents an owner profile record.
type OwnerProfile struct {
	OwnerID   string
	CreatedAt time.Time
}

// ArtifactWriteRequest holds parameters for writing an artifact.
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

// ArtifactRecord represents a stored artifact.
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

// ArtifactReadRangeRequest holds parameters for reading an artifact range.
type ArtifactReadRangeRequest struct {
	ArtifactID string
	Offset     int64
	Limit      int64
}

// ArtifactReadRangeResult holds the result of reading an artifact range.
type ArtifactReadRangeResult struct {
	Record  ArtifactRecord
	Offset  int64
	Content []byte
	EOF     bool
}
