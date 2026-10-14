package workstation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Tab is one page in the workstation's browser.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Frame is one screencast image of a tab. Width and Height are the page's
// viewport in CSS pixels, the coordinates input uses.
type Frame struct {
	JPEG   string `json:"jpeg"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// Input is one thing the owner does in the view: tap or scroll at viewport
// coordinates, type text, press a key, or navigate.
type Input struct {
	Tab  string  `json:"tab"`
	Type string  `json:"type"`
	X    float64 `json:"x,omitempty"`
	Y    float64 `json:"y,omitempty"`
	DY   float64 `json:"dy,omitempty"`
	Text string  `json:"text,omitempty"`
	Key  string  `json:"key,omitempty"`
	URL  string  `json:"url,omitempty"`
}

// Viewport is the screen a phone shows a stream on, in CSS pixels. The zero
// Viewport leaves the page as the agent sees it.
type Viewport struct {
	Width  int
	Height int
}

// ParseViewport reads the optional width and height query values of a frame
// stream; both are given or neither.
func ParseViewport(width, height string) (Viewport, error) {
	if width == "" && height == "" {
		return Viewport{}, nil
	}
	w, werr := strconv.Atoi(width)
	h, herr := strconv.Atoi(height)
	if werr != nil || herr != nil || w < 240 || w > 1280 || h < 240 || h > 2400 {
		return Viewport{}, fmt.Errorf("width must be 240-1280 and height 240-2400, got %q and %q", width, height)
	}
	return Viewport{Width: w, Height: h}, nil
}

// ErrNoView is returned when the workstation's browser exposes no DevTools.
var ErrNoView = errors.New("the workstation has no browser view")

// ErrNoTab is returned for a tab id the browser does not have.
var ErrNoTab = errors.New("no such tab in the workstation's browser")

// keys are the keys the view can press, with their Windows virtual key codes.
var keys = map[string]int{"Enter": 13, "Backspace": 8, "Tab": 9, "Escape": 27, "ArrowUp": 38, "ArrowDown": 40, "ArrowLeft": 37, "ArrowRight": 39}

// View lets the owner watch and use the workstation's browser through its
// DevTools endpoint, so they can sign in to sites for the agent. Each
// connection attaches to one tab and detaches without closing it.
type View struct {
	devtools string
	http     *http.Client
}

// NewView talks to the DevTools endpoint at devtools, such as
// http://127.0.0.1:9222.
func NewView(devtools string, httpClient *http.Client) (*View, error) {
	u, err := url.Parse(devtools)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return nil, fmt.Errorf("devtools must be an http URL: %q", devtools)
	}
	if httpClient == nil {
		return nil, errors.New("view http client is required")
	}
	return &View{devtools: strings.TrimRight(devtools, "/"), http: httpClient}, nil
}

type devtoolsTarget struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
	WS    string `json:"webSocketDebuggerUrl"`
}

func (v *View) targets(ctx context.Context) ([]devtoolsTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.devtools+"/json/list", nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list browser tabs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list browser tabs: HTTP %d", resp.StatusCode)
	}
	var all []devtoolsTarget
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("list browser tabs: %w", err)
	}
	var pages []devtoolsTarget
	for _, t := range all {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "chrome-extension://") && !strings.HasPrefix(t.URL, "devtools://") {
			pages = append(pages, t)
		}
	}
	return pages, nil
}

// Tabs lists the browser's pages, most recently used first.
func (v *View) Tabs(ctx context.Context) ([]Tab, error) {
	targets, err := v.targets(ctx)
	if err != nil {
		return nil, err
	}
	tabs := make([]Tab, 0, len(targets))
	for _, t := range targets {
		tabs = append(tabs, Tab{ID: t.ID, Title: t.Title, URL: t.URL})
	}
	return tabs, nil
}

func (v *View) attach(ctx context.Context, tab string) (*cdpConn, error) {
	targets, err := v.targets(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range targets {
		if t.ID == tab {
			return dialCDP(ctx, t.WS)
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNoTab, tab)
}

// Stream sends the tab's screencast to send until ctx ends or send fails.
// A non-zero viewport lays the page out as a phone of that size meanwhile.
// Each frame is acknowledged after send returns, so a slow viewer slows the
// screencast instead of queueing frames.
func (v *View) Stream(ctx context.Context, tab string, viewport Viewport, send func(Frame) error) error {
	conn, err := v.attach(ctx, tab)
	if err != nil {
		return err
	}
	defer conn.close()
	if viewport != (Viewport{}) {
		// The override belongs to this DevTools session, so the page returns
		// to its own layout when the stream ends.
		if _, err := conn.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": viewport.Width, "height": viewport.Height, "deviceScaleFactor": 2, "mobile": true}); err != nil {
			return err
		}
	}
	frames := make(chan json.RawMessage, 1)
	conn.on("Page.screencastFrame", func(params json.RawMessage) {
		select {
		case frames <- params:
		default:
		}
	})
	if _, err := conn.call(ctx, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 60, "maxWidth": 1280, "maxHeight": 1280}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-conn.done:
			return err
		case raw := <-frames:
			var f struct {
				Data      string `json:"data"`
				SessionID int    `json:"sessionId"`
				Metadata  struct {
					DeviceWidth  float64 `json:"deviceWidth"`
					DeviceHeight float64 `json:"deviceHeight"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(raw, &f); err != nil {
				return fmt.Errorf("screencast frame: %w", err)
			}
			if err := send(Frame{JPEG: f.Data, Width: int(f.Metadata.DeviceWidth), Height: int(f.Metadata.DeviceHeight)}); err != nil {
				return err
			}
			if _, err := conn.call(ctx, "Page.screencastFrameAck", map[string]any{"sessionId": f.SessionID}); err != nil {
				return err
			}
		}
	}
}

