package api

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/workstation"
)

type browserViewFake struct {
	tabs     []workstation.Tab
	inputs   []workstation.Input
	inputErr error
	frames   string
	viewport workstation.Viewport
}

func (f *browserViewFake) BrowserTabs(context.Context) ([]workstation.Tab, error) {
	return f.tabs, nil
}

func (f *browserViewFake) BrowserInput(_ context.Context, in workstation.Input) error {
	f.inputs = append(f.inputs, in)
	return f.inputErr
}

func (f *browserViewFake) BrowserFrames(_ context.Context, tab string, viewport workstation.Viewport) (io.ReadCloser, error) {
	f.viewport = viewport
	if tab != "T1" {
		return nil, &workstation.HTTPError{Path: "/v1/browser/frames", Status: http.StatusNotFound, Message: "no such tab in the workstation's browser: " + tab}
	}
	return io.NopCloser(strings.NewReader(f.frames)), nil
}

func newBrowserRouter(view BrowserView) http.Handler {
	server := &Server{
		deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}),
		browser:    view,
		logger:     slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)),
	}
	router := chi.NewRouter()
	server.registerRoutes(router)
	return router
}

func TestBrowserTabsAndFrames(t *testing.T) {
	view := &browserViewFake{
		tabs:   []workstation.Tab{{ID: "T1", Title: "登录", URL: "https://example.com/login"}},
		frames: "event: frame\ndata: {\"jpeg\":\"/9j/\",\"width\":1280,\"height\":900}\n\n",
	}
	router := newBrowserRouter(view)
	rec := performClientRequest(router, http.MethodGet, "/v1/browser/tabs", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"tabs":[{"id":"T1","title":"登录","url":"https://example.com/login"}]}` {
		t.Fatalf("tabs: %d %s", rec.Code, rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/browser/frames?tab=T1&width=412&height=700", "")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" || rec.Body.String() != view.frames || view.viewport != (workstation.Viewport{Width: 412, Height: 700}) {
		t.Fatalf("frames: %d %q %q %+v", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String(), view.viewport)
	}
	if rec := performClientRequest(router, http.MethodGet, "/v1/browser/frames?tab=T1&width=412", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("half a viewport: %d %s", rec.Code, rec.Body.String())
	}
	rec = performClientRequest(router, http.MethodGet, "/v1/browser/frames?tab=gone", "")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "browser_tab_not_found") {
		t.Fatalf("missing tab: %d %s", rec.Code, rec.Body.String())
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodGet, "/v1/browser/tabs", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d", rec.Code)
	}
}

func TestBrowserInput(t *testing.T) {
	view := &browserViewFake{}
	router := newBrowserRouter(view)
	rec := performClientRequest(router, http.MethodPost, "/v1/browser/input", `{"tab":"T1","type":"tap","x":100,"y":200}`)
	if rec.Code != http.StatusNoContent || len(view.inputs) != 1 || !reflect.DeepEqual(view.inputs[0], workstation.Input{Tab: "T1", Type: "tap", X: 100, Y: 200}) {
		t.Fatalf("tap: %d %s %+v", rec.Code, rec.Body.String(), view.inputs)
	}
	for body, want := range map[string]workstation.Input{
		`{"tab":"T1","type":"down","x":100,"y":200}`: {Tab: "T1", Type: "down", X: 100, Y: 200},
		`{"tab":"T1","type":"move","points":[{"x":101.5,"y":200,"t":8},{"x":103,"y":201,"t":8},{"x":106,"y":201,"t":16}]}`: {Tab: "T1", Type: "move", Points: []workstation.DragPoint{{X: 101.5, Y: 200, T: 8}, {X: 103, Y: 201, T: 8}, {X: 106, Y: 201, T: 16}}},
		`{"tab":"T1","type":"up","x":106,"y":201,"t":24}`:                                                                  {Tab: "T1", Type: "up", X: 106, Y: 201, T: 24},
	} {
		rec := performClientRequest(router, http.MethodPost, "/v1/browser/input", body)
		if rec.Code != http.StatusNoContent || !reflect.DeepEqual(view.inputs[len(view.inputs)-1], want) {
			t.Fatalf("%s: %d %s %+v", body, rec.Code, rec.Body.String(), view.inputs)
		}
	}
	for _, body := range []string{
		`{"type":"tap"}`,
		`{"tab":"T1","type":"drag"}`,
		`{"tab":"T1","type":"tap","extra":1}`,
		`{"tab":"T1","type":"move","x":100,"y":200}`,
		`{"tab":"T1","type":"move","points":[]}`,
		`{"tab":"T1","type":"move","points":[{"x":1,"y":1,"t":20},{"x":2,"y":1,"t":10}]}`,
		`{"tab":"T1","type":"move","points":[{"x":1,"y":1,"t":-1}]}`,
		`{"tab":"T1","type":"tap","x":1,"y":1,"points":[{"x":1,"y":1,"t":1}]}`,
		`{"tab":"T1","type":"down","x":1,"y":1,"t":5}`,
		`{"tab":"T1","type":"up","x":1,"y":1,"t":-5}`,
	} {
		if rec := performClientRequest(router, http.MethodPost, "/v1/browser/input", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	points := make([]string, maxBrowserDragPoints+1)
	for i := range points {
		points[i] = `{"x":1,"y":1,"t":1}`
	}
	if rec := performClientRequest(router, http.MethodPost, "/v1/browser/input", `{"tab":"T1","type":"move","points":[`+strings.Join(points, ",")+`]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("too many points: %d %s", rec.Code, rec.Body.String())
	}
	view.inputErr = &workstation.HTTPError{Path: "/v1/browser/input", Status: http.StatusBadRequest, Message: "navigate needs an http(s) URL"}
	rec = performClientRequest(router, http.MethodPost, "/v1/browser/input", `{"tab":"T1","type":"navigate","url":"ftp://x"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "http(s) URL") {
		t.Fatalf("bad navigate: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBrowserWithoutWorkstation(t *testing.T) {
	rec := performClientRequest(newBrowserRouter(nil), http.MethodGet, "/v1/browser/tabs", "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "browser_unavailable") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
