package decision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileServiceLoadReturnsDefaultProfileWithoutWritingFile(t *testing.T) {
	root := t.TempDir()
	service := NewProfileService(root)

	parsed, err := service.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected parsed profile")
	}
	if parsed.Path == "" {
		t.Fatal("expected profile path")
	}
	if got, want := filepath.Base(parsed.Path), "decision.md"; got != want {
		t.Fatalf("profile file = %q, want %q", got, want)
	}
	if _, err := os.Stat(parsed.Path); !os.IsNotExist(err) {
		t.Fatalf("expected decision.md to stay absent, got err=%v", err)
	}
}

func TestProfileServiceLoadReadsExistingDecisionFile(t *testing.T) {
	root := t.TempDir()
	service := NewProfileService(root)

	expected := strings.Join([]string{
		"# Acorn Decision Profile",
		"",
		"## Defaults",
		"",
		"```acorn-defaults",
		"missing_context: inspect_first",
		"missing_required_capability: block",
		"```",
		"",
	}, "\n")
	path := filepath.Join(root, "decision.md")
	if err := os.WriteFile(path, []byte(expected), 0o644); err != nil {
		t.Fatalf("write decision.md: %v", err)
	}

	parsed, err := service.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected parsed profile")
	}
	if parsed.Hash == "" {
		t.Fatal("expected profile hash")
	}
	if parsed.Profile.Defaults.MissingContext != ActionInspectFirst {
		t.Fatalf("missing_context = %q, want inspect_first", parsed.Profile.Defaults.MissingContext)
	}
	if parsed.Profile.Defaults.MissingRequiredCapability != ActionBlock {
		t.Fatalf("missing_required_capability = %q, want block", parsed.Profile.Defaults.MissingRequiredCapability)
	}
}

func TestDefaultProfileRendersWithoutRoutesOrReflectionBlock(t *testing.T) {
	profile := DefaultProfile()
	raw, err := RenderProfileMarkdown(profile)
	if err != nil {
		t.Fatalf("RenderProfileMarkdown: %v", err)
	}
	if strings.Contains(raw, "acorn-reflection") {
		t.Fatalf("rendered profile still contains reflection block:\n%s", raw)
	}
	if strings.Contains(raw, "acorn-routes") {
		t.Fatalf("rendered profile must not contain routes block (defaults-only):\n%s", raw)
	}
}
