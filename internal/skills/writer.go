package skills

import (
	"bytes"
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
	cleanup := func(cause error) (*Spec, error) {
		_ = os.RemoveAll(dir)
		return nil, cause
	}
	body, err := renderSkillMarkdown(frontmatter{
		ID:              normalized.ID,
		Name:            normalized.Name,
		Version:         normalized.Version,
		Category:        normalized.Category,
		Summary:         normalized.Summary,
		PromotedFrom:    normalized.PromotedFrom,
		Origin:          normalized.Origin,
		LifecycleStatus: normalized.LifecycleStatus,
		TaskPattern:     normalized.TaskPattern,
		Tags:            append([]string(nil), normalized.Tags...),
		Platforms:       append([]string(nil), normalized.Platforms...),
		Requires: frontRequirements{
			Tools:    append([]string(nil), normalized.Requires.Tools...),
			Toolsets: append([]string(nil), normalized.Requires.Toolsets...),
			Bins:     append([]string(nil), normalized.Requires.Bins...),
			Env:      append([]string(nil), normalized.Requires.Env...),
		},
		TriggerHints:   append([]string(nil), normalized.TriggerHints...),
		CreatedByRunID: normalized.CreatedByRunID,
		UpdatedByRunID: normalized.UpdatedByRunID,
		EvidenceRefs:   append([]string(nil), normalized.EvidenceRefs...),
		Replaces:       append([]string(nil), normalized.Replaces...),
		ReplacedBy:     append([]string(nil), normalized.ReplacedBy...),
	}, normalized.Instruction)
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

type LifecycleUpdate struct {
	Status         LifecycleStatus
	EvidenceRefs   []string
	UpdatedByRunID string
	ReplacedBy     []string
}

func (l *Loader) UpdateSkillLifecycle(ctx context.Context, skillID string, update LifecycleUpdate) (*Spec, error) {
	trimmedID := strings.TrimSpace(skillID)
	if trimmedID == "" {
		return nil, fmt.Errorf("update skill lifecycle: skill id is required")
	}
	status := normalizeLifecycleStatus(update.Status)
	if status == "" {
		return nil, fmt.Errorf("update skill lifecycle %s: lifecycle status is required", trimmedID)
	}
	if err := validateLifecycleStatus(status); err != nil {
		return nil, fmt.Errorf("update skill lifecycle %s: %w", trimmedID, err)
	}
	skill, found, err := l.findSkillByID(ctx, trimmedID)
	if err != nil {
		return nil, fmt.Errorf("update skill lifecycle %s: %w", trimmedID, err)
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, trimmedID)
	}
	if !skillModifiable(skill.Source) {
		return nil, fmt.Errorf("update skill lifecycle %s: only workspace, generated, or user skills can be modified (source=%s)", trimmedID, skill.Source)
	}
	if status == LifecycleVerified && len(uniqueNonEmpty(append(append([]string{}, skill.EvidenceRefs...), update.EvidenceRefs...))) == 0 {
		return nil, fmt.Errorf("update skill lifecycle %s: lifecycle_status verified requires evidence_refs", trimmedID)
	}
	updated, err := l.writeSkillContent(skill, func(meta *frontmatter, body string) (frontmatter, string) {
		meta.LifecycleStatus = status
		meta.UpdatedByRunID = strings.TrimSpace(update.UpdatedByRunID)
		if meta.UpdatedByRunID == "" {
			meta.UpdatedByRunID = skill.UpdatedByRunID
		}
		meta.EvidenceRefs = uniqueNonEmpty(append(append([]string{}, skill.EvidenceRefs...), update.EvidenceRefs...))
		meta.ReplacedBy = uniqueNonEmpty(append(append([]string{}, skill.ReplacedBy...), update.ReplacedBy...))
		return *meta, body
	}, fmt.Sprintf("update skill lifecycle %s", trimmedID))
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (l *Loader) ReadSkillFile(ctx context.Context, skillID, relativePath string) (string, error) {
	trimmedID := strings.TrimSpace(skillID)
	if trimmedID == "" {
		return "", fmt.Errorf("read skill file: skill id is required")
	}
	skill, found, err := l.findSkillByID(ctx, trimmedID)
	if err != nil {
		return "", fmt.Errorf("read skill file %s: %w", trimmedID, err)
	}
	if !found {
		return "", fmt.Errorf("%w: %s", ErrNotFound, trimmedID)
	}
	cleanRel, err := normalizeSkillRelativePath(relativePath)
	if err != nil {
		return "", fmt.Errorf("read skill file %s: %w", trimmedID, err)
	}
	target := filepath.Join(skill.Path, cleanRel)
	root := filepath.Clean(skill.Path) + string(os.PathSeparator)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != filepath.Clean(skill.Path) && !strings.HasPrefix(cleanTarget, root) {
		return "", fmt.Errorf("read skill file %s: path %q escapes skill directory", trimmedID, relativePath)
	}
	body, err := os.ReadFile(cleanTarget)
	if err != nil {
		return "", fmt.Errorf("read skill file %s/%s: %w", trimmedID, cleanRel, err)
	}
	return string(body), nil
}

func (l *Loader) WriteSkillFile(ctx context.Context, skillID, relativePath, content string) error {
	trimmedID := strings.TrimSpace(skillID)
	if trimmedID == "" {
		return fmt.Errorf("write skill file: skill id is required")
	}
	skill, found, err := l.findSkillByID(ctx, trimmedID)
	if err != nil {
		return fmt.Errorf("write skill file %s: %w", trimmedID, err)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, trimmedID)
	}
	if !skillModifiable(skill.Source) {
		return fmt.Errorf("write skill file %s: only workspace, generated, or user skills can be modified (source=%s)", trimmedID, skill.Source)
	}
	cleanRel, err := normalizeSkillRelativePath(relativePath)
	if err != nil {
		return fmt.Errorf("write skill file %s: %w", trimmedID, err)
	}
	target := filepath.Join(skill.Path, cleanRel)
	root := filepath.Clean(skill.Path) + string(os.PathSeparator)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != filepath.Clean(skill.Path) && !strings.HasPrefix(cleanTarget, root) {
		return fmt.Errorf("write skill file %s: path %q escapes skill directory", trimmedID, relativePath)
	}
	if err := os.MkdirAll(filepath.Dir(cleanTarget), 0o755); err != nil {
		return fmt.Errorf("write skill file %s/%s: create parent directory: %w", trimmedID, cleanRel, err)
	}
	if err := os.WriteFile(cleanTarget, bytes.ReplaceAll([]byte(content), []byte("\r\n"), []byte("\n")), 0o644); err != nil {
		return fmt.Errorf("write skill file %s/%s: %w", trimmedID, cleanRel, err)
	}
	if _, problem := loadSkillDir(skill.Path, skill.Source); problem != nil {
		return errors.New(problem.Error)
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

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	normalizedID, err := normalizeSkillDirID(input.ID)
	if err != nil {
		return CreateInput{}, err
	}
	normalized := CreateInput{
		ID:              normalizedID,
		Name:            strings.TrimSpace(input.Name),
		Version:         strings.TrimSpace(input.Version),
		Category:        strings.TrimSpace(input.Category),
		Summary:         strings.TrimSpace(input.Summary),
		PromotedFrom:    strings.TrimSpace(input.PromotedFrom),
		Origin:          normalizeOrigin(input.Origin),
		LifecycleStatus: normalizeLifecycleStatus(input.LifecycleStatus),
		TaskPattern:     strings.TrimSpace(input.TaskPattern),
		Instruction:     strings.TrimSpace(input.Instruction),
		Tags:            uniqueNonEmpty(input.Tags),
		Platforms:       uniqueNonEmpty(input.Platforms),
		TriggerHints:    uniqueNonEmpty(input.TriggerHints),
		Requires:        NormalizeRequirements(input.Requires),
		CreatedByRunID:  strings.TrimSpace(input.CreatedByRunID),
		UpdatedByRunID:  strings.TrimSpace(input.UpdatedByRunID),
		EvidenceRefs:    uniqueNonEmpty(input.EvidenceRefs),
		Replaces:        uniqueNonEmpty(input.Replaces),
		ReplacedBy:      uniqueNonEmpty(input.ReplacedBy),
	}
	switch {
	case normalized.Name == "":
		return CreateInput{}, fmt.Errorf("skill name is required")
	case normalized.Instruction == "":
		return CreateInput{}, fmt.Errorf("instruction is required")
	}
	switch normalized.Origin {
	case "":
		normalized.Origin = OriginHuman
	case OriginHuman:
	case OriginDistilled:
		if normalized.TaskPattern == "" {
			return CreateInput{}, fmt.Errorf("task_pattern is required for distilled origin")
		}
	default:
		return CreateInput{}, fmt.Errorf("origin %q is invalid", normalized.Origin)
	}
	if normalized.LifecycleStatus == "" {
		normalized.LifecycleStatus = LifecycleDraft
	}
	if err := validateLifecycleStatus(normalized.LifecycleStatus); err != nil {
		return CreateInput{}, err
	}
	if normalized.LifecycleStatus == LifecycleVerified && len(normalized.EvidenceRefs) == 0 {
		return CreateInput{}, fmt.Errorf("lifecycle_status verified requires evidence_refs")
	}
	return normalized, nil
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
