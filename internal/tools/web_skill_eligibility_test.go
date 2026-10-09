package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
)

// The seed web research skill must stay available when only web_fetch is
// configured, which is the default self-hosted setup.
func TestSeedWebResearchSkillEligibleWithOnlyWebFetch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	appCfg := config.DefaultConfig()
	appCfg.Tools.Workspace.RootDir = repoRoot
	appCfg.Runtime.StorageDir = t.TempDir()
	scan, err := skills.NewLoader(appCfg).ScanSkills(context.Background())
	if err != nil {
		t.Fatalf("ScanSkills: %v", err)
	}
	snapshot, err := skills.BuildSnapshot(*scan, EligibilityContext(catalog, nil))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	for _, view := range snapshot.Skills {
		if view.ID != "skill.web.browser.research" {
			continue
		}
		if !view.Eligible {
			t.Fatalf("web research skill ineligible: %v", view.DisabledReasons)
		}
		return
	}
	t.Fatal("seed web research skill not found")
}
