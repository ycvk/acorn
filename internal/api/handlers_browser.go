package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ycvk/acorn/internal/workstation"
)

// BrowserView reaches the agent's browser on the workstation, so the owner
// can watch it and sign in to sites for the agent.
type BrowserView interface {
	BrowserTabs(ctx context.Context) ([]workstation.Tab, error)
	BrowserInput(ctx context.Context, in workstation.Input) error
	BrowserFrames(ctx context.Context, tab string, viewport workstation.Viewport) (io.ReadCloser, error)
}

type BrowserTab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type BrowserTabListResponse struct {
	Tabs []BrowserTab `json:"tabs"`
}

type BrowserInputRequest struct {
	Tab    string             `json:"tab"`
	Type   string             `json:"type"`
	X      float64            `json:"x,omitempty"`
	Y      float64            `json:"y,omitempty"`
	T      int64              `json:"t,omitempty"`
	Points []BrowserDragPoint `json:"points,omitempty"`
	DY     float64            `json:"dy,omitempty"`
	Text   string             `json:"text,omitempty"`
	Key    string             `json:"key,omitempty"`
	URL    string             `json:"url,omitempty"`
}

type BrowserDragPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	T int64   `json:"t"`
}

// maxBrowserDragPoints bounds the points of one move.
const maxBrowserDragPoints = 1000

var browserInputTypes = map[string]bool{"tap": true, "scroll": true, "down": true, "move": true, "up": true, "text": true, "key": true, "navigate": true, "back": true, "reload": true}

func (s *Server) handleBrowserTabs(w http.ResponseWriter, r *http.Request) {
	if !s.browserReady(w, r) {
		return
	}
	tabs, err := s.browser.BrowserTabs(r.Context())
	if err != nil {
		s.respondBrowserError(w, r, err)
		return
	}
	out := BrowserTabListResponse{Tabs: make([]BrowserTab, 0, len(tabs))}
	for _, t := range tabs {
		out.Tabs = append(out.Tabs, BrowserTab{ID: t.ID, Title: t.Title, URL: t.URL})
	}
	s.respondJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleBrowserInput(w http.ResponseWriter, r *http.Request) {
	if !s.browserReady(w, r) {
		return
	}
	var req BrowserInputRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	req.Tab = strings.TrimSpace(req.Tab)
	if req.Tab == "" {
		s.respondBadRequest(w, r, "tab is required")
		return
	}
	if !browserInputTypes[req.Type] {
		s.respondBadRequest(w, r, "type must be tap, scroll, down, move, up, text, key, navigate, back or reload")
		return
	}
	if err := validateBrowserDrag(req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	in := workstation.Input{Tab: req.Tab, Type: req.Type, X: req.X, Y: req.Y, T: req.T, DY: req.DY, Text: req.Text, Key: req.Key, URL: req.URL}
	for _, p := range req.Points {
		in.Points = append(in.Points, workstation.DragPoint(p))
	}
	if err := s.browser.BrowserInput(r.Context(), in); err != nil {
		s.respondBrowserError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateBrowserDrag checks the time and points of a drag's input: a move
// carries points in time order, an up its time, and nothing else carries
// either.
func validateBrowserDrag(req BrowserInputRequest) error {
	if req.Type != "move" && len(req.Points) > 0 {
		return errors.New("points are only for move")
	}
	if req.Type != "up" && req.T != 0 {
		return errors.New("t is only for up")
	}
	if req.T < 0 {
		return errors.New("t must not be negative")
	}
	if req.Type != "move" {
		return nil
	}
	if len(req.Points) == 0 || len(req.Points) > maxBrowserDragPoints {
		return fmt.Errorf("move needs 1 to %d points, got %d", maxBrowserDragPoints, len(req.Points))
	}
	var last int64
	for i, p := range req.Points {
		if p.T < last {
			return fmt.Errorf("points[%d].t %d is before the previous point's %d", i, p.T, last)
		}
		last = p.T
	}
	return nil
}

// handleBrowserFrames relays the workstation's frame stream as it arrives.
func (s *Server) handleBrowserFrames(w http.ResponseWriter, r *http.Request) {
	if !s.browserReady(w, r) {
		return
	}
	tab := strings.TrimSpace(r.URL.Query().Get("tab"))
	if tab == "" {
		s.respondBadRequest(w, r, "tab is required")
		return
	}
	viewport, err := workstation.ParseViewport(r.URL.Query().Get("width"), r.URL.Query().Get("height"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondInternalError(w, r, errors.New("response writer does not support flushing"))
		return
	}
	body, err := s.browser.BrowserFrames(r.Context(), tab, viewport)
	if err != nil {
		s.respondBrowserError(w, r, err)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	buf := make([]byte, 64<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			flusher.Flush()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && r.Context().Err() == nil {
				s.logInternalError(r, "browser_frames_relay_failed", err)
			}
			return
		}
	}
}

func (s *Server) browserReady(w http.ResponseWriter, r *http.Request) bool {
	if s.browser == nil {
		s.respondError(w, r, http.StatusServiceUnavailable, "browser_unavailable", "the agent has no workstation; set workstation.url")
		return false
	}
	return true
}

func (s *Server) respondBrowserError(w http.ResponseWriter, r *http.Request, err error) {
	var httpErr *workstation.HTTPError
	switch {
	case errors.As(err, &httpErr) && httpErr.Status == http.StatusNotFound:
		s.respondNotFound(w, r, "browser_tab_not_found", httpErr.Message)
	case errors.As(err, &httpErr) && httpErr.Status == http.StatusBadRequest:
		s.respondBadRequest(w, r, httpErr.Message)
	default:
		s.respondError(w, r, http.StatusServiceUnavailable, "browser_unavailable", err.Error())
	}
}
