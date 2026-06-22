package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (l *Loader) CreateSkill(ctx context.Context, input CreateInput) (*Spec, error) {
	_ = ctx
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(l.generatedDir) == "" {
		return nil, fmt.Errorf("generated skill directory is not configured")
	}
	if err := os.MkdirAll(l.generatedDir, 0o755); err != nil {
		return nil, fmt.Errorf("create skill root directory: %w", err)
	}
	exists, err := l.HasSkill(ctx, normalized.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: %s", ErrAlreadyExists, normalized.ID)
	}
	dir := filepath.Join(l.generatedDir, normalized.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create skill directory: %w", err)
	}
	return l.writeSkillDir(dir, normalized)
}

func (l *Loader) writeSkillDir(dir string, normalized CreateInput) (*Spec, error) {
	cleanup := func(cause error) (*Spec, error) {
		_ = os.RemoveAll(dir)
		return nil, cause
	}
	body, err := renderSkillMarkdown(buildCreateFrontmatter(normalized), normalized.Instruction)
	if err != nil {
		return cleanup(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		return cleanup(fmt.Errorf("write skill markdown: %w", err))
	}
	spec, problem := loadSkillDir(dir, GeneratedScope)
	if problem != nil {
		return cleanup(errors.New(problem.Error))
	}
	return &spec, nil
}

func buildCreateFrontmatter(normalized CreateInput) frontmatter {
	return frontmatter{
		ID:           normalized.ID,
		Name:         normalized.Name,
		Version:      normalized.Version,
		Category:     normalized.Category,
		Summary:      normalized.Summary,
		PromotedFrom: normalized.PromotedFrom,
		Origin:       normalized.Origin,
		TaskPattern:  normalized.TaskPattern,
		Tags:         append([]string(nil), normalized.Tags...),
		Platforms:    append([]string(nil), normalized.Platforms...),
		Requires: frontRequirements{
			Tools:    append([]string(nil), normalized.Requires.Tools...),
			Toolsets: append([]string(nil), normalized.Requires.Toolsets...),
			Bins:     append([]string(nil), normalized.Requires.Bins...),
			Env:      append([]string(nil), normalized.Requires.Env...),
		},
		TriggerHints:   append([]string(nil), normalized.TriggerHints...),
		CreatedByRunID: normalized.CreatedByRunID,
	}
}

func (l *Loader) HasSkill(ctx context.Context, skillID string) (bool, error) {
	_ = ctx
	normalizedID, err := normalizeSkillDirID(skillID)
	if err != nil {
		return false, err
	}
	scan, err := l.ScanSkills(ctx)
	if err != nil {
		return false, err
	}
	for _, item := range scan.Skills {
		if item.ID == normalizedID {
			return true, nil
		}
	}
	return false, nil
}

func (l *Loader) PatchSkill(ctx context.Context, skillID, patchContent string) error {
	return l.patchSkill(ctx, skillID, patchContent, "")
}

func (l *Loader) PatchSkillWithSource(ctx context.Context, skillID, patchContent, source string) error {
	return l.patchSkill(ctx, skillID, patchContent, source)
}

func (l *Loader) DeleteSkill(ctx context.Context, skillID string) error {
	trimmedID := strings.TrimSpace(skillID)
	if trimmedID == "" {
		return fmt.Errorf("delete skill: skill id is required")
	}
	skill, found, err := l.findSkillByID(ctx, trimmedID)
	if err != nil {
		return fmt.Errorf("delete skill %s: %w", trimmedID, err)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, trimmedID)
	}
	if !skillModifiable(skill.Source) {
		return fmt.Errorf("delete skill %s: only workspace, generated, or user skills can be deleted (source=%s)", trimmedID, skill.Source)
	}
	if err := os.RemoveAll(skill.Path); err != nil {
		return fmt.Errorf("delete skill %s: %w", trimmedID, err)
	}
	return nil
}

func (l *Loader) patchSkill(ctx context.Context, skillID, patchContent, source string) error {
	return l.appendSkillSection(ctx, skillID, patchContent, "Patch", "patch", source)
}

