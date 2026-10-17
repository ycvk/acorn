package workstation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

const (
	// mcpStartWait bounds how long a request to run servers waits for new
	// ones to come up; uvx and npx fetch the server the first time.
	mcpStartWait = 90 * time.Second
	// mcpSessionIdle closes a client's session to a server that has not been
	// used for this long.
	mcpSessionIdle = time.Hour
)

// stdioProtocolVersion opens a package's server with initialize: servers
// that predate the discover call of later versions reject it, and the server
// may still negotiate an older version.
const stdioProtocolVersion = "2025-11-25"

// MCPServerSpec asks for the MCP server of the package the workstation holds
// under Name at Hash, with Env added to its environment.
type MCPServerSpec struct {
	Name string            `json:"name"`
	Hash string            `json:"hash"`
	Env  map[string]string `json:"env,omitempty"`
}

type MCPServersRequest struct {
	Servers []MCPServerSpec `json:"servers"`
}

// MCPServerStatus reports one server: Ready when it serves, otherwise Error
// says why not.
type MCPServerStatus struct {
	Name  string `json:"name"`
	Hash  string `json:"hash"`
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

type MCPServersStatus struct {
	Servers []MCPServerStatus `json:"servers"`
}

// mcpHost runs the MCP servers of skill packages, each from its package
// directory under the supervisor, and serves each one's tools over
// streamable HTTP.
type mcpHost struct {
	home string
	sup  *Supervisor
	ctx  context.Context
	stop context.CancelFunc

	mu      sync.Mutex
	servers map[string]*mcpServer
}

func newMCPHost(home string, sup *Supervisor) *mcpHost {
	ctx, stop := context.WithCancel(context.Background())
	return &mcpHost{home: home, sup: sup, ctx: ctx, stop: stop, servers: map[string]*mcpServer{}}
}

// mcpServer is one package's server: the process the supervisor keeps
// running and the bridge that serves its tools. The bridge outlives
// restarts, so a client's session survives them.
type mcpServer struct {
	name   string
	bridge *mcp.Server
	http   http.Handler

	toolsMu sync.Mutex
	tools   []string

	mu      sync.Mutex
	spec    MCPServerSpec
	cancel  context.CancelFunc
	done    chan struct{}
	session *mcp.ClientSession
	err     error
	// ended is set once an attempt of the current spec has ended, and
	// attempting while one is starting.
	ended      bool
	attempting bool
	changed    chan struct{}
}

// reconcile makes the host run exactly these servers: it stops the others,
// restarts a server whose package or environment changed, and starts the
// new ones.
func (h *mcpHost) reconcile(specs []MCPServerSpec) []*mcpServer {
	h.mu.Lock()
	defer h.mu.Unlock()
	wanted := map[string]bool{}
	for _, spec := range specs {
		wanted[spec.Name] = true
	}
	for name, srv := range h.servers {
		if !wanted[name] {
			srv.halt()
			delete(h.servers, name)
		}
	}
	out := make([]*mcpServer, 0, len(specs))
	for _, spec := range specs {
		srv := h.servers[spec.Name]
		switch {
		case srv == nil:
			srv = newMCPServer(spec.Name)
			h.servers[spec.Name] = srv
			h.start(srv, spec)
		case !srv.runs(spec):
			srv.halt()
			h.start(srv, spec)
		}
		out = append(out, srv)
	}
	return out
}

func (h *mcpHost) server(name string) *mcpServer {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.servers[name]
}

func (h *mcpHost) statuses() []MCPServerStatus {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]MCPServerStatus, 0, len(h.servers))
	for _, name := range slices.Sorted(maps.Keys(h.servers)) {
		out = append(out, h.servers[name].status())
	}
	return out
}

// close stops every server and waits for them to exit.
func (h *mcpHost) close() {
	h.stop()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, srv := range h.servers {
		srv.halt()
	}
}

func newMCPServer(name string) *mcpServer {
	bridge := mcp.NewServer(&mcp.Implementation{Name: "acorn-" + name, Version: "1"}, nil)
	return &mcpServer{
		name:    name,
		bridge:  bridge,
		http:    mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return bridge }, &mcp.StreamableHTTPOptions{SessionTimeout: mcpSessionIdle}),
		changed: make(chan struct{}),
	}
}

