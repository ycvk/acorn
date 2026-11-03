package workstation

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/textedit"
)

const testToken = "0123456789abcdef0123456789abcdef"

func newTestClient(t *testing.T) (*Client, string) {
	t.Helper()
	return newTestClientWithBrowser(t, "")
}

func newTestClientWithBrowser(t *testing.T, chromium string) (*Client, string) {
	t.Helper()
	home := t.TempDir()
	server, err := NewServer(ServerConfig{Home: home, Token: testToken, Chromium: chromium, Supervisor: NewSupervisor(io.Discard)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	client, err := NewClient(ts.URL, testToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client, home
}

func TestServerRejectsWrongToken(t *testing.T) {
	_, err := NewServer(ServerConfig{Home: t.TempDir(), Token: "short", Supervisor: NewSupervisor(io.Discard)})
	if err == nil || !strings.Contains(err.Error(), TokenEnv) {
		t.Fatalf("short token err = %v", err)
	}
	server, err := NewServer(ServerConfig{Home: t.TempDir(), Token: testToken, Supervisor: NewSupervisor(io.Discard)})
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

// An edit replaces parts of a text file and keeps its line endings and
// permissions; an edit that cannot be placed leaves the file as it was.
func TestEditChangesPartsOfAFile(t *testing.T) {
	client, home := newTestClient(t)
	script := filepath.Join(home, "bin", "fetch.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "#!/bin/sh\r\nurl=https://example.com/a\r\nlimit=2M\r\ncurl --limit-rate $limit $url\r\n"
	if err := os.WriteFile(script, []byte(original), 0o750); err != nil {
		t.Fatal(err)
	}
	got, err := client.EditFile(t.Context(), EditRequest{Path: "bin/fetch.sh", Edits: []textedit.Edit{
		{Old: "limit=2M", New: "limit=5M"},
		{Old: "url=https://example.com/a\n", New: "url=https://example.com/b\nretries=3\n"},
	}})
	if err != nil || got.Path != script || fmt.Sprint(got.Lines) != "[4 2]" {
		t.Fatalf("edit = %+v, %v", got, err)
	}
	data, err := os.ReadFile(script)
	if err != nil || string(data) != "#!/bin/sh\r\nurl=https://example.com/b\r\nretries=3\r\nlimit=5M\r\ncurl --limit-rate $limit $url\r\n" || got.Bytes != len(data) {
		t.Fatalf("file = %q, %v", data, err)
	}
	if info, err := os.Stat(script); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("mode = %v, %v", info.Mode(), err)
	}
	if _, err := client.EditFile(t.Context(), EditRequest{Path: "bin/fetch.sh", Edits: []textedit.Edit{{Old: "retries=3", New: "retries=5"}, {Old: "limit", New: "cap"}}}); err == nil || !strings.Contains(err.Error(), "edit 2: the old text occurs 3 times") {
		t.Fatalf("ambiguous edit err = %v", err)
	}
	if after, err := os.ReadFile(script); err != nil || string(after) != string(data) {
		t.Fatalf("a refused edit changed the file: %q, %v", after, err)
	}
	if entries, err := os.ReadDir(filepath.Dir(script)); err != nil || len(entries) != 1 {
		t.Fatalf("files left beside the script: %v, %v", entries, err)
	}
	if _, err := client.EditFile(t.Context(), EditRequest{Path: "bin/none.sh", Edits: []textedit.Edit{{Old: "a", New: "b"}}}); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing file err = %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "blob"), []byte{0xff, 0xfe, 0x00}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EditFile(t.Context(), EditRequest{Path: "blob", Edits: []textedit.Edit{{Old: "a", New: "b"}}}); err == nil || !strings.Contains(err.Error(), ErrNotText.Error()) {
		t.Fatalf("binary err = %v", err)
	}
}

// Paths starting with ~ name the home directory, as they do in the agent's
// shell.
func TestPathsWithTildeAreUnderTheHomeDirectory(t *testing.T) {
	client, home := newTestClient(t)
	written, err := client.WriteFile(t.Context(), WriteRequest{Path: "~/bin/fetch.sh", Content: "RATE=2M\n"})
	if err != nil || written.Path != filepath.Join(home, "bin", "fetch.sh") {
		t.Fatalf("write = %+v, %v", written, err)
	}
	read, err := client.ReadFile(t.Context(), ReadRequest{Path: "~/bin/fetch.sh", MaxBytes: 100})
	if err != nil || read.Content != "RATE=2M\n" {
		t.Fatalf("read = %+v, %v", read, err)
	}
	edited, err := client.EditFile(t.Context(), EditRequest{Path: "~/bin/fetch.sh", Edits: []textedit.Edit{{Old: "2M", New: "5M"}}})
	if err != nil || edited.Path != written.Path {
		t.Fatalf("edit = %+v, %v", edited, err)
	}
	if st, err := client.Stat(t.Context(), "~"); err != nil || st.Path != home || !st.Dir {
		t.Fatalf("stat ~ = %+v, %v", st, err)
	}
	job, err := client.StartJob(t.Context(), JobStartRequest{Command: "pwd", Dir: "~/bin", WaitSeconds: 10, MaxOutputBytes: 1000})
	if err != nil || job.Job.Dir != filepath.Join(home, "bin") || job.Job.State != JobExited || *job.Job.ExitCode != 0 {
		t.Fatalf("job in ~/bin = %+v, %v", job, err)
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
