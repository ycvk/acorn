package tools

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/workstation"
)

const workstationTestToken = "0123456789abcdef0123456789abcdef"

func workstationSpecs(t *testing.T, timeout int) map[string]core.ToolSpec {
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
	specs, err := BuildWorkstationToolSpecs(WorkstationToolsConfig{Service: client, CommandTimeoutSeconds: timeout, MaxOutputBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]core.ToolSpec{}
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	return byName
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
