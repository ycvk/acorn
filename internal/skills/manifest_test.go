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

const fetchManifest = "mcp:\n  command: uvx\n  args: [mcp-server-fetch, --user-agent, \"Acorn bot\"]\nsecrets: [FETCH_TOKEN]\n"

func TestParseManifestIsStrict(t *testing.T) {
	m, err := ParseManifest([]byte(fetchManifest))
	if err != nil || m.MCP == nil || m.MCP.Command != "uvx" || !slices.Equal(m.MCP.Args, []string{"mcp-server-fetch", "--user-agent", "Acorn bot"}) || !slices.Equal(m.Secrets, []string{"FETCH_TOKEN"}) || len(m.Triggers) != 0 {
		t.Fatalf("manifest = %+v, %v", m, err)
	}
	if got := m.MCP.CommandLine(); got != "uvx mcp-server-fetch --user-agent 'Acorn bot'" {
		t.Fatalf("command line = %q", got)
	}
	triggers, err := ParseManifest([]byte(priceManifest))
	want := []Trigger{
		{Name: "price", Schedule: "*/30 * * * *", Run: "scripts/check_price.py --above 100", Deliver: DeliverImmediate},
		{Name: "weekly", Schedule: "0 9 * * 1", Run: "scripts/weekly.sh", Deliver: DeliverDigest},
	}
	if err != nil || triggers.MCP != nil || !slices.Equal(triggers.Triggers, want) || !slices.Equal(triggers.Secrets, []string{"PRICE_TOKEN"}) {
		t.Fatalf("triggers = %+v, %v", triggers, err)
	}
	trigger := func(fields string) string {
		return "triggers:\n  - name: price\n    schedule: \"*/30 * * * *\"\n    run: scripts/check.py\n    deliver: digest\n" + fields
	}
	for name, content := range map[string]string{
		"empty":                 "",
		"nothing":               "secrets: [A]\n",
		"no mcp":                "mcp: null\n",
		"unknown field":         "mcp:\n  command: uvx\nversion: 1\n",
		"unknown mcp key":       "mcp:\n  command: uvx\n  env: {A: b}\n",
		"secrets under mcp":     "mcp:\n  command: uvx\n  secrets: [A]\n",
		"no command":            "mcp:\n  args: [x]\n",
		"blank command":         "mcp:\n  command: \"  \"\n",
		"lowercase secret":      "mcp:\n  command: uvx\nsecrets: [token]\n",
		"repeated secret":       "mcp:\n  command: uvx\nsecrets: [A, A]\n",
		"two documents":         "mcp:\n  command: uvx\n---\nmcp:\n  command: npx\n",
		"not a mapping":         "- uvx\n",
		"webhook":               "triggers:\n  - name: issues\n    webhook: true\n    run: x\n    deliver: digest\n",
		"trigger without name":  "triggers:\n  - schedule: \"* * * * *\"\n    run: x\n    deliver: digest\n",
		"bad trigger name":      "triggers:\n  - name: Price_1\n    schedule: \"* * * * *\"\n    run: x\n    deliver: digest\n",
		"no schedule":           "triggers:\n  - name: price\n    run: x\n    deliver: digest\n",
		"bad schedule":          "triggers:\n  - name: price\n    schedule: \"every hour\"\n    run: x\n    deliver: digest\n",
		"no run":                "triggers:\n  - name: price\n    schedule: \"* * * * *\"\n    deliver: digest\n",
		"no deliver":            "triggers:\n  - name: price\n    schedule: \"* * * * *\"\n    run: x\n",
		"unknown deliver":       "triggers:\n  - name: price\n    schedule: \"* * * * *\"\n    run: x\n    deliver: later\n",
		"repeated trigger name": trigger("  - name: price\n    schedule: \"0 * * * *\"\n    run: y\n    deliver: digest\n"),
	} {
		if _, err := ParseManifest([]byte(content)); err == nil {
			t.Errorf("%s: parsed %q", name, content)
		}
	}
	if _, err := ParseManifest([]byte(trigger(""))); err != nil {
		t.Fatalf("a trigger-only manifest: %v", err)
	}
}

