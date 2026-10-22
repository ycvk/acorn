package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// newsStoriesTables are shared with the migration that adds them. A news
// item is sorted once into news_scores; news_claims holds the items a
// worker is sorting. Both go with their item.
const newsStoriesTables = `CREATE TABLE IF NOT EXISTS news_stories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    section TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    development TEXT NOT NULL DEFAULT '',
    score INTEGER NOT NULL DEFAULT 0,
    first_seen TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_news_stories_updated ON news_stories(updated_at);
CREATE TABLE IF NOT EXISTS news_scores (
    item_id INTEGER PRIMARY KEY REFERENCES watch_items(id) ON DELETE CASCADE,
    story_id INTEGER REFERENCES news_stories(id),
    score INTEGER NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    sorted_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_news_scores_story ON news_scores(story_id);
CREATE TABLE IF NOT EXISTS news_claims (
    item_id INTEGER PRIMARY KEY REFERENCES watch_items(id) ON DELETE CASCADE,
    token TEXT NOT NULL,
    lease_until TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_news_claims_token ON news_claims(token);`

// addNewsStories (22 to 23) adds the sorting of news items into stories.
func addNewsStories(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, newsStoriesTables)
	return err
}

// newsFailedAttempts is how many failed sortings an item gets.
const newsFailedAttempts = 3

