package workstation

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeDevTools serves /json/list and a DevTools socket per page that answers
// every command, records it, and sends one screencast frame per start.
type fakeDevTools struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []string
	params []map[string]any
}

func newFakeDevTools(t *testing.T) *fakeDevTools {
	t.Helper()
	f := &fakeDevTools{}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		ws := "ws://" + r.Host + "/devtools/page/"
		_ = json.NewEncoder(w).Encode([]map[string]string{
			{"id": "T1", "type": "page", "title": "登录 - 示例", "url": "https://example.com/login", "webSocketDebuggerUrl": ws + "T1"},
			{"id": "EXT", "type": "page", "title": "BrowserSkill", "url": "chrome-extension://abc/popup.html", "webSocketDebuggerUrl": ws + "EXT"},
			{"id": "SW", "type": "service_worker", "url": "chrome-extension://abc/background.js"},
		})
	})
	mux.HandleFunc("/devtools/page/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var msg struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Error(err)
				return
			}
			f.mu.Lock()
			f.calls = append(f.calls, msg.Method)
			f.params = append(f.params, msg.Params)
			f.mu.Unlock()
			result := map[string]any{}
			switch msg.Method {
			case "Page.getNavigationHistory":
				result = map[string]any{"currentIndex": 1, "entries": []map[string]any{{"id": 7}, {"id": 8}}}
			case "Page.getLayoutMetrics":
				// A desktop page zoomed out to a phone's width, scrolled within the zoom.
				result = map[string]any{"cssVisualViewport": map[string]any{"offsetX": 10, "offsetY": 20, "scale": 0.5}}
			}
			reply, _ := json.Marshal(map[string]any{"id": msg.ID, "result": result})
			if err := conn.Write(ctx, websocket.MessageText, reply); err != nil {
				return
			}
			if msg.Method == "Page.startScreencast" {
				frame, _ := json.Marshal(map[string]any{"method": "Page.screencastFrame", "params": map[string]any{
					"data": "/9j/AA==", "sessionId": 3, "metadata": map[string]any{"deviceWidth": 1280, "deviceHeight": 900},
				}})
				if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
					return
				}
			}
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeDevTools) recorded() ([]string, []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...), append([]map[string]any(nil), f.params...)
}

func newViewClient(t *testing.T) (*Client, *fakeDevTools) {
	t.Helper()
	devtools := newFakeDevTools(t)
	view, err := NewView(devtools.server.URL, devtools.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(ServerConfig{Home: t.TempDir(), Token: testToken, View: view})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	client, err := NewClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, devtools
}

func TestViewListsPagesOnly(t *testing.T) {
	client, _ := newViewClient(t)
	tabs, err := client.BrowserTabs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if want := []Tab{{ID: "T1", Title: "登录 - 示例", URL: "https://example.com/login"}}; !reflect.DeepEqual(tabs, want) {
		t.Fatalf("tabs = %+v", tabs)
	}
}

func TestViewStreamsFramesLaidOutForThePhone(t *testing.T) {
	client, devtools := newViewClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body, err := client.BrowserFrames(ctx, "T1", Viewport{Width: 412, Height: 700})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	reader := bufio.NewReader(body)
	var event, data string
	for data == "" {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		}
	}
	var frame Frame
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		t.Fatal(err)
	}
	if event != "frame" || frame != (Frame{JPEG: "/9j/AA==", Width: 1280, Height: 900}) {
		t.Fatalf("event %q frame %+v", event, frame)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		calls, params := devtools.recorded()
		if len(calls) == 3 {
			want := []string{"Emulation.setDeviceMetricsOverride", "Page.startScreencast", "Page.screencastFrameAck"}
			if !reflect.DeepEqual(calls, want) || params[0]["width"] != float64(412) || params[0]["mobile"] != true || params[2]["sessionId"] != float64(3) {
				t.Fatalf("calls %v params %v", calls, params)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("calls = %v, want the frame acknowledged", calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestParseViewport(t *testing.T) {
	if v, err := ParseViewport("", ""); err != nil || v != (Viewport{}) {
		t.Fatalf("absent = %+v %v", v, err)
	}
	if v, err := ParseViewport("412", "700"); err != nil || v != (Viewport{Width: 412, Height: 700}) {
		t.Fatalf("phone = %+v %v", v, err)
	}
	for _, bad := range [][2]string{{"412", ""}, {"x", "700"}, {"100", "700"}, {"412", "5000"}} {
		if _, err := ParseViewport(bad[0], bad[1]); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}

func TestViewInput(t *testing.T) {
	client, devtools := newViewClient(t)
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "tap", X: 100, Y: 200}); err != nil {
		t.Fatal(err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "text", Text: "密码123"}); err != nil {
		t.Fatal(err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "back"}); err != nil {
		t.Fatal(err)
	}
	calls, params := devtools.recorded()
	want := []string{"Page.getLayoutMetrics", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.insertText", "Page.getNavigationHistory", "Page.navigateToHistoryEntry"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v", calls)
	}
	// The tap at (100, 200) on the stream lands at offset + point / scale.
	if params[2]["type"] != "mousePressed" || params[2]["x"] != float64(210) || params[2]["y"] != float64(420) || params[4]["text"] != "密码123" || params[6]["entryId"] != float64(7) {
		t.Fatalf("params = %v", params)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "scroll", X: 100, Y: 200, DY: 150}); err != nil {
		t.Fatal(err)
	}
	calls, params = devtools.recorded()
	if last := params[len(params)-1]; calls[len(calls)-1] != "Input.dispatchMouseEvent" || last["type"] != "mouseWheel" || last["x"] != float64(210) || last["deltaY"] != float64(300) {
		t.Fatalf("scroll = %v %v", calls[len(calls)-1], last)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "nope", Type: "reload"}); err == nil || !strings.Contains(err.Error(), ErrNoTab.Error()) {
		t.Fatalf("unknown tab err = %v", err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "navigate", URL: "javascript:alert(1)"}); err == nil || !strings.Contains(err.Error(), "http(s) URL") {
		t.Fatalf("navigate err = %v", err)
	}
}

func TestInputCommandsForKeys(t *testing.T) {
	enter, err := inputCommands(Input{Type: "key", Key: "Enter"})
	if err != nil || len(enter) != 2 || enter[0].params["text"] != "\r" || enter[1].params["type"] != "keyUp" {
		t.Fatalf("enter = %+v err %v", enter, err)
	}
	if _, err := inputCommands(Input{Type: "key", Key: "F13"}); err == nil {
		t.Fatal("unsupported key accepted")
	}
	if _, err := inputCommands(Input{Type: "text"}); err == nil {
		t.Fatal("empty text accepted")
	}
}

func TestViewIsOffWithoutDevTools(t *testing.T) {
	client, _ := newTestClient(t)
	if _, err := client.BrowserTabs(t.Context()); err == nil || !strings.Contains(err.Error(), ErrNoView.Error()) {
		t.Fatalf("err = %v", err)
	}
}
