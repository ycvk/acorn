package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrMemoryItemNotFound = errors.New("memory item not found")
	// ErrMemoryItemNotDue means a commitment was not claimable: it is no longer
	// active or its wake time has not arrived.
	ErrMemoryItemNotDue = errors.New("memory item not due")
)

// MemoryKind classifies a working-memory item.
type MemoryKind string

const (
	// MemorySaid is the owner's own words, kept verbatim.
	MemorySaid MemoryKind = "said"
	// MemoryThought is a thought of the agent's own.
	MemoryThought MemoryKind = "thought"
	// MemoryCommitment is an appointment: the agent wakes at WakeAt to do Content.
	MemoryCommitment MemoryKind = "commitment"
	// MemoryTendency is a long-lived preference or habit of the owner.
	MemoryTendency MemoryKind = "tendency"
	// MemoryRuler is a concern or working assumption the agent holds.
	MemoryRuler MemoryKind = "ruler"
)

// MemoryStatus is the lifecycle state of a working-memory item.
type MemoryStatus string

const (
	MemoryActive  MemoryStatus = "active"
	MemoryResting MemoryStatus = "resting"
	MemorySunk    MemoryStatus = "sunk"
	// MemoryWoken marks a commitment whose wake has fired and awaits settling.
	MemoryWoken        MemoryStatus = "woken"
	MemorySettled      MemoryStatus = "settled"
	MemoryInternalized MemoryStatus = "internalized"
	MemoryReleased     MemoryStatus = "released"
)

// MemoryItem is one working-memory entry. WakeAt and Recurrence apply to
// commitments only.
type MemoryItem struct {
	ID          int64
	Kind        MemoryKind
	Content     string
	Status      MemoryStatus
	SessionID   string
	SourceRunID string
	WakeAt      time.Time
	Recurrence  string
	ExpiresAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ExperienceHit is one result of an experience search over past runs and
// working memory.
type ExperienceHit struct {
	Source    string // "run" or "memory"
	RunID     string
	MemoryID  int64
	Kind      MemoryKind
	Snippet   string
	CreatedAt time.Time
}

// PresenceStore persists working memory, commitments and context snapshots.
type PresenceStore interface {
	AddMemoryItem(ctx context.Context, item MemoryItem) (MemoryItem, error)
	ListMemoryItems(ctx context.Context, statuses []MemoryStatus) ([]MemoryItem, error)
	LoadMemoryItem(ctx context.Context, id int64) (*MemoryItem, error)
	// UpdateMemoryItem writes content, status, wake_at and expires_at.
	UpdateMemoryItem(ctx context.Context, item MemoryItem) error
	ListDueCommitments(ctx context.Context, now time.Time) ([]MemoryItem, error)
	// ClaimDueCommitment flips an active commitment whose wake time has passed
	// to woken. Exactly one concurrent caller succeeds; the others get
	// ErrMemoryItemNotDue.
	ClaimDueCommitment(ctx context.Context, id int64, now time.Time) error
	SearchExperience(ctx context.Context, query string, limit int) ([]ExperienceHit, error)
	SaveContextSnapshot(ctx context.Context, hash, content string) error
	// CountWakesSince counts wake.fired events recorded at or after since.
	CountWakesSince(ctx context.Context, since time.Time) (int, error)
	SumAutonomousTokensSince(ctx context.Context, since time.Time) (int, error)
	UsageReport(ctx context.Context, since time.Time) (UsageReport, error)
}

// EventWakeFired is recorded on a run started by a commitment wake.
const EventWakeFired = "wake.fired"

// NotificationStatus is the delivery state of an owner notification.
type NotificationStatus string

const (
	NotificationQueued NotificationStatus = "queued"
	NotificationSent   NotificationStatus = "sent"
	NotificationFailed NotificationStatus = "failed"
)

// Notification is a push message to the owner. SendAfter defers delivery
// (quiet hours).
type Notification struct {
	ID        int64
	Title     string
	Body      string
	ThreadID  string
	RunID     string
	Status    NotificationStatus
	SendAfter time.Time
	Error     string
	CreatedAt time.Time
	SentAt    time.Time
}

// PushToken is a device's FCM registration token.
type PushToken struct {
	DeviceID  string
	Token     string
	UpdatedAt time.Time
}

// NotificationStore persists push tokens and notification delivery records.
type NotificationStore interface {
	SetPushToken(ctx context.Context, deviceID, token string) error
	// ListPushTokens returns tokens of devices that are not revoked.
	ListPushTokens(ctx context.Context) ([]PushToken, error)
	DeletePushToken(ctx context.Context, deviceID string) error
	QueueNotification(ctx context.Context, n Notification) (Notification, error)
	ListDueNotifications(ctx context.Context, now time.Time) ([]Notification, error)
	FinishNotification(ctx context.Context, id int64, status NotificationStatus, errText string, at time.Time) error
	CountNotificationsSince(ctx context.Context, since time.Time) (int, error)
}
