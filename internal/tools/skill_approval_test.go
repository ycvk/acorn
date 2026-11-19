package tools

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// fakeSkillChanges answers one change and keeps what it was asked.
type fakeSkillChanges struct {
	change   skills.Change
	err      error
	wrote    []skills.WriteRequest
	imported []string
	stored   map[string]core.Skill
}

func (f *fakeSkillChanges) Skill(_ context.Context, name string) (core.Skill, error) {
	skill, ok := f.stored[name]
	if !ok {
		return core.Skill{}, core.ErrSkillNotFound
	}
	return skill, nil
}

func (f *fakeSkillChanges) WriteChange(_ context.Context, req skills.WriteRequest) (skills.Change, error) {
	f.wrote = append(f.wrote, req)
	return f.change, f.err
}

func (f *fakeSkillChanges) ImportChange(_ context.Context, name, sha256 string, base int64) (skills.Change, error) {
	f.imported = append(f.imported, name, sha256, strconv.FormatInt(base, 10))
	return f.change, f.err
}

func modified(path string, removed, added int) skills.FileDiff {
	var lines []skills.DiffLine
	for range removed {
		lines = append(lines, skills.DiffLine{Kind: skills.LineRemoved, Text: "old"})
	}
	for range added {
		lines = append(lines, skills.DiffLine{Kind: skills.LineAdded, Text: "new"})
	}
	return skills.FileDiff{Path: path, Change: skills.FileModified, Hunks: []skills.Hunk{{OldStart: 4, NewStart: 4, Lines: append([]skills.DiffLine{{Kind: skills.LineSame, Text: "kept"}}, lines...)}}}
}

