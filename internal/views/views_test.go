package views

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRefs struct {
	images    map[string]bool
	linkCalls map[string]int
}

func (f *fakeRefs) Link(_ context.Context, url string) error {
	f.linkCalls[url]++
	if !strings.HasPrefix(url, "https://") {
		return errors.New("only https addresses")
	}
	return nil
}

func (f *fakeRefs) Image(_ context.Context, ref string) error {
	if !f.images[ref] {
		return fmt.Errorf("%s is not a file of this conversation", ref)
	}
	return nil
}

func newRefs() *fakeRefs {
	return &fakeRefs{images: map[string]bool{"file_0123456789abcdef0123456789abcdef": true}, linkCalls: map[string]int{}}
}

func decode(t *testing.T, components, data string) ([]map[string]any, map[string]any) {
	t.Helper()
	var c []map[string]any
	if err := json.Unmarshal([]byte(components), &c); err != nil {
		t.Fatalf("components: %v", err)
	}
	var d map[string]any
	if data != "" {
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			t.Fatalf("data: %v", err)
		}
	}
	return c, d
}

const releasesView = `[
  {"id": "root", "component": "Column", "children": ["head", "table", "books", "trend", "cover"]},
  {"id": "head", "component": "Text", "variant": "title", "text": "五个版本的变化，见 [发布页](https://github.com/cloudwego/eino/releases)"},
  {"id": "table", "component": "Table", "columns": ["版本", "要点"], "rows": {"path": "/releases"}},
  {"id": "books", "component": "List", "items": {"path": "/books"}, "template": "book"},
  {"id": "book", "component": "Card", "children": ["book_title", "book_link"]},
  {"id": "book_title", "component": "Text", "text": {"path": "title"}},
  {"id": "book_link", "component": "Link", "label": "打开", "url": {"path": "url"}},
  {"id": "trend", "component": "Chart", "variant": "bar", "points": {"path": "/stars"}, "unit": "stars"},
  {"id": "cover", "component": "Image", "file": "file_0123456789abcdef0123456789abcdef", "alt": "封面"}
]`

const releasesData = `{
  "releases": [["v0.9.21", "summarization 可以只清理不压缩"], ["v0.9.20", null]],
  "books": [{"title": "小重山", "url": "https://www.jjwxc.net/onebook.php?novelid=8364451"}, {"title": 42, "url": "https://example.com/b"}],
  "stars": [{"label": "9 月", "value": 120}, {"label": "10 月", "value": 180.5}]
}`

func TestCheckAcceptsAViewThatDrawsCompletely(t *testing.T) {
	components, data := decode(t, releasesView, releasesData)
	refs := newRefs()
	checked, err := Check(context.Background(), "  eino v0.9.17 → v0.9.21 ", components, data, refs)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checked.Title != "eino v0.9.17 → v0.9.21" {
		t.Fatalf("title = %q", checked.Title)
	}
	var roundTrip []map[string]any
	if err := json.Unmarshal(checked.Components, &roundTrip); err != nil || len(roundTrip) != 9 {
		t.Fatalf("components = %s (%v)", checked.Components, err)
	}
	if !json.Valid(checked.Data) || !strings.Contains(string(checked.Data), "小重山") {
		t.Fatalf("data = %s", checked.Data)
	}
	if got := refs.linkCalls["https://github.com/cloudwego/eino/releases"]; got != 1 {
		t.Fatalf("the Markdown link in a Text was checked %d times, want 1", got)
	}
}

func TestCheckWithoutDataUsesAnEmptyObject(t *testing.T) {
	components, _ := decode(t, `[{"id": "root", "component": "Text", "text": "hi"}]`, "")
	checked, err := Check(context.Background(), "t", components, nil, newRefs())
	if err != nil || string(checked.Data) != "{}" {
		t.Fatalf("data = %s, err = %v", checked.Data, err)
	}
}