const priceManifest = `triggers:
  - name: price
    schedule: "*/30 * * * *"
    run: scripts/check_price.py --above 100
    deliver: immediate
  - name: weekly
    schedule: "0 9 * * 1"
    run: scripts/weekly.sh
    deliver: digest
secrets: [PRICE_TOKEN]
`

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
	if err := library.SetSecret(ctx, "shop-orders", "FETCH_TOKEN", "v"); !errors.Is(err, core.ErrSkillInvalid) {
		t.Fatalf("set a secret of a skill without a package: err = %v", err)
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: fetchSkill, Put: map[string]string{ManifestFileName: fetchManifest}}); err != nil {
		t.Fatal(err)
	}
	if err := library.SetSecret(ctx, "fetch", "OTHER", "v"); err == nil || !strings.Contains(err.Error(), "FETCH_TOKEN") {
		t.Fatalf("undeclared secret err = %v", err)
	}
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", ""); !errors.Is(err, core.ErrSkillInvalid) {
		t.Fatalf("set an empty secret: err = %v", err)
	}
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", strings.Repeat("x", MaxSecretBytes+1)); !errors.Is(err, core.ErrSkillInvalid) {
		t.Fatalf("set a secret over the limit: err = %v", err)
	}
	if err := library.SetSecret(ctx, "fetch", "FETCH_TOKEN", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if store.secrets["fetch"]["FETCH_TOKEN"] != "s3cret" {
		t.Fatalf("secrets = %v", store.secrets)
	}
	if err := library.ClearSecret(ctx, "fetch", "OTHER"); !errors.Is(err, core.ErrSkillInvalid) {
		t.Fatalf("clear an undeclared secret: err = %v", err)
	}
	if err := library.ClearSecret(ctx, "fetch", "FETCH_TOKEN"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.secrets["fetch"]["FETCH_TOKEN"]; ok {
		t.Fatalf("secrets after clearing = %v", store.secrets)
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
		"locked": "mcp:\n  command: npx\nsecrets: [API_KEY, API_SECRET]\n",
		"quiet":  "triggers:\n  - name: poll\n    schedule: \"0 * * * *\"\n    run: scripts/poll.sh\n    deliver: digest\nsecrets: [POLL_KEY]\n",
		"timer":  "triggers:\n  - name: poll\n    schedule: \"0 * * * *\"\n    run: scripts/poll.sh\n    deliver: digest\n",
	} {
		markdown := strings.ReplaceAll(fetchSkill, "fetch", name)
		if !strings.HasPrefix(manifest, "mcp:") {
			markdown = strings.Replace(markdown, "  acorn-requires-tools: mcp__"+name+"__"+name+"\n", "  note: none\n", 1)
		}
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
	want := map[string]string{"broken": "mcp_server_failed:exit status 127", "fetch": "", "locked": "missing_secrets:API_SECRET", "quiet": "missing_secrets:POLL_KEY", "timer": ""}
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

type fakeTriggerHost struct {
	fakeHost
	runs   []TriggerRun
	result TriggerResult
}

func (h *fakeTriggerHost) RunTrigger(_ context.Context, run TriggerRun) (TriggerResult, error) {
	h.runs = append(h.runs, run)
	return h.result, nil
}

func TestTriggersFireFromTheirPackageWithItsSecrets(t *testing.T) {
	ctx := context.Background()
	library, _ := newTestLibrary(t)
	price := strings.ReplaceAll(strings.Replace(fetchSkill, "  acorn-requires-tools: mcp__fetch__fetch\n", "  note: none\n", 1), "fetch", "price-watch")
	if _, err := library.Write(ctx, WriteRequest{Markdown: price, Put: map[string]string{ManifestFileName: priceManifest, "scripts/check_price.py": "print()"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Write(ctx, WriteRequest{Markdown: fetchSkill, Put: map[string]string{ManifestFileName: fetchManifest}}); err != nil {
		t.Fatal(err)
	}
	triggers, err := library.Triggers(ctx)
	if err != nil || len(triggers) != 2 || triggers[0].ID() != "price-watch/price" || triggers[1].ID() != "price-watch/weekly" || !slices.Equal(triggers[0].Missing, []string{"PRICE_TOKEN"}) {
		t.Fatalf("triggers = %+v, %v", triggers, err)
	}
	host := &fakeTriggerHost{fakeHost: fakeHost{held: map[string]string{}, put: map[string]int{}}, result: TriggerResult{Stdout: `{"key":"price-120","text":"BTC crossed 120k"}` + "\n\n"}}
	runner, err := NewTriggerRunner(library, host, 300, 32768)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Fire(ctx, triggers[0]); !errors.Is(err, ErrMissingSecrets) || len(host.runs) != 0 {
		t.Fatalf("fire without the secret = %v, runs %v", err, host.runs)
	}
	if err := library.SetSecret(ctx, "price-watch", "PRICE_TOKEN", "s3cret"); err != nil {
		t.Fatal(err)
	}
	out, err := runner.Fire(ctx, triggers[0])
	if err != nil || !slices.Equal(out.Events, []Event{{Key: "price-120", Text: "BTC crossed 120k"}}) || len(out.ViewData) != 0 {
		t.Fatalf("output = %+v, %v", out, err)
	}
	run := host.runs[0]
	if run.Skill != "price-watch" || run.Trigger != "price" || run.Hash != triggers[0].Hash || run.Env["PRICE_TOKEN"] != "s3cret" || run.TimeoutSeconds != 300 || run.MaxOutputBytes != 32768 {
		t.Fatalf("run = %+v", run)
	}
	if host.put["price-watch"] != 2 || host.held["price-watch"] != triggers[0].Hash {
		t.Fatalf("the package was not put first: %v", host.put)
	}
	host.put = map[string]int{}
	if _, err := runner.Fire(ctx, triggers[0]); err != nil || len(host.put) != 0 {
		t.Fatalf("a held package was put again: %v, %v", host.put, err)
	}
	for name, result := range map[string]TriggerResult{
		"status 2: Traceback\nValueError: bad price": {ExitCode: 2, Stderr: "Traceback\nValueError: bad price"},
		"timed out":        {TimedOut: true, ExitCode: -1},
		"more than":        {StdoutTruncated: true},
		"neither an event": {Stdout: "price is 120\n"},
	} {
		host.result = result
		if _, err := runner.Fire(ctx, triggers[0]); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestParseOutputReadsEventsAndViewDataOnePerLine(t *testing.T) {
	out, err := ParseOutput(" {\"key\": \"a\", \"text\": \"first\\nline\"}\n\n{\"view_data\": {\"latest\": \"v1.2\"}}\n{\"key\":\"b\",\"text\":\"second\"}\n{\"view_data\":{\"latest\":\"v1.3\"}}")
	if err != nil || !slices.Equal(out.Events, []Event{{Key: "a", Text: "first\nline"}, {Key: "b", Text: "second"}}) {
		t.Fatalf("events = %+v, %v", out.Events, err)
	}
	if len(out.ViewData) != 2 || out.ViewData[0]["latest"] != "v1.2" || out.ViewData[1]["latest"] != "v1.3" {
		t.Fatalf("view data = %+v", out.ViewData)
	}
	if out, err := ParseOutput("\n  \n"); err != nil || len(out.Events) != 0 || len(out.ViewData) != 0 {
		t.Fatalf("no output = %+v, %v", out, err)
	}
	for _, line := range []string{
		`{"key":"a"}`,
		`{"text":"x"}`,
		`{"key":"a","text":"x","extra":1}`,
		`{"key":"a","text":"x"} trailing`,
		`["a","x"]`,
		`{"key":"a","text":"x"}` + "\n" + `{"key":"a","text":"y"}`,
		`{"key":"` + strings.Repeat("k", 257) + `","text":"x"}`,
		`{"view_data":[1,2]}`,
		`{"view_data":null}`,
		`{"view_data":{"a":1},"key":"a"}`,
		`{"view_data":{"a":1}} trailing`,
	} {
		if _, err := ParseOutput(line); err == nil {
			t.Fatalf("parsed %q", line)
		}
	}
}
