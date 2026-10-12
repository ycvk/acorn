package tools

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/skills"
)

// The seed web research skill must stay available when only web_fetch is
// configured, which is the default self-hosted setup.
func TestSeedWebResearchSkillEligibleWithOnlyWebFetch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := testWebToolsConfig(t)
	cfg.Search = nil
	cfg.SearchDisabledReason = "web_access.search.api_key is not configured"
	specs, err := BuildWebToolSpecs(cfg)
	if err != nil {
		t.Fatalf("BuildWebToolSpecs: %v", err)
	}
	catalog, err := NewCatalog(context.Background(), specs)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	builtIn, err := skills.BuiltIn()
	if err != nil {
		t.Fatalf("built-in skills: %v", err)
	}
	snapshot, err := skills.BuildSnapshot(skills.ScanResult{Skills: builtIn}, EligibilityContext(catalog, nil))
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	eligible := map[string]bool{}
	for _, view := range snapshot.Skills {
		eligible[view.ID] = view.Eligible
	}
	if ok, found := eligible["skill.web.browser.research"]; !found || !ok {
		t.Fatalf("web research skill found=%v eligible=%v", found, ok)
	}
	// The browser skill drives bsk through shell, so it waits for a workstation.
	if ok, found := eligible["skill.browser"]; !found || ok {
		t.Fatalf("browser skill found=%v eligible=%v, want present and ineligible without shell", found, ok)
	}
}
