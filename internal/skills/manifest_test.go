package skills

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

const fetchSkill = `---
name: fetch
description: Fetch a web page as markdown through the fetch MCP server.
metadata:
  acorn-requires-tools: mcp__fetch__fetch
---
Call mcp__fetch__fetch with the URL.
`

const fetchManifest = "mcp:\n  command: uvx\n  args: [mcp-server-fetch, --user-agent, \"Acorn bot\"]\n  secrets: [FETCH_TOKEN]\n"

func TestParseManifestIsStrictAndDeclaresAnMCPServer(t *testing.T) {
	m, err := ParseManifest([]byte(fetchManifest))
	if err != nil || m.MCP == nil || m.MCP.Command != "uvx" || !slices.Equal(m.MCP.Args, []string{"mcp-server-fetch", "--user-agent", "Acorn bot"}) || !slices.Equal(m.MCP.Secrets, []string{"FETCH_TOKEN"}) {
		t.Fatalf("manifest = %+v, %v", m.MCP, err)
	}
	if got := m.MCP.CommandLine(); got != "uvx mcp-server-fetch --user-agent 'Acorn bot'" {
		t.Fatalf("command line = %q", got)
	}
	for name, content := range map[string]string{
		"empty":            "",
		"no mcp":           "mcp: null\n",
		"unknown field":    "mcp:\n  command: uvx\nversion: 1\n",
		"unknown mcp key":  "mcp:\n  command: uvx\n  env: {A: b}\n",
		"triggers":         "mcp:\n  command: uvx\ntriggers:\n  - name: price\n",
		"no command":       "mcp:\n  args: [x]\n",
		"blank command":    "mcp:\n  command: \"  \"\n",
		"lowercase secret": "mcp:\n  command: uvx\n  secrets: [token]\n",
		"repeated secret":  "mcp:\n  command: uvx\n  secrets: [A, A]\n",
		"two documents":    "mcp:\n  command: uvx\n---\nmcp:\n  command: npx\n",
		"not a mapping":    "- uvx\n",
	} {
		if _, err := ParseManifest([]byte(content)); err == nil {
			t.Errorf("%s: parsed %q", name, content)
		}
	}
}

func TestPackagesWithAManifestAreCheckedOnEveryWrite(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t, "github")
	bad := map[string]string{ManifestFileName: "mcp:\n  command: uvx\nextra: true\n"}
	if _, err := library.Write(ctx, WriteRequest{Markdown: fetchSkill, Put: bad}); err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("skill_write with a bad manifest err = %v", err)
	}
	entries := []core.SkillFile{{Path: "SKILL.md", Content: []byte(fetchSkill)}, {Path: ManifestFileName, Content: []byte(bad[ManifestFileName])}}
	if _, err := library.Stage(ctx, entries); err == nil {
		t.Fatal("staged a package with a bad manifest")
	}
	if _, err := library.Import(ctx, entries, false); err == nil {
		t.Fatal("imported a package with a bad manifest")
	}
	if len(store.writes) != 0 {
		t.Fatalf("a bad manifest was stored: %+v", store.writes)
	}

	written, err := library.Write(ctx, WriteRequest{Markdown: fetchSkill, Put: map[string]string{ManifestFileName: fetchManifest}})
	if err != nil || written.Revision != 1 {
		t.Fatalf("write = %+v, %v", written, err)
	}
	scan, err := library.Scan(ctx)
	if err != nil || len(scan.Skills) != 1 || scan.Skills[0].MCP == nil || scan.Skills[0].MCP.Command != "uvx" {
		t.Fatalf("scan = %+v, %v", scan, err)
	}
	// Editing other files keeps the manifest of the base revision.
	if _, err := library.Write(ctx, WriteRequest{Markdown: fetchSkill, Put: map[string]string{"references/usage.md": "x"}, BaseRevision: 1}); err != nil {
		t.Fatal(err)
	}
	if scan, err := library.Scan(ctx); err != nil || scan.Skills[0].MCP == nil {
		t.Fatalf("scan after edit = %+v, %v", scan, err)
	}

	github := strings.Replace(fetchSkill, "name: fetch", "name: github", 1)
	if _, err := library.Write(ctx, WriteRequest{Markdown: github, Put: map[string]string{ManifestFileName: fetchManifest}}); err == nil || !strings.Contains(err.Error(), "mcp.providers") {
		t.Fatalf("a package named like a provider err = %v", err)
	}
	githubEntries := []core.SkillFile{{Path: "SKILL.md", Content: []byte(github)}, {Path: ManifestFileName, Content: []byte(fetchManifest)}}
	if _, err := library.Stage(ctx, githubEntries); err == nil || !strings.Contains(err.Error(), "mcp.providers") {
		t.Fatalf("staging a package named like a provider err = %v", err)
	}
	if _, err := library.Import(ctx, githubEntries, false); err == nil || !strings.Contains(err.Error(), "mcp.providers") {
		t.Fatalf("installing a package named like a provider err = %v", err)
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: github}); err != nil {
		t.Fatalf("a skill without a server may share a provider's name: %v", err)
	}
}

