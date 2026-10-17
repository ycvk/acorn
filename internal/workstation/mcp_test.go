package workstation

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// buildMCPFixture builds the stdio MCP server in testdata/mcp_server.
func buildMCPFixture(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "mcp-fixture")
	if out, err := exec.Command("go", "build", "-o", binary, "./testdata/mcp_server").CombinedOutput(); err != nil {
		t.Fatalf("build MCP fixture: %v\n%s", err, out)
	}
	return binary
}

func fixturePackage(binary, tools string) []core.SkillFile {
	return []core.SkillFile{
		{Path: skills.ManifestFileName, Content: []byte("mcp:\n  command: " + binary + "\n  secrets: [FIXTURE_TOKEN]\n")},
		{Path: "tools.txt", Content: []byte(tools)},
	}
}

type mcpHarness struct {
	client *Client
	url    string
	home   string
	out    *syncBuffer
}

func newMCPHarness(t *testing.T) *mcpHarness {
	t.Helper()
	home := t.TempDir()
	out := &syncBuffer{}
	sup := &Supervisor{MinBackoff: 50 * time.Millisecond, MaxBackoff: 200 * time.Millisecond, StableAfter: time.Minute, StopGrace: time.Second, Output: out}
	server, err := NewServer(ServerConfig{Home: home, Token: testToken, Supervisor: sup})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(server.Close)
	client, err := NewClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return &mcpHarness{client: client, url: ts.URL, home: home, out: out}
}

// connect opens an MCP session with a package's server the way the backend
// does, through the workstation's bearer token.
func (h *mcpHarness) connect(t *testing.T, name string) *mcp.ClientSession {
	t.Helper()
	provider := h.client.MCPProvider(name)
	httpClient := &http.Client{Transport: bearer{token: strings.TrimPrefix(provider.Headers["Authorization"], "Bearer ")}}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: provider.URL, HTTPClient: httpClient, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

type bearer struct{ token string }

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func callText(t *testing.T, session *mcp.ClientSession, tool string, args any) string {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	if result.IsError || len(result.Content) != 1 {
		t.Fatalf("call %s = %+v", tool, result)
	}
	return result.Content[0].(*mcp.TextContent).Text
}

func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestPackageMCPServersRunFromTheirPackageWithTheirSecrets(t *testing.T) {
	binary := buildMCPFixture(t)
	h := newMCPHarness(t)
	ctx := t.Context()
	if err := h.client.PutSkillPackage(ctx, "fixture", "hash-1", "---\nname: fixture\n---\nx\n", fixturePackage(binary, "echo\n")); err != nil {
		t.Fatal(err)
	}
	failed, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-1", Env: map[string]string{"FIXTURE_TOKEN": "token-1"}}})
	if err != nil || len(failed) != 0 {
		t.Fatalf("run = %v, %v", failed, err)
	}
	session := h.connect(t, "fixture")
	if got := toolNames(t, session); !slices.Equal(got, []string{"cwd", "echo", "garble", "pid", "token"}) {
		t.Fatalf("tools = %v", got)
	}
	if got := callText(t, session, "echo", map[string]string{"text": "hi"}); got != "echo: hi" {
		t.Fatalf("echo = %q", got)
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(h.home, "skills", "fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if got := callText(t, session, "cwd", nil); got != dir {
		t.Fatalf("cwd = %q, want %q", got, dir)
	}
	if got := callText(t, session, "token", nil); got != "token-1" {
		t.Fatalf("token = %q", got)
	}
	if !strings.Contains(h.out.String(), "[mcp:fixture] serving echo") {
		t.Fatalf("the server's stderr is not logged under its name: %q", h.out.String())
	}
	if strings.Contains(h.out.String(), "token-1") {
		t.Fatalf("a secret reached the log: %q", h.out.String())
	}
	statuses, err := h.client.MCPServers(ctx)
	if err != nil || len(statuses) != 1 || !statuses[0].Ready || statuses[0].Hash != "hash-1" {
		t.Fatalf("statuses = %+v, %v", statuses, err)
	}

	unauthorized, err := http.Post(h.url+"/v1/mcp/fixture", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without the token = %d", unauthorized.StatusCode)
	}
}

func TestPackageMCPServersRestartOnANewRevisionAndStopWhenDropped(t *testing.T) {
	binary := buildMCPFixture(t)
	h := newMCPHarness(t)
	ctx := t.Context()
	env := map[string]string{"FIXTURE_TOKEN": "token-1"}
	if err := h.client.PutSkillPackage(ctx, "fixture", "hash-1", "---\nname: fixture\n---\nx\n", fixturePackage(binary, "echo\n")); err != nil {
		t.Fatal(err)
	}
	if failed, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-1", Env: env}}); err != nil || len(failed) != 0 {
		t.Fatalf("run = %v, %v", failed, err)
	}
	session := h.connect(t, "fixture")
	first := callText(t, session, "pid", nil)

	// The same request again keeps the running server.
	if failed, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-1", Env: env}}); err != nil || len(failed) != 0 {
		t.Fatalf("rerun = %v, %v", failed, err)
	}
	if again := callText(t, session, "pid", nil); again != first {
		t.Fatalf("an unchanged server restarted: %s then %s", first, again)
	}

	// A new revision restarts it with the new package; the session stays.
	if err := h.client.PutSkillPackage(ctx, "fixture", "hash-2", "---\nname: fixture\n---\ny\n", fixturePackage(binary, "echo\nshout\n")); err != nil {
		t.Fatal(err)
	}
	if failed, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-2", Env: env}}); err != nil || len(failed) != 0 {
		t.Fatalf("run revision 2 = %v, %v", failed, err)
	}
	second := callText(t, session, "pid", nil)
	if second == first {
		t.Fatal("a new revision did not restart the server")
	}
	if got := toolNames(t, session); !slices.Contains(got, "shout") {
		t.Fatalf("tools after the new revision = %v", got)
	}
	assertExited(t, first)

	// New secrets restart it too.
	if failed, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-2", Env: map[string]string{"FIXTURE_TOKEN": "token-2"}}}); err != nil || len(failed) != 0 {
		t.Fatalf("run with a new secret = %v, %v", failed, err)
	}
	if got := callText(t, session, "token", nil); got != "token-2" {
		t.Fatalf("token after the change = %q", got)
	}
	third := callText(t, session, "pid", nil)

	// A server no longer asked for stops, and its endpoint goes.
	if failed, err := h.client.RunMCPServers(ctx, nil); err != nil || len(failed) != 0 {
		t.Fatalf("run none = %v, %v", failed, err)
	}
	assertExited(t, third)
	if statuses, err := h.client.MCPServers(ctx); err != nil || len(statuses) != 0 {
		t.Fatalf("statuses after stop = %+v, %v", statuses, err)
	}
	if _, err := session.ListTools(ctx, nil); err == nil {
		t.Fatal("the endpoint of a stopped server still answers")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url+"/v1/mcp/fixture", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("stopped endpoint = %d", resp.StatusCode)
	}
}