func TestSkillChangeStatementSaysWhatTheOwnerDecidesOn(t *testing.T) {
	shipped := &core.Skill{ID: "skill-creator", Revision: 6, SeedSHA256: "60f2"}
	own := &core.Skill{ID: "shop-orders", Revision: 2}
	retired := &core.Skill{ID: "shop-orders", Revision: 3, Retired: true}
	for _, tc := range []struct {
		name   string
		tool   string
		change skills.Change
		want   string
	}{
		{"a new skill without files", "skill_write", skills.Change{Name: "shop-orders", Description: "Look up orders.", Files: []skills.FileDiff{{Path: "SKILL.md", Change: skills.FileAdded}}},
			"新建技能 shop-orders：Look up orders."},
		{"a built-in skill", "skill_write", skills.Change{Name: "skill-creator", Base: 6, Current: shipped, Files: []skills.FileDiff{
			modified("SKILL.md", 38, 44), {Path: "action", Change: skills.FileAdded}, {Path: "file", Change: skills.FileAdded},
		}}, "改写内置技能 skill-creator（现为第 6 版）：SKILL.md 删 38 行、加 44 行；新增文件 action、file"},
		{"only lines added", "skill_write", skills.Change{Name: "shop-orders", Base: 2, Current: own, Files: []skills.FileDiff{modified("SKILL.md", 0, 3)}},
			"改写技能 shop-orders（现为第 2 版）：SKILL.md 加 3 行"},
		{"only lines removed", "skill_write", skills.Change{Name: "shop-orders", Base: 2, Current: own, Files: []skills.FileDiff{modified("SKILL.md", 2, 0)}},
			"改写技能 shop-orders（现为第 2 版）：SKILL.md 删 2 行"},
		{"a retired skill", "skill_write", skills.Change{Name: "shop-orders", Base: 3, Current: retired, Files: []skills.FileDiff{{Path: "notes.md", Change: skills.FileRemoved}}},
			"改写技能 shop-orders（现为第 3 版，已停用，写入后恢复使用）：SKILL.md 不变；删除文件 notes.md"},
		{"an import over a retired skill", "skill_import", skills.Change{Name: "shop-orders", Base: 3, Current: retired, Files: []skills.FileDiff{modified("scripts/a.py", 1, 1)}},
			"导入技能包，改写技能 shop-orders（现为第 3 版，已停用，导入后恢复使用）：SKILL.md 不变；改动文件 scripts/a.py"},
		{"an import on another revision", "skill_import", skills.Change{Name: "shop-orders", Base: 1, Current: own},
			"技能 shop-orders 现在是第 2 版，这次导入基于第 1 版，会失败。"},
		{"a text that does not parse", "skill_write", skills.Change{Base: 2, Fails: errors.New("invalid frontmatter: yaml: unmarshal errors:\n  line 1: field id not found\n  line 2: field summary not found")},
			"这次写入会失败：invalid frontmatter: yaml: unmarshal errors: line 1: field id not found line 2: field summary not found"},
		{"a file that is not there", "skill_import", skills.Change{Name: "pdf", Fails: errors.New("skill package not staged: abc")},
			"技能 pdf 的这次导入会失败：skill package not staged: abc"},
	} {
		if got := SkillChangeStatement(tc.tool, tc.change); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSkillChangedLinesShowTheLinesOfRewrittenFiles(t *testing.T) {
	own := &core.Skill{ID: "shop-orders", Revision: 2}
	change := skills.Change{Name: "shop-orders", Base: 2, Current: own, Files: []skills.FileDiff{
		modified("SKILL.md", 1, 2), {Path: "notes.md", Change: skills.FileAdded, Hunks: []skills.Hunk{{OldStart: 1, NewStart: 1, Lines: []skills.DiffLine{{Kind: skills.LineAdded, Text: "n"}}}}},
		{Path: "assets/logo.png", Change: skills.FileModified, Binary: true}, {Path: "old.md", Change: skills.FileRemoved},
	}}
	want := "── SKILL.md 相对第 2 版的改动 ──\n@@ -4 +4 @@\n kept\n-old\n+new\n+new\n\n── assets/logo.png 相对第 2 版的改动 ──\n(binary)"
	if got := SkillChangedLines(change); got != want {
		t.Fatalf("lines = %q", got)
	}
	change.Current = nil
	if got := SkillChangedLines(change); got != "" {
		t.Fatalf("a new skill has no changed lines: %q", got)
	}
}

func TestApprovalDescriberAsksWhatASkillCallChanges(t *testing.T) {
	ctx := context.Background()
	changes := &fakeSkillChanges{change: skills.Change{Name: "shop-orders", Description: "Look up orders.", Files: []skills.FileDiff{{Path: "SKILL.md", Change: skills.FileAdded}, {Path: "scripts/a.py", Change: skills.FileAdded}}}}
	describe := ApprovalDescriber{Skills: changes}
	got, err := describe.DescribeApproval(ctx, "skill_write", `{"markdown":"text","files":{"scripts/a.py":"x"},"delete":["old.md"],"base_revision":4}`)
	if err != nil || got != "新建技能 shop-orders，附 1 个文件：Look up orders." {
		t.Fatalf("write = %q, %v", got, err)
	}
	want := skills.WriteRequest{Markdown: "text", Put: map[string]string{"scripts/a.py": "x"}, Delete: []string{"old.md"}, BaseRevision: 4}
	if len(changes.wrote) != 1 || !reflect.DeepEqual(changes.wrote[0], want) {
		t.Fatalf("asked about %+v", changes.wrote)
	}
	got, err = describe.DescribeApproval(ctx, "skill_import", `{"name":"shop-orders","sha256":"abc","base_revision":2}`)
	if err != nil || got != "导入技能包，新建技能 shop-orders，附 1 个文件：Look up orders." || !reflect.DeepEqual(changes.imported, []string{"shop-orders", "abc", "2"}) {
		t.Fatalf("import = %q, %v; asked about %v", got, err, changes.imported)
	}
	// Arguments the tool could not read are no call to decide on.
	if _, err := describe.DescribeApproval(ctx, "skill_write", `{"markdown":"text","files":"scripts/a.py"}`); err == nil {
		t.Fatal("unreadable arguments must not be described")
	}
	changes.err = errors.New("database is closed")
	if _, err := describe.DescribeApproval(ctx, "skill_write", `{"markdown":"text"}`); !errors.Is(err, changes.err) {
		t.Fatalf("err = %v", err)
	}
	if _, err := describe.DescribeApproval(ctx, "skill_inspect", `{"path":"pkg"}`); err == nil {
		t.Fatal("skill_inspect has no description")
	}
}

func TestApprovalDescriberSaysWhichSkillARetirementTakesOutOfUse(t *testing.T) {
	ctx := context.Background()
	describe := ApprovalDescriber{Skills: &fakeSkillChanges{stored: map[string]core.Skill{
		"skill-creator": {ID: "skill-creator", Revision: 6, Author: core.SkillAuthorRelease, SeedSHA256: "seed"},
		"shop-orders":   {ID: "shop-orders", Revision: 2, Author: core.SkillAuthorAgent},
		"old":           {ID: "old", Revision: 3, Retired: true, Author: core.SkillAuthorOwner},
	}}}
	for _, tc := range []struct{ arguments, want string }{
		{`{"name":"skill-creator","base_revision":6}`, "停用内置技能 skill-creator（现为第 6 版，发布版本写的）：之后的 run 不再用它，在技能页可以恢复。"},
		{`{"name":"shop-orders","base_revision":2}`, "停用技能 shop-orders（现为第 2 版，Acorn写的）：之后的 run 不再用它，在技能页可以恢复。"},
		{`{"name":"shop-orders","base_revision":1}`, "技能 shop-orders 现在是第 2 版，这次停用基于第 1 版，会失败。"},
		{`{"name":"old","base_revision":3}`, "技能 old 已经停用，这次停用会失败。"},
		{`{"name":"gone","base_revision":1}`, "技能 gone 不存在，这次停用会失败。"},
	} {
		got, err := describe.DescribeApproval(ctx, "skill_retire", tc.arguments)
		if err != nil || got != tc.want {
			t.Errorf("%s = %q, %v; want %q", tc.arguments, got, err, tc.want)
		}
	}
}
