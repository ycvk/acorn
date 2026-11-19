package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

// SkillChanges previews what a skill write would do to the stored skills,
// and reads the skill a retirement takes out of use.
type SkillChanges interface {
	WriteChange(ctx context.Context, req skills.WriteRequest) (skills.Change, error)
	ImportChange(ctx context.Context, name, sha256 string, baseRevision int64) (skills.Change, error)
	Skill(ctx context.Context, name string) (core.Skill, error)
}

// SkillApproval says what a skill_write or skill_import call does to the
// stored skills: whether it adds a skill or rewrites one, the revision it
// rewrites and whether a release ships that skill, and which files change.
func SkillApproval(ctx context.Context, changes SkillChanges, toolName, arguments string) (string, error) {
	change, err := SkillChange(ctx, changes, toolName, arguments)
	if err != nil {
		return "", err
	}
	return SkillChangeStatement(toolName, change), nil
}

// SkillRetireApproval says which skill a skill_retire call takes out of use:
// whether a release ships it, its revision and who wrote it, or why the call
// would fail.
func SkillRetireApproval(ctx context.Context, changes SkillChanges, arguments string) (string, error) {
	var in SkillRetireInput
	if err := json.Unmarshal([]byte(arguments), &in); err != nil {
		return "", fmt.Errorf("skill_retire arguments: %w", err)
	}
	name := strings.TrimSpace(in.Name)
	skill, err := changes.Skill(ctx, name)
	switch {
	case errors.Is(err, core.ErrSkillNotFound):
		return fmt.Sprintf("技能 %s 不存在，这次停用会失败。", name), nil
	case err != nil:
		return "", err
	case skill.Retired:
		return fmt.Sprintf("技能 %s 已经停用，这次停用会失败。", name), nil
	case skill.Revision != in.BaseRevision:
		return fmt.Sprintf("技能 %s 现在是第 %d 版，这次停用基于第 %d 版，会失败。", name, skill.Revision, in.BaseRevision), nil
	}
	kind := "技能"
	if skill.SeedSHA256 != "" {
		kind = "内置技能"
	}
	return fmt.Sprintf("停用%s %s（现为第 %d 版，%s写的）：之后的 run 不再用它，在技能页可以恢复。", kind, name, skill.Revision, skillAuthorName(skill.Author)), nil
}

// skillAuthorName is who wrote a revision, as the owner reads it.
func skillAuthorName(author core.SkillAuthor) string {
	switch author {
	case core.SkillAuthorOwner:
		return "你"
	case core.SkillAuthorAgent:
		return "Acorn"
	default:
		return "发布版本"
	}
}

// SkillChange is the change a skill_write or skill_import call would make.
func SkillChange(ctx context.Context, changes SkillChanges, toolName, arguments string) (skills.Change, error) {
	switch toolName {
	case "skill_write":
		var in SkillWriteInput
		if err := json.Unmarshal([]byte(arguments), &in); err != nil {
			return skills.Change{}, fmt.Errorf("skill_write arguments: %w", err)
		}
		return changes.WriteChange(ctx, skills.WriteRequest{Markdown: in.Markdown, Put: in.Files, Delete: in.Delete, BaseRevision: in.BaseRevision})
	case "skill_import":
		var in SkillImportInput
		if err := json.Unmarshal([]byte(arguments), &in); err != nil {
			return skills.Change{}, fmt.Errorf("skill_import arguments: %w", err)
		}
		return changes.ImportChange(ctx, in.Name, in.SHA256, in.BaseRevision)
	default:
		return skills.Change{}, fmt.Errorf("%s does not write a skill", toolName)
	}
}

