package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/workstation"
)

const workstationTestToken = "0123456789abcdef0123456789abcdef"

func workstationSpecs(t *testing.T, timeout int) map[string]core.ToolSpec {
	t.Helper()
	specs, _ := workstationSpecsWithFiles(t, timeout, 1<<20)
	return specs
}

// workstationSpecsWithFiles builds the workstation tools on a real
// workstation server and store; send_file runs as run_send in thread
// thread_send.
func workstationSpecsWithFiles(t *testing.T, timeout int, maxSend int64) (map[string]core.ToolSpec, *store.Store) {
	t.Helper()
	server, err := workstation.NewServer(workstation.ServerConfig{Home: t.TempDir(), Token: workstationTestToken})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)
	client, err := workstation.NewClient(ts.URL, workstationTestToken, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.CreateSession(context.Background(), "thread_send", "files"); err != nil {
		t.Fatal(err)
	}
	specs, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{
		Service: client, Files: db, Context: fixedArtifactContext{runID: "run_send", sessionID: "thread_send"},
		CommandTimeoutSeconds: timeout, MaxOutputBytes: 4096, MaxSendBytes: maxSend,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]core.ToolSpec{}
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	return byName, db
}

func invoke(t *testing.T, spec core.ToolSpec, args string) (string, error) {
	t.Helper()
	return spec.Tool.(einotool.InvokableTool).InvokableRun(context.Background(), args)
}

func TestWorkstationToolsRunCommandsAndFiles(t *testing.T) {
	specs := workstationSpecs(t, 30)
	for _, name := range []string{"shell", "files"} {
		spec := specs[name]
		if !spec.Enabled() || spec.Loading.Mode != core.ToolLoadingModeDeferred {
			t.Fatalf("%s enabled=%v loading=%s", name, spec.Enabled(), spec.Loading.Mode)
		}
	}
	if _, err := invoke(t, specs["files"], `{"action":"write","path":"data/n.txt","content":"3\n4\n"}`); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, specs["shell"], `{"command":"awk '{s+=$1} END {print s}' n.txt","dir":"data"}`)
	if err != nil {
		t.Fatal(err)
	}
	var result workstation.ExecResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || strings.TrimSpace(result.Stdout) != "7" || result.ExitCode != 0 {
		t.Fatalf("shell = %s err = %v", out, err)
	}
	out, err = invoke(t, specs["files"], `{"action":"read","path":"data/n.txt"}`)
	if err != nil || !strings.Contains(out, `"content":"3\n4\n"`) {
		t.Fatalf("read = %s err = %v", out, err)
	}
	if _, err := invoke(t, specs["files"], `{"action":"delete","path":"data/n.txt"}`); err == nil || !strings.Contains(err.Error(), "read or write") {
		t.Fatalf("delete err = %v", err)
	}
}

func TestShellCannotRaiseTheConfiguredTimeout(t *testing.T) {
	specs := workstationSpecs(t, 1)
	out, err := invoke(t, specs["shell"], `{"command":"sleep 5","timeout_seconds":600}`)
	if err != nil {
		t.Fatal(err)
	}
	var result workstation.ExecResult
	if err := json.Unmarshal([]byte(out), &result); err != nil || !result.TimedOut {
		t.Fatalf("shell = %s err = %v", out, err)
	}
}

func TestWorkstationToolsDisabledWithoutService(t *testing.T) {
	if _, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{}); err == nil {
		t.Fatal("missing service without a reason must fail")
	}
	specs, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{DisabledReason: "workstation.url is not configured"})
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.Enabled() || spec.Health.Reason != "workstation.url is not configured" {
			t.Fatalf("%s enabled=%v reason=%q", spec.Name, spec.Enabled(), spec.Health.Reason)
		}
	}
}

func TestSendFileHandsTheOwnerAWorkstationFile(t *testing.T) {
	specs, db := workstationSpecsWithFiles(t, 30, 1<<20)
	if spec := specs["send_file"]; !spec.Enabled() || spec.Loading.Mode != core.ToolLoadingModeEager {
		t.Fatalf("send_file enabled=%v loading=%s", spec.Enabled(), spec.Loading.Mode)
	}
	if _, err := invoke(t, specs["shell"], `{"command":"mkdir -p books && printf 'PK\\003\\004book' > books/hongloumeng.epub && head -c 2000000 /dev/zero > big.bin"}`); err != nil {
		t.Fatal(err)
	}
	out, err := invoke(t, specs["send_file"], `{"path":"books/hongloumeng.epub","name":"红楼梦.epub"}`)
	if err != nil {
		t.Fatal(err)
	}
	var sent SendFileOutput
	if err := json.Unmarshal([]byte(out), &sent); err != nil || sent.Name != "红楼梦.epub" || sent.MIMEType != "application/epub+zip" || sent.SizeBytes != 8 {
		t.Fatalf("send_file = %s err = %v", out, err)
	}
	file, content, err := db.OpenThreadFile(context.Background(), "thread_send", sent.FileID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(content)
	_ = content.Close()
	if err != nil || !bytes.Equal(data, []byte("PK\003\004book")) || file.Origin != core.ThreadFileFromAgent || file.RunID != "run_send" {
		t.Fatalf("stored file = %+v %q err = %v", file, data, err)
	}
	if _, err := invoke(t, specs["send_file"], `{"path":"big.bin"}`); err == nil || !strings.Contains(err.Error(), "more than the 1 MiB") {
		t.Fatalf("oversized send err = %v", err)
	}
	if _, err := invoke(t, specs["send_file"], `{"path":"missing.pdf"}`); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing send err = %v", err)
	}
	if files, err := db.ThreadFiles(context.Background(), "thread_send"); err != nil || len(files) != 1 {
		t.Fatalf("thread files = %+v err = %v", files, err)
	}
}
