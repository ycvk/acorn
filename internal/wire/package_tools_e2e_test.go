package wire

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/workstation"
)

const packageSecret = "fixture-secret-0b5e"

func requestToolNames(request map[string]any) []string {
	raw, _ := request["tools"].([]any)
	var names []string
	for _, item := range raw {
		tool, _ := item.(map[string]any)
		function, _ := tool["function"].(map[string]any)
		if name, ok := function["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// TestPackageToolsComeWithTheirSkill installs a package whose MCP server
// needs a secret: the skill and its tools stay out of runs until the owner
// sets the secret, then a call to the package's tool waits for approval and
// runs on the workstation, and retiring the skill takes the tools away and
// stops the server. The secret's value never reaches the model or the
// approval.
func TestPackageToolsComeWithTheirSkill(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mcp-fixture")
	if out, err := exec.Command("go", "build", "-o", binary, "../workstation/testdata/mcp_server").CombinedOutput(); err != nil {
		t.Fatalf("build MCP fixture: %v\n%s", err, out)
	}
	home := t.TempDir()
	ws, err := workstation.NewServer(workstation.ServerConfig{Home: home, Token: fileTestToken, Supervisor: workstation.NewSupervisor(&strings.Builder{})})
	if err != nil {
		t.Fatal(err)
	}
	wsServer := httptest.NewServer(ws.Handler())
	defer wsServer.Close()
	defer ws.Close()
	provider := &fakeOpenAI{replies: []string{
		textReply("还没有这个工具"),
		toolCallChunk("call_echo", "mcp__fixture__echo", map[string]string{"text": "你好"}),
		textReply("工具说：你好"),
		textReply("工具已经不在了"),
	}}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, fmt.Sprintf("workstation:\n  url: %s\n  token: %s\n", wsServer.URL, fileTestToken))
	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("container: %v", err)
	}
	defer c.Close()

	description := "Echo text through the fixture MCP server."
	skill, err := c.Skills().Import(ctx, []core.SkillFile{
		{Path: "SKILL.md", Content: []byte("---\nname: fixture\ndescription: " + description + "\nmetadata:\n  acorn-requires-tools: mcp__fixture__echo\n---\nCall mcp__fixture__echo.\n")},
		{Path: skills.ManifestFileName, Content: []byte("mcp:\n  command: " + binary + "\nsecrets: [FIXTURE_TOKEN]\n")},
		{Path: "tools.txt", Content: []byte("echo\n")},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	status, err := c.PackageToolsStatus(ctx)
	if err != nil || len(status) != 1 || status[0].State != "missing_secrets" || !slices.Equal(status[0].MissingSecrets, []string{"FIXTURE_TOKEN"}) {
		t.Fatalf("doctor before the secret = %+v, %v", status, err)
	}
	thread, err := c.threads.CreateThread(ctx, "包的工具")
	if err != nil {
		t.Fatal(err)
	}

	first, err := c.runs.CreateRun(ctx, thread.ID, "", "用 fixture 回显你好", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, c, first.ID, "completed")
	body, err := json.Marshal(provider.request(0))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(requestToolNames(provider.request(0)), func(n string) bool { return strings.HasPrefix(n, "mcp__fixture__") }) || strings.Contains(string(body), description) {
		t.Fatalf("a skill missing its secret reached the model: %v", requestToolNames(provider.request(0)))
	}

	if err := c.Skills().SetSecret(ctx, "fixture", "FIXTURE_TOKEN", packageSecret); err != nil {
		t.Fatal(err)
	}
	second, err := c.runs.CreateRun(ctx, thread.ID, "", "再试一次", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, c, second.ID, "interrupted")
	if names := requestToolNames(provider.request(1)); !slices.Contains(names, "mcp__fixture__echo") || !slices.Contains(names, "mcp__fixture__token") {
		t.Fatalf("tools with the secret set = %v", names)
	}
	if body, err := json.Marshal(provider.request(1)); err != nil || !strings.Contains(string(body), description) {
		t.Fatalf("the skill is not listed: %v", err)
	}
	actions, err := c.pendingAction.List(ctx, 10)
	if err != nil || len(actions) != 1 || actions[0].ToolName != "mcp__fixture__echo" {
		t.Fatalf("pending actions = %+v, %v", actions, err)
	}
	detail, err := c.pendingAction.Get(ctx, actions[0].ActionID)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := json.Marshal(detail); err != nil || strings.Contains(string(data), packageSecret) {
		t.Fatalf("the secret reached the approval: %s", data)
	}
	decideOverHTTP(t, c, actions[0].ActionID, "accept")
	waitRunStatus(t, c, second.ID, "completed")
	if content, ok := toolMessageFor(provider.request(2), "call_echo"); !ok || !strings.Contains(content, "echo: 你好") {
		t.Fatalf("tool result = %q", content)
	}
	if status, err := c.PackageToolsStatus(ctx); err != nil || status[0].State != "ready" {
		t.Fatalf("doctor while it serves = %+v, %v", status, err)
	}

	if _, err := c.skillLibrary.Retire(ctx, "fixture", skill.Revision, ""); err != nil {
		t.Fatal(err)
	}
	third, err := c.runs.CreateRun(ctx, thread.ID, "", "还在吗", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, c, third.ID, "completed")
	if slices.ContainsFunc(requestToolNames(provider.request(3)), func(n string) bool { return strings.HasPrefix(n, "mcp__fixture__") }) {
		t.Fatalf("a retired skill's tools reached the model: %v", requestToolNames(provider.request(3)))
	}
	client, err := buildWorkstationClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		running, err := client.MCPServers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(running) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the retired skill's server still runs: %+v", running)
		}
		time.Sleep(20 * time.Millisecond)
	}

	for i := range provider.requestCount() {
		body, err := json.Marshal(provider.request(i))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), packageSecret) {
			t.Fatalf("model request %d holds the secret", i)
		}
	}
}

func TestStartFailsWhenAProviderTakesAPackagesName(t *testing.T) {
	provider := &fakeOpenAI{}
	server := httptest.NewServer(provider)
	defer server.Close()
	cfg := writeTestConfig(t, server.URL, "")
	ctx := context.Background()
	c, err := NewContainer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Skills().Import(ctx, []core.SkillFile{
		{Path: "SKILL.md", Content: []byte("---\nname: github\ndescription: Read issues.\n---\nUse the tools.\n")},
		{Path: skills.ManifestFileName, Content: []byte("mcp:\n  command: npx\n")},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.MCP.Providers = []config.MCPProviderConfig{{Enabled: true, Name: "github", Transport: "stdio", Command: "github-mcp-server"}}
	if _, err := NewContainer(ctx, cfg); err == nil || !strings.Contains(err.Error(), "mcp.providers") {
		t.Fatalf("start with a provider named like a package = %v", err)
	}
}