// start keeps the server running under spec until halted.
func (h *mcpHost) start(srv *mcpServer, spec MCPServerSpec) {
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	srv.mu.Lock()
	srv.spec, srv.cancel, srv.done = spec, cancel, done
	srv.session, srv.err, srv.ended, srv.attempting = nil, nil, false, false
	srv.notifyLocked()
	srv.mu.Unlock()
	go func() {
		defer close(done)
		h.sup.Keep(ctx, "mcp:"+spec.Name, func(ctx context.Context) error { return h.attempt(ctx, srv, spec) })
	}()
}

// attempt runs the server once: it starts the package's command, connects
// to it over stdio, mirrors its tools on the bridge and waits for it to exit.
func (h *mcpHost) attempt(ctx context.Context, srv *mcpServer, spec MCPServerSpec) error {
	srv.begin()
	dir := filepath.Join(h.home, skillsDir, spec.Name)
	server, err := packageServer(dir, spec.Hash)
	if err != nil {
		srv.finish(err)
		return err
	}
	attemptCtx, stop := context.WithCancel(ctx)
	defer stop()
	cmd := h.sup.Command(attemptCtx, server.Command, server.Args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+h.home)
	for _, name := range slices.Sorted(maps.Keys(spec.Env)) {
		cmd.Env = append(cmd.Env, name+"="+spec.Env[name])
	}
	cmd.Stderr = h.sup.Lines("mcp:" + spec.Name)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		srv.finish(err)
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		srv.finish(err)
		return err
	}
	if err := cmd.Start(); err != nil {
		err = fmt.Errorf("start %s: %w", server.Command, err)
		srv.finish(err)
		return err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "acorn-workstation", Version: "1"}, &mcp.ClientOptions{
		ToolListChangedHandler: func(ctx context.Context, req *mcp.ToolListChangedRequest) {
			if err := srv.syncTools(ctx, req.Session); err != nil {
				h.sup.log().line("mcp:"+spec.Name, "tool list change: "+err.Error())
			}
		},
	})
	connectCtx, cancelConnect := context.WithTimeout(attemptCtx, mcpStartWait)
	messages := newRPCLines(stdout, func(line string) { h.sup.log().line("mcp:"+spec.Name, "stray stdout: "+line) })
	session, err := client.Connect(connectCtx, &mcp.IOTransport{Reader: messages, Writer: stdin}, &mcp.ClientSessionOptions{ProtocolVersion: stdioProtocolVersion})
	cancelConnect()
	if err == nil {
		if err = srv.syncTools(attemptCtx, session); err != nil {
			_ = session.Close() // the attempt fails with the tool error
		}
	}
	if err != nil {
		stop()
		err = errors.Join(err, h.sup.Wait(cmd))
		srv.finish(err)
		return err
	}
	srv.up(session)
	// The server serves while both its process and its session last. Stray
	// stdout lines are skipped; a session that still ends, such as on a
	// message the client cannot decode, stops the process too.
	exited := make(chan error, 1)
	go func() { exited <- h.sup.Wait(cmd) }()
	ended := make(chan error, 1)
	go func() { ended <- session.Wait() }()
	select {
	case err = <-exited:
	case sessionErr := <-ended:
		stop()
		err = errors.Join(fmt.Errorf("the MCP session ended: %w", sessionErr), <-exited)
	}
	srv.finish(err)
	_ = session.Close() // the process has exited and its pipes are closed
	return err
}

// maxStdoutLine bounds one line of a server's stdout, as the SDK bounds one
// message.
const maxStdoutLine = mcp.DefaultMaxLineLength

// maxStrayLog bounds how much of a stray stdout line the log keeps.
const maxStrayLog = 1024

// rpcLines passes on the lines of a server's stdout that are JSON-RPC
// messages and hands every other line to stray, as the official SDKs' stdio
// clients do: a server's dependencies sometimes print there, such as npm
// installing what a library needs on first use. Blank lines are dropped.
type rpcLines struct {
	src     *bufio.Reader
	closer  io.Closer
	stray   func(string)
	pending []byte
	err     error
}

func newRPCLines(stdout io.ReadCloser, stray func(string)) *rpcLines {
	return &rpcLines{src: bufio.NewReader(stdout), closer: stdout, stray: stray}
}

