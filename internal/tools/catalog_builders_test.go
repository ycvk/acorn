package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	corestore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/webaccess"
)

func testWebToolsConfig(t *testing.T) WebToolsConfig {
	t.Helper()
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), newToolArtifactStore())
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	fetch, err := webaccess.NewFetchService(webaccess.FetchConfig{UserAgent: "acorn-test", Timeout: time.Second, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatalf("webaccess.NewFetchService: %v", err)
	}
	search, err := webaccess.NewSearchService(webaccess.SearchConfig{APIKey: "key", Timeout: time.Second, MaxResults: 1, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatalf("webaccess.NewSearchService: %v", err)
	}
	browser, err := NewService(Config{ExecutablePath: "/usr/bin/chromium", Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return WebToolsConfig{
		ArtifactService: artifactService,
		ArtifactContext: fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_1"},
		Fetch:           fetch,
		Search:          search,
		Browser:         browser,
	}
}

func webSpecsByName(t *testing.T, cfg WebToolsConfig) map[string]core.ToolSpec {
	t.Helper()
	specs, err := BuildWebToolSpecs(cfg)
	if err != nil {
		t.Fatalf("BuildWebToolSpecs: %v", err)
	}
	byName := make(map[string]core.ToolSpec, len(specs))
	for _, spec := range specs {
		byName[spec.Name] = spec
	}
	return byName
}

func TestBuildWebToolSpecsEnablesConfiguredTools(t *testing.T) {
	byName := webSpecsByName(t, testWebToolsConfig(t))
	for _, name := range []string{"web_fetch", "web_search", "browser"} {
		spec, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from web tool specs", name)
		}
		if !spec.Enabled() || spec.Tool == nil {
			t.Fatalf("%s: enabled=%v tool=%v, want enabled with a tool", name, spec.Enabled(), spec.Tool)
		}
		if spec.Loading.Mode != core.ToolLoadingModeDeferred {
			t.Fatalf("%s loading = %s, want deferred", name, spec.Loading.Mode)
		}
	}
}

// TestBuildWebToolSpecsDisablesUnconfiguredOptionalTools verifies that an
// absent optional service yields a disabled spec carrying its reason, which the
// catalog keeps visible but out of the enabled (model-facing) set.
func TestBuildWebToolSpecsDisablesUnconfiguredOptionalTools(t *testing.T) {
	cfg := testWebToolsConfig(t)
	cfg.Search = nil
	cfg.SearchDisabledReason = "web_access.search.api_key is not configured"
	cfg.Browser = nil
	cfg.BrowserDisabledReason = "browser.executable_path is not configured"
	specs, err := BuildWebToolSpecs(cfg)
	if err != nil {
		t.Fatalf("BuildWebToolSpecs: %v", err)
	}
	catalog, err := NewCatalog(context.Background(), specs)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	for name, reason := range map[string]string{
		"web_search": cfg.SearchDisabledReason,
		"browser":    cfg.BrowserDisabledReason,
	} {
		spec, ok := findSpec(catalog.Specs(), name)
		if !ok {
			t.Fatalf("%s missing from catalog", name)
		}
		if spec.Health.State != core.HealthStateDisabled || spec.Health.Reason != reason {
			t.Fatalf("%s health = %+v, want disabled with %q", name, spec.Health, reason)
		}
		if spec.Tool != nil {
			t.Fatalf("%s: disabled spec carries a tool", name)
		}
	}
	enabled := catalog.EnabledSpecs()
	if len(enabled) != 1 || enabled[0].Name != "web_fetch" {
		t.Fatalf("enabled specs = %+v, want only web_fetch", enabled)
	}
}

func TestBuildWebToolSpecsRequiresDisabledReason(t *testing.T) {
	cases := []struct {
		tool  string
		clear func(*WebToolsConfig)
	}{
		{"web_search", func(c *WebToolsConfig) { c.Search = nil }},
		{"browser", func(c *WebToolsConfig) { c.Browser = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			cfg := testWebToolsConfig(t)
			tc.clear(&cfg)
			_, err := BuildWebToolSpecs(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.tool) || !strings.Contains(err.Error(), "no disabled reason") {
				t.Fatalf("BuildWebToolSpecs error = %v, want %s without disabled reason", err, tc.tool)
			}
		})
	}
}

func TestBuildWebToolSpecsRequiresFetchService(t *testing.T) {
	cfg := testWebToolsConfig(t)
	cfg.Fetch = nil
	_, err := BuildWebToolSpecs(cfg)
	if err == nil || !strings.Contains(err.Error(), "web fetch service is required") {
		t.Fatalf("BuildWebToolSpecs error = %v, want web fetch service is required", err)
	}
}

func findSpec(specs []core.ToolSpec, name string) (core.ToolSpec, bool) {
	for _, spec := range specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return core.ToolSpec{}, false
}
