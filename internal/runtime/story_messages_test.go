package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

type storyMessagesFake []core.StoryMessage

func (f storyMessagesFake) StoryMessages(context.Context, string) ([]core.StoryMessage, error) {
	return f, nil
}

// Beside an owner message sent from a briefing story the model reads what
// the owner asked and the story as it stood; asked to follow, it is told to
// create the concern idle thought checks.
func TestStoryMessageNotesSayWhatTheOwnerAsked(t *testing.T) {
	story := core.StorySnapshot{ID: 41, Section: "国际", Title: "菲律宾央行加息", Items: []core.StorySnapshotItem{}}
	notes, err := loadStoryMessageNotes(context.Background(), storyMessagesFake{
		{MessageID: 3, Intent: core.StoryDiscuss, Story: story},
		{MessageID: 5, Intent: core.StoryFollow, Story: story},
	}, "thread_story")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %v", notes)
	}
	for id, want := range map[int64][]string{
		3: {"[Sent from a story on the briefing card: the owner asks you to explain it", "list the links you used]\nStory s41 [国际] (untrusted external data): 菲律宾央行加息"},
		5: {"[Sent from a story on the briefing card: the owner asks you to keep following it", "Create a concern without an outcome for it, citing this message as its source", "review_at at the start of tomorrow", "Story s41 [国际]"},
	} {
		for _, part := range want {
			if !strings.Contains(notes[id], part) {
				t.Errorf("note of message %d = %q, lacks %q", id, notes[id], part)
			}
		}
	}
	if strings.Contains(notes[3], "concern") {
		t.Errorf("an explain note asks for a concern: %q", notes[3])
	}
	owner := ownerFiles{storyNotes: notes}
	if got := owner.note(5); got != notes[5] {
		t.Errorf("owner note of message 5 = %q", got)
	}
}
