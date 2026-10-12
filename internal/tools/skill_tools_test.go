package tools

import (
	"context"
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
	out, err := runPresenceTool(t, write, `{"markdown":"---\nid: skill.orders\n---\nsteps","base_revision":2}`)
	if err != nil || library.markdown != "---\nid: skill.orders\n---\nsteps" || library.base != 2 || library.runID != "run_9" {
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
	if _, err := runPresenceTool(t, write, `{"markdown":"x"}`); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("write err = %v", err)
	}
}
