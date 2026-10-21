package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrInvalidAction rejects an action the view does not offer: a component
// that is no button or choice, a List item that is not there, or options
// the choice does not list.
var ErrInvalidAction = errors.New("invalid view action")

// Option is one choice of a ChoicePicker.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ActionRequest is what the app sends when the owner presses a button or
// submits a choice.
type ActionRequest struct {
	ComponentID string
	// Scope holds the index of the item in each List the component is drawn
	// for, outermost first; empty outside Lists.
	Scope []int
	// Selected are the option ids the owner chose; a button sends none.
	Selected []string
}

// Action is a request checked against the view it came from, with what it
// sends resolved.
type Action struct {
	// Key names the action within its view; each key is sent once.
	Key         string
	ComponentID string
	Scope       []int
	Name        string
	// Text is the owner's message: the button's label or the chosen options.
	Text     string
	Selected []Option
	Context  map[string]any
}

// ResolveAction checks req against a stored view's components and data and
// resolves the action it sends. A request the view does not offer fails
// with ErrInvalidAction.
func ResolveAction(components, data json.RawMessage, req ActionRequest) (Action, error) {
	var decoded []map[string]any
	if err := json.Unmarshal(components, &decoded); err != nil {
		return Action{}, fmt.Errorf("stored view components: %w", err)
	}
	var values any
	if err := json.Unmarshal(data, &values); err != nil {
		return Action{}, fmt.Errorf("stored view data: %w", err)
	}
	c := &checker{ctx: context.Background(), nodes: map[string]*node{}, parent: map[string]string{}, data: values}
	c.checkStructure(decoded)
	if len(c.problems) > 0 {
		return Action{}, fmt.Errorf("stored view does not pass its checks: %w", c.err())
	}
	n := c.nodes[req.ComponentID]
	if n == nil || (n.kind != "Button" && n.kind != "ChoicePicker") {
		return Action{}, fmt.Errorf("%w: the view has no button or choice %q", ErrInvalidAction, req.ComponentID)
	}
	lists := c.enclosingLists(n.id)
	if len(req.Scope) != len(lists) {
		return Action{}, fmt.Errorf("%w: %q is drawn inside %d lists, and the request names %d items", ErrInvalidAction, n.id, len(lists), len(req.Scope))
	}
	var scopes []any
	for k, list := range lists {
		items, ok := c.resolveArray("list "+list.id, "items", list.props["items"], scopes)
		index := req.Scope[k]
		if !ok || index < 0 || index >= len(items) {
			return Action{}, fmt.Errorf("%w: list %q has no item %d", ErrInvalidAction, list.id, index)
		}
		scopes = append(scopes, items[index])
	}
	at := fmt.Sprintf("component %q (%s)", n.id, n.kind)
	action := Action{Key: actionKey(n.id, req.Scope), ComponentID: n.id, Scope: slices.Clone(req.Scope)}
	spec, ok := n.props["action"].(map[string]any)
	if !ok {
		return Action{}, fmt.Errorf("stored view: %s has no action", at)
	}
	if action.Name, ok = spec["name"].(string); !ok {
		return Action{}, fmt.Errorf("stored view: %s action has no name", at)
	}
	if action.Context, ok = c.resolveContext(at, "action", n.props["action"], scopes); !ok {
		return Action{}, fmt.Errorf("stored view: %s action context: %w", at, c.err())
	}
	switch n.kind {
	case "Button":
		if len(req.Selected) > 0 {
			return Action{}, fmt.Errorf("%w: button %q sends no options", ErrInvalidAction, n.id)
		}
		if action.Text, ok = c.resolveText(at, "label", n.props["label"], scopes); !ok {
			return Action{}, fmt.Errorf("stored view: %w", c.err())
		}
	case "ChoicePicker":
		options, ok := c.resolveOptions(at, "options", n.props["options"], scopes)
		if !ok {
			return Action{}, fmt.Errorf("stored view: %w", c.err())
		}
		// An absent multiple means a single choice.
		selected, err := chosen(options, req.Selected, n.props["multiple"] == true)
		if err != nil {
			return Action{}, fmt.Errorf("%w: choice %q: %v", ErrInvalidAction, n.id, err)
		}
		action.Selected = selected
		labels := make([]string, len(selected))
		for i, option := range selected {
			labels[i] = option.Label
		}
		action.Text = "选了：" + strings.Join(labels, "、")
	}
	if utf8.RuneCountInString(action.Text) > MaxActionRunes {
		return Action{}, fmt.Errorf("%w: the message would be longer than %d characters", ErrInvalidAction, MaxActionRunes)
	}
	return action, nil
}

// chosen returns the options ids name, in the order the choice lists them.
func chosen(options []Option, ids []string, multiple bool) ([]Option, error) {
	switch {
	case len(ids) == 0:
		return nil, errors.New("no option chosen")
	case !multiple && len(ids) > 1:
		return nil, errors.New("only one option can be chosen")
	}
	want := map[string]bool{}
	for _, id := range ids {
		if want[id] {
			return nil, fmt.Errorf("option %q is chosen twice", id)
		}
		want[id] = true
	}
	var selected []Option
	for _, option := range options {
		if want[option.ID] {
			selected = append(selected, option)
			delete(want, option.ID)
		}
	}
	if len(want) > 0 {
		return nil, fmt.Errorf("options %s are not offered", strings.Join(sortedKeys(want), ", "))
	}
	return selected, nil
}

// enclosingLists returns the Lists whose template id is drawn inside,
// outermost first.
func (c *checker) enclosingLists(id string) []*node {
	var lists []*node
	for {
		parent, ok := c.parent[id]
		if !ok {
			return lists
		}
		if p := c.nodes[parent]; p.template == id {
			lists = append([]*node{p}, lists...)
		}
		id = parent
	}
}

// actionKey names a component's action for one List item: the component id,
// then the item indices.
func actionKey(componentID string, scope []int) string {
	key := componentID
	for _, index := range scope {
		key += "/" + strconv.Itoa(index)
	}
	return key
}

// resolveOptions resolves a ChoicePicker's options and checks each is a
// distinct {id, label}.
func (c *checker) resolveOptions(at, name string, value any, scopes []any) ([]Option, bool) {
	items, ok := c.resolveArray(at, name, value, scopes)
	if !ok {
		return nil, false
	}
	if len(items) == 0 || len(items) > maxOptions {
		c.add(fmt.Sprintf("%s: %s must list 1 to %d options", at, name, maxOptions))
		return nil, false
	}
	options := make([]Option, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		option, isObject := item.(map[string]any)
		id, idOK := option["id"].(string)
		label, labelOK := option["label"].(string)
		if !isObject || len(option) != 2 || !idOK || strings.TrimSpace(id) == "" || !labelOK || strings.TrimSpace(label) == "" {
			c.add(fmt.Sprintf(`%s: %s: option %d must be {"id": string, "label": string}`, at, name, i))
			return nil, false
		}
		if seen[id] {
			c.add(fmt.Sprintf("%s: %s: option id %q repeats", at, name, id))
			return nil, false
		}
		seen[id] = true
		options = append(options, Option{ID: id, Label: label})
	}
	return options, true
}

// resolveContext resolves the context an action sends against the data.
func (c *checker) resolveContext(at, name string, value any, scopes []any) (map[string]any, bool) {
	action, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	resolved := map[string]any{}
	raw, present := action["context"]
	if !present {
		return resolved, true
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, key := range sortedKeys(values) {
		found, ok := c.resolve(at, name+" context "+key, values[key], scopes)
		if !ok {
			return nil, false
		}
		resolved[key] = found
	}
	return resolved, true
}