func TestCheckRejectsWithAProblemTheAgentCanFix(t *testing.T) {
	cases := []struct {
		name, components, data, want string
	}{
		{"unknown component", `[{"id": "root", "component": "Slider"}]`, ``, `"Slider" is not in the catalog`},
		{"unknown property", `[{"id": "root", "component": "Text", "text": "a", "color": "red"}]`, ``, `Text has no property "color"`},
		{"missing property", `[{"id": "root", "component": "Table", "columns": ["a"]}]`, ``, `rows is required`},
		{"enum", `[{"id": "root", "component": "Text", "text": "a", "variant": "huge"}]`, ``, `variant must be one of body, title, caption`},
		{"duplicate id", `[{"id": "root", "component": "Column", "children": ["a"]}, {"id": "a", "component": "Divider"}, {"id": "a", "component": "Divider"}]`, ``, `id "a" is used twice`},
		{"bad id", `[{"id": "root", "component": "Column", "children": ["a"]}, {"id": "a b", "component": "Divider"}]`, ``, `components[1]: id must be`},
		{"no root", `[{"id": "top", "component": "Divider"}]`, ``, `no component has id "root"`},
		{"missing child", `[{"id": "root", "component": "Column", "children": ["gone"]}]`, ``, `no component has id "gone"`},
		{"two parents", `[{"id": "root", "component": "Column", "children": ["a", "b"]}, {"id": "a", "component": "Row", "children": ["c"]}, {"id": "b", "component": "Row", "children": ["c"]}, {"id": "c", "component": "Divider"}]`, ``, `component "c" is already inside "a"`},
		{"root inside", `[{"id": "root", "component": "Column", "children": ["a"]}, {"id": "a", "component": "Card", "children": ["root"]}]`, ``, `component "root" is inside "a"`},
		{"self", `[{"id": "root", "component": "Column", "children": ["root"]}]`, ``, `cannot contain itself`},
		{"unreachable", `[{"id": "root", "component": "Divider"}, {"id": "lost", "component": "Divider"}]`, ``, `component "lost" is not inside root`},
		{"relative path outside a list", `[{"id": "root", "component": "Text", "text": {"path": "name"}}]`, `{"name": "x"}`, `path "name" is relative`},
		{"path finds nothing", `[{"id": "root", "component": "Text", "text": {"path": "/missing"}}]`, `{}`, `path "/missing" finds nothing`},
		{"bad pointer", `[{"id": "root", "component": "Text", "text": {"path": "/a~2"}}]`, `{}`, `is not a JSON Pointer`},
		{"object that is not a binding", `[{"id": "root", "component": "Text", "text": {"path": "/a", "default": "x"}}]`, `{"a": "x"}`, `must be exactly {"path": "..."}`},
		{"text type", `[{"id": "root", "component": "Text", "text": {"path": "/a"}}]`, `{"a": [1]}`, `text must be a string or number, got an array`},
		{"text length", `[{"id": "root", "component": "Text", "text": "` + strings.Repeat("长", MaxTextRunes+1) + `"}]`, ``, `text is longer than 2000 characters`},
		{"row width", `[{"id": "root", "component": "Table", "columns": ["a", "b"], "rows": [["1", "2"], ["3"]]}]`, ``, `row 1 must be an array of 2 cells`},
		{"cell type", `[{"id": "root", "component": "Table", "columns": ["a"], "rows": [[{"x": 1}]]}]`, ``, `row 0 cell 0 must be a string, number or null, got an object`},
		{"too many columns", `[{"id": "root", "component": "Table", "columns": ["1","2","3","4","5","6","7","8","9"], "rows": []}]`, ``, `columns must list 1 to 8 headings`},
		{"list items written out", `[{"id": "root", "component": "List", "items": [1, 2], "template": "t"}, {"id": "t", "component": "Divider"}]`, ``, `items must be {"path": ...}`},
		{"list over a non-array", `[{"id": "root", "component": "List", "items": {"path": "/a"}, "template": "t"}, {"id": "t", "component": "Divider"}]`, `{"a": "x"}`, `items must be an array, got a string`},
		{"template field missing in an item", `[{"id": "root", "component": "List", "items": {"path": "/a"}, "template": "t"}, {"id": "t", "component": "Text", "text": {"path": "name"}}]`, `{"a": [{"name": "x"}, {}]}`, `component "t" (Text) in item 1 of list "root": text: path "name" finds nothing`},
		{"chart point", `[{"id": "root", "component": "Chart", "variant": "line", "points": [{"label": "a", "value": "1"}]}]`, ``, `point 0 must be {"label": string, "value": number}`},
		{"empty chart", `[{"id": "root", "component": "Chart", "variant": "line", "points": []}]`, ``, `a chart needs at least one point`},
		{"link policy", `[{"id": "root", "component": "Link", "label": "x", "url": "http://10.0.0.1/admin"}]`, ``, `link "http://10.0.0.1/admin" cannot be shown: only https addresses`},
		{"markdown link policy", `[{"id": "root", "component": "Text", "text": "see [this](javascript:alert(1))"}]`, ``, `link "javascript:alert(1" cannot be shown`},
		{"bare link policy", `[{"id": "root", "component": "Text", "text": "router at http://192.168.1.1/admin, see"}]`, ``, `link "http://192.168.1.1/admin" cannot be shown`},
		{"www link policy", `[{"id": "root", "component": "Text", "text": "on www.example.com today"}]`, ``, `link "http://www.example.com" cannot be shown`},
		{"image", `[{"id": "root", "component": "Image", "file": "file_ffffffffffffffffffffffffffffffff"}]`, ``, `file: file_ffffffffffffffffffffffffffffffff is not a file of this conversation`},
		{"empty components", `[]`, ``, `components is empty`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			components, data := decode(t, tc.components, tc.data)
			_, err := Check(context.Background(), "view", components, data, newRefs())
			if !errors.Is(err, ErrInvalidView) {
				t.Fatalf("err = %v, want ErrInvalidView", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v\nwant it to mention %q", err, tc.want)
			}
		})
	}
}

