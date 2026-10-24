package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// briefingPicksTables are shared with the migration that adds them. A
// briefing's picks are recorded once, after its run succeeded, and leave
// with its thread. A reaction outlives its story and the briefing it was
// made on: it keeps the story's section, title and sources. sources are
// JSON arrays of watch ids.
const briefingPicksTables = `CREATE TABLE IF NOT EXISTS briefing_picks (
    run_id TEXT PRIMARY KEY,
    briefed_at TEXT NOT NULL,
    unmatched INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_briefing_picks_briefed ON briefing_picks(briefed_at);
CREATE TABLE IF NOT EXISTS briefing_stories (
    run_id TEXT NOT NULL REFERENCES briefing_picks(run_id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    story_id INTEGER NOT NULL,
    section TEXT NOT NULL,
    title TEXT NOT NULL,
    summary TEXT NOT NULL,
    url TEXT NOT NULL,
    focus INTEGER NOT NULL DEFAULT 0,
    sources TEXT NOT NULL,
    PRIMARY KEY (run_id, position),
    UNIQUE (run_id, story_id)
);
CREATE TABLE IF NOT EXISTS news_reactions (
    story_id INTEGER PRIMARY KEY,
    reaction TEXT NOT NULL CHECK (reaction IN ('more', 'less')),
    section TEXT NOT NULL,
    title TEXT NOT NULL,
    sources TEXT NOT NULL,
    device_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_news_reactions_created ON news_reactions(created_at);`

// addBriefingPicks (28 to 29) adds the stories briefings picked and the
// owner's reactions to them.
func addBriefingPicks(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, briefingPicksTables)
	return err
}

func (s *Store) UnrecordedBriefings(ctx context.Context, since time.Time) ([]core.RoutineOutput, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rr.slot, rr.thread_id, rr.run_id, r.output_text, r.finished_at FROM routine_runs rr JOIN runs r ON r.run_id = rr.run_id
		 WHERE rr.routine = 'briefing' AND r.status = ? AND r.finished_at >= ? AND NOT EXISTS (SELECT 1 FROM briefing_picks p WHERE p.run_id = rr.run_id)
		 ORDER BY r.finished_at, rr.run_id`,
		string(core.RunStatusSucceeded), formatTimestamp(since))
	if err != nil {
		return nil, fmt.Errorf("unrecorded briefings: %w", err)
	}
	var out []core.RoutineOutput
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			o        core.RoutineOutput
			finished string
		)
		if err := scan(&o.Slot, &o.ThreadID, &o.RunID, &o.Text, &finished); err != nil {
			return err
		}
		var err error
		o.FinishedAt, err = parseTimestamp(time.RFC3339Nano, finished, "runs.finished_at")
		out = append(out, o)
		return err
	}); err != nil {
		return nil, fmt.Errorf("unrecorded briefings: %w", err)
	}
	return out, nil
}

func (s *Store) BriefedNewsStories(ctx context.Context, runID string) ([]core.NewsStory, error) {
	if runID == "" {
		return nil, errors.New("briefed news stories: runID is required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+newsStoryColumns+` FROM news_stories st
		 WHERE st.id IN (SELECT s.story_id FROM news_scores s JOIN watch_items i ON i.id = s.item_id WHERE i.run_id = ?)
		 ORDER BY st.id`, runID)
	if err != nil {
		return nil, fmt.Errorf("briefed news stories: %w", err)
	}
	stories, err := scanNewsStories(rows)
	if err != nil {
		return nil, fmt.Errorf("briefed news stories: %w", err)
	}
	if err := s.attachStoryItems(ctx, stories, false, 0); err != nil {
		return nil, fmt.Errorf("briefed news stories: %w", err)
	}
	return stories, nil
}

func (s *Store) RecordBriefingPicks(ctx context.Context, picks core.BriefingPicks) (err error) {
	if picks.RunID == "" || picks.BriefedAt.IsZero() || picks.Unmatched < 0 {
		return errors.New("record briefing picks: runID and briefedAt are required and unmatched must not be negative")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record briefing picks: %w", err)
	}
	defer rollbackOnErr(tx, &err, "record briefing picks")
	result, err := tx.ExecContext(ctx, `INSERT INTO briefing_picks (run_id, briefed_at, unmatched) VALUES (?, ?, ?) ON CONFLICT(run_id) DO NOTHING`,
		picks.RunID, formatTimestamp(picks.BriefedAt), picks.Unmatched)
	if err != nil {
		return fmt.Errorf("record briefing picks: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("record briefing picks: %w", err)
	}
	if n == 0 {
		return tx.Commit()
	}
	for i, story := range picks.Stories {
		sources, err := json.Marshal(nonNilIDs(story.Sources))
		if err != nil {
			return fmt.Errorf("record briefing picks: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO briefing_stories (run_id, position, story_id, section, title, summary, url, focus, sources) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			picks.RunID, i+1, story.StoryID, story.Section, story.Title, story.Summary, story.URL, story.Focus, string(sources)); err != nil {
			return fmt.Errorf("record briefing story %d: %w", story.StoryID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) BriefingStories(ctx context.Context, runID string) ([]core.BriefingStory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT b.story_id, b.section, b.title, b.summary, b.url, b.focus, b.sources, COALESCE(r.reaction, '')
		 FROM briefing_stories b LEFT JOIN news_reactions r ON r.story_id = b.story_id
		 WHERE b.run_id = ? ORDER BY b.position`, runID)
	if err != nil {
		return nil, fmt.Errorf("briefing stories: %w", err)
	}
	var stories []core.BriefingStory
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			story             core.BriefingStory
			sources, reaction string
		)
		if err := scan(&story.StoryID, &story.Section, &story.Title, &story.Summary, &story.URL, &story.Focus, &sources, &reaction); err != nil {
			return err
		}
		story.Reaction = core.NewsReaction(reaction)
		if err := json.Unmarshal([]byte(sources), &story.Sources); err != nil {
			return err
		}
		stories = append(stories, story)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("briefing stories: %w", err)
	}
	return stories, nil
}