func (s *Store) ClaimNewsBatch(ctx context.Context, now time.Time, lease time.Duration, limit int, issues bool) (batch core.NewsBatch, err error) {
	if limit <= 0 || lease <= 0 {
		return core.NewsBatch{}, errors.New("claim news batch: limit and lease must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return core.NewsBatch{}, fmt.Errorf("claim news batch: %w", err)
	}
	defer rollbackOnErr(tx, &err, "claim news batch")
	rows, err := tx.QueryContext(ctx,
		`SELECT i.id, i.watch_id, i.item_key, i.title, i.url, i.summary, i.published_at, i.status, i.run_id, i.seen_at, w.name, w.section, w.issue
		 FROM watch_items i JOIN watches w ON w.id = i.watch_id
		 LEFT JOIN news_scores s ON s.item_id = i.id
		 LEFT JOIN news_claims c ON c.item_id = i.id
		 WHERE w.mode = ? AND w.status <> ? AND i.status = ? AND i.seen_at >= ? AND s.item_id IS NULL AND w.issue = ?
		   AND (c.item_id IS NULL OR c.lease_until <= ?)
		 ORDER BY i.id LIMIT ?`,
		string(core.WatchModeNews), string(core.WatchPaused), string(core.WatchItemPending), formatTimestamp(now.Add(-core.NewsRetention)), issues, formatTimestamp(now), limit)
	if err != nil {
		return core.NewsBatch{}, fmt.Errorf("claim news batch: %w", err)
	}
	var items []core.NewsBatchItem
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			it                          core.NewsBatchItem
			itemStatus, published, seen string
			section                     sql.NullString
		)
		if err := scan(&it.Item.ID, &it.Item.WatchID, &it.Item.Key, &it.Item.Title, &it.Item.URL, &it.Item.Summary, &published, &itemStatus, &it.Item.RunID, &seen, &it.Source, &section, &it.Issue); err != nil {
			return err
		}
		it.Item.Status, it.Section = core.WatchItemStatus(itemStatus), section.String
		var err error
		if it.Item.PublishedAt, err = parseOptionalTime(published, "watch_items.published_at"); err != nil {
			return err
		}
		it.Item.SeenAt, err = parseTimestamp(fixedTimestampLayout, seen, "watch_items.seen_at")
		items = append(items, it)
		return err
	}); err != nil {
		return core.NewsBatch{}, fmt.Errorf("claim news batch: %w", err)
	}
	if len(items) == 0 {
		return core.NewsBatch{}, tx.Commit()
	}
	token := rand.Text()
	for _, it := range items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO news_claims (item_id, token, lease_until, attempts) VALUES (?, ?, ?, 1)
			 ON CONFLICT(item_id) DO UPDATE SET token = excluded.token, lease_until = excluded.lease_until, attempts = news_claims.attempts + 1`,
			it.Item.ID, token, formatTimestamp(now.Add(lease))); err != nil {
			return core.NewsBatch{}, fmt.Errorf("claim news item %d: %w", it.Item.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return core.NewsBatch{}, fmt.Errorf("claim news batch: %w", err)
	}
	return core.NewsBatch{Token: token, Items: items}, nil
}

func (s *Store) CommitNewsBatch(ctx context.Context, token string, commit core.NewsCommit) (err error) {
	if token == "" || commit.At.IsZero() {
		return errors.New("commit news batch: token and at are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("commit news batch: %w", err)
	}
	defer rollbackOnErr(tx, &err, "commit news batch")
	claimed, err := claimedNewsItems(ctx, tx, token)
	if err != nil {
		return err
	}
	if len(claimed) == 0 {
		return core.ErrNewsLeaseLost
	}
	scored := map[int64]bool{}
	for _, score := range commit.Scores {
		if !claimed[score.ItemID] || scored[score.ItemID] {
			return fmt.Errorf("commit news batch: item %d is not claimed by the batch or is scored twice", score.ItemID)
		}
		scored[score.ItemID] = true
	}
	if len(scored) != len(claimed) {
		return fmt.Errorf("commit news batch: %d of %d claimed items are scored", len(scored), len(claimed))
	}
	at := formatTimestamp(commit.At)
	stories := map[string]int64{}
	for _, change := range commit.Stories {
		if change.ID == 0 {
			if change.Ref == "" || change.Section == "" || change.Title == "" {
				return fmt.Errorf("commit news batch: a new story needs a ref, section and title (%q)", change.Ref)
			}
			result, err := tx.ExecContext(ctx, `INSERT INTO news_stories (section, title, summary, development, first_seen, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				change.Section, change.Title, change.Summary, change.Development, at, at)
			if err != nil {
				return fmt.Errorf("commit news batch: add story %q: %w", change.Ref, err)
			}
			if stories[change.Ref], err = result.LastInsertId(); err != nil {
				return fmt.Errorf("commit news batch: add story %q: %w", change.Ref, err)
			}
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE news_stories SET title = ?, summary = ?, development = ?, updated_at = ? WHERE id = ?`,
			change.Title, change.Summary, change.Development, at, change.ID)
		if err != nil {
			return fmt.Errorf("commit news batch: update story %d: %w", change.ID, err)
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return fmt.Errorf("commit news batch: story %d does not exist (%v)", change.ID, err)
		}
		stories["s"+strconv.FormatInt(change.ID, 10)] = change.ID
	}
	touched := map[int64]bool{}
	resolve := func(ref string) (any, error) {
		if ref == "" {
			return nil, nil
		}
		id, ok := stories[ref]
		if !ok {
			existing, err := strconv.ParseInt(strings.TrimPrefix(ref, "s"), 10, 64)
			if !strings.HasPrefix(ref, "s") || err != nil {
				return nil, fmt.Errorf("commit news batch: unknown story %q", ref)
			}
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM news_stories WHERE id = ?)`, existing).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				return nil, fmt.Errorf("commit news batch: story %q does not exist", ref)
			}
			id = existing
		}
		touched[id] = true
		return id, nil
	}
	for _, score := range commit.Scores {
		story, err := resolve(score.Story)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO news_scores (item_id, story_id, score, sorted_at) VALUES (?, ?, ?, ?)`, score.ItemID, story, score.Score, at); err != nil {
			return fmt.Errorf("commit news batch: score item %d: %w", score.ItemID, err)
		}
	}
	entries := map[int64]int{}
	for _, entry := range commit.Entries {
		if !claimed[entry.IssueID] {
			return fmt.Errorf("commit news batch: issue %d is not claimed by the batch", entry.IssueID)
		}
		story, err := resolve(entry.Story)
		if err != nil {
			return err
		}
		entries[entry.IssueID]++
		var (
			watchID        int64
			key, published string
		)
		if err := tx.QueryRowContext(ctx, `SELECT watch_id, item_key, published_at FROM watch_items WHERE id = ?`, entry.IssueID).Scan(&watchID, &key, &published); err != nil {
			return fmt.Errorf("commit news batch: issue %d: %w", entry.IssueID, err)
		}
		result, err := tx.ExecContext(ctx,
			`INSERT INTO watch_items (watch_id, item_key, title, url, summary, published_at, status, run_id, seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, '', ?)`,
			watchID, fmt.Sprintf("%s#%d", key, entries[entry.IssueID]), entry.Title, entry.URL, entry.Summary, published, string(core.WatchItemPending), at)
		if err != nil {
			return fmt.Errorf("commit news batch: add entry of issue %d: %w", entry.IssueID, err)
		}
		itemID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("commit news batch: add entry of issue %d: %w", entry.IssueID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO news_scores (item_id, story_id, score, sorted_at) VALUES (?, ?, ?, ?)`, itemID, story, entry.Score, at); err != nil {
			return fmt.Errorf("commit news batch: score entry of issue %d: %w", entry.IssueID, err)
		}
	}
	for id := range touched {
		if _, err := tx.ExecContext(ctx,
			`UPDATE news_stories SET updated_at = ?, score = (SELECT COALESCE(MAX(score), 0) FROM news_scores WHERE story_id = ?) WHERE id = ?`, at, id, id); err != nil {
			return fmt.Errorf("commit news batch: rescore story %d: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM news_claims WHERE token = ?`, token); err != nil {
		return fmt.Errorf("commit news batch: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit news batch: %w", err)
	}
	return nil
}

func claimedNewsItems(ctx context.Context, tx *sql.Tx, token string) (map[int64]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT item_id FROM news_claims WHERE token = ?`, token)
	if err != nil {
		return nil, fmt.Errorf("news batch claims: %w", err)
	}
	claimed := map[int64]bool{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var id int64
		if err := scan(&id); err != nil {
			return err
		}
		claimed[id] = true
		return nil
	})
	return claimed, err
}

func (s *Store) DeferNewsBatch(ctx context.Context, token string, until time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE news_claims SET lease_until = ?, attempts = attempts - 1 WHERE token = ?`, formatTimestamp(until), token)
	if err != nil {
		return fmt.Errorf("defer news batch: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		return errors.Join(core.ErrNewsLeaseLost, err)
	}
	return nil
}

func (s *Store) FailNewsBatch(ctx context.Context, token, reason string, at time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("fail news batch: %w", err)
	}
	defer rollbackOnErr(tx, &err, "fail news batch")
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO news_scores (item_id, score, error, sorted_at) SELECT item_id, 0, ?, ? FROM news_claims WHERE token = ? AND attempts >= ?`,
		reason, formatTimestamp(at), token, newsFailedAttempts); err != nil {
		return fmt.Errorf("fail news batch: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM news_claims WHERE token = ? AND attempts >= ?`, token, newsFailedAttempts); err != nil {
		return fmt.Errorf("fail news batch: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE news_claims SET lease_until = ?, error = ? WHERE token = ?`, formatTimestamp(at), reason, token); err != nil {
		return fmt.Errorf("fail news batch: %w", err)
	}
	return tx.Commit()
}

