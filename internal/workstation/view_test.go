package workstation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
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
// every command and records it with the socket it came on and when.
// Screenshots return shots in turn, repeating the last, and a non-nil hold
// delays their replies until it closes. A screencast sends cast as its frame.
// Mouse events are answered after answerAfter, while later commands go on, as
// a page that handles input at its next frame.
type fakeDevTools struct {
	server      *httptest.Server
	mu          sync.Mutex
	calls       []string
	params      []map[string]any
	sockets     []int
	times       []time.Time
	opened      int
	shots       []string
	taken       int
	hold        chan struct{}
	cast        string
	answerAfter time.Duration
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
		f.mu.Lock()
		f.opened++
		socket := f.opened
		f.mu.Unlock()
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
			f.sockets = append(f.sockets, socket)
			f.times = append(f.times, time.Now())
			cast, answerAfter := f.cast, f.answerAfter
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
			if msg.Method == "Input.dispatchMouseEvent" && answerAfter > 0 {
				go func() {
					time.Sleep(answerAfter)
					_ = conn.Write(ctx, websocket.MessageText, reply)
				}()
				continue
			}
			if err := conn.Write(ctx, websocket.MessageText, reply); err != nil {
				return
			}
			if msg.Method == "Page.startScreencast" {
				frame, _ := json.Marshal(map[string]any{"method": "Page.screencastFrame", "params": map[string]any{
					"data": cast, "sessionId": 1, "metadata": map[string]any{"deviceWidth": 412, "deviceHeight": 700, "pageScaleFactor": 1},
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

// call is one recorded command with the socket it came on and when.
type call struct {
	method string
	params map[string]any
	socket int
	at     time.Time
}

func (f *fakeDevTools) since(n int) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for i := n; i < len(f.calls); i++ {
		out = append(out, call{f.calls[i], f.params[i], f.sockets[i], f.times[i]})
	}
	return out
}

func (f *fakeDevTools) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
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

func TestViewDragPlaysOnOneSessionAtTheFingersTimes(t *testing.T) {
	client, devtools := newViewClient(t)
	before := devtools.count()
	sent := time.Now()
	for _, in := range []Input{
		{Tab: "T1", Type: "down", X: 100, Y: 200},
		{Tab: "T1", Type: "move", Points: []DragPoint{{X: 110, Y: 200, T: 40}, {X: 120, Y: 201, T: 80}}},
		{Tab: "T1", Type: "move", Points: []DragPoint{{X: 130, Y: 202, T: 120}}},
		{Tab: "T1", Type: "up", X: 140, Y: 202, T: 160},
	} {
		if err := client.BrowserInput(t.Context(), in); err != nil {
			t.Fatal(err)
		}
	}
	calls := devtools.since(before)
	var methods []string
	for _, c := range calls {
		methods = append(methods, c.method)
		if c.socket != calls[0].socket {
			t.Fatalf("calls came on sockets %d and %d, want one session for the drag", calls[0].socket, c.socket)
		}
	}
	if want := []string{"Page.getLayoutMetrics", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent", "Input.dispatchMouseEvent"}; !reflect.DeepEqual(methods, want) {
		t.Fatalf("calls = %v", methods)
	}
	// Each point lands at offset + point / scale, the viewport read once at the down.
	for i, w := range []struct {
		kind    string
		x, y    float64
		buttons float64
		after   time.Duration
	}{
		{"mouseMoved", 210, 420, 0, 0},
		{"mousePressed", 210, 420, 1, 0},
		{"mouseMoved", 230, 420, 1, 40 * time.Millisecond},
		{"mouseMoved", 250, 422, 1, 80 * time.Millisecond},
		{"mouseMoved", 270, 424, 1, 120 * time.Millisecond},
		{"mouseReleased", 290, 424, 0, 160 * time.Millisecond},
	} {
		c := calls[i+1]
		buttons, _ := c.params["buttons"].(float64)
		if c.params["type"] != w.kind || c.params["x"] != w.x || c.params["y"] != w.y || buttons != w.buttons {
			t.Fatalf("event %d = %v, want %s at %v,%v buttons %v", i, c.params, w.kind, w.x, w.y, w.buttons)
		}
		// Timers never fire early. The exact times are TestReplay's.
		if gap := c.at.Sub(sent); gap < dragDelay+w.after {
			t.Fatalf("event %d came %s after the down was sent, want at least %s", i, gap, dragDelay+w.after)
		}
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "move", Points: []DragPoint{{X: 1, Y: 1, T: 200}}}); err == nil || !strings.Contains(err.Error(), ErrNoDrag.Error()) {
		t.Fatalf("move after the up err = %v", err)
	}
}

func TestViewDragDoesNotWaitForThePageToHandleEachEvent(t *testing.T) {
	client, devtools := newViewClient(t)
	devtools.mu.Lock()
	devtools.answerAfter = 40 * time.Millisecond
	devtools.mu.Unlock()
	before := devtools.count()
	points := []DragPoint{}
	for i := int64(1); i <= 10; i++ {
		points = append(points, DragPoint{X: 100 + float64(i), Y: 200, T: 8 * i})
	}
	for _, in := range []Input{
		{Tab: "T1", Type: "down", X: 100, Y: 200},
		{Tab: "T1", Type: "move", Points: points},
		{Tab: "T1", Type: "up", X: 110, Y: 200, T: 88},
	} {
		if err := client.BrowserInput(t.Context(), in); err != nil {
			t.Fatal(err)
		}
	}
	var moves []time.Time
	for _, c := range devtools.since(before) {
		if c.method == "Input.dispatchMouseEvent" && c.params["type"] == "mouseMoved" && c.params["buttons"] == float64(1) {
			moves = append(moves, c.at)
		}
	}
	// Waiting for each answer would spread the ten moves over 400 ms.
	if len(moves) != 10 || moves[9].Sub(moves[0]) > 150*time.Millisecond {
		t.Fatalf("%d moves over %s, want 10 over about 72ms", len(moves), moves[len(moves)-1].Sub(moves[0]))
	}
}

func TestViewDragRejectsPointsOutOfOrder(t *testing.T) {
	client, devtools := newViewClient(t)
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "move", Points: []DragPoint{{X: 1, Y: 1, T: 10}}}); err == nil || !strings.Contains(err.Error(), ErrNoDrag.Error()) {
		t.Fatalf("move without a drag err = %v", err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "down", X: 100, Y: 200}); err != nil {
		t.Fatal(err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "move", Points: []DragPoint{{X: 110, Y: 200, T: 40}}}); err != nil {
		t.Fatal(err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "move", Points: []DragPoint{{X: 120, Y: 200, T: 30}}}); err == nil || !strings.Contains(err.Error(), "time order") {
		t.Fatalf("earlier point err = %v", err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "move", Points: []DragPoint{{X: 120, Y: 200, T: 60_000}}}); err == nil || !strings.Contains(err.Error(), "ahead of the drag") {
		t.Fatalf("future point err = %v", err)
	}
	time.Sleep(dragDelay + 100*time.Millisecond)
	// A down while the drag lost its up releases that drag where the page had it.
	before := devtools.count()
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "down", X: 50, Y: 60}); err != nil {
		t.Fatal(err)
	}
	if err := client.BrowserInput(t.Context(), Input{Tab: "T1", Type: "up", X: 50, Y: 60, T: 10}); err != nil {
		t.Fatal(err)
	}
	var events []string
	for _, c := range devtools.since(before) {
		if c.method == "Input.dispatchMouseEvent" {
			events = append(events, fmt.Sprintf("%s %v,%v", c.params["type"], c.params["x"], c.params["y"]))
		}
	}
	if want := []string{"mouseReleased 230,420", "mouseMoved 110,140", "mousePressed 110,140", "mouseReleased 110,140"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v", events)
	}
}