func (r *rpcLines) Read(p []byte) (int, error) {
	for len(r.pending) == 0 && r.err == nil {
		line, err := r.readLine()
		switch {
		case isRPCMessage(line):
			r.pending = line
		case len(bytes.TrimSpace(line)) > 0:
			r.stray(strayText(line))
		}
		r.err = err
	}
	if len(r.pending) == 0 {
		return 0, r.err
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

// readLine reads up to and including the next newline; a line longer than
// maxStdoutLine fails the stream.
func (r *rpcLines) readLine() ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.src.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxStdoutLine {
			return nil, fmt.Errorf("a stdout line is longer than %d bytes", maxStdoutLine)
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, err
		}
	}
}

func (r *rpcLines) Close() error {
	return r.closer.Close()
}

// isRPCMessage reports whether a line is a JSON-RPC message or a batch of
// them.
func isRPCMessage(line []byte) bool {
	type message struct {
		JSONRPC string `json:"jsonrpc"`
	}
	var one message
	if json.Unmarshal(line, &one) == nil {
		return one.JSONRPC == "2.0"
	}
	var batch []message
	if json.Unmarshal(line, &batch) != nil || len(batch) == 0 {
		return false
	}
	return !slices.ContainsFunc(batch, func(m message) bool { return m.JSONRPC != "2.0" })
}

func strayText(line []byte) string {
	text := strings.TrimRight(string(line), "\r\n")
	if len(text) > maxStrayLog {
		return fmt.Sprintf("%s… (%d more bytes)", strings.ToValidUTF8(text[:maxStrayLog], ""), len(text)-maxStrayLog)
	}
	return text
}

// packageServer reads the MCP server of the package in dir, which must be
// the package with that hash.
func packageServer(dir, hash string) (skills.MCPServer, error) {
	held, err := os.ReadFile(filepath.Join(dir, packageMarker))
	if errors.Is(err, fs.ErrNotExist) {
		return skills.MCPServer{}, fmt.Errorf("the workstation holds no package %s", filepath.Base(dir))
	}
	if err != nil {
		return skills.MCPServer{}, err
	}
	if got := strings.TrimSpace(string(held)); got != hash {
		return skills.MCPServer{}, fmt.Errorf("the workstation holds package %s at %s, not %s", filepath.Base(dir), got, hash)
	}
	content, err := os.ReadFile(filepath.Join(dir, skills.ManifestFileName))
	if err != nil {
		return skills.MCPServer{}, fmt.Errorf("package %s: %w", filepath.Base(dir), err)
	}
	m, err := skills.ParseManifest(content)
	if err != nil {
		return skills.MCPServer{}, err
	}
	return *m.MCP, nil
}

// syncTools mirrors the server's tools on the bridge.
func (s *mcpServer) syncTools(ctx context.Context, session *mcp.ClientSession) error {
	var tools []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list tools: %w", err)
		}
		tools = append(tools, tool)
	}
	s.toolsMu.Lock()
	defer s.toolsMu.Unlock()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	var stale []string
	for _, name := range s.tools {
		if !slices.Contains(names, name) {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		s.bridge.RemoveTools(stale...)
	}
	s.tools = names
	for _, tool := range tools {
		if err := addTool(s.bridge, tool, s.forward(tool.Name)); err != nil {
			return err
		}
	}
	return nil
}

// addTool turns the SDK's panic on a malformed tool into an error, so one
// package's server cannot bring the workstation down.
func addTool(server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tool %s: %v", tool.Name, r)
		}
	}()
	server.AddTool(tool, handler)
	return nil
}

// forward passes a call to the running server.
func (s *mcpServer) forward(tool string) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		s.mu.Lock()
		session := s.session
		s.mu.Unlock()
		if session == nil {
			return nil, fmt.Errorf("the MCP server of %s is not running", s.name)
		}
		params := &mcp.CallToolParams{Name: tool}
		if len(req.Params.Arguments) > 0 {
			params.Arguments = req.Params.Arguments
		}
		return session.CallTool(ctx, params)
	}
}

func (s *mcpServer) runs(spec MCPServerSpec) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spec.Hash == spec.Hash && maps.Equal(s.spec.Env, spec.Env)
}

// halt stops the server's process and waits for it to exit.
func (s *mcpServer) halt() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	cancel()
	<-done
}

