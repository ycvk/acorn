package runtime

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// pieceAsk tells the model how to write the piece about a story of today's
// focus. The App shows the reply as the story's reading page, so it is the
// piece alone, under headings the page is built around.
const pieceAsk = "[Written for the morning briefing: this story is one of today's focus, and your reply is the piece the owner reads on opening it. " +
	"Read the originals below and search for other reporting first, so the piece rests on at least two independent sources where they exist. " +
	"Then reply with the piece alone, in the owner's language, under these four headings in this order: `## 发生了什么` (the facts, with dates and numbers), " +
	"`## 背景` (what led here and what a reader needs to know to follow it), `## 各方说法` (who says what, each claim attributed to who made it, unverified claims marked as unverified), " +
	"`## 接下来看什么` (what is still open and what to watch for). End with `## 来源`, a list of the links you used with the name of each outlet. " +
	"No greeting, no title line, no closing question. When this conversation already holds an earlier piece, say what is new since it. " +
	"Use web_search, news_search and web_fetch only; the owner is not there to approve anything]"

// loadStoryMessageNotes tells the model, for each message sent from a story
// on the briefing card, what is asked and the story as it stood then. The
// message itself holds only what the owner sees.
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
	switch m.Intent {
	case core.StoryFollow:
		ask = "[Sent from a story on the briefing card: the owner asks you to keep following it. Create a concern without an outcome for it, citing this message as its source, with review_at at the start of tomorrow in the owner's time zone; then say where the story stands and what you will watch for. Idle thought checks it for developments]"
	case core.StoryPiece:
		ask = pieceAsk
	}
	return ask + "\n" + m.Story.Text()
}
