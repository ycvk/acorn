package workstation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeDevTools serves /json/list and a DevTools socket per page that answers
// every command and records it. Screenshots return shots in turn, repeating
// the last, and a non-nil hold delays their replies until it closes.
type fakeDevTools struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []string
	params []map[string]any
	shots  []string
	taken  int
	hold   chan struct{}
}

// testShot is a base64 JPEG of width by height pixels in one gray level.
func testShot(t *testing.T, width, height int, gray uint8) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, width, height))
	for i := range img.Pix {
		img.Pix[i] = gray
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
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
			result := map[string]any{}
			if msg.Method == "Page.captureScreenshot" && len(f.shots) > 0 {
				result["data"] = f.shots[min(f.taken, len(f.shots)-1)]
				f.taken++
			}
			hold := f.hold
			f.mu.Unlock()
			if msg.Method == "Page.captureScreenshot" && hold != nil {
				<-hold
			}
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
	server, err := NewServer(ServerConfig{Home: t.TempDir(), Token: testToken, View: view, Supervisor: NewSupervisor(io.Discard)})
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

func TestViewStreamsSharpChangedImagesLaidOutForThePhone(t *testing.T) {
	client, devtools := newViewClient(t)
	first, second := testShot(t, 824, 1400, 40), testShot(t, 824, 1400, 200)
	devtools.mu.Lock()
	// The page sits still for three screenshots, then changes.
	devtools.shots = []string{first, first, first, second}
	devtools.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body, err := client.BrowserFrames(ctx, "T1", Viewport{Width: 412, Height: 700})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	reader := bufio.NewReader(body)
	var frames []Frame
	for len(frames) < 2 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok {
			var frame Frame
			if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &frame); err != nil {
				t.Fatal(err)
			}
			frames = append(frames, frame)
		}
	}
	if frames[0] != (Frame{JPEG: first, Width: 412, Height: 700}) || frames[1].JPEG != second {
		t.Fatalf("frames = %+v", frames)
	}
	calls, params := devtools.recorded()
	if len(calls) < 6 || calls[0] != "Emulation.setDeviceMetricsOverride" || calls[1] != "Page.bringToFront" {
		t.Fatalf("calls = %v", calls)
	}
	if params[0]["width"] != float64(412) || params[0]["deviceScaleFactor"] != float64(streamScale) || params[0]["mobile"] != true {
		t.Fatalf("override = %v", params[0])
	}
	for _, call := range calls[2:6] {
		if call != "Page.captureScreenshot" {
			t.Fatalf("calls = %v, want screenshots after the override", calls)
		}
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

func TestViewDrag(t *testing.T) {
	client, devtools := newViewClient(t)
	for _, in := range []Input{{Tab: "T1", Type: "down", X: 100, Y: 200}, {Tab: "T1", Type: "move", X: 140, Y: 200}, {Tab: "T1", Type: "up", X: 180, Y: 200}} {
		if err := client.BrowserInput(t.Context(), in); err != nil {
			t.Fatal(err)
		}
	}
	calls, params := devtools.recorded()
	want := []string{"Page.getLayoutMetrics", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Page.getLayoutMetrics", "Input.dispatchMouseEvent", "Page.getLayoutMetrics", "Input.dispatchMouseEvent"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v", calls)
	}
	// Each point lands at offset + point / scale, as a tap does.
	for i, w := range map[int]map[string]any{
		2: {"type": "mousePressed", "x": float64(210), "y": float64(420), "buttons": float64(1)},
		4: {"type": "mouseMoved", "x": float64(290), "y": float64(420), "buttons": float64(1)},
		6: {"type": "mouseReleased", "x": float64(370), "y": float64(420), "buttons": float64(0)},
	} {
		for k, v := range w {
			if params[i][k] != v {
				t.Fatalf("params[%d] = %v, want %s %v", i, params[i], k, v)
			}
		}
	}
}

func TestInputCommandsForDrags(t *testing.T) {
	at := func(kind string, extra map[string]any) command {
		params := map[string]any{"type": kind, "x": 3.5, "y": 7.0}
		for k, v := range extra {
			params[k] = v
		}
		return command{"Input.dispatchMouseEvent", params}
	}
	for kind, want := range map[string][]command{
		"down": {at("mouseMoved", nil), at("mousePressed", map[string]any{"button": "left", "buttons": 1, "clickCount": 1})},
		"move": {at("mouseMoved", map[string]any{"button": "left", "buttons": 1})},
		"up":   {at("mouseReleased", map[string]any{"button": "left", "buttons": 0, "clickCount": 1})},
	} {
		got, err := inputCommands(Input{Type: kind, X: 3.5, Y: 7})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s = %+v err %v", kind, got, err)
		}
	}
}

func TestViewInputAtAPointWaitsForAScreenshot(t *testing.T) {
	client, devtools := newViewClient(t)
	hold := make(chan struct{})
	devtools.mu.Lock()
	devtools.shots = []string{testShot(t, 824, 1400, 40)}
	devtools.hold = hold
	devtools.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body, err := client.BrowserFrames(ctx, "T1", Viewport{Width: 412, Height: 700})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	for {
		if calls, _ := devtools.recorded(); len(calls) > 0 && calls[len(calls)-1] == "Page.captureScreenshot" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	moved := make(chan error, 1)
	go func() { moved <- client.BrowserInput(ctx, Input{Tab: "T1", Type: "move", X: 100, Y: 200}) }()
	time.Sleep(200 * time.Millisecond)
	if calls, _ := devtools.recorded(); calls[len(calls)-1] != "Page.captureScreenshot" {
		t.Fatalf("calls = %v, want the move to wait for the screenshot", calls)
	}
	close(hold)
	if err := <-moved; err != nil {
		t.Fatal(err)
	}
	calls, _ := devtools.recorded()
	if !slices.Contains(calls, "Input.dispatchMouseEvent") {
		t.Fatalf("calls = %v", calls)
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