// SkillChangeStatement puts a change in one sentence for the owner, on one
// line.
func SkillChangeStatement(toolName string, c skills.Change) string {
	act, lead := "写入", ""
	if toolName == "skill_import" {
		act, lead = "导入", "导入技能包，"
	}
	switch {
	case c.Fails != nil && c.Name == "":
		return fmt.Sprintf("这次%s会失败：%s", act, oneLine(c.Fails.Error()))
	case c.Fails != nil:
		return fmt.Sprintf("技能 %s 的这次%s会失败：%s", c.Name, act, oneLine(c.Fails.Error()))
	case c.Stale() && c.Current == nil:
		return fmt.Sprintf("技能 %s 不存在，这次%s基于第 %d 版，会失败。", c.Name, act, c.Base)
	case c.Stale() && c.Base == 0:
		return fmt.Sprintf("技能 %s 已经有了（第 %d 版），这次%s按新建提交，会失败。", c.Name, c.Current.Revision, act)
	case c.Stale():
		return fmt.Sprintf("技能 %s 现在是第 %d 版，这次%s基于第 %d 版，会失败。", c.Name, c.Current.Revision, act, c.Base)
	case c.Current == nil:
		attached := ""
		if n := len(c.Files) - 1; n > 0 {
			attached = fmt.Sprintf("，附 %d 个文件", n)
		}
		return fmt.Sprintf("%s新建技能 %s%s：%s", lead, c.Name, attached, c.Description)
	}
	kind := "技能"
	if c.Current.SeedSHA256 != "" {
		kind = "内置技能"
	}
	state := ""
	if c.Current.Retired {
		state = fmt.Sprintf("，已停用，%s后恢复使用", act)
	}
	return fmt.Sprintf("%s改写%s %s（现为第 %d 版%s）：%s", lead, kind, c.Name, c.Current.Revision, state, changedParts(c.Files))
}

// changedParts lists what differs from the stored package: how many lines
// of SKILL.md, and the other files by what happens to them.
func changedParts(files []skills.FileDiff) string {
	if len(files) == 0 {
		return "内容与当前版本相同"
	}
	parts := []string{"SKILL.md 不变"}
	var added, modified, removed []string
	for _, f := range files {
		switch {
		case f.Path == skills.SkillFileName:
			parts[0] = "SKILL.md " + lineCounts(f)
		case f.Change == skills.FileAdded:
			added = append(added, f.Path)
		case f.Change == skills.FileRemoved:
			removed = append(removed, f.Path)
		default:
			modified = append(modified, f.Path)
		}
	}
	for _, group := range []struct {
		label string
		paths []string
	}{{"新增文件 ", added}, {"改动文件 ", modified}, {"删除文件 ", removed}} {
		if len(group.paths) > 0 {
			parts = append(parts, group.label+strings.Join(group.paths, "、"))
		}
	}
	return strings.Join(parts, "；")
}

// lineCounts says how many lines a file loses and gains.
func lineCounts(f skills.FileDiff) string {
	var removed, added int
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			switch l.Kind {
			case skills.LineRemoved:
				removed++
			case skills.LineAdded:
				added++
			}
		}
	}
	switch {
	case removed > 0 && added > 0:
		return fmt.Sprintf("删 %d 行、加 %d 行", removed, added)
	case removed > 0:
		return fmt.Sprintf("删 %d 行", removed)
	default:
		return fmt.Sprintf("加 %d 行", added)
	}
}

// SkillChangedLines shows, for a rewrite, the lines that differ in each file
// the stored package already has, with the unchanged lines around them. It
// is empty when no such file changes.
func SkillChangedLines(c skills.Change) string {
	if c.Current == nil {
		return ""
	}
	var blocks []string
	for _, f := range c.Files {
		if f.Change != skills.FileModified {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "── %s 相对第 %d 版的改动 ──", f.Path, c.Current.Revision)
		if f.Binary {
			b.WriteString("\n(binary)")
		}
		for _, h := range f.Hunks {
			fmt.Fprintf(&b, "\n@@ -%d +%d @@", h.OldStart, h.NewStart)
			for _, l := range h.Lines {
				mark := " "
				switch l.Kind {
				case skills.LineRemoved:
					mark = "-"
				case skills.LineAdded:
					mark = "+"
				}
				b.WriteString("\n" + mark + l.Text)
			}
		}
		blocks = append(blocks, b.String())
	}
	return strings.Join(blocks, "\n\n")
}
