package runtime

import (
	"encoding/json"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

// TestViewActionNoteSaysWhereTheMessageCameFrom checks that the model sees,
// beside the owner's words, the view, the component, the List items it was
// pressed in, the options chosen and what the action carried.
func TestViewActionNoteSaysWhereTheMessageCameFrom(t *testing.T) {
	view := core.ThreadView{ID: "view_1", Title: "读完之后"}
	pressed := core.ViewAction{ComponentID: "read_later", Scope: []int{1}, Name: "read_later", Context: json.RawMessage(`{"url":"https://example.com/a2ui"}`)}
	want := "[Sent from your view \"读完之后\" (view_1) through read_later: action read_later]\nList items: [1]\nContext: {\"url\":\"https://example.com/a2ui\"}"
	if got := viewActionNote(view, pressed); got != want {
		t.Fatalf("note = %q\nwant %q", got, want)
	}

	chosen := core.ViewAction{ComponentID: "pick", Name: "follow_up", Selected: []core.ViewOption{{ID: "summary", Label: "写一段摘要"}, {ID: "remind", Label: "周末提醒我再看"}}, Context: json.RawMessage(`{}`)}
	want = "[Sent from your view \"读完之后\" (view_1) through pick: action follow_up]\nChosen: summary (写一段摘要), remind (周末提醒我再看)"
	if got := viewActionNote(view, chosen); got != want {
		t.Fatalf("note = %q\nwant %q", got, want)
	}
}
