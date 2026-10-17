package workstation

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

func TestTriggerCommandsRunFromTheirPackageWithTheirSecrets(t *testing.T) {
	client, home := newTestClient(t)
	ctx := t.Context()
	files := []core.SkillFile{
		{Path: skills.ManifestFileName, Content: []byte("triggers:\n  - name: price\n    schedule: \"*/30 * * * *\"\n    run: scripts/check.sh --above 100\n    deliver: immediate\nsecrets: [PRICE_TOKEN]\n")},
		{Path: "scripts/check.sh", Content: []byte("#!/bin/bash\nprintf '{\"key\":\"%s\",\"text\":\"%s %s\"}\\n' \"$(pwd)\" \"$1\" \"${#PRICE_TOKEN}\"\necho warn >&2\n")},
	}
	if err := client.PutSkillPackage(ctx, "price-watch", "hash-1", "---\nname: price-watch\n---\nx\n", files); err != nil {
		t.Fatal(err)
	}
	run := skills.TriggerRun{Skill: "price-watch", Hash: "hash-1", Trigger: "price", Env: map[string]string{"PRICE_TOKEN": "s3cret"}, TimeoutSeconds: 10, MaxOutputBytes: 4096}
	result, err := client.RunTrigger(ctx, run)
	if err != nil || result.ExitCode != 0 || result.Stderr != "warn\n" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	var event struct{ Key, Text string }
	if err := json.Unmarshal([]byte(result.Stdout), &event); err != nil {
		t.Fatalf("stdout %q: %v", result.Stdout, err)
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(home, "skills", "price-watch"))
	if err != nil {
		t.Fatal(err)
	}
	if event.Key != dir || event.Text != "--above 6" {
		t.Fatalf("event = %+v; want the package directory and the secret's length", event)
	}

	for want, bad := range map[string]skills.TriggerRun{
		"at hash-1, not hash-0":   {Skill: "price-watch", Hash: "hash-0", Trigger: "price", TimeoutSeconds: 10, MaxOutputBytes: 4096},
		"has no trigger weekly":   {Skill: "price-watch", Hash: "hash-1", Trigger: "weekly", TimeoutSeconds: 10, MaxOutputBytes: 4096},
		"holds no package other":  {Skill: "other", Hash: "hash-1", Trigger: "price", TimeoutSeconds: 10, MaxOutputBytes: 4096},
		"timeout_seconds must be": {Skill: "price-watch", Hash: "hash-1", Trigger: "price", MaxOutputBytes: 4096},
	} {
		if _, err := client.RunTrigger(ctx, bad); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v", want, err)
		}
	}
}
