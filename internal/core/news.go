package core

import (
	"context"
	"errors"
	"time"
)

// ErrNewsLeaseLost means a news batch's claim expired and another worker
// took its items, so the batch's result was not written.
var ErrNewsLeaseLost = errors.New("news batch lease lost")

// NewsRetention is how long news items are sorted, offered and searchable
// after they are first seen.
const NewsRetention = 14 * 24 * time.Hour

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
// many scored 4 or 5, how many share their story with another source, and
// how many a briefing written since links.
type NewsSourceStats struct {
	Watch    Watch
	Items    int
	Scored   int
	ScoreSum int
	High     int
	Shared   int
	Picked   int
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
	// pending sorted items of sources not on trial, highest score first, each
	// with those items.
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
}
