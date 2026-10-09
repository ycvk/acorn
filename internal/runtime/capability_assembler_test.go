package runtime

import (
	"testing"

	"github.com/ycvk/acorn/internal/config"
)

// TestBuildWebToolsConfigDisablesUnconfiguredTools verifies that web_search
// and browser carry a config-named disabled reason until their config is set.
func TestBuildWebToolsConfigDisablesUnconfiguredTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebAccess.Search.APIKey = " "
	cfg.Browser.ExecutablePath = ""
	webCfg, closers, err := buildWebToolsConfig(RuntimeDeps{Config: cfg})
	if err != nil {
		t.Fatalf("buildWebToolsConfig: %v", err)
	}
	if webCfg.Fetch == nil {
		t.Fatal("web fetch service is missing")
	}
	if webCfg.Search != nil || webCfg.SearchDisabledReason != "web_access.search.api_key is not configured" {
		t.Fatalf("search = %v, reason = %q", webCfg.Search, webCfg.SearchDisabledReason)
	}
	if webCfg.Browser != nil || webCfg.BrowserDisabledReason != "browser.executable_path is not configured" {
		t.Fatalf("browser = %v, reason = %q", webCfg.Browser, webCfg.BrowserDisabledReason)
	}
	if len(closers) != 0 {
		t.Fatalf("closers = %d, want 0", len(closers))
	}
}

func TestBuildWebToolsConfigEnablesConfiguredTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WebAccess.Search.APIKey = "tvly-test"
	cfg.Browser.ExecutablePath = "/usr/bin/chromium"
	webCfg, closers, err := buildWebToolsConfig(RuntimeDeps{Config: cfg})
	if err != nil {
		t.Fatalf("buildWebToolsConfig: %v", err)
	}
	if webCfg.Search == nil || webCfg.SearchDisabledReason != "" {
		t.Fatalf("search = %v, reason = %q, want enabled", webCfg.Search, webCfg.SearchDisabledReason)
	}
	if webCfg.Browser == nil || webCfg.BrowserDisabledReason != "" {
		t.Fatalf("browser = %v, reason = %q, want enabled", webCfg.Browser, webCfg.BrowserDisabledReason)
	}
	if len(closers) != 1 {
		t.Fatalf("closers = %d, want 1 (browser)", len(closers))
	}
}
