package views

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const nextStepsView = `[
  {"id": "root", "component": "Column", "children": ["note", "reads", "pick"]},
  {"id": "note", "component": "Button", "label": "整理成笔记", "action": {"name": "save_note", "context": {"url": {"path": "/article"}, "kind": "summary"}}},
  {"id": "reads", "component": "List", "items": {"path": "/related"}, "template": "read"},
  {"id": "read", "component": "Card", "children": ["read_title", "read_save"]},
  {"id": "read_title", "component": "Text", "text": {"path": "title"}},
  {"id": "read_save", "component": "Button", "label": "稍后读", "variant": "quiet", "action": {"name": "read_later", "context": {"url": {"path": "url"}}}},
  {"id": "pick", "component": "ChoicePicker", "label": "接下来做哪些？", "options": {"path": "/next"}, "multiple": true, "submit": "就这些", "action": {"name": "next_steps"}}
]`

const nextStepsData = `{
  "article": "https://example.com/long-read",
  "related": [{"title": "第一篇", "url": "https://example.com/1"}, {"title": "第二篇", "url": "https://example.com/2"}],
  "next": [{"id": "a", "label": "写摘要"}, {"id": "b", "label": "找原始论文"}, {"id": "c", "label": "设个提醒"}]
}`

func TestTheNextStepsViewPassesItsChecks(t *testing.T) {
	components, data := decode(t, nextStepsView, nextStepsData)
	if _, err := Check(context.Background(), "读完之后", components, data, newRefs()); err != nil {
		t.Fatal(err)
	}
}

func TestResolveActionSendsWhatTheViewOffers(t *testing.T) {
	components, data := json.RawMessage(nextStepsView), json.RawMessage(nextStepsData)

	button, err := ResolveAction(components, data, ActionRequest{ComponentID: "note"})
	if err != nil {
		t.Fatal(err)
	}
	if button.Key != "note" || button.Name != "save_note" || button.Text != "整理成笔记" ||
		!reflect.DeepEqual(button.Context, map[string]any{"url": "https://example.com/long-read", "kind": "summary"}) {
		t.Fatalf("button = %+v", button)
	}

	item, err := ResolveAction(components, data, ActionRequest{ComponentID: "read_save", Scope: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if item.Key != "read_save/1" || item.Text != "稍后读" || item.Context["url"] != "https://example.com/2" {
		t.Fatalf("list item button = %+v", item)
	}

	choice, err := ResolveAction(components, data, ActionRequest{ComponentID: "pick", Selected: []string{"c", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if choice.Key != "pick" || choice.Text != "选了：写摘要、设个提醒" || !reflect.DeepEqual(choice.Selected, []Option{{"a", "写摘要"}, {"c", "设个提醒"}}) {
		t.Fatalf("choice = %+v", choice)
	}
}

func TestResolveActionRejectsWhatTheViewDoesNotOffer(t *testing.T) {
	single := strings.Replace(nextStepsView, `"multiple": true`, `"multiple": false`, 1)
	cases := []struct {
		name, components string
		req              ActionRequest
		want             string
	}{
		{"unknown component", nextStepsView, ActionRequest{ComponentID: "delete_all"}, `no button or choice "delete_all"`},
		{"not a button", nextStepsView, ActionRequest{ComponentID: "read_title", Scope: []int{0}}, `no button or choice "read_title"`},
		{"missing item", nextStepsView, ActionRequest{ComponentID: "read_save"}, "drawn inside 1 lists, and the request names 0 items"},
		{"item out of range", nextStepsView, ActionRequest{ComponentID: "read_save", Scope: []int{2}}, `list "reads" has no item 2`},
		{"scope outside lists", nextStepsView, ActionRequest{ComponentID: "note", Scope: []int{0}}, "drawn inside 0 lists, and the request names 1 items"},
		{"button with options", nextStepsView, ActionRequest{ComponentID: "note", Selected: []string{"a"}}, "sends no options"},
		{"nothing chosen", nextStepsView, ActionRequest{ComponentID: "pick"}, "no option chosen"},
		{"two in a single choice", single, ActionRequest{ComponentID: "pick", Selected: []string{"a", "b"}}, "only one option"},
		{"forged option", nextStepsView, ActionRequest{ComponentID: "pick", Selected: []string{"a", "z"}}, "options z are not offered"},
		{"repeated option", nextStepsView, ActionRequest{ComponentID: "pick", Selected: []string{"a", "a"}}, `option "a" is chosen twice`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveAction(json.RawMessage(tc.components), json.RawMessage(nextStepsData), tc.req)
			if !errors.Is(err, ErrInvalidAction) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrInvalidAction mentioning %q", err, tc.want)
			}
		})
	}
}

func TestCheckRejectsActionsTheAppCannotSend(t *testing.T) {
	cases := []struct{ name, components, data, want string }{
		{"button without action", `[{"id": "root", "component": "Button", "label": "好"}]`, ``, "action is required"},
		{"action name", `[{"id": "root", "component": "Button", "label": "好", "action": {"name": "Save Note"}}]`, ``, "name must start with a lowercase letter"},
		{"action extra key", `[{"id": "root", "component": "Button", "label": "好", "action": {"name": "ok", "tool": "shell"}}]`, ``, `action has no property "tool"`},
		{"context not an object", `[{"id": "root", "component": "Button", "label": "好", "action": {"name": "ok", "context": "x"}}]`, ``, "context must be an object"},
		{"context path", `[{"id": "root", "component": "Button", "label": "好", "action": {"name": "ok", "context": {"u": {"path": "/gone"}}}}]`, `{}`, `action context u: path "/gone" finds nothing`},
		{"multiple not a bool", `[{"id": "root", "component": "ChoicePicker", "options": [{"id": "a", "label": "A"}], "multiple": "yes", "submit": "好", "action": {"name": "ok"}}]`, ``, "multiple must be true or false"},
		{"option shape", `[{"id": "root", "component": "ChoicePicker", "options": [{"id": "a"}], "submit": "好", "action": {"name": "ok"}}]`, ``, `option 0 must be {"id": string, "label": string}`},
		{"option repeats", `[{"id": "root", "component": "ChoicePicker", "options": [{"id": "a", "label": "A"}, {"id": "a", "label": "B"}], "submit": "好", "action": {"name": "ok"}}]`, ``, `option id "a" repeats`},
		{"no options", `[{"id": "root", "component": "ChoicePicker", "options": [], "submit": "好", "action": {"name": "ok"}}]`, ``, "must list 1 to 20 options"},
		{"no submit", `[{"id": "root", "component": "ChoicePicker", "options": [{"id": "a", "label": "A"}], "action": {"name": "ok"}}]`, ``, "submit is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			components, data := decode(t, tc.components, tc.data)
			_, err := Check(context.Background(), "view", components, data, newRefs())
			if !errors.Is(err, ErrInvalidView) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v\nwant it to mention %q", err, tc.want)
			}
		})
	}
}
