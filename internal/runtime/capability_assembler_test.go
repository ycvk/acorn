package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// TestBuildWebToolsConfigDisablesUnconfiguredSearch verifies that web_search
// carries a config-named disabled reason until its key is set.
func TestBuildWebToolsConfigDisablesUnconfiguredSearch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebAccess.Search.APIKey = " "
	webCfg, err := buildWebToolsConfig(RuntimeDeps{Config: cfg})
	if err != nil {
		t.Fatalf("buildWebToolsConfig: %v", err)
	}
	if webCfg.Fetch == nil {
		t.Fatal("web fetch service is missing")
	}
	if webCfg.Search != nil || webCfg.SearchDisabledReason != "web_access.search.api_key is not configured" {
		t.Fatalf("search = %v, reason = %q", webCfg.Search, webCfg.SearchDisabledReason)
	}
}

func TestBuildWebToolsConfigEnablesConfiguredSearch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebAccess.Search.APIKey = "tvly-test"
	webCfg, err := buildWebToolsConfig(RuntimeDeps{Config: cfg})
	if err != nil {
		t.Fatalf("buildWebToolsConfig: %v", err)
	}
	if webCfg.Search == nil || webCfg.SearchDisabledReason != "" {
		t.Fatalf("search = %v, reason = %q, want enabled", webCfg.Search, webCfg.SearchDisabledReason)
	}
}
