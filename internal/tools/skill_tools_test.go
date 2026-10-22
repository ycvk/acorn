package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/settings"
	"github.com/ycvk/acorn/internal/skills"
)

type fakeSkillLibrary struct {
	write          skills.WriteRequest
	retired, runID string
	imported       string
	base           int64
	staged         []core.SkillFile
	current        *core.Skill
	err            error
}

func (l *fakeSkillLibrary) Skill(_ context.Context, name string) (core.Skill, error) {
	if l.current == nil || l.current.ID != name {
		return core.Skill{}, core.ErrSkillNotFound
	}
	return *l.current, nil
}

func (l *fakeSkillLibrary) Write(_ context.Context, req skills.WriteRequest) (core.Skill, error) {
	l.write, l.base, l.runID = req, req.BaseRevision, req.RunID
	return core.Skill{ID: "shop-orders", Revision: req.BaseRevision + 1, Files: []core.SkillFileInfo{{Path: "scripts/a.py"}}, UpdatedAt: knowledgeTestTime}, l.err
}

func (l *fakeSkillLibrary) Retire(_ context.Context, name string, base int64, runID string) (core.Skill, error) {
	l.retired, l.base, l.runID = name, base, runID
	return core.Skill{ID: name, Revision: base + 1, Retired: true, UpdatedAt: knowledgeTestTime}, l.err
}

func (l *fakeSkillLibrary) Stage(_ context.Context, entries []core.SkillFile) (core.SkillPackage, error) {
	l.staged = entries
	return core.SkillPackage{SHA256: "abc", Name: "pdf", Markdown: "---\nname: pdf\ndescription: Fill PDF forms.\nlicense: MIT\n---\nsteps\n", Files: []core.SkillFile{{Path: "scripts/fill.py", Content: []byte("print(1)")}}}, l.err
}

func (l *fakeSkillLibrary) ImportStaged(_ context.Context, name, sha256 string, base int64, runID string) (core.Skill, error) {
	l.imported, l.base, l.runID = name+"@"+sha256, base, runID
	return core.Skill{ID: name, Revision: base + 1, UpdatedAt: knowledgeTestTime}, l.err
}

type fakePackageReader struct {
	path string
}

func (r *fakePackageReader) ReadPackage(_ context.Context, path string) ([]core.SkillFile, error) {
	r.path = path
	return []core.SkillFile{{Path: "pdf/SKILL.md", Content: []byte("x")}}, nil
}

func TestSkillToolsCarryTheRunAndBaseRevision(t *testing.T) {
	library := &fakeSkillLibrary{}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	deps := SkillToolDeps{Library: library, Context: fixedArtifactContext{runID: "run_9", sessionID: "thread_1", callID: "call_1"}, Settings: settings.Fixed(core.Settings{Location: loc})}
	write, err := buildSkillWriteTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	retire, err := buildSkillRetireTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	markdown := "---\nname: shop-orders\ndescription: Orders.\n---\nsteps"
	args, err := json.Marshal(map[string]any{"markdown": markdown, "files": map[string]string{"scripts/a.py": "print(1)"}, "delete": []string{"old.md"}, "base_revision": 2})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, write, string(args))
	if err != nil || library.write.Markdown != markdown || library.write.Put["scripts/a.py"] != "print(1)" || library.write.Delete[0] != "old.md" || library.base != 2 || library.runID != "run_9" {
		t.Fatalf("write = %s, %v (library %+v)", out, err, library)
	}
	if !strings.Contains(out, `"revision":3`) || !strings.Contains(out, `"files":1`) || !strings.Contains(out, "2026-10-03 Sat 09:00") || strings.Contains(out, "retired") {
		t.Fatalf("write output = %s", out)
	}
	out, err = runPresenceTool(t, retire, `{"name":"shop-orders","base_revision":3}`)
	if err != nil || library.retired != "shop-orders" || library.base != 3 || !strings.Contains(out, `"retired":true`) {
		t.Fatalf("retire = %s, %v", out, err)
	}
	library.err = core.ErrSkillConflict
	if _, err := runPresenceTool(t, write, string(args)); !errors.Is(err, core.ErrSkillConflict) {
		t.Fatalf("write err = %v", err)
	}
}

func TestSkillInspectStagesThePackageForImport(t *testing.T) {
	library := &fakeSkillLibrary{current: &core.Skill{ID: "pdf", Revision: 4}}
	reader := &fakePackageReader{}
	deps := SkillToolDeps{Library: library, Packages: reader, Context: fixedArtifactContext{runID: "run_9"}, Settings: settings.Fixed(core.Settings{Location: time.UTC})}
	inspect, err := buildSkillInspectTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	importTool, err := buildSkillImportTool(deps)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runPresenceTool(t, inspect, `{"path":"downloads/pdf.zip"}`)
	if err != nil || reader.path != "downloads/pdf.zip" || len(library.staged) != 1 {
		t.Fatalf("inspect = %s, %v", out, err)
	}
	for _, want := range []string{`"name":"pdf"`, `"sha256":"abc"`, `"license":"MIT"`, `"path":"scripts/fill.py","bytes":8`, `"current_revision":4`} {
		if !strings.Contains(out, want) {
			t.Fatalf("inspect output %s lacks %s", out, want)
		}
	}
	out, err = runPresenceTool(t, importTool, `{"name":"pdf","sha256":"abc","base_revision":4}`)
	if err != nil || library.imported != "pdf@abc" || library.base != 4 || library.runID != "run_9" || !strings.Contains(out, `"revision":5`) {
		t.Fatalf("import = %s, %v (library %+v)", out, err, library)
	}
}

func TestSkillWriteRejectsToolsNoRunHas(t *testing.T) {
	library := &fakeSkillLibrary{}
	write, err := buildSkillWriteTool(SkillToolDeps{Library: library, Context: fixedArtifactContext{runID: "run_9"}, Settings: settings.Fixed(core.Settings{Location: time.UTC})})
	if err != nil {
		t.Fatal(err)
	}
	markdown := "---\nname: csv\ndescription: Count a CSV.\nmetadata:\n  acorn-requires-tools: tool_search shell mcp__drive__search\n---\nsteps"
	args, err := json.Marshal(map[string]any{"markdown": markdown})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, write, string(args)); err == nil || !strings.Contains(err.Error(), "acorn-requires-tools names tool_search,") || library.write.Markdown != "" {
		t.Fatalf("err = %v, library got %q", err, library.write.Markdown)
	}
	markdown = strings.Replace(markdown, "tool_search ", "", 1)
	args, err = json.Marshal(map[string]any{"markdown": markdown})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runPresenceTool(t, write, string(args)); err != nil || library.write.Markdown != markdown {
		t.Fatalf("known tools: err = %v, library got %q", err, library.write.Markdown)
	}
}
