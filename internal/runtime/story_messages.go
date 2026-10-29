package runtime

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// loadStoryMessageNotes tells the model, for each owner message sent from a
// story on the briefing card, what the owner asked and the story as it stood
// then. The message itself holds only what the owner saw.
func loadStoryMessageNotes(ctx context.Context, stories core.StoryMessageLister, sessionID string) (map[int64]string, error) {
	messages, err := stories.StoryMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("story messages: %w", err)
	}
	notes := map[int64]string{}
	for _, m := range messages {
		notes[m.MessageID] = storyMessageNote(m)
	}
	return notes, nil
}

func storyMessageNote(m core.StoryMessage) string {
	ask := "[Sent from a story on the briefing card: the owner asks you to explain it — what happened and why it matters. Read the originals when the summary is not enough and list the links you used]"
	if m.Intent == core.StoryFollow {
		ask = "[Sent from a story on the briefing card: the owner asks you to keep following it. Create a concern without an outcome for it, citing this message as its source, with review_at at the start of tomorrow in the owner's time zone; then say where the story stands and what you will watch for. Idle thought checks it for developments]"
	}
	return ask + "\n" + m.Story.Text()
}
