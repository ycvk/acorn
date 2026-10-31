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

// newsStoryMessagesTable is shared with the migration that adds it. A row
// keeps the story as the owner saw it, as JSON, so it outlives the story,
// which is pruned with its items; it leaves with its conversation. The
// newest row of a story names the story's conversation.
const newsStoryMessagesTable = `CREATE TABLE IF NOT EXISTS news_story_messages (
    message_id INTEGER PRIMARY KEY,
    session_id TEXT NOT NULL,
    story_id INTEGER NOT NULL,
    intent TEXT NOT NULL CHECK (intent IN ('discuss', 'follow')),
    story TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_news_story_messages_story ON news_story_messages(story_id, message_id);
CREATE INDEX IF NOT EXISTS idx_news_story_messages_session ON news_story_messages(session_id, message_id);`

// addNewsStoryMessages (29 to 30) adds the owner messages sent from stories
// on the briefing card.
func addNewsStoryMessages(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, newsStoryMessagesTable)
	return err
}

// storySnapshotItems bounds the items a story keeps for the model when the
// owner sends a message from it.
const storySnapshotItems = 10

// NewsStory loads a story with its newest items, else ErrNewsStoryNotFound.
func (s *Store) NewsStory(ctx context.Context, id int64) (core.NewsStory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+newsStoryColumns+` FROM news_stories st WHERE st.id = ?`, id)
	if err != nil {
		return core.NewsStory{}, fmt.Errorf("news story %d: %w", id, err)
	}
	stories, err := scanNewsStories(rows)
	if err != nil {
		return core.NewsStory{}, fmt.Errorf("news story %d: %w", id, err)
	}
	if len(stories) == 0 {
		return core.NewsStory{}, fmt.Errorf("%w: %d", core.ErrNewsStoryNotFound, id)
	}
	if err := s.attachStoryItems(ctx, stories, false, storySnapshotItems); err != nil {
		return core.NewsStory{}, fmt.Errorf("news story %d: %w", id, err)
	}
	return stories[0], nil
}

// BriefingStoryTitle is the title the newest recorded briefing that picked
// the story gave it, empty when no briefing picked it.
func (s *Store) BriefingStoryTitle(ctx context.Context, storyID int64) (string, error) {
	var title string
	err := s.db.QueryRowContext(ctx,
		`SELECT b.title FROM briefing_stories b JOIN briefing_picks p ON p.run_id = b.run_id
		 WHERE b.story_id = ? ORDER BY p.briefed_at DESC, b.run_id DESC LIMIT 1`, storyID).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("briefing title of story %d: %w", storyID, err)
	}
	return title, nil
}

// storyThread selects the newest conversation an owner message from the
// story went to; story is the SQL expression of the story's id.
func storyThread(story string) string {
	return `SELECT session_id FROM news_story_messages WHERE story_id = ` + story + ` ORDER BY message_id DESC LIMIT 1`
}

// SendStoryMessage records an owner message sent from a story in the
// story's conversation, creating it under in.Title when the story has none.
// A conversation whose latest run is running or waits for the owner is
// refused with ErrThreadBusy.
func (s *Store) SendStoryMessage(ctx context.Context, in core.StoryMessageWrite) (core.SessionMessageRecord, error) {
	switch {
	case !in.Intent.Valid():
		return core.SessionMessageRecord{}, fmt.Errorf("send story message: intent %q must be discuss or follow", in.Intent)
	case strings.TrimSpace(in.Text) == "" || strings.TrimSpace(in.Title) == "":
		return core.SessionMessageRecord{}, errors.New("send story message: text and title are required")
	case in.Story.ID <= 0 || strings.TrimSpace(in.Story.Title) == "":
		return core.SessionMessageRecord{}, errors.New("send story message: the story's id and title are required")
	case strings.TrimSpace(in.NewThreadID) == "":
		return core.SessionMessageRecord{}, errors.New("send story message: NewThreadID is required")
	}
	story, err := json.Marshal(in.Story)
	if err != nil {
		return core.SessionMessageRecord{}, fmt.Errorf("send story message: %w", err)
	}
	now := time.Now().UTC()
	message := core.SessionMessageRecord{Role: "user", Content: strings.TrimSpace(in.Text), CreatedAt: now}
	err = s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, storyThread("?"), in.Story.ID).Scan(&message.SessionID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			message.SessionID = in.NewThreadID
			if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(session_id, title, created_at, updated_at) VALUES(?, ?, ?, ?)`,
				message.SessionID, strings.TrimSpace(in.Title), formatTimestamp(now), formatTimestamp(now)); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			var status string
			err := tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE session_id = ? ORDER BY turn_index DESC, finished_at DESC LIMIT 1`, message.SessionID).Scan(&status)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if status == string(core.RunStatusRunning) || status == string(core.RunStatusInterrupted) {
				return fmt.Errorf("%w: %s", core.ErrThreadBusy, message.SessionID)
			}
		}
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_index), 0) + 1 FROM session_messages WHERE session_id = ?`, message.SessionID).Scan(&message.TurnIndex); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO session_messages(session_id, turn_index, role, content, run_id, created_at) VALUES(?, ?, ?, ?, '', ?)`,
			message.SessionID, message.TurnIndex, message.Role, message.Content, formatTimestamp(now))
		if err != nil {
			return err
		}
		if message.ID, err = result.LastInsertId(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO news_story_messages (message_id, session_id, story_id, intent, story, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			message.ID, message.SessionID, in.Story.ID, string(in.Intent), string(story), formatTimestamp(now)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE session_id = ?`, formatTimestamp(now), message.SessionID)
		return err
	})
	if err != nil {
		return core.SessionMessageRecord{}, fmt.Errorf("send story message from story %d: %w", in.Story.ID, err)
	}
	return message, nil
}

// StoryMessages lists the owner messages sent from stories in a
// conversation, oldest first.
func (s *Store) StoryMessages(ctx context.Context, sessionID string) ([]core.StoryMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT message_id, session_id, intent, story, created_at FROM news_story_messages WHERE session_id = ? ORDER BY message_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("story messages: %w", err)
	}
	var messages []core.StoryMessage
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			m                      core.StoryMessage
			intent, story, created string
		)
		if err := scan(&m.MessageID, &m.SessionID, &intent, &story, &created); err != nil {
			return err
		}
		m.Intent = core.StoryIntent(intent)
		if err := json.Unmarshal([]byte(story), &m.Story); err != nil {
			return fmt.Errorf("news_story_messages.story of message %d: %w", m.MessageID, err)
		}
		var err error
		m.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "news_story_messages.created_at")
		messages = append(messages, m)
		return err
	}); err != nil {
		return nil, fmt.Errorf("story messages: %w", err)
	}
	return messages, nil
}
