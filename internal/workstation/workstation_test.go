package workstation

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "0123456789abcdef0123456789abcdef"

func newTestClient(t *testing.T) (*Client, string) {
	t.Helper()
	home := t.TempDir()
	server, err := NewServer(home, testToken)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	client, err := NewClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, home
}

func TestServerRejectsWrongToken(t *testing.T) {
	_, err := NewServer(t.TempDir(), "short")
	if err == nil || !strings.Contains(err.Error(), TokenEnv) {
		t.Fatalf("short token err = %v", err)
	}
	server, err := NewServer(t.TempDir(), testToken)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	client, err := NewClient(ts.URL, strings.Repeat("x", MinTokenLength), ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(t.Context()); err == nil || !strings.Contains(err.Error(), "invalid workstation token") {
		t.Fatalf("status err = %v", err)
	}
}

func TestExecReportsOutputExitCodeAndDirectory(t *testing.T) {
	client, home := newTestClient(t)
	if err := os.Mkdir(filepath.Join(home, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := client.Exec(t.Context(), ExecRequest{Command: "pwd; echo oops >&2; exit 3", Dir: "work", TimeoutSeconds: 10, MaxOutputBytes: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if got.ExitCode != 3 || strings.TrimSpace(got.Stderr) != "oops" || got.TimedOut {
		t.Fatalf("result = %+v", got)
	}
	if want, _ := filepath.EvalSymlinks(filepath.Join(home, "work")); strings.TrimSpace(got.Stdout) != filepath.Join(home, "work") && strings.TrimSpace(got.Stdout) != want {
		t.Fatalf("pwd = %q", got.Stdout)
	}
	if _, err := client.Exec(t.Context(), ExecRequest{Command: "true", Dir: "missing", TimeoutSeconds: 10, MaxOutputBytes: 1000}); err == nil || !strings.Contains(err.Error(), "start command") {
		t.Fatalf("missing dir err = %v", err)
	}
}

func TestExecTimeoutKillsBackgroundChildren(t *testing.T) {
	client, home := newTestClient(t)
	marker := filepath.Join(home, "survived")
	started := time.Now()
	got, err := client.Exec(t.Context(), ExecRequest{Command: "(sleep 3; touch " + marker + ") & sleep 30", TimeoutSeconds: 1, MaxOutputBytes: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !got.TimedOut || time.Since(started) > 10*time.Second {
		t.Fatalf("result = %+v after %s", got, time.Since(started))
	}
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("background child outlived the timeout: %v", err)
	}
}

func TestExecKeepsHeadAndTailOfLongOutput(t *testing.T) {
	client, _ := newTestClient(t)
	got, err := client.Exec(t.Context(), ExecRequest{Command: "echo START; head -c 5000 /dev/zero | tr '\\0' x; echo; echo END", TimeoutSeconds: 10, MaxOutputBytes: 200})
	if err != nil {
		t.Fatal(err)
	}
	if !got.StdoutTruncated || !strings.HasPrefix(got.Stdout, "START\n") || !strings.HasSuffix(got.Stdout, "END\n") || !strings.Contains(got.Stdout, "bytes omitted") {
		t.Fatalf("stdout = %q", got.Stdout)
	}
	if len(got.Stdout) > 200 {
		t.Fatalf("stdout has %d bytes", len(got.Stdout))
	}
}

func TestFilesRoundTripAndReportFailures(t *testing.T) {
	client, home := newTestClient(t)
	written, err := client.WriteFile(t.Context(), WriteRequest{Path: "notes/今天.md", Content: "你好，workstation\n"})
	if err != nil {
		t.Fatal(err)
	}
	if written.Path != filepath.Join(home, "notes", "今天.md") {
		t.Fatalf("written = %+v", written)
	}
	read, err := client.ReadFile(t.Context(), ReadRequest{Path: written.Path, MaxBytes: 1000})
	if err != nil || read.Content != "你好，workstation\n" || read.Truncated {
		t.Fatalf("read = %+v err = %v", read, err)
	}
	short, err := client.ReadFile(t.Context(), ReadRequest{Path: "notes/今天.md", MaxBytes: 4})
	if err != nil || short.Content != "你" || !short.Truncated || short.Size != int64(len("你好，workstation\n")) {
		t.Fatalf("short = %+v err = %v", short, err)
	}
	if err := os.WriteFile(filepath.Join(home, "blob"), []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ReadFile(t.Context(), ReadRequest{Path: "blob", MaxBytes: 1000}); err == nil || !strings.Contains(err.Error(), ErrNotText.Error()) {
		t.Fatalf("binary err = %v", err)
	}
	if _, err := client.ReadFile(t.Context(), ReadRequest{Path: "nope.txt", MaxBytes: 1000}); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing err = %v", err)
	}
}

func TestClientValidatesConfiguration(t *testing.T) {
	for _, tc := range []struct{ url, token string }{
		{"127.0.0.1:7810", testToken},
		{"ftp://host", testToken},
		{"http://127.0.0.1:7810", "short"},
	} {
		if _, err := NewClient(tc.url, tc.token, http.DefaultClient); err == nil {
			t.Fatalf("NewClient(%q, %q) succeeded", tc.url, tc.token)
		}
	}
}