// ReactToNewsStory keeps the story's section, title and the sources of its
// items with the reaction.
func (s *Store) ReactToNewsStory(ctx context.Context, storyID int64, reaction core.NewsReaction, deviceID string, at time.Time) error {
	if !reaction.Valid() {
		return fmt.Errorf("react to news story: reaction %q must be more or less", reaction)
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO news_reactions (story_id, reaction, section, title, sources, device_id, created_at)
		 SELECT st.id, ?, st.section, st.title, (
		   SELECT json_group_array(watch_id) FROM (SELECT DISTINCT i.watch_id FROM news_scores s JOIN watch_items i ON i.id = s.item_id WHERE s.story_id = st.id ORDER BY i.watch_id)),
		   ?, ? FROM news_stories st WHERE st.id = ?
		 ON CONFLICT(story_id) DO UPDATE SET reaction = excluded.reaction, section = excluded.section, title = excluded.title,
		   sources = excluded.sources, device_id = excluded.device_id, created_at = excluded.created_at`,
		string(reaction), deviceID, formatTimestamp(at), storyID)
	if err != nil {
		return fmt.Errorf("react to news story %d: %w", storyID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("react to news story %d: %w", storyID, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %d", core.ErrNewsStoryNotFound, storyID)
	}
	return nil
}

func (s *Store) ClearNewsReaction(ctx context.Context, storyID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM news_reactions WHERE story_id = ?`, storyID); err != nil {
		return fmt.Errorf("clear news reaction %d: %w", storyID, err)
	}
	return nil
}

func (s *Store) NewsReactions(ctx context.Context, since time.Time) ([]core.StoryReaction, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT story_id, reaction, section, title, sources, device_id, created_at FROM news_reactions WHERE created_at >= ? ORDER BY created_at DESC, story_id DESC`,
		formatTimestamp(since))
	if err != nil {
		return nil, fmt.Errorf("news reactions: %w", err)
	}
	var reactions []core.StoryReaction
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			r                     core.StoryReaction
			reaction, sources, at string
		)
		if err := scan(&r.StoryID, &reaction, &r.Section, &r.Title, &sources, &r.DeviceID, &at); err != nil {
			return err
		}
		r.Reaction = core.NewsReaction(reaction)
		if err := json.Unmarshal([]byte(sources), &r.Sources); err != nil {
			return err
		}
		var err error
		r.At, err = parseTimestamp(fixedTimestampLayout, at, "news_reactions.created_at")
		reactions = append(reactions, r)
		return err
	}); err != nil {
		return nil, fmt.Errorf("news reactions: %w", err)
	}
	return reactions, nil
}

func (s *Store) PruneNewsReactions(ctx context.Context, before time.Time) error {
	at := formatTimestamp(before)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM news_reactions WHERE created_at < ?`, at); err != nil {
		return fmt.Errorf("prune news reactions: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM briefing_picks WHERE briefed_at < ?`, at); err != nil {
		return fmt.Errorf("prune briefing picks: %w", err)
	}
	return nil
}

func nonNilIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
