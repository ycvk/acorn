package core

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNewsLeaseLost means a news batch's claim expired and another worker
	// took its items, so the batch's result was not written.
	ErrNewsLeaseLost = errors.New("news batch lease lost")
	// ErrNewsStoryNotFound means no story has the id, or it was pruned.
	ErrNewsStoryNotFound = errors.New("news story not found")
)

// NewsRetention is how long news items are sorted, offered and searchable
// after they are first seen.
const NewsRetention = 14 * 24 * time.Hour

// NewsReactionRetention is how long the owner's reactions and the stories
// briefings picked are kept.
const NewsReactionRetention = 30 * 24 * time.Hour

// NewsReaction is what the owner wants of a story's kind.
type NewsReaction string

const (
	NewsReactionMore NewsReaction = "more"
	NewsReactionLess NewsReaction = "less"
)

// Valid reports whether r is more or less.
func (r NewsReaction) Valid() bool {
	return r == NewsReactionMore || r == NewsReactionLess
}

// BriefingStory is a story a briefing picked: the section it stood in, the
// title and sentence the briefing wrote and the link it gave, whether it was
// among today's focus, the news sources of its items, and the owner's
// reaction to the story, empty without one.
type BriefingStory struct {
	StoryID  int64
	Section  string
	Title    string
	Summary  string
	URL      string
	Focus    bool
	Sources  []int64
	Reaction NewsReaction
}

// BriefingPicks is what a finished briefing picked, in its order, and how
// many of its news lines or focus lines matched no story it offered.
type BriefingPicks struct {
	RunID     string
	BriefedAt time.Time
	Stories   []BriefingStory
	Unmatched int
}

// StoryReaction is the owner's reaction to a story, with the story's
// section, title and news sources when the owner reacted.
type StoryReaction struct {
	StoryID  int64
	Reaction NewsReaction
	Section  string
	Title    string
	Sources  []int64
	DeviceID string
	At       time.Time
}

// NewsStory is one event across news sources: the items that report it, a
// summary of it so far and what its latest items added.
type NewsStory struct {
	ID          int64
	Section     string
	Title       string
	Summary     string
	Development string
	// Score is the highest score of its items, 1 to 5.
	Score     int
	FirstSeen time.Time
	UpdatedAt time.Time
	Items     []NewsStoryItem
}

// NewsStoryItem is an item of a story with its source's name.
type NewsStoryItem struct {
	WatchItem
	Source string
	Score  int
}

// NewsBatchItem is a news item waiting to be sorted, with its source.
type NewsBatchItem struct {
	Item    WatchItem
	Source  string
	Section string
	Issue   bool
}

// NewsBatch is a claimed set of news items; Token proves the claim when the
// result is written.
type NewsBatch struct {
	Token string
	Items []NewsBatchItem
}

// NewsCommit is the sorting of a batch: the stories it creates or extends,
// the score and story of each item, and the entries read from each issue.
// Story refers to an existing story as "s<id>" or to a new one by its Ref.
type NewsCommit struct {
	At      time.Time
	Stories []NewsStoryChange
	Scores  []NewsItemScore
	Entries []NewsIssueEntry
}

// NewsStoryChange creates a story (ID 0) or rewrites an existing one.
type NewsStoryChange struct {
	Ref         string
	ID          int64
	Section     string
	Title       string
	Summary     string
	Development string
}

// NewsItemScore sorts one claimed item. An issue has score 0 and no story;
// its entries carry the news.
type NewsItemScore struct {
	ItemID int64
	Score  int
	Story  string
}

// NewsIssueEntry is a piece of news read from an issue's page. It becomes an
// item of the issue's watch.
type NewsIssueEntry struct {
	IssueID int64
	Title   string
	URL     string
	Summary string
	Score   int
	Story   string
}

// BriefingNews is what news a briefing offers: the stories with items since
// the last briefing, and the ids of every pending news item it marks, sorted
// or from a paused source. Items not yet sorted stay pending.
type BriefingNews struct {
	Stories []NewsStory
	IDs     []int64
}

// NewsSourceStats is what a news source brought since a time: items first
// seen, how many were sorted with a score and the sum of those scores, how
// many scored 4 or 5, how many share their story with another source, how
// many stories with its items briefings picked and how many of those were
// among today's focus, and how many stories with its items the owner
// reacted to with more or less.
type NewsSourceStats struct {
	Watch    Watch
	Items    int
	Scored   int
	ScoreSum int
	High     int
	Shared   int
	Picked   int
	Focus    int
	More     int
	Less     int
}

