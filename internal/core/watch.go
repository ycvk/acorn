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
	// ErrWatchChanged means a watch's status, failures or last check changed
	// while a check of it ran, so the check's result was not recorded.
	ErrWatchChanged        = errors.New("watch changed during the check")
	ErrNewsSectionNotFound = errors.New("news section not found")
	// ErrNewsSectionInUse means a news section still has sources, so it
	// cannot be removed.
	ErrNewsSectionInUse = errors.New("news section still has sources")
)

// WatchKind is the type of source a watch follows.
type WatchKind string

const (
	WatchRSS         WatchKind = "rss"
	WatchGitHub      WatchKind = "github"
	WatchWeb         WatchKind = "web"
	WatchWebRendered WatchKind = "web_rendered"
	// WatchTrigger is a news source whose items are the item lines a skill
	// package trigger prints; its target is skill/trigger and it is never
	// fetched.
	WatchTrigger WatchKind = "trigger"
)

// WatchMode decides what happens with a watch's new items: immediate wakes
// the agent, digest keeps them for the morning briefing, and news offers them
// to the briefing to pick from under the watch's section.
type WatchMode string

const (
	WatchModeDigest    WatchMode = "digest"
	WatchModeImmediate WatchMode = "immediate"
	WatchModeNews      WatchMode = "news"
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
	ID       int64
	Name     string
	Kind     WatchKind
	Target   string
	Selector string
	Mode     WatchMode
	// Section is the news section of a news watch; other modes have none.
	Section string
	// Issue marks a news watch whose items are issues, each read on its page.
	Issue         bool
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

// Pause stops checking the watch until it is resumed.
func (w Watch) Pause(now time.Time) Watch {
	w.Status, w.UpdatedAt = WatchPaused, now
	return w
}

// Resume checks the watch right away and forgets its failures.
func (w Watch) Resume(now time.Time) Watch {
	w.Status, w.Failures, w.LastError, w.NextCheckAt, w.UpdatedAt = WatchActive, 0, "", now, now
	return w
}

// NewsSection groups news watches in the morning briefing. Preference is
// what the owner wants from it, a few lines in order of priority.
type NewsSection struct {
	Name       string
	Position   int
	Preference string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PendingBriefing is what the other watches hold for the briefing: items
// taken from each watch in turn up to a limit, and every watch's pending count.
type PendingBriefing struct {
	Items  []WatchItem
	Counts map[int64]int
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

// WatchCheck is what one check of a watch records: the check fields of the
// watch and the items it found.
type WatchCheck struct {
	At          time.Time
	NextCheckAt time.Time
	Status      WatchStatus
	Failures    int
	LastError   string
	Snapshot    string
	Items       []WatchItem
}

// WatchStore persists watches and their items.
type WatchStore interface {
	AddWatch(ctx context.Context, w Watch) (Watch, error)
	LoadWatch(ctx context.Context, id int64) (*Watch, error)
	ListWatches(ctx context.Context) ([]Watch, error)
	UpdateWatch(ctx context.Context, w Watch) error
	// ListDueWatches lists the watches to fetch now, leaving out paused
	// watches and trigger sources.
	ListDueWatches(ctx context.Context, now time.Time, limit int) ([]Watch, error)
	// ClaimDueWatch moves the next check of a due, unpaused watch to now+lease.
	// Exactly one concurrent caller succeeds; the others get ErrWatchNotDue.
	ClaimDueWatch(ctx context.Context, id int64, now time.Time, lease time.Duration) error
	// RecordWatchCheck writes a check that started from the watch from: its
	// check fields and, in the same transaction, the items not seen before on
	// the watch, which it returns with ids. When from's status, failures or
	// last check no longer match the stored watch, such as after a pause or a
	// resume made during the check, it writes nothing and returns
	// ErrWatchChanged.
	RecordWatchCheck(ctx context.Context, from Watch, check WatchCheck) ([]WatchItem, error)
	ListWatchItems(ctx context.Context, status WatchItemStatus, limit int) ([]WatchItem, error)
	MarkWatchItems(ctx context.Context, ids []int64, status WatchItemStatus, runID string) error
	// PendingBriefingItems takes the pending items of the watches other than
	// news watches in turns, oldest first within a watch, up to limit.
	PendingBriefingItems(ctx context.Context, limit int) (PendingBriefing, error)

	ListNewsSections(ctx context.Context) ([]NewsSection, error)
	// SaveNewsSection adds a section or changes the position and preference
	// of the one with its name.
	SaveNewsSection(ctx context.Context, section NewsSection) (NewsSection, error)
	// RenameNewsSection renames a section and moves its watches with it.
	RenameNewsSection(ctx context.Context, from, to string, at time.Time) error
	// DeleteNewsSection removes a section without watches; one with watches
	// fails with ErrNewsSectionInUse.
	DeleteNewsSection(ctx context.Context, name string) error
}
