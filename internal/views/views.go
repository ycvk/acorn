// Package views checks the views the agent shows the owner against Acorn's
// component catalog. A view is a flat list of components in the shape of
// A2UI v0.9: each has an id, containers list their children's ids, one is
// root, and values are either written out or {"path": ...} into the view's
// data. The app draws the catalog's components with its own design system.
package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Catalog is the catalog version views are written in and checked against.
const Catalog = "acorn.views.v1"

// Limits on one view.
const (
	MaxComponents  = 200
	MaxBytes       = 64 << 10
	MaxTitleRunes  = 80
	MaxTextRunes   = 2000
	MaxColumns     = 8
	MaxItems       = 100
	maxCellRunes   = 500
	maxProblems    = 20
	maxColumnRunes = 40
	// maxDrawn caps how many components a view draws in all, counting the
	// copy of a List's template for each item.
	maxDrawn = 5000
)

// ErrInvalidView wraps the problems of a view that failed its checks.
var ErrInvalidView = errors.New("the view was not shown")

// References checks what a view points to outside itself.
type References interface {
	// Link returns why the owner should not be sent to url, or nil.
	Link(ctx context.Context, url string) error
	// Image returns why ref, a file id of this conversation or a knowledge
	// attachment path, cannot be shown, or nil.
	Image(ctx context.Context, ref string) error
}

// Checked is a view that passed every check, as compact JSON ready to store.
type Checked struct {
	Title      string
	Components json.RawMessage
	Data       json.RawMessage
}

type propKind int

const (
	propChildren propKind = iota // ids of components drawn inside, in order
	propTemplate                 // id of the component a List draws per item
	propText                     // text, or a path to a string or number
	propEnum                     // one of a fixed set of strings
	propColumns                  // column headings, written out
	propRows                     // rows of cells, written out or a path
	propItems                    // a path to the array a List repeats over
	propPoints                   // chart points {label, value}, written out or a path
	propURL                      // a web address, or a path to one
	propImage                    // a file id or attachment path, or a path to one
	propAction                   // what pressing a button or choosing sends: {name, context}
	propOptions                  // choices {id, label}, written out or a path
	propBool                     // true or false, written out
)

type propSpec struct {
	kind     propKind
	required bool
	values   []string
}

// catalog lists each component's properties besides id and component.
var catalog = map[string]map[string]propSpec{
	"Column":  {"children": {kind: propChildren, required: true}},
	"Row":     {"children": {kind: propChildren, required: true}},
	"Card":    {"children": {kind: propChildren, required: true}},
	"Divider": {},
	"Text": {
		"text":    {kind: propText, required: true},
		"variant": {kind: propEnum, values: []string{"body", "title", "caption"}},
	},
	"Badge": {
		"text": {kind: propText, required: true},
		"tone": {kind: propEnum, values: []string{"neutral", "accent", "success", "warning", "danger"}},
	},
	"Link": {
		"label": {kind: propText, required: true},
		"url":   {kind: propURL, required: true},
	},
	"Image": {
		"file": {kind: propImage, required: true},
		"alt":  {kind: propText},
	},
	"Table": {
		"columns": {kind: propColumns, required: true},
		"rows":    {kind: propRows, required: true},
	},
	"List": {
		"items":    {kind: propItems, required: true},
		"template": {kind: propTemplate, required: true},
	},
	"Chart": {
		"variant": {kind: propEnum, required: true, values: []string{"line", "bar"}},
		"points":  {kind: propPoints, required: true},
		"unit":    {kind: propText},
	},
	"Button": {
		"label":   {kind: propText, required: true},
		"action":  {kind: propAction, required: true},
		"variant": {kind: propEnum, values: []string{"primary", "quiet"}},
	},
	"ChoicePicker": {
		"label":    {kind: propText},
		"options":  {kind: propOptions, required: true},
		"multiple": {kind: propBool},
		"submit":   {kind: propText, required: true},
		"action":   {kind: propAction, required: true},
	},
}

// Limits on what an action carries.
const (
	maxOptions = 20
	// MaxActionRunes caps the text an action sends as the owner's message.
	MaxActionRunes = 500
)

var actionName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var componentID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// markdownLink finds the targets of inline Markdown links in a Text, and
// bareLink the addresses the app's Markdown turns into links by themselves.
var (
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	bareLink     = regexp.MustCompile(`(?:https?://|www\.)[^\s<>()\[\]]+`)
)

