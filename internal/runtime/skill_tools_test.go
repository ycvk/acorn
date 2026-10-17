package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

type echoArgs struct {
	Text string `json:"text"`
}

// newPackageServer serves an MCP server with the named echo tools over
// streamable HTTP, behind a bearer token.
func newPackageServer(t *testing.T, token string, tools ...string) *httptest.Server {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "package", Version: "1"}, nil)
	for _, name := range tools {
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "Echo."}, func(_ context.Context, _ *mcp.CallToolRequest, args echoArgs) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: name + ": " + args.Text}}}, nil, nil
		})
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

type fakeServeLibrary struct {
	fakeSyncLibrary
	serve func(skills.Snapshot) skills.Snapshot
	err   error
}

func (l *fakeServeLibrary) Serve(_ context.Context, _ skills.ServerHost, snapshot skills.Snapshot) (skills.Snapshot, error) {
	if l.err != nil {
		return skills.Snapshot{}, l.err
	}
	return l.serve(snapshot), nil
}

type fakeSkillServers struct {
	urls      map[string]string
	token     string
	providers []string
}

func (h *fakeSkillServers) RunMCPServers(context.Context, []skills.MCPServerRun) (map[string]string, error) {
	return nil, nil
}

func (h *fakeSkillServers) MCPProvider(name string) core.ProviderConfig {
	h.providers = append(h.providers, name)
	return core.ProviderConfig{Name: name, Enabled: true, Transport: "streamable_http", URL: h.urls[name], Headers: map[string]string{"Authorization": "Bearer " + h.token}, StartupTimeoutSeconds: 5}
}

func packageView(name string, eligible bool, requires ...string) skills.View {
	return skills.View{Spec: skills.Spec{Name: name, Description: name, RequiresTools: requires, MCP: &skills.MCPServer{Command: "uvx"}, Files: []core.SkillFileInfo{{Path: skills.ManifestFileName}}}, Eligible: eligible}
}

func TestSkillToolsJoinOnlyRunsWhoseSkillServes(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	fetch := newPackageServer(t, token, "fetch")
	partial := newPackageServer(t, token, "list")
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	host := &fakeSkillServers{token: token, urls: map[string]string{"fetch": fetch.URL, "partial": partial.URL, "down": down.URL}}
	library := &fakeServeLibrary{serve: func(s skills.Snapshot) skills.Snapshot {
		out := skills.CopySnapshot(s)
		for i, view := range out.Skills {
			if view.Name == "locked" {
				out.Skills[i] = skills.Disable(view, "missing_secrets:API_KEY")
			}
		}
		return out
	}}
	snapshot := skills.Snapshot{Skills: []skills.View{
		packageView("fetch", true, "mcp__fetch__fetch"),
		packageView("locked", true),
		packageView("down", true),
		packageView("partial", true, "mcp__partial__list", "mcp__partial__create"),
		packageView("retired-elsewhere", false),
		{Spec: skills.Spec{Name: "plain", Description: "plain"}, Eligible: true},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deps := RuntimeDeps{Config: config.DefaultConfig(), Skills: library, SkillServers: host}
	served, specs := serveSkillTools(ctx, deps, snapshot)

	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
		if spec.Kind != core.ToolKindMCP || spec.Category != core.ToolCategoryIntegration {
			t.Fatalf("spec %s = %+v", spec.Name, spec.ToolContract)
		}
	}
	if !slices.Equal(names, []string{"mcp__fetch__fetch"}) {
		t.Fatalf("tools = %v", names)
	}
	if !slices.Equal(host.providers, []string{"fetch", "down", "partial"}) {
		t.Fatalf("connected to %v; a skill out of the run must not connect", host.providers)
	}
	want := map[string]string{
		"fetch":             "",
		"locked":            "missing_secrets:API_KEY",
		"down":              "mcp_connect_failed:",
		"partial":           "missing_required_tools:mcp__partial__create",
		"retired-elsewhere": "",
		"plain":             "",
	}
	for _, view := range served.Skills {
		reasons := strings.Join(view.DisabledReasons, ";")
		if !strings.HasPrefix(reasons, want[view.Name]) || (want[view.Name] == "") != (reasons == "") {
			t.Fatalf("%s reasons = %q", view.Name, reasons)
		}
		if view.Name == "fetch" && !view.Eligible || view.Name == "plain" && !view.Eligible {
			t.Fatalf("%s left the run", view.Name)
		}
	}
	result, err := invokeSpec(ctx, specs[0], `{"text":"hi"}`)
	if err != nil || !strings.Contains(result, "fetch: hi") {
		t.Fatalf("call = %q, %v", result, err)
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := invokeSpec(context.Background(), specs[0], `{"text":"again"}`); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the connection outlived the run")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func invokeSpec(ctx context.Context, spec core.ToolSpec, args string) (string, error) {
	return spec.Tool.(einotool.InvokableTool).InvokableRun(ctx, args)
}

func TestSkillsWithServersLeaveTheRunWhenTheWorkstationCannotServe(t *testing.T) {
	library := &fakeServeLibrary{err: context.DeadlineExceeded}
	snapshot := skills.Snapshot{Skills: []skills.View{packageView("fetch", true), {Spec: skills.Spec{Name: "plain", Description: "plain"}, Eligible: true}}}
	served, specs := serveSkillTools(t.Context(), RuntimeDeps{Config: config.DefaultConfig(), Skills: library, SkillServers: &fakeSkillServers{}}, snapshot)
	if len(specs) != 0 || served.Skills[0].Eligible || !slices.Equal(served.Skills[0].DisabledReasons, []string{"mcp_unavailable"}) || !served.Skills[1].Eligible {
		t.Fatalf("served = %+v specs = %d", served.Skills, len(specs))
	}
	if out, specs := serveSkillTools(t.Context(), RuntimeDeps{Skills: library}, snapshot); len(specs) != 0 || !out.Skills[0].Eligible {
		t.Fatalf("without a workstation the snapshot is left as it is: %+v", out.Skills)
	}
}

func TestProviderNamespacesAreTheToolNamespaces(t *testing.T) {
	got := ProviderNamespaces([]core.ProviderConfig{{Name: "github"}, {Name: "my_files"}, {Name: " "}})
	if !slices.Equal(got, []string{"github", "my-files"}) {
		t.Fatalf("namespaces = %v", got)
	}
}
