package skills

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (l *Loader) ScanSkills(ctx context.Context) (*ScanResult, error) {
	_ = ctx
	merged := make(map[string]loadedSkill)
	problems := make([]Problem, 0)
	for _, source := range l.sourceRoots() {
		if err := l.scanInto(merged, &problems, source); err != nil {
			return nil, err
		}
	}
	items := make([]Spec, 0, len(merged))
	for _, item := range merged {
		items = append(items, item.spec)
	}
	sortSkills(items)
	items, nameProblems := filterDuplicateSkillNames(items)
	problems = append(problems, nameProblems...)
	sortSkillProblems(problems)
	return &ScanResult{Skills: items, Problems: problems}, nil
}

func (l *Loader) sourceRoots() []sourceRoot {
	sources := make([]sourceRoot, 0, 5)
	priority := 0
	if root := strings.TrimSpace(l.builtinDir); root != "" {
		sources = append(sources, sourceRoot{scope: BuiltinScope, root: root, priority: priority})
		priority++
	}
	if root := strings.TrimSpace(l.userDir); root != "" {
		sources = append(sources, sourceRoot{scope: UserScope, root: root, priority: priority})
		priority++
	}
	if root := strings.TrimSpace(l.workspaceDir); root != "" {
		sources = append(sources, sourceRoot{scope: WorkspaceScope, root: root, priority: priority})
		priority++
	}
	if root := strings.TrimSpace(l.generatedDir); root != "" {
		sources = append(sources, sourceRoot{scope: GeneratedScope, root: root, priority: priority})
	}
	return sources
}

func (l *Loader) scanInto(target map[string]loadedSkill, problems *[]Problem, source sourceRoot) error {
	dirs, err := discoverSkillDirs(source.root)
	if err != nil {
		return fmt.Errorf("discover skills under %s: %w", source.root, err)
	}
	for _, dir := range dirs {
		item, problem := loadSkillDir(dir, source.scope)
		if problem != nil {
			*problems = append(*problems, *problem)
		}
		if item.ID == "" {
			continue
		}
		existing, exists := target[item.ID]
		if !exists {
			target[item.ID] = loadedSkill{spec: item, scope: source.scope, root: source.root, priority: source.priority}
			continue
		}
		if existing.spec.Source == BuiltinScope && item.Source != BuiltinScope {
			*problems = append(*problems, Problem{
				ID:     item.ID,
				Name:   item.Name,
				Source: item.Source,
				Path:   item.Path,
				Error:  fmt.Sprintf("skill id %q cannot shadow builtin skill %q", item.ID, existing.spec.Path),
			})
			continue
		}
		if existing.priority == source.priority && samePathRoot(existing.root, source.root) {
			*problems = append(*problems, Problem{
				ID:     item.ID,
				Name:   item.Name,
				Source: item.Source,
				Path:   item.Path,
				Error:  fmt.Sprintf("duplicate skill id %q", item.ID),
			})
			continue
		}
		if source.priority > existing.priority {
			*problems = append(*problems, shadowedSkillProblem(existing.spec, item))
			target[item.ID] = loadedSkill{spec: item, scope: source.scope, root: source.root, priority: source.priority}
			continue
		}
		*problems = append(*problems, shadowedSkillProblem(item, existing.spec))
	}
	return nil
}

func discoverSkillDirs(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		skillMarkdown := filepath.Join(path, "SKILL.md")
		info, err := os.Stat(skillMarkdown)
		if err == nil && info.Mode().IsRegular() {
			dirs = append(dirs, path)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}
func loadSkillDir(dir, scope string) (Spec, *Problem) {
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return Spec{}, nil
		}
		return Spec{}, skillProblemForDir(dir, scope, "", "", fmt.Sprintf("read skill markdown: %v", err))
	}
	meta, markdownBody, nameFromMarkdown, instruction, err := parseSkillMarkdown(string(body))
	if err != nil {
		return Spec{}, skillProblemForDir(dir, scope, "", "", fmt.Sprintf("parse skill markdown: %v", err))
	}
	scripts, err := discoverSkillScripts(dir)
	if err != nil {
		return Spec{}, skillProblemForDir(dir, scope, meta.ID, firstNonEmpty(meta.Name, nameFromMarkdown), err.Error())
	}
	spec := Spec{
		ID:           firstNonEmpty(meta.ID, filepath.Base(dir)),
		Name:         firstNonEmpty(meta.Name, nameFromMarkdown, filepath.Base(dir)),
		Version:      strings.TrimSpace(meta.Version),
		Category:     strings.TrimSpace(meta.Category),
		Summary:      strings.TrimSpace(meta.Summary),
		Instruction:  instruction,
		PromotedFrom: strings.TrimSpace(meta.PromotedFrom),
		Source:       strings.TrimSpace(scope),
		Origin:       meta.Origin,
		TaskPattern:  strings.TrimSpace(meta.TaskPattern),
		Path:         dir,
		Scripts:      scripts,
		Files:        discoverSkillFiles(dir),
		Tags:         append([]string(nil), meta.Tags...),
		Platforms:    append([]string(nil), meta.Platforms...),
		TriggerHints: append([]string(nil), meta.TriggerHints...),
		Requires: Requirements{
			Tools:    append([]string(nil), meta.Requires.Tools...),
			Toolsets: append([]string(nil), meta.Requires.Toolsets...),
			Bins:     append([]string(nil), meta.Requires.Bins...),
			Env:      append([]string(nil), meta.Requires.Env...),
		},
		CreatedByRunID: strings.TrimSpace(meta.CreatedByRunID),
		Replaces:       append([]string(nil), meta.Replaces...),
	}
	if strings.TrimSpace(spec.Instruction) == "" && strings.TrimSpace(markdownBody) != "" {
		spec.Instruction = strings.TrimSpace(markdownBody)
	}
	normalized, err := NormalizeSpec(spec)
	if err != nil {
		return Spec{}, skillProblemForDir(dir, scope, spec.ID, spec.Name, err.Error())
	}
	return normalized, nil
}

func discoverSkillScripts(dir string) ([]string, error) {
	root := filepath.Join(dir, "scripts")
	entries := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("discover skill scripts %s: %w", dir, err)
	}
	sort.Strings(entries)
	return entries, nil
}

func discoverSkillFiles(dir string) []string {
	entries := make([]string, 0)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil
	}
	sort.Strings(entries)
	return entries
}
