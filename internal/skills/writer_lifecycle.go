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

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	normalizedID, err := normalizeSkillDirID(input.ID)
	if err != nil {
		return CreateInput{}, err
	}
	normalized := buildNormalizedCreateInput(input, normalizedID)
	if err := applyCreateInputDefaults(&normalized); err != nil {
		return CreateInput{}, err
	}
	return normalized, nil
}

func buildNormalizedCreateInput(input CreateInput, normalizedID string) CreateInput {
	return CreateInput{
		ID:             normalizedID,
		Name:           strings.TrimSpace(input.Name),
		Version:        strings.TrimSpace(input.Version),
		Category:       strings.TrimSpace(input.Category),
		Summary:        strings.TrimSpace(input.Summary),
		PromotedFrom:   strings.TrimSpace(input.PromotedFrom),
		Origin:         normalizeOrigin(input.Origin),
		TaskPattern:    strings.TrimSpace(input.TaskPattern),
		Instruction:    strings.TrimSpace(input.Instruction),
		Tags:           uniqueNonEmpty(input.Tags),
		Platforms:      uniqueNonEmpty(input.Platforms),
		TriggerHints:   uniqueNonEmpty(input.TriggerHints),
		Requires:       NormalizeRequirements(input.Requires),
		CreatedByRunID: strings.TrimSpace(input.CreatedByRunID),
	}
}

func applyCreateInputDefaults(normalized *CreateInput) error {
	switch {
	case normalized.Name == "":
		return fmt.Errorf("skill name is required")
	case normalized.Instruction == "":
		return fmt.Errorf("instruction is required")
	}
	switch normalized.Origin {
	case "":
		normalized.Origin = OriginHuman
	case OriginHuman:
	case OriginDistilled:
		if normalized.TaskPattern == "" {
			return fmt.Errorf("task_pattern is required for distilled origin")
		}
	default:
		return fmt.Errorf("origin %q is invalid", normalized.Origin)
	}
	return nil
}
