package mcpprovider

import (
	"context"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
)

func buildFixtureServer(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve current file")
	}
	fixtureDir := filepath.Join(filepath.Dir(file), "testdata", "fixture_server")
	binary := filepath.Join(t.TempDir(), "fixture-mcp-server")
	run := testCommand(t, "go", "build", "-o", binary, fixtureDir)
	if output, err := run.CombinedOutput(); err != nil {
		t.Fatalf("build fixture server: %v\n%s", err, string(output))
	}
	return binary
}

func testCommand(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	return exec.Command(name, args...)
}

func findToolByName(t *testing.T, tools []einotool.BaseTool, name string) einotool.BaseTool {
	t.Helper()
	for _, item := range tools {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info.Name == name {
			return item
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}
