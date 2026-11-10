package runtime

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

// pieceAsk tells the model how to write the piece about a story of today's
// focus. The App shows the reply as the story's reading page, so it is the
// piece alone, under headings the page is built around.
const pieceAsk = "[Written for the morning briefing: this story is one of today's focus, and your reply is the piece the owner reads on opening it.\n" +
	"Before writing, read the originals below with web_fetch and look for other reporting with web_search or news_search, so the piece rests on at least two independent outlets where they exist. Use only those tools; the owner is not there to approve anything.\n" +
	"Reply with the piece alone, in the owner's language. It has exactly these five headings, in this order, and no others:\n" +
	"`## 发生了什么`: the facts, with dates and numbers.\n" +
	"`## 背景`: what led here and what a reader needs to know to follow it.\n" +
	"`## 各方说法`: who says what, each claim attributed to who made it, unverified claims marked as unverified.\n" +
	"`## 接下来看什么`: what is still open and what to watch for.\n" +
	"`## 来源`: one line for each page you read, as a Markdown link `- [outlet: headline](address)` with the address copied exactly.\n" +
	"The first four sections are about 1,200 characters together and never past 1,800: the owner reads them on a phone in about three minutes, so choose what matters most. The sources do not count toward that.\n" +
	"No greeting, no title line, no text before the first heading and no closing question.\n" +
	"The owner reads the piece on a page of its own, without this conversation, so it stands alone: never refer to the conversation or to anything written before. " +
	"When the conversation already holds an earlier explanation or piece, lead 发生了什么 with what has changed since, then give the rest of the facts as for a new reader]"

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