const newsStoryColumns = `st.id, st.section, st.title, st.summary, st.development, st.score, st.first_seen, st.updated_at`

func (s *Store) RecentNewsStories(ctx context.Context, sections []string, since time.Time, limit int) ([]core.NewsStory, error) {
	if len(sections) == 0 {
		return nil, nil
	}
	args := []any{formatTimestamp(since)}
	for _, section := range sections {
		args = append(args, section)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+newsStoryColumns+` FROM news_stories st WHERE st.updated_at >= ? AND st.section IN (`+placeholders(len(sections))+`)
		 ORDER BY st.updated_at DESC, st.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("recent news stories: %w", err)
	}
	return scanNewsStories(rows)
}

func (s *Store) BriefingNewsStories(ctx context.Context, perSection int) (core.BriefingNews, error) {
	if perSection <= 0 {
		return core.BriefingNews{}, errors.New("briefing news stories: perSection must be positive")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, section, title, summary, development, score, first_seen, updated_at FROM (
		   SELECT `+newsStoryColumns+`, ROW_NUMBER() OVER (PARTITION BY st.section ORDER BY st.score DESC, st.updated_at DESC, st.id DESC) AS rank
		   FROM news_stories st
		   WHERE EXISTS (SELECT 1 FROM news_scores s JOIN watch_items i ON i.id = s.item_id WHERE s.story_id = st.id AND i.status = ?))
		 WHERE rank <= ? ORDER BY section, rank`,
		string(core.WatchItemPending), perSection)
	if err != nil {
		return core.BriefingNews{}, fmt.Errorf("briefing news stories: %w", err)
	}
	stories, err := scanNewsStories(rows)
	if err != nil {
		return core.BriefingNews{}, fmt.Errorf("briefing news stories: %w", err)
	}
	if err := s.attachStoryItems(ctx, stories, string(core.WatchItemPending), 0); err != nil {
		return core.BriefingNews{}, fmt.Errorf("briefing news stories: %w", err)
	}
	idRows, err := s.db.QueryContext(ctx,
		`SELECT i.id FROM watch_items i JOIN watches w ON w.id = i.watch_id LEFT JOIN news_scores s ON s.item_id = i.id
		 WHERE i.status = ? AND w.mode = ? AND (s.item_id IS NOT NULL OR w.status = ?) ORDER BY i.id`,
		string(core.WatchItemPending), string(core.WatchModeNews), string(core.WatchPaused))
	if err != nil {
		return core.BriefingNews{}, fmt.Errorf("briefing news stories: %w", err)
	}
	var ids []int64
	if err := scanRows(idRows, func(scan func(...any) error) error {
		var id int64
		if err := scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	}); err != nil {
		return core.BriefingNews{}, fmt.Errorf("briefing news stories: %w", err)
	}
	return core.BriefingNews{Stories: stories, IDs: ids}, nil
}