// Do performs one input on its tab. Taps and scrolls arrive in the stream's
// coordinates and are moved onto the page first.
func (v *View) Do(ctx context.Context, in Input) error {
	commands, err := inputCommands(in)
	if err != nil {
		return err
	}
	conn, err := v.attach(ctx, in.Tab)
	if err != nil {
		return err
	}
	defer conn.close()
	if in.Type == "back" {
		return goBack(ctx, conn)
	}
	if in.Type == "tap" || in.Type == "scroll" {
		viewport, err := visualViewport(ctx, conn)
		if err != nil {
			return err
		}
		if commands, err = inputCommands(viewport.toPage(in)); err != nil {
			return err
		}
	}
	for _, c := range commands {
		if _, err := conn.call(ctx, c.method, c.params); err != nil {
			return err
		}
	}
	return nil
}

// cssVisualViewport is the part of the page the screencast shows: its offset
// in the layout viewport and its scale, both in CSS pixels. A phone-sized view
// of a desktop page is zoomed out, so a point in the stream is further into
// the page than its pixel position.
type cssVisualViewport struct {
	OffsetX float64 `json:"offsetX"`
	OffsetY float64 `json:"offsetY"`
	Scale   float64 `json:"scale"`
}

func visualViewport(ctx context.Context, conn *cdpConn) (cssVisualViewport, error) {
	raw, err := conn.call(ctx, "Page.getLayoutMetrics", map[string]any{})
	if err != nil {
		return cssVisualViewport{}, err
	}
	var metrics struct {
		Viewport cssVisualViewport `json:"cssVisualViewport"`
	}
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return cssVisualViewport{}, fmt.Errorf("layout metrics: %w", err)
	}
	if metrics.Viewport.Scale <= 0 {
		return cssVisualViewport{}, fmt.Errorf("layout metrics: visual viewport scale is %v", metrics.Viewport.Scale)
	}
	return metrics.Viewport, nil
}

// toPage moves a point and scroll distance from the stream onto the page.
func (v cssVisualViewport) toPage(in Input) Input {
	in.X = v.OffsetX + in.X/v.Scale
	in.Y = v.OffsetY + in.Y/v.Scale
	in.DY /= v.Scale
	return in
}

type command struct {
	method string
	params map[string]any
}