// textLinks lists the addresses a Text links to.
func textLinks(text string) []string {
	var links []string
	for _, m := range markdownLink.FindAllStringSubmatch(text, -1) {
		links = append(links, m[1])
	}
	for _, link := range bareLink.FindAllString(markdownLink.ReplaceAllString(text, "]"), -1) {
		link = strings.TrimRight(link, `.,;:!?'"`)
		if strings.HasPrefix(link, "www.") {
			link = "http://" + link
		}
		links = append(links, link)
	}
	return links
}

// Check validates a view against the catalog and its own data and returns it
// ready to store. A view that fails returns an error wrapping
// ErrInvalidView that lists what to fix.
func Check(ctx context.Context, title string, components []map[string]any, data map[string]any, refs References) (Checked, error) {
	if refs == nil {
		return Checked{}, errors.New("views.Check needs References")
	}
	c := &checker{ctx: ctx, refs: refs, nodes: map[string]*node{}, parent: map[string]string{}, links: map[string]error{}, images: map[string]error{}}
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		c.add("title is required")
	case utf8.RuneCountInString(title) > MaxTitleRunes:
		c.add(fmt.Sprintf("title is longer than %d characters", MaxTitleRunes))
	case strings.ContainsAny(title, "\r\n"):
		c.add("title must be one line")
	}
	if data == nil {
		data = map[string]any{}
	}
	componentsJSON, err := json.Marshal(components)
	if err != nil {
		return Checked{}, fmt.Errorf("encode view components: %w", err)
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return Checked{}, fmt.Errorf("encode view data: %w", err)
	}
	if size := len(title) + len(componentsJSON) + len(dataJSON); size > MaxBytes {
		c.add(fmt.Sprintf("the view is %d bytes; at most %d fit, so show fewer components or less data", size, MaxBytes))
		return Checked{}, c.err()
	}
	switch {
	case len(components) == 0:
		c.add("components is empty; a view needs at least a root component")
	case len(components) > MaxComponents:
		c.add(fmt.Sprintf("%d components; at most %d fit in one view", len(components), MaxComponents))
	default:
		c.checkStructure(components)
		if len(c.problems) == 0 {
			c.data = data
			c.walk("root", nil, "")
		}
	}
	if len(c.problems) > 0 {
		return Checked{}, c.err()
	}
	return Checked{Title: title, Components: componentsJSON, Data: dataJSON}, nil
}

// node is a component whose id and type passed, with what checkStructure
// read from it.
type node struct {
	id       string
	kind     string
	props    map[string]any
	children []string
	template string
	columns  int
}

type checker struct {
	ctx      context.Context
	refs     References
	data     any
	nodes    map[string]*node
	order    []*node
	parent   map[string]string
	problems []string
	drawn    int
	links    map[string]error
	images   map[string]error
}

func (c *checker) add(problem string) {
	if len(c.problems) < maxProblems {
		c.problems = append(c.problems, problem)
	}
}

func (c *checker) err() error {
	more := ""
	if len(c.problems) == maxProblems {
		more = "\n- (more problems may follow)"
	}
	return fmt.Errorf("%w; fix these and call show_view again:\n- %s%s", ErrInvalidView, strings.Join(c.problems, "\n- "), more)
}

// checkStructure checks ids, component types, properties that do not depend
// on data, and that the components form one tree under root.
func (c *checker) checkStructure(components []map[string]any) {
	for i, component := range components {
		id, ok := component["id"].(string)
		if !ok || !componentID.MatchString(id) {
			c.add(fmt.Sprintf("components[%d]: id must be 1 to 64 letters, digits, _ or -", i))
			continue
		}
		if _, taken := c.nodes[id]; taken {
			c.add(fmt.Sprintf("components[%d]: id %q is used twice", i, id))
			continue
		}
		n := &node{id: id, props: component}
		c.nodes[id] = n
		c.order = append(c.order, n)
	}
	for _, n := range c.order {
		c.checkProps(n)
	}
	if _, ok := c.nodes["root"]; !ok {
		c.add(`no component has id "root"`)
		return
	}
	if parent, ok := c.parent["root"]; ok {
		c.add(fmt.Sprintf(`component "root" is inside %q; root must be the outermost component`, parent))
	}
	reached := map[string]bool{}
	var reach func(id string)
	reach = func(id string) {
		n, ok := c.nodes[id]
		if !ok || reached[id] {
			return
		}
		reached[id] = true
		for _, child := range n.children {
			reach(child)
		}
		if n.template != "" {
			reach(n.template)
		}
	}
	reach("root")
	for _, n := range c.order {
		if !reached[n.id] {
			c.add(fmt.Sprintf("component %q is not inside root; put it in a container's children or remove it", n.id))
		}
	}
}