// newsItemsPerStory bounds the items a search lists under each story.
const newsItemsPerStory = 5

// SearchNewsStories lists the stories changed since query.Since, highest
// score first, whose title, summary, development or items contain every word
// of query.Text.
func (s *Store) SearchNewsStories(ctx context.Context, query core.NewsQuery) ([]core.NewsStory, error) {
	if query.Limit <= 0 {
		return nil, errors.New("search news stories: limit must be positive")
	}
	where := []string{"st.updated_at >= ?", "(? = '' OR st.section = ?)"}
	args := []any{formatTimestamp(query.Since), query.Section, query.Section}
	for _, word := range strings.Fields(query.Text) {
		where = append(where, `(st.title LIKE ? ESCAPE '\' OR st.summary LIKE ? ESCAPE '\' OR st.development LIKE ? ESCAPE '\' OR EXISTS (
		     SELECT 1 FROM news_scores s JOIN watch_items i ON i.id = s.item_id WHERE s.story_id = st.id AND (i.title LIKE ? ESCAPE '\' OR i.summary LIKE ? ESCAPE '\')))`)
		pattern := "%" + escapeLike(word) + "%"
		args = append(args, pattern, pattern, pattern, pattern, pattern)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+newsStoryColumns+` FROM news_stories st WHERE `+strings.Join(where, " AND ")+`
		 ORDER BY st.score DESC, st.updated_at DESC, st.id DESC LIMIT ?`,
		append(args, query.Limit)...)
	if err != nil {
		return nil, fmt.Errorf("search news stories: %w", err)
	}
	stories, err := scanNewsStories(rows)
	if err != nil {
		return nil, fmt.Errorf("search news stories: %w", err)
	}
	if err := s.attachStoryItems(ctx, stories, "", newsItemsPerStory); err != nil {
		return nil, fmt.Errorf("search news stories: %w", err)
	}
	return stories, nil
}

