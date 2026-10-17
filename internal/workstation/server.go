package workstation

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

// Server executes requests inside the workstation. Every request needs the
// bearer token; relative paths resolve against home.
type Server struct {
	home     string
	token    string
	chromium string
	view     *View
	mcp      *mcpHost
}

// ServerConfig sets up a Server. Chromium is the browser that renders pages
// and View the owner's view of the agent's browser; each is off when unset.
// Supervisor keeps the skill packages' MCP servers running.
type ServerConfig struct {
	Home       string
	Token      string
	Chromium   string
	View       *View
	Supervisor *Supervisor
}

func NewServer(cfg ServerConfig) (*Server, error) {
	home, token := cfg.Home, cfg.Token
	if !filepath.IsAbs(home) {
		return nil, fmt.Errorf("workstation home must be absolute: %q", home)
	}
	info, err := os.Stat(home)
	if err != nil {
		return nil, fmt.Errorf("workstation home: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workstation home %s is not a directory", home)
	}
	if len(token) < MinTokenLength {
		return nil, fmt.Errorf("%s must be at least %d characters", TokenEnv, MinTokenLength)
	}
	if cfg.Supervisor == nil {
		return nil, errors.New("workstation supervisor is required")
	}
	home = filepath.Clean(home)
	return &Server{home: home, token: token, chromium: cfg.Chromium, view: cfg.View, mcp: newMCPHost(home, cfg.Supervisor)}, nil
}

// Close stops the skill packages' MCP servers and waits for them to exit.
func (s *Server) Close() {
	s.mcp.close()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.authorized(s.status))
	mux.HandleFunc("POST /v1/exec", s.authorized(s.exec))
	mux.HandleFunc("POST /v1/files/read", s.authorized(s.read))
	mux.HandleFunc("POST /v1/files/write", s.authorized(s.write))
	mux.HandleFunc("POST /v1/files/package", s.authorized(s.readPackageHandler))
	mux.HandleFunc("GET /v1/files/raw", s.authorized(s.rawFile))
	mux.HandleFunc("GET /v1/uploads", s.authorized(s.uploads))
	mux.HandleFunc("PUT /v1/uploads/{id}", s.authorized(s.putUpload))
	mux.HandleFunc("DELETE /v1/uploads/{id}", s.authorized(s.deleteUpload))
	mux.HandleFunc("GET /v1/skills", s.authorized(s.skillPackages))
	mux.HandleFunc("PUT /v1/skills/{name}", s.authorized(s.putSkillPackage))
	mux.HandleFunc("DELETE /v1/skills/{name}", s.authorized(s.deleteSkillPackage))
	mux.HandleFunc("GET /v1/mcp", s.authorized(s.mcpServers))
	mux.HandleFunc("PUT /v1/mcp", s.authorized(s.runMCPServers))
	mux.HandleFunc("/v1/mcp/{name}", s.authorized(s.mcpEndpoint))
	mux.HandleFunc("POST /v1/triggers/run", s.authorized(s.runTrigger))
	mux.HandleFunc("POST /v1/render", s.authorized(s.render))
	mux.HandleFunc("GET /v1/browser/tabs", s.authorized(s.browserTabs))
	mux.HandleFunc("GET /v1/browser/frames", s.authorized(s.browserFrames))
	mux.HandleFunc("POST /v1/browser/input", s.authorized(s.browserInput))
	return mux
}

func (s *Server) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, errors.New("invalid workstation token"))
			return
		}
		next(w, r)
	}
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Status{Home: s.home})
}

