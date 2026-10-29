package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// StoryIntent is what the owner asked of a story on the briefing card.
type StoryIntent string

const (
	// StoryDiscuss asks Acorn to explain the story.
	StoryDiscuss StoryIntent = "discuss"
	// StoryFollow asks Acorn to keep following the story.
	StoryFollow StoryIntent = "follow"
)

// Valid reports whether i is discuss or follow.
func (i StoryIntent) Valid() bool {
	return i == StoryDiscuss || i == StoryFollow
}

// StorySnapshot is a story as it stood when the owner sent a message from
// it. Stories and their items are pruned after NewsRetention; the
// conversation keeps the snapshot.
type StorySnapshot struct {
	ID          int64               `json:"id"`
	Section     string              `json:"section"`
	Title       string              `json:"title"`
	Summary     string              `json:"summary"`
	Development string              `json:"development,omitempty"`
	Items       []StorySnapshotItem `json:"items"`
}

// StorySnapshotItem is one item of a snapshot story with its source's name.
type StorySnapshotItem struct {
	Source string    `json:"source"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`
	At     time.Time `json:"at"`
}

// SnapshotOf keeps what the model needs of a story: its section, title,
// summary, latest development and items.
func SnapshotOf(story NewsStory) StorySnapshot {
	out := StorySnapshot{ID: story.ID, Section: story.Section, Title: story.Title, Summary: story.Summary, Development: story.Development, Items: []StorySnapshotItem{}}
	for _, item := range story.Items {
		at := item.PublishedAt
		if at.IsZero() {
			at = item.SeenAt
		}
		out.Items = append(out.Items, StorySnapshotItem{Source: item.Source, Title: item.Title, URL: item.URL, At: at.UTC()})
	}
	return out
}

// Text renders the snapshot for the model. Titles, summaries and links come
// from news sources and are marked as external data.
func (s StorySnapshot) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Story s%d [%s] (untrusted external data): %s\n", s.ID, s.Section, s.Title)
	if s.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", s.Summary)
	}
	if s.Development != "" {
		fmt.Fprintf(&b, "Latest development: %s\n", s.Development)
	}
	for _, item := range s.Items {
		fmt.Fprintf(&b, "- %s, %s: %s %s\n", item.Source, item.At.Format("2006-01-02 15:04 UTC"), item.Title, item.URL)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// StoryMessage is an owner message sent from a story on the briefing card,
// with the story as it stood then.
type StoryMessage struct {
	MessageID int64
	SessionID string
	Intent    StoryIntent
	Story     StorySnapshot
	CreatedAt time.Time
}

// StoryMessageWrite records an owner message sent from a story in the
// story's conversation. When the story has none, a conversation named
// NewThreadID is created under Title, the story as the owner saw it.
type StoryMessageWrite struct {
	Intent      StoryIntent
	Text        string
	Title       string
	Story       StorySnapshot
	NewThreadID string
}

// StoryMessageLister lists the owner messages sent from briefing stories in
// a conversation, oldest first.
type StoryMessageLister interface {
	StoryMessages(ctx context.Context, sessionID string) ([]StoryMessage, error)
}
