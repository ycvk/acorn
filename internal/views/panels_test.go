package views

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
)

type panelStoreFake struct {
	panels map[string][]core.ThreadView
	fed    map[string]string
	gone   map[string]bool
}

func (f *panelStoreFake) FedPanels(_ context.Context, source string) ([]core.ThreadView, error) {
	return f.panels[source], nil
}

func (f *panelStoreFake) FeedThreadView(_ context.Context, viewID string, components, data json.RawMessage) (bool, error) {
	if f.gone[viewID] {
		return false, nil
	}
	for _, views := range f.panels {
		for _, view := range views {
			if view.ID == viewID && string(view.Components) != string(components) {
				return false, errors.New("fed against other components")
			}
		}
	}
	f.fed[viewID] = string(data)
	return true, nil
}

type noFiles struct{}

func (noFiles) ThreadFiles(context.Context, string) ([]core.ThreadFile, error) { return nil, nil }

type noAttachments struct{}

func (noAttachments) ReadAttachment(context.Context, string) (core.Attachment, error) {
	return core.Attachment{}, errors.New("no attachments")
}

type httpsOnly struct{}

func (httpsOnly) Validate(_ context.Context, raw string) (webaccess.ValidatedURL, error) {
	if !strings.HasPrefix(raw, "https://") {
		return webaccess.ValidatedURL{}, errors.New("private_address: blocked")
	}
	return webaccess.ValidatedURL{Normalized: raw}, nil
}

// TestFeedReplacesTheDataOfThePanelsItFits feeds a trigger's view_data to the
// pinned views naming it: a view the data fits takes it, a view it does not
// fit keeps its data and is named in the error, and a view that changed
// meanwhile is left alone.
func TestFeedReplacesTheDataOfThePanelsItFits(t *testing.T) {
	latest := core.ThreadView{ID: "view_latest", SessionID: "repos", Title: "eino 最新版本",
		Components: json.RawMessage(`[{"id":"root","component":"Column","children":["v","notes"]},{"id":"v","component":"Text","text":{"path":"/latest"}},{"id":"notes","component":"Link","label":"发布说明","url":{"path":"/url"}}]`)}
	table := core.ThreadView{ID: "view_table", SessionID: "repos", Title: "最近的版本",
		Components: json.RawMessage(`[{"id":"root","component":"Table","columns":["版本","日期"],"rows":{"path":"/rows"}}]`)}
	moved := core.ThreadView{ID: "view_moved", SessionID: "repos", Title: "刚改过",
		Components: json.RawMessage(`[{"id":"root","component":"Text","text":{"path":"/latest"}}]`)}
	store := &panelStoreFake{panels: map[string][]core.ThreadView{"repo-releases/check": {latest, table, moved}}, fed: map[string]string{}, gone: map[string]bool{"view_moved": true}}
	feeder, err := NewFeeder(store, Sources{Files: noFiles{}, Attachments: noAttachments{}, Links: httpsOnly{}})
	if err != nil {
		t.Fatal(err)
	}

	err = feeder.Feed(context.Background(), "repo-releases/check", map[string]any{"latest": "v0.9.22", "url": "https://github.com/cloudwego/eino/releases/tag/v0.9.22"})
	if err == nil || !strings.Contains(err.Error(), `"最近的版本" (view_table), which keeps its data`) || !strings.Contains(err.Error(), "/rows") || strings.Contains(err.Error(), "view_latest") {
		t.Fatalf("feed err = %v", err)
	}
	if store.fed["view_latest"] != `{"latest":"v0.9.22","url":"https://github.com/cloudwego/eino/releases/tag/v0.9.22"}` || len(store.fed) != 1 {
		t.Fatalf("fed = %v", store.fed)
	}

	err = feeder.Feed(context.Background(), "repo-releases/check", map[string]any{"latest": "v0.9.23", "url": "http://10.0.0.1/", "rows": []any{[]any{"v0.9.23", "10-22"}}})
	if err == nil || !strings.Contains(err.Error(), "view_latest") || !strings.Contains(err.Error(), "private_address") {
		t.Fatalf("blocked link err = %v", err)
	}
	if store.fed["view_table"] != `{"latest":"v0.9.23","rows":[["v0.9.23","10-22"]],"url":"http://10.0.0.1/"}` || !strings.Contains(store.fed["view_latest"], "v0.9.22") {
		t.Fatalf("fed = %v", store.fed)
	}

	if err := feeder.Feed(context.Background(), "other/trigger", map[string]any{"x": 1}); err != nil {
		t.Fatalf("a trigger feeding no panel err = %v", err)
	}
}