// inputCommands translates an input into DevTools commands. It is separate
// from Do so the translation can be tested without a browser.
func inputCommands(in Input) ([]command, error) {
	mouse := func(kind string, extra map[string]any) command {
		params := map[string]any{"type": kind, "x": in.X, "y": in.Y}
		for k, v := range extra {
			params[k] = v
		}
		return command{"Input.dispatchMouseEvent", params}
	}
	switch in.Type {
	case "tap":
		return []command{
			mouse("mouseMoved", nil),
			mouse("mousePressed", map[string]any{"button": "left", "clickCount": 1}),
			mouse("mouseReleased", map[string]any{"button": "left", "clickCount": 1}),
		}, nil
	case "scroll":
		return []command{mouse("mouseWheel", map[string]any{"deltaX": 0, "deltaY": in.DY})}, nil
	case "text":
		if in.Text == "" {
			return nil, errors.New("text input needs text")
		}
		return []command{{"Input.insertText", map[string]any{"text": in.Text}}}, nil
	case "key":
		code, ok := keys[in.Key]
		if !ok {
			return nil, fmt.Errorf("unsupported key %q", in.Key)
		}
		down := map[string]any{"type": "rawKeyDown", "key": in.Key, "code": in.Key, "windowsVirtualKeyCode": code}
		if in.Key == "Enter" {
			down = map[string]any{"type": "keyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13, "text": "\r"}
		}
		up := map[string]any{"type": "keyUp", "key": in.Key, "code": in.Key, "windowsVirtualKeyCode": code}
		return []command{{"Input.dispatchKeyEvent", down}, {"Input.dispatchKeyEvent", up}}, nil
	case "navigate":
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("navigate needs an http(s) URL: %q", in.URL)
		}
		return []command{{"Page.navigate", map[string]any{"url": u.String()}}}, nil
	case "reload":
		return []command{{"Page.reload", map[string]any{}}}, nil
	case "back":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported input type %q", in.Type)
	}
}

func goBack(ctx context.Context, conn *cdpConn) error {
	raw, err := conn.call(ctx, "Page.getNavigationHistory", map[string]any{})
	if err != nil {
		return err
	}
	var history struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return fmt.Errorf("navigation history: %w", err)
	}
	if history.CurrentIndex < 1 {
		return nil
	}
	_, err = conn.call(ctx, "Page.navigateToHistoryEntry", map[string]any{"entryId": history.Entries[history.CurrentIndex-1].ID})
	return err
}

// cdpConn is a DevTools connection to one page.
type cdpConn struct {
	ws      *websocket.Conn
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan cdpReply
	events  map[string]func(json.RawMessage)
	done    chan error
	cancel  context.CancelFunc
}

type cdpReply struct {
	result json.RawMessage
	err    error
}

func dialCDP(ctx context.Context, wsURL string) (*cdpConn, error) {
	ws, resp, err := websocket.Dial(ctx, wsURL, nil)
	if resp != nil && resp.Body != nil {
		// Dial leaves a body only on a failed handshake, already read into memory.
		resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("attach to browser tab: %w", err)
	}
	// Screencast frames of a full page exceed the default message limit.
	ws.SetReadLimit(32 << 20)
	readCtx, cancel := context.WithCancel(context.Background())
	c := &cdpConn{ws: ws, pending: map[int64]chan cdpReply{}, events: map[string]func(json.RawMessage){}, done: make(chan error, 1), cancel: cancel}
	go c.read(readCtx)
	return c, nil
}

func (c *cdpConn) on(method string, handle func(json.RawMessage)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events[method] = handle
}

func (c *cdpConn) read(ctx context.Context) {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			c.fail(fmt.Errorf("browser tab connection: %w", err))
			return
		}
		var msg struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			c.fail(fmt.Errorf("browser tab message: %w", err))
			return
		}
		c.mu.Lock()
		if msg.ID != 0 {
			if ch, ok := c.pending[msg.ID]; ok {
				delete(c.pending, msg.ID)
				reply := cdpReply{result: msg.Result}
				if msg.Error != nil {
					reply.err = errors.New(msg.Error.Message)
				}
				ch <- reply
			}
		} else if handle := c.events[msg.Method]; handle != nil {
			handle(msg.Params)
		}
		c.mu.Unlock()
	}
}

func (c *cdpConn) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, ch := range c.pending {
		ch <- cdpReply{err: err}
		delete(c.pending, id)
	}
	select {
	case c.done <- err:
	default:
	}
}

func (c *cdpConn) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	reply := make(chan cdpReply, 1)
	c.mu.Lock()
	c.pending[id] = reply
	c.mu.Unlock()
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := c.ws.Write(writeCtx, websocket.MessageText, data); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	select {
	case r := <-reply:
		if r.err != nil {
			return nil, fmt.Errorf("%s: %w", method, r.err)
		}
		return r.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// close detaches from the tab; the tab stays open. The connection is often
// already gone when a viewer leaves, so a failed close is only logged.
func (c *cdpConn) close() {
	c.cancel()
	if err := c.ws.CloseNow(); err != nil {
		slog.Debug("close browser tab connection", "error", err)
	}
}