func TestViewStreamsTheScreencastWhileADragPlays(t *testing.T) {
	client, devtools := newViewClient(t)
	shot, cast := testShot(t, 824, 1400, 40), testShot(t, 412, 700, 120)
	devtools.mu.Lock()
	devtools.shots = []string{shot}
	devtools.cast = cast
	devtools.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body, err := client.BrowserFrames(ctx, "T1", Viewport{Width: 412, Height: 700})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	frames := make(chan Frame, 8)
	go func() {
		reader := bufio.NewReader(body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if data, ok := strings.CutPrefix(line, "data: "); ok {
				var frame Frame
				if json.Unmarshal([]byte(strings.TrimSpace(data)), &frame) == nil {
					frames <- frame
				}
			}
		}
	}()
	next := func() Frame {
		select {
		case f := <-frames:
			return f
		case <-ctx.Done():
			t.Fatal("no frame")
			return Frame{}
		}
	}
	if f := next(); f.JPEG != shot || f.Width != 412 || f.Height != 700 {
		t.Fatalf("first frame = %dx%d", f.Width, f.Height)
	}
	before := devtools.count()
	if err := client.BrowserInput(ctx, Input{Tab: "T1", Type: "down", X: 100, Y: 200}); err != nil {
		t.Fatal(err)
	}
	// The screencast's frames have the units of the screenshots'.
	if f := next(); f.JPEG != cast || f.Width != 412 || f.Height != 700 {
		t.Fatalf("drag frame = %dx%d, want the screencast's", f.Width, f.Height)
	}
	if err := client.BrowserInput(ctx, Input{Tab: "T1", Type: "up", X: 120, Y: 200, T: 30}); err != nil {
		t.Fatal(err)
	}
	if f := next(); f.JPEG != shot {
		t.Fatal("no screenshot after the drag")
	}
	var pressed, released, stopped, shotAfter bool
	for _, c := range devtools.since(before) {
		switch {
		case c.method == "Input.dispatchMouseEvent" && c.params["type"] == "mousePressed":
			pressed = true
		case c.method == "Input.dispatchMouseEvent" && c.params["type"] == "mouseReleased":
			released = true
		case c.method == "Page.stopScreencast":
			stopped = true
		case c.method == "Page.captureScreenshot" && pressed && !released:
			t.Fatal("a screenshot was taken while the button was pressed")
		case c.method == "Page.captureScreenshot" && released:
			shotAfter = true
		}
	}
	if !pressed || !released || !stopped || !shotAfter {
		t.Fatalf("pressed %v released %v stopped %v screenshot after %v", pressed, released, stopped, shotAfter)
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
	tapped := make(chan error, 1)
	go func() { tapped <- client.BrowserInput(ctx, Input{Tab: "T1", Type: "tap", X: 100, Y: 200}) }()
	time.Sleep(200 * time.Millisecond)
	if calls, _ := devtools.recorded(); calls[len(calls)-1] != "Page.captureScreenshot" {
		t.Fatalf("calls = %v, want the tap to wait for the screenshot", calls)
	}
	close(hold)
	if err := <-tapped; err != nil {
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