func (c *checker) checkProps(n *node) {
	kind, isString := n.props["component"].(string)
	spec, known := catalog[kind]
	if !isString || !known {
		c.add(fmt.Sprintf("component %q: %q is not in the catalog (%s)", n.id, kind, strings.Join(sortedKeys(catalog), ", ")))
		return
	}
	n.kind = kind
	at := fmt.Sprintf("component %q (%s)", n.id, kind)
	for _, name := range sortedKeys(n.props) {
		if name == "id" || name == "component" {
			continue
		}
		if _, ok := spec[name]; !ok {
			c.add(fmt.Sprintf("%s: %s has no property %q", at, kind, name))
		}
	}
	for _, name := range sortedKeys(spec) {
		prop := spec[name]
		value, present := n.props[name]
		if !present {
			if prop.required {
				c.add(fmt.Sprintf("%s: %s is required", at, name))
			}
			continue
		}
		c.checkStatic(at, n, name, prop, value)
	}
}

// checkStatic checks a property's shape before any data is looked up.
func (c *checker) checkStatic(at string, n *node, name string, prop propSpec, value any) {
	switch prop.kind {
	case propChildren:
		children, ok := value.([]any)
		if !ok || len(children) == 0 {
			c.add(fmt.Sprintf("%s: %s must list at least one component id", at, name))
			return
		}
		for _, child := range children {
			if id, ok := c.place(at, n.id, child); ok {
				n.children = append(n.children, id)
			}
		}
	case propTemplate:
		if id, ok := c.place(at, n.id, value); ok {
			n.template = id
		}
	case propEnum:
		if s, ok := value.(string); !ok || !slices.Contains(prop.values, s) {
			c.add(fmt.Sprintf("%s: %s must be one of %s", at, name, strings.Join(prop.values, ", ")))
		}
	case propColumns:
		columns, ok := value.([]any)
		if !ok || len(columns) == 0 || len(columns) > MaxColumns {
			c.add(fmt.Sprintf("%s: %s must list 1 to %d headings", at, name, MaxColumns))
			return
		}
		for _, column := range columns {
			if s, ok := column.(string); !ok || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > maxColumnRunes {
				c.add(fmt.Sprintf("%s: %s must be non-empty strings of at most %d characters", at, name, maxColumnRunes))
				return
			}
		}
		n.columns = len(columns)
	case propItems:
		if _, ok := bindingPath(value); !ok {
			c.add(fmt.Sprintf(`%s: %s must be {"path": ...} pointing at an array in data`, at, name))
		}
	case propBool:
		if _, ok := value.(bool); !ok {
			c.add(fmt.Sprintf("%s: %s must be true or false", at, name))
		}
	case propAction:
		c.checkAction(at, name, value)
	default:
		if path, ok := bindingPath(value); ok {
			if !validPath(path) {
				c.add(fmt.Sprintf("%s: %s: path %q is not a JSON Pointer", at, name, path))
			}
		} else if obj, isObject := value.(map[string]any); isObject {
			c.add(fmt.Sprintf(`%s: %s: an object here must be exactly {"path": "..."}, got keys %s`, at, name, strings.Join(sortedKeys(obj), ", ")))
		}
	}
}

// checkAction checks an action's shape: a name the agent recognises when it
// comes back, and context values written out or bound to the data.
func (c *checker) checkAction(at, name string, value any) {
	action, ok := value.(map[string]any)
	if !ok {
		c.add(fmt.Sprintf(`%s: %s must be {"name": "...", "context": {...}}`, at, name))
		return
	}
	for _, key := range sortedKeys(action) {
		if key != "name" && key != "context" {
			c.add(fmt.Sprintf("%s: %s has no property %q; it takes name and context", at, name, key))
		}
	}
	if actionNameText, isString := action["name"].(string); !isString || !actionName.MatchString(actionNameText) {
		c.add(fmt.Sprintf("%s: %s name must start with a lowercase letter and hold only lowercase letters, digits and _", at, name))
	}
	raw, present := action["context"]
	if !present {
		return
	}
	context, isObject := raw.(map[string]any)
	if !isObject {
		c.add(fmt.Sprintf("%s: %s context must be an object", at, name))
		return
	}
	for _, key := range sortedKeys(context) {
		if path, bound := bindingPath(context[key]); bound && !validPath(path) {
			c.add(fmt.Sprintf("%s: %s context %s: path %q is not a JSON Pointer", at, name, key, path))
		}
	}
}

