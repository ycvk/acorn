package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/views"
	"github.com/ycvk/acorn/internal/webaccess"
)

type viewStoreFake struct {
	saved   []core.ThreadViewWrite
	updated []core.ThreadViewUpdate
	stored  map[string]core.ThreadView
	events  []string
	shown   []core.ViewShownData
}

func (f *viewStoreFake) SaveThreadView(_ context.Context, in core.ThreadViewWrite) (core.ThreadView, error) {
	f.saved = append(f.saved, in)
	return core.ThreadView{ID: "view_1", SessionID: in.SessionID, RunID: in.RunID, Title: in.Title, Catalog: in.Catalog, Components: in.Components, Data: in.Data, CreatedAt: time.Unix(1, 0).UTC()}, nil
}

func (f *viewStoreFake) ThreadView(_ context.Context, sessionID, viewID string) (core.ThreadView, error) {
	view, ok := f.stored[viewID]
	if !ok || view.SessionID != sessionID {
		return core.ThreadView{}, core.ErrThreadViewNotFound
	}
	return view, nil
}

func (f *viewStoreFake) UpdateThreadView(_ context.Context, in core.ThreadViewUpdate) (core.ThreadView, error) {
	f.updated = append(f.updated, in)
	view := f.stored[in.ViewID]
	view.Title, view.Components, view.Data = in.Title, in.Components, in.Data
	f.stored[in.ViewID] = view
	return view, nil
}

func (f *viewStoreFake) AppendEvent(_ context.Context, runID, kind string, payload any) (core.EventRecord, error) {
	f.events = append(f.events, runID+" "+kind)
	if data, ok := payload.(core.ViewShownData); ok {
		f.shown = append(f.shown, data)
	}
	return core.EventRecord{}, nil
}

type viewFilesFake struct {
	files []core.ThreadFile
}

func (f viewFilesFake) ThreadFiles(context.Context, string) ([]core.ThreadFile, error) {
	return f.files, nil
}

type attachmentsFake map[string]string

func (f attachmentsFake) ReadAttachment(_ context.Context, rel string) (core.Attachment, error) {
	mime, ok := f[rel]
	if !ok {
		return core.Attachment{}, errors.New("no such attachment")
	}
	return core.Attachment{MIME: mime}, nil
}

type triggersFake []skills.PackageTrigger

func (f triggersFake) Triggers(context.Context) ([]skills.PackageTrigger, error) {
	return f, nil
}

type linkPolicyFake struct{}

func (linkPolicyFake) Validate(_ context.Context, raw string) (webaccess.ValidatedURL, error) {
	if !strings.HasPrefix(raw, "https://") {
		return webaccess.ValidatedURL{}, errors.New("private_address: blocked")
	}
	return webaccess.ValidatedURL{Normalized: raw}, nil
}

