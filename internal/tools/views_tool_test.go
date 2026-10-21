package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/views"
	"github.com/ycvk/acorn/internal/webaccess"
)

type viewStoreFake struct {
	saved  []core.ThreadViewWrite
	events []string
	shown  []core.ViewShownData
}

func (f *viewStoreFake) SaveThreadView(_ context.Context, in core.ThreadViewWrite) (core.ThreadView, error) {
	f.saved = append(f.saved, in)
	return core.ThreadView{ID: "view_1", SessionID: in.SessionID, RunID: in.RunID, Title: in.Title, Catalog: in.Catalog, Components: in.Components, Data: in.Data, CreatedAt: time.Unix(1, 0).UTC()}, nil
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
		Files: viewFilesFake{files: []core.ThreadFile{
			{ID: "file_0123456789abcdef0123456789abcdef", MIME: "image/png"},
			{ID: "file_ffffffffffffffffffffffffffffffff", MIME: "text/plain"},
		}},
		Attachments: attachmentsFake{"attachments/2026/cover.jpg": "image/jpeg"},
		Links:       linkPolicyFake{},
		Context:     fixedArtifactContext{runID: runID, sessionID: "thread_eino", callID: "call_1"},
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