// attachStoryItems adds each story's items with status, all when status is
// empty, newest first and at most limit of them when limit is positive.
func (s *Store) attachStoryItems(ctx context.Context, stories []core.NewsStory, status string, limit int) error {
	rowLimit := limit
	if rowLimit <= 0 {
		rowLimit = -1 // SQLite reads a negative LIMIT as no limit.
	}
	for i := range stories {
		rows, err := s.db.QueryContext(ctx,
			`SELECT i.id, i.watch_id, i.item_key, i.title, i.url, i.summary, i.published_at, i.status, i.run_id, i.seen_at, w.name, s.score
			 FROM news_scores s JOIN watch_items i ON i.id = s.item_id JOIN watches w ON w.id = i.watch_id
			 WHERE s.story_id = ? AND (? = '' OR i.status = ?)
			 ORDER BY COALESCE(NULLIF(i.published_at, ''), i.seen_at) DESC, i.id DESC LIMIT ?`,
			stories[i].ID, status, status, rowLimit)
		if err != nil {
			return err
		}
		if err := scanRows(rows, func(scan func(...any) error) error {
			var (
				item                        core.NewsStoryItem
				itemStatus, published, seen string
			)
			if err := scan(&item.ID, &item.WatchID, &item.Key, &item.Title, &item.URL, &item.Summary, &published, &itemStatus, &item.RunID, &seen, &item.Source, &item.Score); err != nil {
				return err
			}
			item.Status = core.WatchItemStatus(itemStatus)
			var err error
			if item.PublishedAt, err = parseOptionalTime(published, "watch_items.published_at"); err != nil {
				return err
			}
			item.SeenAt, err = parseTimestamp(fixedTimestampLayout, seen, "watch_items.seen_at")
			stories[i].Items = append(stories[i].Items, item)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) FailedNewsItems(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM news_scores WHERE error <> '' AND sorted_at >= ?`, formatTimestamp(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("failed news items: %w", err)
	}
	return n, nil
}

func (s *Store) PruneNews(ctx context.Context, before time.Time) (pruned int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("prune news: %w", err)
	}
	defer rollbackOnErr(tx, &err, "prune news")
	const expired = `SELECT i.id FROM watch_items i JOIN watches w ON w.id = i.watch_id
		WHERE w.mode = ? AND i.seen_at < ? AND NOT EXISTS (SELECT 1 FROM memory_sources m WHERE m.id = 'watch:' || i.id)`
	args := []any{string(core.WatchModeNews), formatTimestamp(before)}
	if _, err := tx.ExecContext(ctx, `UPDATE news_scores SET story_id = NULL WHERE story_id IS NOT NULL AND item_id IN (`+expired+`)`, args...); err != nil {
		return 0, fmt.Errorf("prune news scores: %w", err)
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE watch_items SET title = '', url = '', summary = '' WHERE (title <> '' OR url <> '' OR summary <> '') AND id IN (`+expired+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("prune news items: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune news items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM news_stories WHERE NOT EXISTS (SELECT 1 FROM news_scores s WHERE s.story_id = news_stories.id)`); err != nil {
		return 0, fmt.Errorf("prune news stories: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("prune news: %w", err)
	}
	return int(n), nil
}

// placeholders is n comma-separated SQL parameters.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func scanNewsStories(rows *sql.Rows) ([]core.NewsStory, error) {
	var stories []core.NewsStory
	err := scanRows(rows, func(scan func(...any) error) error {
		var (
			story          core.NewsStory
			first, updated string
		)
		if err := scan(&story.ID, &story.Section, &story.Title, &story.Summary, &story.Development, &story.Score, &first, &updated); err != nil {
			return err
		}
		var err error
		if story.FirstSeen, err = parseTimestamp(fixedTimestampLayout, first, "news_stories.first_seen"); err != nil {
			return err
		}
		story.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "news_stories.updated_at")
		stories = append(stories, story)
		return err
	})
	return stories, err
}