func (s *Server) exec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeError(w, http.StatusBadRequest, errors.New("command is required"))
		return
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > MaxTimeoutSeconds {
		writeError(w, http.StatusBadRequest, fmt.Errorf("timeout_seconds must be between 1 and %d", MaxTimeoutSeconds))
		return
	}
	if req.MaxOutputBytes < 2 || req.MaxOutputBytes > MaxOutputBytes {
		writeError(w, http.StatusBadRequest, fmt.Errorf("max_output_bytes must be between 2 and %d", MaxOutputBytes))
		return
	}
	result, err := s.run(r.Context(), req, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// run kills the command's whole process group on timeout or when the caller
// goes away, so background children cannot outlive the request. env is added
// to the command's environment.
func (s *Server) run(ctx context.Context, req ExecRequest, env map[string]string) (ExecResult, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", req.Command)
	cmd.Dir = s.resolve(req.Dir)
	cmd.Env = append(os.Environ(), "HOME="+s.home)
	for _, name := range slices.Sorted(maps.Keys(env)) {
		cmd.Env = append(cmd.Env, name+"="+env[name])
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	stdout := newCappedBuffer(req.MaxOutputBytes / 2)
	stderr := newCappedBuffer(req.MaxOutputBytes / 2)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return ExecResult{}, fmt.Errorf("start command: %w", err)
	}
	err := cmd.Wait()
	result := ExecResult{
		ExitCode:        cmd.ProcessState.ExitCode(),
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded),
		DurationMS:      time.Since(started).Milliseconds(),
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !errors.Is(err, exec.ErrWaitDelay) {
		return ExecResult{}, fmt.Errorf("wait for command: %w", err)
	}
	return result, nil
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	var req ReadRequest
	if !decode(w, r, &req) {
		return
	}
	if req.MaxBytes < 1 || req.MaxBytes > MaxOutputBytes {
		writeError(w, http.StatusBadRequest, fmt.Errorf("max_bytes must be between 1 and %d", MaxOutputBytes))
		return
	}
	path, err := s.requirePath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := readText(path, req.MaxBytes)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, ErrNotText):
		writeError(w, http.StatusUnprocessableEntity, err)
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func readText(path string, maxBytes int) (ReadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ReadResult{}, err
	}
	if info.IsDir() {
		return ReadResult{}, fmt.Errorf("%s is a directory", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)))
	if err != nil {
		return ReadResult{}, err
	}
	truncated := info.Size() > int64(len(data))
	if truncated {
		data = trimPartialRune(data)
	}
	if !utf8.Valid(data) {
		return ReadResult{}, fmt.Errorf("%s: %w; inspect it with shell", path, ErrNotText)
	}
	return ReadResult{Path: path, Content: string(data), Size: info.Size(), Truncated: truncated}, nil
}

func (s *Server) write(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWriteBytes)
	var req WriteRequest
	if !decode(w, r, &req) {
		return
	}
	path, err := s.requirePath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResult{Path: path, Bytes: len(req.Content)})
}

func (s *Server) render(w http.ResponseWriter, r *http.Request) {
	var req RenderRequest
	if !decode(w, r, &req) {
		return
	}
	if s.chromium == "" {
		writeError(w, http.StatusNotImplemented, ErrNoBrowser)
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("url must be an http(s) URL: %q", req.URL))
		return
	}
	if req.TimeoutSeconds < 1 || req.TimeoutSeconds > 120 {
		writeError(w, http.StatusBadRequest, errors.New("timeout_seconds must be between 1 and 120"))
		return
	}
	html, err := s.dumpDOM(r.Context(), u.String(), time.Duration(req.TimeoutSeconds)*time.Second)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, RenderResult{HTML: html})
}