func TestImportRechecksProvidersAddedAfterStaging(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	staged, err := library.Stage(ctx, []core.SkillFile{{Path: "SKILL.md", Content: []byte(fetchSkill)}, {Path: ManifestFileName, Content: []byte(fetchManifest)}})
	if err != nil {
		t.Fatal(err)
	}
	later, err := NewLibrary(store, library.clock, []string{"fetch"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := later.ImportStaged(ctx, "fetch", staged.SHA256, 0, "run-1"); err == nil || !strings.Contains(err.Error(), "mcp.providers") {
		t.Fatalf("import after the provider was configured err = %v", err)
	}
	if _, err := library.ImportStaged(ctx, "fetch", staged.SHA256, 0, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := library.CheckProviders(ctx); err != nil {
		t.Fatalf("no conflict yet: %v", err)
	}
	if err := later.CheckProviders(ctx); err == nil || !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("a provider added after the skill must stop the start: %v", err)
	}
	if _, err := later.Retire(ctx, "fetch", 1, "run-2"); err != nil {
		t.Fatal(err)
	}
	if err := later.CheckProviders(ctx); err != nil {
		t.Fatalf("a retired skill does not conflict: %v", err)
	}
}

func TestSecretsMustBeDeclaredByTheSkillsServer(t *testing.T) {
	ctx := context.Background()
	library, store := newTestLibrary(t)
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", "v"); !errors.Is(err, core.ErrSkillNotFound) {
		t.Fatalf("unknown skill err = %v", err)
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: ordersSkill}); err != nil {
		t.Fatal(err)
	}
	if err := library.SetSecret(ctx, "shop-orders", "FETCH_TOKEN", "v"); err == nil {
		t.Fatal("set a secret of a skill without a server")
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: fetchSkill, Put: map[string]string{ManifestFileName: fetchManifest}}); err != nil {
		t.Fatal(err)
	}
	if err := library.SetSecret(ctx, "fetch", "OTHER", "v"); err == nil || !strings.Contains(err.Error(), "FETCH_TOKEN") {
		t.Fatalf("undeclared secret err = %v", err)
	}
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", ""); err == nil {
		t.Fatal("set an empty secret")
	}
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if store.secrets["fetch"]["FETCH_TOKEN"] != "s3cret" {
		t.Fatalf("secrets = %v", store.secrets)
	}
}

type fakeServerHost struct {
	runs   []MCPServerRun
	failed map[string]string
	err    error
}

func (h *fakeServerHost) RunMCPServers(_ context.Context, servers []MCPServerRun) (map[string]string, error) {
	h.runs = servers
	return h.failed, h.err
}

func TestServeRunsTheServersOfEligibleSkillsWithTheirSecrets(t *testing.T) {
	ctx := context.Background()
	library, _ := newTestLibrary(t)
	for name, manifest := range map[string]string{
		"fetch":  fetchManifest,
		"broken": "mcp:\n  command: nonexistent\n",
		"locked": "mcp:\n  command: npx\n  secrets: [API_KEY, API_SECRET]\n",
	} {
		markdown := strings.ReplaceAll(fetchSkill, "fetch", name)
		if _, err := library.Write(ctx, WriteRequest{Markdown: markdown, Put: map[string]string{ManifestFileName: manifest}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if err := library.SetSecret(ctx, "locked", "API_KEY", "k"); err != nil {
		t.Fatal(err)
	}
	scan, err := library.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Each skill requires its own server's tool, which is not in the run yet.
	snapshot := BuildSnapshot(*scan, EligibilityContext{AvailableTools: []string{"files"}})
	for _, view := range snapshot.Skills {
		if !view.Eligible {
			t.Fatalf("own tools are checked once the server serves: %+v", view)
		}
	}
	host := &fakeServerHost{failed: map[string]string{"broken": "exit status 127"}}
	served, err := library.Serve(ctx, host, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(host.runs) != 2 || host.runs[0].Name != "broken" || host.runs[1].Name != "fetch" || !maps.Equal(host.runs[1].Env, map[string]string{"FETCH_TOKEN": "s3cret"}) || len(host.runs[0].Env) != 0 {
		t.Fatalf("runs = %+v", host.runs)
	}
	if host.runs[1].Hash != snapshot.Skills[1].Hash() {
		t.Fatalf("hash = %s, want the synced package's %s", host.runs[1].Hash, snapshot.Skills[1].Hash())
	}
	want := map[string]string{"broken": "mcp_server_failed:exit status 127", "fetch": "", "locked": "missing_secrets:API_SECRET"}
	for _, view := range served.Skills {
		reason := strings.Join(view.DisabledReasons, ";")
		if reason != want[view.Name] || view.Eligible != (reason == "") {
			t.Fatalf("%s: eligible=%v reasons=%q", view.Name, view.Eligible, reason)
		}
	}
	data, err := json.Marshal(served)
	if err != nil || strings.Contains(string(data), "s3cret") {
		t.Fatalf("a secret's value is in the snapshot: %s", data)
	}

	host = &fakeServerHost{err: errors.New("workstation unreachable")}
	if _, err := library.Serve(ctx, host, snapshot); err == nil {
		t.Fatal("serve hid the host's failure")
	}
	host = &fakeServerHost{}
	if _, err := library.Serve(ctx, host, Snapshot{}); err != nil || host.runs == nil || len(host.runs) != 0 {
		t.Fatalf("with no servers the host still stops the others: %+v, %v", host.runs, err)
	}
}
