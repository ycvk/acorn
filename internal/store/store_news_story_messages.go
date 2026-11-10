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

// newsStoryMessagesTable is shared with the migrations that add and rebuild
// it. A row is an owner message sent from a story or the wake input of a
// piece about it. It keeps the story as it stood then, as JSON, so it
// outlives the story, which is pruned with its items; it leaves with its
// conversation. The newest row of a story names the story's conversation.
const newsStoryMessagesTable = `CREATE TABLE IF NOT EXISTS news_story_messages (
    message_id INTEGER PRIMARY KEY,
    session_id TEXT NOT NULL,
    story_id INTEGER NOT NULL,
    intent TEXT NOT NULL CHECK (intent IN ('discuss', 'follow', 'piece')),
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

// storyThread selects the newest conversation a message from the story
// went to; story is the SQL expression of the story's id.
func storyThread(story string) string {
	return `SELECT session_id FROM news_story_messages WHERE story_id = ` + story + ` ORDER BY message_id DESC LIMIT 1`
}

// SendStoryMessage records an owner message sent from a story in the
// story's conversation, creating it under in.Title when the story has none.
// A conversation whose latest run is running or waits for the owner is
// refused with ErrThreadBusy.
func (s *Store) SendStoryMessage(ctx context.Context, in core.StoryMessageWrite) (core.SessionMessageRecord, error) {
	if !in.Intent.Valid() {
		return core.SessionMessageRecord{}, fmt.Errorf("send story message: intent %q must be discuss or follow", in.Intent)
	}
	return s.writeStoryMessage(ctx, "user", in.Intent, in.Text, in.Title, in.Story, in.NewThreadID)
}

// WriteStoryPiece records the wake input of a piece in the story's
// conversation, creating it under in.Title when the story has none. A
// conversation whose latest run is running or waits for the owner is refused
// with ErrThreadBusy.
func (s *Store) WriteStoryPiece(ctx context.Context, in core.StoryPieceWrite) (core.SessionMessageRecord, error) {
	return s.writeStoryMessage(ctx, core.MessageRoleWake, core.StoryPiece, in.Text, in.Title, in.Story, in.NewThreadID)
}

func (s *Store) writeStoryMessage(ctx context.Context, role string, intent core.StoryIntent, text, title string, snapshot core.StorySnapshot, newThreadID string) (core.SessionMessageRecord, error) {
	switch {
	case strings.TrimSpace(text) == "" || strings.TrimSpace(title) == "":
		return core.SessionMessageRecord{}, errors.New("story message: text and title are required")
	case snapshot.ID <= 0 || strings.TrimSpace(snapshot.Title) == "":
		return core.SessionMessageRecord{}, errors.New("story message: the story's id and title are required")
	case strings.TrimSpace(newThreadID) == "":
		return core.SessionMessageRecord{}, errors.New("story message: NewThreadID is required")
	}
	story, err := json.Marshal(snapshot)
	if err != nil {
		return core.SessionMessageRecord{}, fmt.Errorf("story message: %w", err)
	}
	now := time.Now().UTC()
	message := core.SessionMessageRecord{Role: role, Content: strings.TrimSpace(text), CreatedAt: now}
	err = s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, storyThread("?"), snapshot.ID).Scan(&message.SessionID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			message.SessionID = newThreadID
			if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(session_id, title, created_at, updated_at) VALUES(?, ?, ?, ?)`,
				message.SessionID, strings.TrimSpace(title), formatTimestamp(now), formatTimestamp(now)); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			var status string
			err := tx.QueryRowContext(ctx, `SELECT status FROM runs r WHERE session_id = ? AND `+notSubRun+` ORDER BY turn_index DESC, finished_at DESC LIMIT 1`, message.SessionID).Scan(&status)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if status == string(core.RunStatusRunning) || status == string(core.RunStatusInterrupted) {
				return fmt.Errorf("%w: %s", core.ErrThreadBusy, message.SessionID)
			}
		}
		if err := appendUnboundMessage(ctx, tx, &message); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO news_story_messages (message_id, session_id, story_id, intent, story, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			message.ID, message.SessionID, snapshot.ID, string(intent), string(story), formatTimestamp(now))
		return err
	})
	if err != nil {
		return core.SessionMessageRecord{}, fmt.Errorf("%s message from story %d: %w", intent, snapshot.ID, err)
	}
	return message, nil
}

// StoryMessages lists the messages sent from stories in a conversation,
// oldest first.
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
		m.CreatedAt, err = parseTimestamp(created, "news_story_messages.created_at")
		messages = append(messages, m)
		return err
	}); err != nil {
		return nil, fmt.Errorf("story messages: %w", err)
	}
	return messages, nil
}