// place records that child is drawn inside parent and returns its id.
func (c *checker) place(at, parent string, child any) (string, bool) {
	id, ok := child.(string)
	switch {
	case !ok:
		c.add(fmt.Sprintf("%s: children and template are component ids", at))
	case c.nodes[id] == nil:
		c.add(fmt.Sprintf("%s: no component has id %q", at, id))
	case id == parent:
		c.add(fmt.Sprintf("%s: a component cannot contain itself", at))
	default:
		if other, taken := c.parent[id]; taken {
			c.add(fmt.Sprintf("%s: component %q is already inside %q; each component has one place", at, id, other))
			return "", false
		}
		c.parent[id] = parent
		return id, true
	}
	return "", false
}

// walk draws the tree from id the way the app will, checking every value
// against the data. scopes are the items of the Lists id is drawn inside,
// innermost last; where says which item that is.
func (c *checker) walk(id string, scopes []any, where string) {
	if len(c.problems) >= maxProblems || c.drawn > maxDrawn {
		return
	}
	if c.drawn++; c.drawn > maxDrawn {
		c.add(fmt.Sprintf("the view draws more than %d components once its Lists repeat; show fewer items", maxDrawn))
		return
	}
	n := c.nodes[id]
	at := fmt.Sprintf("component %q (%s)%s", n.id, n.kind, where)
	spec := catalog[n.kind]
	for _, name := range sortedKeys(spec) {
		value, present := n.props[name]
		if !present {
			continue
		}
		switch spec[name].kind {
		case propChildren:
			for _, child := range n.children {
				c.walk(child, scopes, where)
			}
		case propText:
			if text, ok := c.resolveText(at, name, value, scopes); ok {
				for _, link := range textLinks(text) {
					c.checkLink(at, name, link)
				}
			}
		case propURL:
			if url, ok := c.resolveString(at, name, value, scopes); ok {
				c.checkLink(at, name, url)
			}
		case propImage:
			if ref, ok := c.resolveString(at, name, value, scopes); ok {
				c.checkImage(at, name, ref)
			}
		case propRows:
			c.checkRows(at, name, value, n.columns, scopes)
		case propPoints:
			c.checkPoints(at, name, value, scopes)
		case propOptions:
			c.resolveOptions(at, name, value, scopes)
		case propAction:
			c.resolveContext(at, name, value, scopes)
		case propItems:
			items, ok := c.resolveArray(at, name, value, scopes)
			if !ok {
				continue
			}
			for i, item := range items {
				c.walk(n.template, append(slices.Clone(scopes), item), fmt.Sprintf(" in item %d of list %q", i, n.id))
			}
		}
	}
}

func (c *checker) resolve(at, name string, value any, scopes []any) (any, bool) {
	path, bound := bindingPath(value)
	if !bound {
		return value, true
	}
	root := c.data
	pointer := path
	if !strings.HasPrefix(path, "/") {
		if len(scopes) == 0 {
			c.add(fmt.Sprintf(`%s: %s: path %q is relative, which only works inside a List's template; start it with "/"`, at, name, path))
			return nil, false
		}
		root = scopes[len(scopes)-1]
		if path != "" {
			pointer = "/" + path
		}
	}
	found, ok := lookup(root, pointer)
	if !ok {
		c.add(fmt.Sprintf("%s: %s: path %q finds nothing in data", at, name, path))
		return nil, false
	}
	return found, true
}

func (c *checker) resolveText(at, name string, value any, scopes []any) (string, bool) {
	found, ok := c.resolve(at, name, value, scopes)
	if !ok {
		return "", false
	}
	var text string
	switch v := found.(type) {
	case string:
		text = v
	case float64:
		text = strconv.FormatFloat(v, 'f', -1, 64)
	default:
		c.add(fmt.Sprintf("%s: %s must be a string or number, got %s", at, name, typeName(found)))
		return "", false
	}
	if utf8.RuneCountInString(text) > MaxTextRunes {
		c.add(fmt.Sprintf("%s: %s is longer than %d characters", at, name, MaxTextRunes))
		return "", false
	}
	return text, true
}