// NewsQuery finds stories updated since a time, optionally in a section and
// matching text in a story or its items.
type NewsQuery struct {
	Section string
	Text    string
	Since   time.Time
	Limit   int
}

// NewsStore keeps the sorting of news items into scored stories.
type NewsStore interface {
	// ClaimNewsBatch claims up to limit pending news items first seen within
	// NewsRetention, not sorted and not claimed by a live lease, oldest
	// first. With issues it claims issues only, otherwise other items only.
	// An empty batch means nothing is waiting.
	ClaimNewsBatch(ctx context.Context, now time.Time, lease time.Duration, limit int, issues bool) (NewsBatch, error)
	// CommitNewsBatch writes a batch's sorting in one transaction while its
	// claim holds, else ErrNewsLeaseLost.
	CommitNewsBatch(ctx context.Context, token string, commit NewsCommit) error
	// DeferNewsBatch keeps a batch's items claimed until until, when the
	// budget ran out.
	DeferNewsBatch(ctx context.Context, token string, until time.Time) error
	// FailNewsBatch records why a batch could not be sorted. Items that fail
	// three times are sorted with score 0 and the error, which keeps them
	// out of briefings.
	FailNewsBatch(ctx context.Context, token string, reason string, at time.Time) error
	// RecentNewsStories lists stories of the sections updated since since,
	// newest first.
	RecentNewsStories(ctx context.Context, sections []string, since time.Time, limit int) ([]NewsStory, error)
	// BriefingNewsStories lists, per section, up to perSection stories with
	// pending sorted items of sources neither on trial nor paused, highest
	// score first, each with those items. A story shows in its own section
	// when that section has such a source, else in the section of its best
	// offered item's source.
	BriefingNewsStories(ctx context.Context, perSection int) (BriefingNews, error)
	SearchNewsStories(ctx context.Context, query NewsQuery) ([]NewsStory, error)
	// FailedNewsItems counts the items given up on since since.
	FailedNewsItems(ctx context.Context, since time.Time) (int, error)
	// NewsSourceStats lists what each news source that is not paused
	// brought since since, in id order.
	NewsSourceStats(ctx context.Context, since time.Time) ([]NewsSourceStats, error)
	// PruneNews keeps only the key and status of news items first seen
	// before before, so a feed that still lists them does not bring them
	// back, takes them out of their stories and deletes the stories left
	// without items. Items registered as memory sources, from before their
	// watch became a news source, stay whole. It returns the items pruned.
	PruneNews(ctx context.Context, before time.Time) (int, error)

	// UnrecordedBriefings lists the succeeded briefing runs finished since
	// since whose picks are not recorded, oldest first.
	UnrecordedBriefings(ctx context.Context, since time.Time) ([]RoutineOutput, error)
	// BriefedNewsStories lists the stories with items the briefing run
	// marked, each with all its items.
	BriefedNewsStories(ctx context.Context, runID string) ([]NewsStory, error)
	// RecordBriefingPicks records what a briefing picked once; a briefing
	// already recorded is left as it is.
	RecordBriefingPicks(ctx context.Context, picks BriefingPicks) error
	// BriefingStories lists the stories a briefing picked, in its order,
	// each with the owner's current reaction; empty when the briefing is
	// not recorded.
	BriefingStories(ctx context.Context, runID string) ([]BriefingStory, error)
	// LastBriefingUnmatched counts the unmatched lines of the latest
	// recorded briefing.
	LastBriefingUnmatched(ctx context.Context) (int, error)
	// ReactToNewsStory records the owner's reaction to a story, replacing
	// an earlier one, else ErrNewsStoryNotFound.
	ReactToNewsStory(ctx context.Context, storyID int64, reaction NewsReaction, deviceID string, at time.Time) error
	// ClearNewsReaction drops the owner's reaction to a story.
	ClearNewsReaction(ctx context.Context, storyID int64) error
	// NewsReactions lists the reactions since since, newest first.
	NewsReactions(ctx context.Context, since time.Time) ([]StoryReaction, error)
	// PruneNewsReactions drops the reactions and the recorded briefing picks
	// from before before.
	PruneNewsReactions(ctx context.Context, before time.Time) error
}