func (s *mcpServer) begin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempting = true
	s.notifyLocked()
}

func (s *mcpServer) up(session *mcp.ClientSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session, s.err, s.attempting = session, nil, false
	s.notifyLocked()
}

func (s *mcpServer) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		err = errors.New("the server exited")
	}
	s.session, s.err, s.ended, s.attempting = nil, err, true, false
	s.notifyLocked()
}

func (s *mcpServer) notifyLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// settled reports whether the server serves, or an attempt to run it ended
// and no other is starting, and a channel closed when that may change.
func (s *mcpServer) settled() (bool, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session != nil || s.ended && !s.attempting, s.changed
}

func (s *mcpServer) status() MCPServerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := MCPServerStatus{Name: s.name, Hash: s.spec.Hash, Ready: s.session != nil}
	switch {
	case status.Ready:
	case s.err != nil:
		status.Error = s.err.Error()
	default:
		status.Error = "starting"
	}
	return status
}

func (s *Server) runMCPServers(w http.ResponseWriter, r *http.Request) {
	var req MCPServersRequest
	if !decode(w, r, &req) {
		return
	}
	seen := map[string]bool{}
	for _, spec := range req.Servers {
		switch {
		case !skillNamePattern.MatchString(spec.Name) || len(spec.Name) > 64:
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid skill name %q", spec.Name))
			return
		case seen[spec.Name]:
			writeError(w, http.StatusBadRequest, fmt.Errorf("server %s appears twice", spec.Name))
			return
		case strings.TrimSpace(spec.Hash) == "":
			writeError(w, http.StatusBadRequest, fmt.Errorf("server %s needs the package hash", spec.Name))
			return
		}
		seen[spec.Name] = true
	}
	servers := s.mcp.reconcile(req.Servers)
	timeout := time.NewTimer(mcpStartWait)
	defer timeout.Stop()
	out := MCPServersStatus{Servers: make([]MCPServerStatus, 0, len(servers))}
	for _, srv := range servers {
		settled, changed := srv.settled()
	wait:
		for !settled {
			select {
			case <-changed:
				settled, changed = srv.settled()
			case <-timeout.C:
				break wait
			case <-r.Context().Done():
				return
			}
		}
		out.Servers = append(out.Servers, srv.status())
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) mcpServers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, MCPServersStatus{Servers: s.mcp.statuses()})
}

// mcpEndpoint serves a package's MCP server over streamable HTTP.
func (s *Server) mcpEndpoint(w http.ResponseWriter, r *http.Request) {
	srv := s.mcp.server(r.PathValue("name"))
	if srv == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no MCP server %s", r.PathValue("name")))
		return
	}
	srv.http.ServeHTTP(w, r)
}

// RunMCPServers makes the workstation run exactly these packages' MCP
// servers and returns why each one that does not serve fails.
func (c *Client) RunMCPServers(ctx context.Context, servers []skills.MCPServerRun) (map[string]string, error) {
	req := MCPServersRequest{Servers: make([]MCPServerSpec, 0, len(servers))}
	for _, s := range servers {
		req.Servers = append(req.Servers, MCPServerSpec{Name: s.Name, Hash: s.Hash, Env: s.Env})
	}
	var out MCPServersStatus
	if err := c.call(ctx, http.MethodPut, "/v1/mcp", req, &out); err != nil {
		return nil, err
	}
	failed := map[string]string{}
	for _, status := range out.Servers {
		if !status.Ready {
			failed[status.Name] = status.Error
		}
	}
	return failed, nil
}

// MCPServers reports the packages' MCP servers the workstation runs.
func (c *Client) MCPServers(ctx context.Context) ([]MCPServerStatus, error) {
	var out MCPServersStatus
	return out.Servers, c.call(ctx, http.MethodGet, "/v1/mcp", nil, &out)
}

// MCPProvider is how to reach the MCP server of the package name.
func (c *Client) MCPProvider(name string) core.ProviderConfig {
	return core.ProviderConfig{
		Name:                  name,
		Enabled:               true,
		Transport:             "streamable_http",
		URL:                   c.base + "/v1/mcp/" + name,
		Headers:               map[string]string{"Authorization": "Bearer " + c.token},
		StartupTimeoutSeconds: 30,
	}
}