func TestCheckListsEveryProblemAtOnce(t *testing.T) {
	components, data := decode(t, `[{"id": "root", "component": "Column", "children": ["a", "b"]}, {"id": "a", "component": "Text"}, {"id": "b", "component": "Badge", "text": "x", "tone": "loud"}]`, ``)
	_, err := Check(context.Background(), "", components, data, newRefs())
	for _, want := range []string{"title is required", `component "a" (Text): text is required`, `component "b" (Badge): tone must be one of`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v\nwant it to mention %q", err, want)
		}
	}
}

func TestCheckLimitsSize(t *testing.T) {
	many := []map[string]any{{"id": "root", "component": "Column", "children": []any{}}}
	for i := range MaxComponents {
		id := fmt.Sprintf("c%d", i)
		many[0]["children"] = append(many[0]["children"].([]any), id)
		many = append(many, map[string]any{"id": id, "component": "Divider"})
	}
	if _, err := Check(context.Background(), "t", many, nil, newRefs()); err == nil || !strings.Contains(err.Error(), "201 components; at most 200 fit") {
		t.Fatalf("err = %v", err)
	}
	components, _ := decode(t, `[{"id": "root", "component": "Text", "text": {"path": "/big"}}]`, "")
	big := map[string]any{"big": strings.Repeat("x", MaxBytes)}
	if _, err := Check(context.Background(), "t", components, big, newRefs()); err == nil || !strings.Contains(err.Error(), "show fewer components or less data") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Check(context.Background(), strings.Repeat("标", MaxTitleRunes+1), components, map[string]any{"big": "x"}, newRefs()); err == nil || !strings.Contains(err.Error(), "title is longer than 80") {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckLimitsHowMuchNestedListsDraw(t *testing.T) {
	components, _ := decode(t, `[
	  {"id": "root", "component": "List", "items": {"path": "/xs"}, "template": "outer"},
	  {"id": "outer", "component": "List", "items": {"path": "/xs"}, "template": "inner"},
	  {"id": "inner", "component": "Divider"}
	]`, "")
	xs := make([]any, MaxItems)
	_, err := Check(context.Background(), "t", components, map[string]any{"xs": xs}, newRefs())
	if err == nil || !strings.Contains(err.Error(), "draws more than 5000 components") {
		t.Fatalf("err = %v", err)
	}
}

// TestTheViewsSkillExamplesPass keeps the examples the agent copies from in
// step with the checks.
func TestTheViewsSkillExamplesPass(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join("..", "skills", "seed", "views", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	examples := 0
	for rest := string(skill); ; {
		var ok bool
		if _, rest, ok = strings.Cut(rest, "```json\n"); !ok {
			break
		}
		example, after, closed := strings.Cut(rest, "```")
		if !closed {
			t.Fatal("a json example in the views skill is not closed")
		}
		rest = after
		examples++
		var view struct {
			Title      string           `json:"title"`
			Components []map[string]any `json:"components"`
			Data       map[string]any   `json:"data"`
		}
		if err := json.Unmarshal([]byte(example), &view); err != nil {
			t.Fatalf("example %d: %v", examples, err)
		}
		if _, err := Check(context.Background(), view.Title, view.Components, view.Data, newRefs()); err != nil {
			t.Fatalf("the skill's example %d fails: %v", examples, err)
		}
	}
	if examples != 2 {
		t.Fatalf("the views skill has %d json examples, want 2", examples)
	}
	for name := range catalog {
		if !strings.Contains(string(skill), "`"+name+"`") {
			t.Errorf("the views skill does not mention %s", name)
		}
	}
}

func TestLookupFollowsJSONPointer(t *testing.T) {
	var doc any
	if err := json.Unmarshal([]byte(`{"a/b": {"~k": [10, 20]}, "": 1}`), &doc); err != nil {
		t.Fatal(err)
	}
	for pointer, want := range map[string]any{"/a~1b/~0k/1": 20.0, "/": 1.0, "": doc} {
		got, ok := lookup(doc, pointer)
		if !ok || fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("lookup(%q) = %v, %v; want %v", pointer, got, ok, want)
		}
	}
	for _, pointer := range []string{"/a~1b/~0k/01", "/a~1b/~0k/2", "/x", "a"} {
		if _, ok := lookup(doc, pointer); ok {
			t.Fatalf("lookup(%q) found something", pointer)
		}
	}
}
