package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// briefingPicksTables are shared with the migrations that add and rebuild
// them. A briefing's picks are recorded once, after its run succeeded, and
// leave with its thread; pieces_at and pushed_at are when the pieces about
// its focus stories were started and its push was sent, empty until then. A
// reaction outlives its story and the briefing it was made on: it keeps the
// story's section, title and sources. sources are JSON arrays of watch ids.
const briefingPicksTables = `CREATE TABLE IF NOT EXISTS briefing_picks (
    run_id TEXT PRIMARY KEY,
    briefed_at TEXT NOT NULL,
    unmatched INTEGER NOT NULL DEFAULT 0,
    pieces_at TEXT NOT NULL DEFAULT '',
    pushed_at TEXT NOT NULL DEFAULT ''
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

// addBriefingPieces (37 to 38) lets a story message be the wake input of a
// piece and records when a briefing's pieces were started and its push was
// sent. The briefings recorded before it were pushed as they were recorded
// and get no pieces. Both tables are rebuilt so their stored definitions
// match a new database.
func addBriefingPieces(ctx context.Context, tx *sql.Tx) error {
	messages := "message_id, session_id, story_id, intent, story, created_at"
	for _, statement := range []string{
		`CREATE TEMP TABLE briefing_picks_v37 AS SELECT run_id, briefed_at, unmatched FROM briefing_picks`,
		`DROP TABLE briefing_picks`,
		briefingPicksTables,
		`INSERT INTO briefing_picks(run_id, briefed_at, unmatched, pieces_at, pushed_at) SELECT run_id, briefed_at, unmatched, briefed_at, briefed_at FROM briefing_picks_v37`,
		`DROP TABLE briefing_picks_v37`,
		`CREATE TEMP TABLE news_story_messages_v37 AS SELECT ` + messages + ` FROM news_story_messages`,
		`DROP TABLE news_story_messages`,
		newsStoryMessagesTable,
		`INSERT INTO news_story_messages(` + messages + `) SELECT ` + messages + ` FROM news_story_messages_v37`,
		`DROP TABLE news_story_messages_v37`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
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
		o.FinishedAt, err = parseTimestamp(finished, "runs.finished_at")
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

// OpenBriefings lists the recorded briefings finished since since whose
// pieces are not started or whose push is not sent, oldest first.
func (s *Store) OpenBriefings(ctx context.Context, since time.Time) ([]core.OpenBriefing, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT rr.slot, rr.thread_id, rr.run_id, r.output_text, r.finished_at, p.pieces_at <> '', p.pushed_at <> ''
		 FROM briefing_picks p JOIN routine_runs rr ON rr.run_id = p.run_id AND rr.routine = 'briefing' JOIN runs r ON r.run_id = p.run_id
		 WHERE p.briefed_at >= ? AND (p.pieces_at = '' OR p.pushed_at = '')
		 ORDER BY p.briefed_at, p.run_id`, formatTimestamp(since))
	if err != nil {
		return nil, fmt.Errorf("open briefings: %w", err)
	}
	var out []core.OpenBriefing
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			b        core.OpenBriefing
			finished string
		)
		if err := scan(&b.Slot, &b.ThreadID, &b.RunID, &b.Text, &finished, &b.PiecesStarted, &b.Pushed); err != nil {
			return err
		}
		var err error
		b.FinishedAt, err = parseTimestamp(finished, "runs.finished_at")
		out = append(out, b)
		return err
	}); err != nil {
		return nil, fmt.Errorf("open briefings: %w", err)
	}
	return out, nil
}

func (s *Store) ClaimBriefingPieces(ctx context.Context, runID string, at time.Time) (bool, error) {
	return s.claimBriefing(ctx, "pieces_at", runID, at)
}

func (s *Store) ClaimBriefingPush(ctx context.Context, runID string, at time.Time) (bool, error) {
	return s.claimBriefing(ctx, "pushed_at", runID, at)
}