func (l *Loader) appendSkillSection(ctx context.Context, skillID, content, sectionTitle, action, source string) error {
	trimmedID := strings.TrimSpace(skillID)
	if trimmedID == "" {
		return fmt.Errorf("%s skill: skill id is required", action)
	}
	trimmedContent, err := validateSkillWriteContent(action, trimmedID, content)
	if err != nil {
		return err
	}
	skill, found, err := l.findSkillByID(ctx, trimmedID)
	if err != nil {
		return fmt.Errorf("%s skill %s: %w", action, trimmedID, err)
	}
	if !found {
		return fmt.Errorf("skill %s not found", trimmedID)
	}
	if !skillModifiable(skill.Source) {
		return fmt.Errorf("%s skill %s: only workspace, generated, or user skills can be modified (source=%s)", action, trimmedID, skill.Source)
	}
	block := trimmedContent
	if trimmedSource := strings.TrimSpace(source); trimmedSource != "" {
		block = "> Source: " + trimmedSource + "\n\n" + block
	}
	_, err = l.writeSkillContent(skill, func(meta *frontmatter, body string) (frontmatter, string) {
		updated := strings.TrimRight(body, "\n") + "\n\n## " + sectionTitle + "\n\n" + block
		return *meta, updated
	}, fmt.Sprintf("%s skill %s", action, trimmedID))
	return err
}

func (l *Loader) findSkillByID(ctx context.Context, skillID string) (Spec, bool, error) {
	scan, err := l.ScanSkills(ctx)
	if err != nil {
		return Spec{}, false, fmt.Errorf("scan skills: %w", err)
	}
	for _, skill := range scan.Skills {
		if skill.ID == skillID {
			return skill, true, nil
		}
	}
	return Spec{}, false, nil
}

func (l *Loader) writeSkillContent(skill Spec, mutate func(meta *frontmatter, body string) (frontmatter, string), action string) (Spec, error) {
	skillPath := filepath.Join(skill.Path, "SKILL.md")
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		return Spec{}, fmt.Errorf("%s: read skill markdown: %w", action, err)
	}
	meta, markdownBody, nameFromMarkdown, _, err := parseSkillMarkdown(string(raw))
	if err != nil {
		return Spec{}, fmt.Errorf("%s: parse skill markdown: %w", action, err)
	}
	meta.ID = firstNonEmpty(meta.ID, skill.ID)
	meta.Name = firstNonEmpty(meta.Name, nameFromMarkdown, skill.Name)
	updatedMeta, updatedBody := mutate(&meta, strings.TrimRight(markdownBody, "\n"))
	body, err := renderSkillMarkdownBody(updatedMeta, updatedBody)
	if err != nil {
		return Spec{}, fmt.Errorf("%s: render skill markdown: %w", action, err)
	}
	if err := os.WriteFile(skillPath, []byte(body), 0o644); err != nil {
		return Spec{}, fmt.Errorf("%s: write skill markdown: %w", action, err)
	}
	reloaded, problem := loadSkillDir(skill.Path, skill.Source)
	if problem != nil {
		return Spec{}, errors.New(problem.Error)
	}
	return reloaded, nil
}

func normalizeSkillDirID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return "", fmt.Errorf("skill id is required")
	}
	if trimmed != filepath.Base(trimmed) {
		return "", fmt.Errorf("skill id %q is invalid", trimmed)
	}
	if strings.Contains(trimmed, "..") || strings.Contains(trimmed, string(os.PathSeparator)) {
		return "", fmt.Errorf("skill id %q is invalid", trimmed)
	}
	return trimmed, nil
}

func validateSkillWriteContent(action, skillID, content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", fmt.Errorf("%s skill %s: %s content is empty", action, skillID, action)
	}
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	for _, line := range strings.Split(normalized, "\n") {
		if strings.TrimSpace(line) == "---" {
			return "", fmt.Errorf("%s skill %s: %s content contains frontmatter delimiters", action, skillID, action)
		}
	}
	return trimmed, nil
}

func skillModifiable(source string) bool {
	switch strings.TrimSpace(source) {
	case WorkspaceScope, GeneratedScope, UserScope:
		return true
	default:
		return false
	}
}

func normalizeSkillRelativePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		trimmed = "SKILL.md"
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("path %q is invalid", path)
	}
	clean := filepath.Clean(trimmed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q is invalid", path)
	}
	return clean, nil
}