func (c *checker) resolveString(at, name string, value any, scopes []any) (string, bool) {
	found, ok := c.resolve(at, name, value, scopes)
	if !ok {
		return "", false
	}
	s, ok := found.(string)
	if !ok || strings.TrimSpace(s) == "" {
		c.add(fmt.Sprintf("%s: %s must be a non-empty string, got %s", at, name, typeName(found)))
		return "", false
	}
	return s, true
}

func (c *checker) resolveArray(at, name string, value any, scopes []any) ([]any, bool) {
	found, ok := c.resolve(at, name, value, scopes)
	if !ok {
		return nil, false
	}
	items, ok := found.([]any)
	switch {
	case !ok:
		c.add(fmt.Sprintf("%s: %s must be an array, got %s", at, name, typeName(found)))
		return nil, false
	case len(items) > MaxItems:
		c.add(fmt.Sprintf("%s: %s has %d entries; at most %d fit", at, name, len(items), MaxItems))
		return nil, false
	}
	return items, true
}

func (c *checker) checkRows(at, name string, value any, columns int, scopes []any) {
	rows, ok := c.resolveArray(at, name, value, scopes)
	if !ok {
		return
	}
	for i, row := range rows {
		cells, ok := row.([]any)
		if !ok || len(cells) != columns {
			c.add(fmt.Sprintf("%s: %s: row %d must be an array of %d cells, one per column", at, name, i, columns))
			continue
		}
		for j, cell := range cells {
			switch v := cell.(type) {
			case nil, float64:
			case string:
				if utf8.RuneCountInString(v) > maxCellRunes {
					c.add(fmt.Sprintf("%s: %s: row %d cell %d is longer than %d characters", at, name, i, j, maxCellRunes))
				}
			default:
				c.add(fmt.Sprintf("%s: %s: row %d cell %d must be a string, number or null, got %s", at, name, i, j, typeName(cell)))
			}
		}
	}
}

func (c *checker) checkPoints(at, name string, value any, scopes []any) {
	points, ok := c.resolveArray(at, name, value, scopes)
	if !ok {
		return
	}
	if len(points) == 0 {
		c.add(fmt.Sprintf("%s: %s is empty; a chart needs at least one point", at, name))
	}
	for i, point := range points {
		if !isPoint(point) {
			c.add(fmt.Sprintf(`%s: %s: point %d must be {"label": string, "value": number}`, at, name, i))
		}
	}
}

func isPoint(value any) bool {
	point, ok := value.(map[string]any)
	if !ok || len(point) != 2 {
		return false
	}
	label, labelOK := point["label"].(string)
	_, valueOK := point["value"].(float64)
	return labelOK && strings.TrimSpace(label) != "" && valueOK
}

func (c *checker) checkLink(at, name, url string) {
	err, seen := c.links[url]
	if !seen {
		err = c.refs.Link(c.ctx, url)
		c.links[url] = err
	}
	if err != nil {
		c.add(fmt.Sprintf("%s: %s: link %q cannot be shown: %v", at, name, url, err))
	}
}

func (c *checker) checkImage(at, name, ref string) {
	err, seen := c.images[ref]
	if !seen {
		err = c.refs.Image(c.ctx, ref)
		c.images[ref] = err
	}
	if err != nil {
		c.add(fmt.Sprintf("%s: %s: %v", at, name, err))
	}
}

// bindingPath returns the path of a {"path": "..."} value.
func bindingPath(value any) (string, bool) {
	obj, ok := value.(map[string]any)
	if !ok || len(obj) != 1 {
		return "", false
	}
	path, ok := obj["path"].(string)
	return path, ok
}

// validPath reports whether path is an absolute JSON Pointer or a relative
// one (the same without its leading slash, or empty for the item itself).
func validPath(path string) bool {
	for _, token := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		for i := 0; i < len(token); i++ {
			if token[i] == '~' && (i+1 == len(token) || (token[i+1] != '0' && token[i+1] != '1')) {
				return false
			}
		}
	}
	return true
}

// lookup follows an RFC 6901 JSON Pointer through decoded JSON.
func lookup(root any, pointer string) (any, bool) {
	if pointer == "" {
		return root, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := root
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) || (len(token) > 1 && token[0] == '0') {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func typeName(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	case []any:
		return "an array"
	case map[string]any:
		return "an object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
