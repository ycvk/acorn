package agent

import (
	"testing"

	"github.com/ycvk/acorn/internal/skills"
)

func TestStableSkillsFromSnapshotPreservesSpecs(t *testing.T) {
	snapshot := &skills.Snapshot{
		Skills: []skills.View{
			{
				Spec: skills.Spec{
					ID:       "skill.inspect.repo",
					Name:     "Inspect Repo",
					Source:   skills.WorkspaceScope,
					Origin:   skills.OriginHuman,
					Summary:  "Inspect a repository",
					Requires: skills.Requirements{Tools: []string{"read_file"}},
				},
				Eligible: true,
			},
			{
				Spec: skills.Spec{
					ID:       "skill.web.browser.research",
					Name:     "Web Browser Research",
					Source:   skills.BuiltinScope,
					Origin:   skills.OriginHuman,
					Summary:  "Search and browse the web",
					Requires: skills.Requirements{Tools: []string{"load_tools"}},
				},
				Eligible:        false,
				DisabledReasons: []string{"missing_required_tools:web_search"},
			},
		},
	}
	items := stableSkillsFromSnapshot(snapshot)
	if got, want := len(items), 2; got != want {
		t.Fatalf("stable skills = %d, want %d", got, want)
	}
	if items[1].ID != "skill.web.browser.research" {
		t.Fatalf("second skill id = %q", items[1].ID)
	}
	if items[1].Summary != "Search and browse the web" {
		t.Fatalf("summary = %q", items[1].Summary)
	}
	if len(items[1].Requires.Tools) != 1 || items[1].Requires.Tools[0] != "load_tools" {
		t.Fatalf("requires.tools = %#v", items[1].Requires.Tools)
	}
}
