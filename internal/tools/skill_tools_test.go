package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type fakeSkillLibrary struct {
	markdown, retired, runID string
	base                     int64
	err                      error
}

func (l *fakeSkillLibrary) Write(_ context.Context, markdown string, base int64, runID string) (core.Skill, error) {
	l.markdown, l.base, l.runID = markdown, base, runID
	return core.Skill{ID: "skill.orders", Revision: base + 1, UpdatedAt: knowledgeTestTime}, l.err
}

func (l *fakeSkillLibrary) Retire(_ context.Context, id string, base int64, runID string) (core.Skill, error) {
	l.retired, l.base, l.runID = id, base, runID
	return core.Skill{ID: id, Revision: base + 1, Retired: true, UpdatedAt: knowledgeTestTime}, l.err
}

func TestSkillToolsCarryTheRunAndBaseRevision(t *testing.T) {
	library := &fakeSkillLibrary{}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	deps := SkillToolDeps{Library: library, Context: fixedArtifactContext{runID: "run_9", sessionID: "thread_1", callID: "call_1"}, Location: loc}
	write, err := buildSkillWriteTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	retire, err := buildSkillRetireTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, write, `{"markdown":"---\nid: skill.orders\nsummary: Orders.\n---\n# Orders\n\nsteps","base_revision":2}`)
	if err != nil || library.markdown != "---\nid: skill.orders\nsummary: Orders.\n---\n# Orders\n\nsteps" || library.base != 2 || library.runID != "run_9" {
		t.Fatalf("write = %s, %v (library %+v)", out, err, library)
	}
	if !strings.Contains(out, `"revision":3`) || !strings.Contains(out, "2026-10-03 Sat 09:00") || strings.Contains(out, "retired") {
		t.Fatalf("write output = %s", out)
	}
	out, err = runPresenceTool(t, retire, `{"id":"skill.orders","base_revision":3}`)
	if err != nil || library.retired != "skill.orders" || library.base != 3 || !strings.Contains(out, `"retired":true`) {
		t.Fatalf("retire = %s, %v", out, err)
	}
	library.err = core.ErrSkillConflict
	if _, err := runPresenceTool(t, write, `{"markdown":"---\nid: skill.orders\n---\n# Orders\n\nsteps"}`); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("write err = %v", err)
	}
}

func TestSkillWriteRejectsToolsNoRunHas(t *testing.T) {
	library := &fakeSkillLibrary{}
	write, err := buildSkillWriteTool(SkillToolDeps{Library: library, Context: fixedArtifactContext{runID: "run_9"}, Location: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	markdown := "---\nid: skill.csv\nsummary: Count a CSV.\nrequires:\n  tools: [tool_search, shell, mcp__drive__search]\n---\n# CSV\n\nsteps"
	args, err := json.Marshal(map[string]any{"markdown": markdown})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, write, string(args)); err == nil || !strings.Contains(err.Error(), "requires.tools names tool_search,") || library.markdown != "" {
		t.Fatalf("err = %v, library got %q", err, library.markdown)
	}
	markdown = strings.Replace(markdown, "tool_search, ", "", 1)
	args, err = json.Marshal(map[string]any{"markdown": markdown})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, write, string(args)); err != nil || library.markdown != markdown {
		t.Fatalf("known tools: err = %v, library got %q", err, library.markdown)
	}
}