// dumpDOM runs a throwaway headless browser with its own profile, so pages
// rendered for watches never see the agent's browser state.
func (s *Server) dumpDOM(ctx context.Context, target string, timeout time.Duration) (string, error) {
	profile, err := os.MkdirTemp("", "acorn-render-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(profile)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	budget := strconv.Itoa(int(timeout.Milliseconds() / 2))
	cmd := exec.CommandContext(ctx, s.chromium, "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--no-first-run", "--user-data-dir="+profile, "--virtual-time-budget="+budget, "--dump-dom", target)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	stdout := newCappedBuffer(maxRenderBytes)
	stderr := newCappedBuffer(4096)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("render %s: timed out after %s", target, timeout)
		}
		return "", fmt.Errorf("render %s: %w: %s", target, err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Truncated() {
		return "", fmt.Errorf("render %s: page is larger than %d bytes", target, maxRenderBytes)
	}
	return stdout.String(), nil
}

func (s *Server) browserTabs(w http.ResponseWriter, r *http.Request) {
	if s.view == nil {
		writeError(w, http.StatusNotImplemented, ErrNoView)
		return
	}
	tabs, err := s.view.Tabs(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tabs": tabs})
}

func (s *Server) browserInput(w http.ResponseWriter, r *http.Request) {
	if s.view == nil {
		writeError(w, http.StatusNotImplemented, ErrNoView)
		return
	}
	var in Input
	if !decode(w, r, &in) {
		return
	}
	switch err := s.view.Do(r.Context(), in); {
	case errors.Is(err, ErrNoTab):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{})
	}
}

// browserFrames streams a tab as server-sent events: one "frame" event per
// changed image, a comment every 15 seconds to keep proxies open, and an
// "error" event when the stream ends on a failure.
func (s *Server) browserFrames(w http.ResponseWriter, r *http.Request) {
	if s.view == nil {
		writeError(w, http.StatusNotImplemented, ErrNoView)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming is not supported"))
		return
	}
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		writeError(w, http.StatusBadRequest, errors.New("tab is required"))
		return
	}
	viewport, err := ParseViewport(r.URL.Query().Get("width"), r.URL.Query().Get("height"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	var mu sync.Mutex
	write := func(chunk string) error {
		mu.Lock()
		defer mu.Unlock()
		if _, err := io.WriteString(w, chunk); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := write(": keepalive\n\n"); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	err = s.view.Stream(ctx, tab, viewport, func(f Frame) error {
		data, err := json.Marshal(f)
		if err != nil {
			return err
		}
		return write("event: frame\ndata: " + string(data) + "\n\n")
	})
	if err != nil && ctx.Err() == nil {
		data, merr := json.Marshal(errorBody{Error: err.Error()})
		if merr != nil {
			slog.Error("browser frame stream error", "error", err, "encode_error", merr)
			return
		}
		if werr := write("event: error\ndata: " + string(data) + "\n\n"); werr != nil {
			slog.Error("browser frame stream error", "error", err, "write_error", werr)
		}
	}
}

func (s *Server) requirePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path is required")
	}
	return s.resolve(path), nil
}

func (s *Server) resolve(path string) string {
	if path == "" {
		return s.home
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(s.home, path)
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		slog.Error("workstation response write failed", "status", status, "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, errorBody{Error: err.Error()})
}

// cappedBuffer keeps the first and last limit bytes of a stream.
type cappedBuffer struct {
	limit int
	head  []byte
	tail  []byte
	total int64
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.total += int64(len(p))
	rest := p
	if room := b.limit/2 - len(b.head); room > 0 {
		n := min(room, len(rest))
		b.head = append(b.head, rest[:n]...)
		rest = rest[n:]
	}
	b.tail = append(b.tail, rest...)
	if keep := b.limit - b.limit/2; len(b.tail) > 2*keep {
		b.tail = append(b.tail[:0:0], b.tail[len(b.tail)-keep:]...)
	}
	return len(p), nil
}

func (b *cappedBuffer) Truncated() bool {
	return b.total > int64(b.limit)
}

func (b *cappedBuffer) String() string {
	if !b.Truncated() {
		return string(b.head) + string(b.tail)
	}
	keep := b.limit - b.limit/2
	tail := b.tail[len(b.tail)-keep:]
	omitted := b.total - int64(len(b.head)) - int64(len(tail))
	return strings.ToValidUTF8(string(b.head), "") +
		fmt.Sprintf("\n[... %d bytes omitted ...]\n", omitted) +
		strings.ToValidUTF8(string(tail), "")
}

func trimPartialRune(data []byte) []byte {
	for i := 0; i < utf8.UTFMax && i < len(data); i++ {
		if utf8.Valid(data[:len(data)-i]) {
			return data[:len(data)-i]
		}
	}
	return data
}
