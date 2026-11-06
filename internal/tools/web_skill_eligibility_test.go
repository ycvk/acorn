package tools

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// With only web_fetch configured, the default self-hosted setup, the seed
// skills that need the workstation stay listed and ineligible.
func TestSeedWorkstationSkillsIneligibleWithOnlyWebFetch(t *testing.T) {
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
	specsOf := make([]skills.Spec, 0, len(builtIn))
	for _, pkg := range builtIn {
		spec := pkg.Spec
		spec.Files = fileInfos(pkg.Files)
		specsOf = append(specsOf, spec)
	}
	snapshot := skills.BuildSnapshot(skills.ScanResult{Skills: specsOf}, EligibilityContext(catalog))
	eligible := map[string]bool{}
	for _, view := range snapshot.Skills {
		eligible[view.Name] = view.Eligible
	}
	// The browser skill drives bsk through shell, so it waits for a workstation.
	if ok, found := eligible["browser"]; !found || ok {
		t.Fatalf("browser skill found=%v eligible=%v, want present and ineligible without shell", found, ok)
	}
	// browser-skill carries reference files, which only the workstation holds.
	if ok, found := eligible["browser-skill"]; !found || ok {
		t.Fatalf("browser-skill found=%v eligible=%v, want present and ineligible without files", found, ok)
	}
}

func fileInfos(files []core.SkillFile) []core.SkillFileInfo {
	out := make([]core.SkillFileInfo, 0, len(files))
	for _, f := range files {
		out = append(out, core.SkillFileInfo{Path: f.Path, Size: int64(len(f.Content))})
	}
	return out
}
