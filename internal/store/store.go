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
	ErrPlanNotFound             = errors.New("plan not found")
	ErrDeviceNotFound           = errors.New("device not found")
	ErrPairingCodeNotFound      = errors.New("pairing code not found")
	ErrPairingCodeUsed          = errors.New("pairing code already used")
	ErrPairingCodeExpired       = errors.New("pairing code expired")
	ErrDevicePushTokenNotFound  = errors.New("device push token not found")
	ErrNotificationNotFound     = errors.New("notification not found")
)

// Types
type RunCreateParams struct {
	RunID             string
	SessionID         string
	TurnIndex         int
	Input             string
	CheckpointID      string
	OrchestrationMode events.OrchestrationMode
	ParentRunID       string
	Depth             int
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

type DevicePushToken struct {
	PushTokenID string
	DeviceID    string
	Provider    string
	Platform    string
	TokenValue  string
	TokenHash   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	RevokedAt   *time.Time
}

type PairingCode struct {
	CodeHash  string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type Notification struct {
	NotificationID string
	Kind           string
	RunID          string
	ActionID       string
	CreatedAt      time.Time
}

type NotificationDelivery struct {
	DeliveryID     string
	NotificationID string
	DeviceID       string
	PushTokenID    string
	Provider       string
	Status         string
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreatePendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        events.PendingActionKind
	Subject     string
	PayloadJSON string
	Status      events.PendingActionStatus
	Mode        events.PendingActionDecisionMode
	Reason      string
	Rule        string
}

type PlanEvidence struct {
	ID            string    `json:"id"`
	StepID        string    `json:"step_id"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	Summary       string    `json:"summary"`
	ToolResultRef string    `json:"tool_result_ref,omitempty"`
	ToolName      string    `json:"tool_name,omitempty"`
	Command       []string  `json:"command,omitempty"`
	Paths         []string  `json:"paths,omitempty"`
	DiffRef       string    `json:"diff_ref,omitempty"`
	ChildRunID    string    `json:"child_run_id,omitempty"`
	Error         string    `json:"error,omitempty"`
	SourceRunID   string    `json:"source_run_id,omitempty"`
	RecordedAt    time.Time `json:"recorded_at"`
}

type PlanRepoTarget struct {
	Path       string `json:"path"`
	Symbol     string `json:"symbol,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
	Reason     string `json:"reason"`
	Confidence string `json:"confidence"`
}

type VerificationIntent struct {
	Kind    string   `json:"kind"`
	Command []string `json:"command,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Reason  string   `json:"reason"`
}

type PlanStep struct {
	ID                 string               `json:"id"`
	Action             string               `json:"action"`
	Status             PlanStepStatus       `json:"status"`
	DependsOn          []string             `json:"depends_on,omitempty"`
	RepoTargets        []PlanRepoTarget     `json:"repo_targets,omitempty"`
	VerificationIntent []VerificationIntent `json:"verification_intent,omitempty"`
	Risk               PlanStepRisk         `json:"risk,omitempty"`
	ToolHints          []string             `json:"tool_hints,omitempty"`
	Evidence           []PlanEvidence       `json:"evidence,omitempty"`
}

type PlanStepStatus string

const (
	PlanStepPending    PlanStepStatus = "pending"
	PlanStepInProgress PlanStepStatus = "in_progress"
	PlanStepCompleted  PlanStepStatus = "completed"
	PlanStepFailed     PlanStepStatus = "failed"
	PlanStepSkipped    PlanStepStatus = "skipped"
)

type PlanStepRisk string

const (
	PlanStepRiskRead     PlanStepRisk = "read"
	PlanStepRiskWrite    PlanStepRisk = "write"
	PlanStepRiskExecute  PlanStepRisk = "execute"
	PlanStepRiskDelegate PlanStepRisk = "delegate"
)

type PlanRecord struct {
	PlanID    string     `json:"plan_id"`
	SessionID string     `json:"session_id"`
	RunID     string     `json:"run_id"`
	Steps     []PlanStep `json:"steps"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