func TestPackageMCPServersThatFailReportWhy(t *testing.T) {
	binary := buildMCPFixture(t)
	h := newMCPHarness(t)
	ctx := t.Context()
	if err := h.client.PutSkillPackage(ctx, "fixture", "hash-1", "---\nname: fixture\n---\nx\n", fixturePackage(binary, "echo\n")); err != nil {
		t.Fatal(err)
	}
	missing := []core.SkillFile{{Path: skills.ManifestFileName, Content: []byte("mcp:\n  command: ./scripts/missing-server\n")}}
	if err := h.client.PutSkillPackage(ctx, "missing", "hash-m", "---\nname: missing\n---\nx\n", missing); err != nil {
		t.Fatal(err)
	}
	failed, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{
		{Name: "fixture", Hash: "hash-1"},
		{Name: "missing", Hash: "hash-m"},
		{Name: "stale", Hash: "hash-s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 3 || !strings.Contains(failed["fixture"], "exit status 3") || !strings.Contains(failed["missing"], "missing-server") || !strings.Contains(failed["stale"], "holds no package stale") {
		t.Fatalf("failed = %v", failed)
	}
	if !strings.Contains(h.out.String(), "[mcp:fixture] FIXTURE_TOKEN is not set") {
		t.Fatalf("log = %q", h.out.String())
	}
	failed, err = h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-0", Env: map[string]string{"FIXTURE_TOKEN": "t"}}})
	if err != nil || !strings.Contains(failed["fixture"], "at hash-1, not hash-0") {
		t.Fatalf("hash mismatch = %v, %v", failed, err)
	}
	failed, err = h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "fixture", Hash: "hash-1", Env: map[string]string{"FIXTURE_TOKEN": "t"}}})
	if err != nil || len(failed) != 0 {
		t.Fatalf("fixed server = %v, %v", failed, err)
	}
	if _, err := h.client.RunMCPServers(ctx, []skills.MCPServerRun{{Name: "Bad_Name", Hash: "h"}}); err == nil {
		t.Fatal("ran a server with an invalid name")
	}
}

// assertExited waits for a process to be gone.
func assertExited(t *testing.T, pid string) {
	t.Helper()
	n, err := strconv.Atoi(pid)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(n, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process %d is still running", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestAPackageServerWhoseSessionBreaksIsRestarted(t *testing.T) {
	binary := buildMCPFixture(t)
	h := newMCPHarness(t)
	ctx := t.Context()
	run := []skills.MCPServerRun{{Name: "fixture", Hash: "hash-1", Env: map[string]string{"FIXTURE_TOKEN": "t"}}}
	if err := h.client.PutSkillPackage(ctx, "fixture", "hash-1", "---\nname: fixture\n---\nx\n", fixturePackage(binary, "echo\n")); err != nil {
		t.Fatal(err)
	}
	if failed, err := h.client.RunMCPServers(ctx, run); err != nil || len(failed) != 0 {
		t.Fatalf("run = %v, %v", failed, err)
	}
	session := h.connect(t, "fixture")
	first := callText(t, session, "pid", nil)
	// The garbled line breaks the stdio session; the process stays up.
	_, _ = session.CallTool(ctx, &mcp.CallToolParams{Name: "garble"})
	assertExited(t, first)
	deadline := time.Now().Add(10 * time.Second)
	for {
		failed, err := h.client.RunMCPServers(ctx, run)
		if err != nil {
			t.Fatal(err)
		}
		if len(failed) == 0 {
			if again := callText(t, session, "pid", nil); again != first {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server did not come back: %v\n%s", failed, h.out.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(h.out.String(), "the MCP session ended") {
		t.Fatalf("log = %q", h.out.String())
	}
}
