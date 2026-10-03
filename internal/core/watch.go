package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrWatchNotFound = errors.New("watch not found")
	// ErrWatchNotDue means a watch was not claimable: it is paused, already
	// claimed, or its next check has not arrived.
	ErrWatchNotDue = errors.New("watch not due")
	// ErrBriefingTaken means the day's briefing was already started.
	ErrBriefingTaken = errors.New("briefing already taken")
)

// WatchKind is the type of source a watch follows.
type WatchKind string

const (
	WatchRSS         WatchKind = "rss"
	WatchGitHub      WatchKind = "github"
	WatchWeb         WatchKind = "web"
	WatchWebRendered WatchKind = "web_rendered"
)

// WatchMode decides what happens with a watch's new items: immediate wakes
// the agent, digest keeps them for the morning briefing.
type WatchMode string

const (
	WatchModeDigest    WatchMode = "digest"
	WatchModeImmediate WatchMode = "immediate"
)

// WatchStatus is a watch's scheduling state. A failing watch is still
// checked, with backoff.
type WatchStatus string

const (
	WatchActive  WatchStatus = "active"
	WatchPaused  WatchStatus = "paused"
	WatchFailing WatchStatus = "failing"
)

// WatchItemStatus tracks what has been done with an item.
type WatchItemStatus string

const (
	// WatchItemBaseline items were present at the first check; nothing is done with them.
	WatchItemBaseline WatchItemStatus = "baseline"
	WatchItemPending  WatchItemStatus = "pending"
	WatchItemWoken    WatchItemStatus = "woken"
	WatchItemBriefed  WatchItemStatus = "briefed"
)

// Watch is a source the agent follows on the owner's behalf.
type Watch struct {
	ID            int64
	Name          string
	Kind          WatchKind
	Target        string
	Selector      string
	Mode          WatchMode
	Interval      time.Duration
	Status        WatchStatus
	SessionID     string
	NextCheckAt   time.Time
	LastCheckedAt time.Time
	LastError     string
	Failures      int
	Snapshot      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// WatchItem is one new thing seen on a watch.
type WatchItem struct {
	ID          int64
	WatchID     int64
	Key         string
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
	Status      WatchItemStatus
	RunID       string
	SeenAt      time.Time
}

// WatchStore persists watches, their items and the daily briefings.
type WatchStore interface {
	AddWatch(ctx context.Context, w Watch) (Watch, error)
	LoadWatch(ctx context.Context, id int64) (*Watch, error)
	ListWatches(ctx context.Context) ([]Watch, error)
	UpdateWatch(ctx context.Context, w Watch) error
	ListDueWatches(ctx context.Context, now time.Time, limit int) ([]Watch, error)
	// ClaimDueWatch moves the next check of a due, unpaused watch to now+lease.
	// Exactly one concurrent caller succeeds; the others get ErrWatchNotDue.
	ClaimDueWatch(ctx context.Context, id int64, now time.Time, lease time.Duration) error
	// AddWatchItems inserts the items not seen before on their watch and
	// returns those, with ids.
	AddWatchItems(ctx context.Context, items []WatchItem) ([]WatchItem, error)
	ListWatchItems(ctx context.Context, status WatchItemStatus, limit int) ([]WatchItem, error)
	MarkWatchItems(ctx context.Context, ids []int64, status WatchItemStatus, runID string) error
	// ClaimBriefing records the briefing of an owner-local day; a second claim
	// of the same day gets ErrBriefingTaken.
	ClaimBriefing(ctx context.Context, day, threadID string, at time.Time) error
	SetBriefingRun(ctx context.Context, day, runID string) error
	// LatestBriefingThread is the thread of the most recent briefing, or "".
	LatestBriefingThread(ctx context.Context) (string, error)
}