// claimBriefing sets an empty step column of a recorded briefing and
// reports whether it did.
func (s *Store) claimBriefing(ctx context.Context, column, runID string, at time.Time) (bool, error) {
	if runID == "" || at.IsZero() {
		return false, fmt.Errorf("claim briefing %s: runID and at are required", column)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE briefing_picks SET `+column+` = ? WHERE run_id = ? AND `+column+` = ''`, formatTimestamp(at), runID)
	if err != nil {
		return false, fmt.Errorf("claim briefing %s: %w", column, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim briefing %s: %w", column, err)
	}
	return n == 1, nil
}

// BriefingStories lists a briefing's picks. A story's piece is the newest
// piece message of the story sent since the briefing was recorded, read
// through the run its wake input started.
func (s *Store) BriefingStories(ctx context.Context, runID string) ([]core.BriefingStory, error) {
	rows, err := s.db.QueryContext(ctx,
		`WITH picked AS (
		   SELECT b.position, b.story_id, b.section, b.title, b.summary, b.url, b.focus, b.sources, COALESCE(r.reaction, '') AS reaction,
		     COALESCE((`+storyThread("b.story_id")+`), '') AS thread,
		     COALESCE((SELECT json_group_array(name) FROM (SELECT w.name FROM json_each(b.sources) j JOIN watches w ON w.id = j.value ORDER BY w.id)), '[]') AS source_names,
		     (SELECT m.message_id FROM news_story_messages m WHERE m.story_id = b.story_id AND m.intent = 'piece' AND m.created_at >= p.briefed_at
		      ORDER BY m.message_id DESC LIMIT 1) AS piece
		   FROM briefing_stories b JOIN briefing_picks p ON p.run_id = b.run_id LEFT JOIN news_reactions r ON r.story_id = b.story_id WHERE b.run_id = ?)
		 SELECT story_id, section, title, summary, url, focus, sources, source_names, reaction, thread,
		   EXISTS (SELECT 1 FROM memory_concerns c WHERE c.thread_id = picked.thread AND c.state IN ('active', 'waiting') AND `+visibleConcern+`),
		   piece IS NOT NULL, COALESCE(pm.run_id, ''), COALESCE(pr.status, ''), COALESCE(pr.output_text, ''), COALESCE(pr.finished_at, '')
		 FROM picked LEFT JOIN session_messages pm ON pm.id = picked.piece LEFT JOIN runs pr ON pr.run_id = pm.run_id AND pm.run_id <> ''
		 ORDER BY position`, runID)
	if err != nil {
		return nil, fmt.Errorf("briefing stories: %w", err)
	}
	var stories []core.BriefingStory
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			story                    core.BriefingStory
			sources, names, reaction string
			hasPiece                 bool
			piece                    core.BriefingPiece
			status, finished         string
		)
		if err := scan(&story.StoryID, &story.Section, &story.Title, &story.Summary, &story.URL, &story.Focus, &sources, &names, &reaction, &story.ThreadID, &story.Following,
			&hasPiece, &piece.RunID, &status, &piece.Body, &finished); err != nil {
			return err
		}
		story.Reaction = core.NewsReaction(reaction)
		if err := json.Unmarshal([]byte(sources), &story.Sources); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(names), &story.SourceNames); err != nil {
			return err
		}
		if hasPiece {
			if err := finishBriefingPiece(&piece, core.RunStatus(status), finished); err != nil {
				return err
			}
			story.Piece = &piece
		}
		stories = append(stories, story)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("briefing stories: %w", err)
	}
	return stories, nil
}

// finishBriefingPiece sets a piece's status from its run's: a running run is
// still writing, a succeeded one with a reply is ready and keeps the reply,
// and any other outcome, a run that never started among them, failed.
func finishBriefingPiece(piece *core.BriefingPiece, status core.RunStatus, finished string) error {
	body := strings.TrimSpace(piece.Body)
	piece.Body = ""
	switch {
	case status == core.RunStatusRunning:
		piece.Status = core.BriefingPieceWriting
	case status == core.RunStatusSucceeded && body != "":
		piece.Status, piece.Body = core.BriefingPieceReady, body
		var err error
		piece.FinishedAt, err = parseTimestamp(finished, "runs.finished_at")
		return err
	default:
		piece.Status = core.BriefingPieceFailed
	}
	return nil
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
		r.At, err = parseTimestamp(at, "news_reactions.created_at")
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