func showViewTool(t *testing.T, store *viewStoreFake, runID string) einotool.InvokableTool {
	t.Helper()
	tool, err := buildShowViewTool(ViewToolDeps{
		Store: store,
		Sources: views.Sources{
			Files: viewFilesFake{files: []core.ThreadFile{
				{ID: "file_0123456789abcdef0123456789abcdef", MIME: "image/png"},
				{ID: "file_ffffffffffffffffffffffffffffffff", MIME: "text/plain"},
			}},
			Attachments: attachmentsFake{"attachments/2026/cover.jpg": "image/jpeg"},
			Links:       linkPolicyFake{},
		},
		Triggers: triggersFake{{Trigger: skills.Trigger{Name: "check"}, Skill: "repo-releases"}},
		Context:  fixedArtifactContext{runID: runID, sessionID: "thread_eino", callID: "call_1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tool.(einotool.InvokableTool)
}

func TestShowViewStoresACheckedViewAndAnnouncesIt(t *testing.T) {
	store := &viewStoreFake{}
	out, err := showViewTool(t, store, "run_eino").InvokableRun(context.Background(), `{
	  "title": "eino 最近五个版本",
	  "components": [
	    {"id": "root", "component": "Column", "children": ["table", "shot", "cover", "src"]},
	    {"id": "table", "component": "Table", "columns": ["版本", "要点"], "rows": {"path": "/releases"}},
	    {"id": "shot", "component": "Image", "file": "file_0123456789abcdef0123456789abcdef"},
	    {"id": "cover", "component": "Image", "file": "attachments/2026/cover.jpg", "alt": "封面"},
	    {"id": "src", "component": "Link", "label": "发布页", "url": "https://github.com/cloudwego/eino/releases"}
	  ],
	  "data": {"releases": [["v0.9.21", "…"]]}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	var shown ShowViewOutput
	if err := json.Unmarshal([]byte(out), &shown); err != nil || shown.ViewID != "view_1" {
		t.Fatalf("show_view = %s err = %v", out, err)
	}
	if len(store.saved) != 1 || store.saved[0].SessionID != "thread_eino" || store.saved[0].RunID != "run_eino" || store.saved[0].Catalog != views.Catalog || store.saved[0].Title != "eino 最近五个版本" {
		t.Fatalf("saved = %+v", store.saved)
	}
	if len(store.events) != 1 || store.events[0] != "run_eino view.shown" || store.shown[0].View.ID != "view_1" || !strings.Contains(string(store.shown[0].View.Data), "v0.9.21") {
		t.Fatalf("events = %v shown = %+v", store.events, store.shown)
	}
}

func TestShowViewRejectsWhatItCannotDraw(t *testing.T) {
	cases := map[string]struct{ components, want string }{
		"invalid view":         {`[{"id": "root", "component": "Slider"}]`, `"Slider" is not in the catalog`},
		"text file as image":   {`[{"id": "root", "component": "Image", "file": "file_ffffffffffffffffffffffffffffffff"}]`, "is text/plain, not an image"},
		"missing attachment":   {`[{"id": "root", "component": "Image", "file": "attachments/none.png"}]`, "knowledge attachment attachments/none.png cannot be read"},
		"neither":              {`[{"id": "root", "component": "Image", "file": "https://example.com/a.png"}]`, "neither a file id of this conversation"},
		"link the policy bars": {`[{"id": "root", "component": "Link", "label": "router", "url": "http://192.168.1.1"}]`, "private_address: blocked"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store := &viewStoreFake{}
			_, err := showViewTool(t, store, "run_eino").InvokableRun(context.Background(), `{"title": "t", "components": `+tc.components+`}`)
			if !errors.Is(err, views.ErrInvalidView) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrInvalidView mentioning %q", err, tc.want)
			}
			if len(store.saved) != 0 || len(store.events) != 0 {
				t.Fatalf("a rejected view left saved = %v events = %v", store.saved, store.events)
			}
		})
	}
}

func TestUpdateViewReplacesWhatItIsGivenAndChecksTheWhole(t *testing.T) {
	store := &viewStoreFake{stored: map[string]core.ThreadView{"view_7": {
		ID: "view_7", SessionID: "thread_eino", Title: "eino",
		Components: json.RawMessage(`[{"id":"root","component":"Table","columns":["版本"],"rows":{"path":"/releases"}}]`),
		Data:       json.RawMessage(`{"releases":[["v0.9.21"]]}`),
		Actions:    []core.ViewAction{{Key: "save", ComponentID: "save"}},
	}}}
	tool, err := buildUpdateViewTool(ViewToolDeps{Store: store, Sources: views.Sources{Files: viewFilesFake{}, Attachments: attachmentsFake{}, Links: linkPolicyFake{}}, Triggers: triggersFake{}, Context: fixedArtifactContext{runID: "run_2", sessionID: "thread_eino"}})
	if err != nil {
		t.Fatal(err)
	}
	update := tool.(einotool.InvokableTool)

	if _, err := update.InvokableRun(context.Background(), `{"view_id": "view_7", "data": {"releases": [["v0.9.22"], ["v0.9.21"]]}}`); err != nil {
		t.Fatal(err)
	}
	if len(store.updated) != 1 || store.updated[0].Title != "eino" || !strings.Contains(string(store.updated[0].Components), `"Table"`) || string(store.updated[0].Data) != `{"releases":[["v0.9.22"],["v0.9.21"]]}` {
		t.Fatalf("updated = %+v", store.updated)
	}
	if len(store.events) != 1 || store.events[0] != "run_2 view.updated" || len(store.shown[0].View.Actions) != 1 {
		t.Fatalf("events = %v shown = %+v", store.events, store.shown)
	}

	if _, err := update.InvokableRun(context.Background(), `{"view_id": "view_7", "data": {"releases": "gone"}}`); !errors.Is(err, views.ErrInvalidView) || !strings.Contains(err.Error(), "rows must be an array") {
		t.Fatalf("an update the view cannot draw err = %v", err)
	}
	if _, err := update.InvokableRun(context.Background(), `{"view_id": "view_8", "title": "t"}`); !errors.Is(err, core.ErrThreadViewNotFound) {
		t.Fatalf("a view not in this conversation err = %v", err)
	}
	if len(store.updated) != 1 || len(store.events) != 1 {
		t.Fatalf("rejected updates left updated = %v events = %v", store.updated, store.events)
	}
}

func TestShowViewNeedsARun(t *testing.T) {
	_, err := showViewTool(t, &viewStoreFake{}, "").InvokableRun(context.Background(), `{"title": "t", "components": [{"id": "root", "component": "Divider"}]}`)
	if err == nil || !strings.Contains(err.Error(), "needs the current conversation and run") {
		t.Fatalf("err = %v", err)
	}
}

func TestShowViewLoadsOnDemand(t *testing.T) {
	spec, ok := ConfiguredLocalSpec("show_view")
	if !ok || spec.Loading.Mode != core.ToolLoadingModeDeferred {
		t.Fatalf("show_view spec = %+v, %v", spec, ok)
	}
}

// TestShowViewTakesATriggerOfASkillInUseAsItsSource checks that a view names
// the trigger that feeds it while pinned, and only a trigger that exists.
func TestShowViewTakesATriggerOfASkillInUseAsItsSource(t *testing.T) {
	store := &viewStoreFake{}
	tool := showViewTool(t, store, "run_repos")
	view := `{"title": "eino 最新版本", "components": [{"id": "root", "component": "Text", "text": {"path": "/latest"}}], "data": {"latest": "v0.9.21"}, "source": "%s"}`
	out, err := tool.InvokableRun(context.Background(), fmt.Sprintf(view, "repo-releases/check"))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.saved) != 1 || store.saved[0].Source != "repo-releases/check" || !strings.Contains(out, "pins it to the now page") {
		t.Fatalf("saved = %+v out = %s", store.saved, out)
	}
	_, err = tool.InvokableRun(context.Background(), fmt.Sprintf(view, "repo-releases/hourly"))
	if err == nil || !strings.Contains(err.Error(), "the triggers are repo-releases/check") || len(store.saved) != 1 {
		t.Fatalf("unknown source err = %v, saved %d", err, len(store.saved))
	}
}
