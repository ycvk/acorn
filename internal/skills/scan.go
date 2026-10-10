package skills

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

func (l *Loader) ScanSkills(ctx context.Context) (*ScanResult, error) {
	_ = ctx
	dirs, err := discoverSkillDirs(l.dir)
	if err != nil {
		return nil, fmt.Errorf("discover skills under %s: %w", l.dir, err)
	}
	seen := make(map[string]string, len(dirs))
	items := make([]Spec, 0, len(dirs))
	problems := make([]Problem, 0)
	for _, dir := range dirs {
		item, problem := loadSkillDir(dir)
		if problem != nil {
			problems = append(problems, *problem)
			continue
		}
		if previous, ok := seen[item.ID]; ok {
			problems = append(problems, Problem{ID: item.ID, Name: item.Name, Path: item.Path, Error: fmt.Sprintf("duplicate skill id %q (conflicts with %s)", item.ID, previous)})
			continue
		}
		seen[item.ID] = item.Path
		items = append(items, item)
	}
	sortSkills(items)
	items, nameProblems := filterDuplicateSkillNames(items)
	problems = append(problems, nameProblems...)
	sortSkillProblems(problems)
	return &ScanResult{Skills: items, Problems: problems}, nil
}

func discoverSkillDirs(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
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
func loadSkillDir(dir string) (Spec, *Problem) {
	body, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Spec{}, skillProblemForDir(dir, "", "", fmt.Sprintf("read skill markdown: %v", err))
	}
	meta, nameFromMarkdown, instruction, err := parseSkillMarkdown(string(body))
	if err != nil {
		return Spec{}, skillProblemForDir(dir, "", "", fmt.Sprintf("parse skill markdown: %v", err))
	}
	scripts, err := discoverSkillScripts(dir)
	if err != nil {
		return Spec{}, skillProblemForDir(dir, meta.ID, FirstNonEmpty(meta.Name, nameFromMarkdown), err.Error())
	}
	spec := Spec{
		ID:           FirstNonEmpty(meta.ID, filepath.Base(dir)),
		Name:         FirstNonEmpty(meta.Name, nameFromMarkdown, filepath.Base(dir)),
		Version:      meta.Version,
		Category:     meta.Category,
		Summary:      meta.Summary,
		Instruction:  instruction,
		Path:         dir,
		Scripts:      scripts,
		Files:        discoverSkillFiles(dir),
		Tags:         meta.Tags,
		Platforms:    meta.Platforms,
		TriggerHints: meta.TriggerHints,
		Requires: Requirements{
			Tools:    meta.Requires.Tools,
			Toolsets: meta.Requires.Toolsets,
			Bins:     meta.Requires.Bins,
			Env:      meta.Requires.Env,
		},
	}
	normalized, err := NormalizeSpec(spec)
	if err != nil {
		return Spec{}, skillProblemForDir(dir, spec.ID, spec.Name, err.Error())
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
