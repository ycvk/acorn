package skills

import (
	"context"
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
