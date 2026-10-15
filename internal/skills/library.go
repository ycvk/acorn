package skills

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// seedFS holds the built-in skills that ship with the binary, one directory
// per skill named after it.
//
//go:embed seed
var seedFS embed.FS

// stageLifetime is how long a package read for import waits for approval.
const stageLifetime = 7 * 24 * time.Hour

// Library is the skill collection over the skill store: the built-in seed, the
// agent's and the owner's writes, and the parsed view runs and clients read.
type Library struct {
	store core.SkillStore
	clock func() time.Time
}

func NewLibrary(store core.SkillStore, clock func() time.Time) (*Library, error) {
	switch {
	case store == nil:
		return nil, errors.New("skill library: store is required")
	case clock == nil:
		return nil, errors.New("skill library: clock is required")
	}
	return &Library{store: store, clock: clock}, nil
}

// Package is a skill with the files it carries.
type Package struct {
	Spec     Spec
	Markdown string
	Files    []core.SkillFile
}

// BuiltIn reads the skills that ship with the binary.
func BuiltIn() ([]Package, error) {
	dirs, err := fs.ReadDir(seedFS, "seed")
	if err != nil {
		return nil, fmt.Errorf("list built-in skills: %w", err)
	}
	var packages []Package
	for _, dir := range dirs {
		root := path.Join("seed", dir.Name())
		var entries []core.SkillFile
		err := fs.WalkDir(seedFS, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			content, err := seedFS.ReadFile(p)
			if err != nil {
				return err
			}
			entries = append(entries, core.SkillFile{Path: strings.TrimPrefix(p, root+"/"), Content: content})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read built-in skill %s: %w", dir.Name(), err)
		}
		pkg, err := packageOf(entries)
		if err != nil {
			return nil, fmt.Errorf("built-in skill %s: %w", dir.Name(), err)
		}
		if pkg.Spec.Name != dir.Name() {
			return nil, fmt.Errorf("built-in skill directory %s holds skill %s", dir.Name(), pkg.Spec.Name)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// packageOf checks the files read from a skill's directory or archive.
func packageOf(entries []core.SkillFile) (Package, error) {
	markdown, files, err := FromEntries(entries)
	if err != nil {
		return Package{}, err
	}
	markdown = normalizeMarkdown(markdown)
	spec, err := validateSkill(markdown)
	if err != nil {
		return Package{}, err
	}
	files, err = normalizeFiles(markdown, files)
	if err != nil {
		return Package{}, err
	}
	return Package{Spec: spec, Markdown: markdown, Files: files}, nil
}

// Seed writes every built-in skill whose shipped package differs from what a
// release last wrote, as a new revision on top of the current one. A skill
// keeps its retired state.
func (l *Library) Seed(ctx context.Context) error {
	builtIn, err := BuiltIn()
	if err != nil {
		return err
	}
	for _, pkg := range builtIn {
		seed := PackageHash(pkg.Markdown, pkg.Files)
		write := core.SkillWrite{ID: pkg.Spec.Name, Markdown: pkg.Markdown, Files: pkg.Files, Author: core.SkillAuthorRelease, SeedSHA256: seed, At: l.clock()}
		current, err := l.store.Skill(ctx, pkg.Spec.Name)
		switch {
		case errors.Is(err, core.ErrSkillNotFound):
		case err != nil:
			return err
		case current.SeedSHA256 == seed:
			continue
		default:
			write.BaseRevision, write.Retired = current.Revision, current.Retired
		}
		if _, err := l.store.WriteSkill(ctx, write); err != nil {
			return err
		}
	}
	return nil
}

// Scan parses the skills in use. A stored skill that no longer parses is a
// problem, not a skill.
func (l *Library) Scan(ctx context.Context) (*ScanResult, error) {
	stored, err := l.store.Skills(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Spec, 0, len(stored))
	problems := []Problem{}
	for _, skill := range stored {
		if skill.Retired {
			continue
		}
		spec, err := Parse(skill.Markdown)
		if err != nil {
			problems = append(problems, Problem{Name: skill.ID, Error: fmt.Sprintf("parse skill: %v", err)})
			continue
		}
		spec.Revision, spec.Markdown, spec.Files = skill.Revision, skill.Markdown, slices.Clone(skill.Files)
		items = append(items, spec)
	}
	sortSkills(items)
	sortSkillProblems(problems)
	return &ScanResult{Skills: items, Problems: problems}, nil
}

// Skill reads a stored skill, retired or not.
func (l *Library) Skill(ctx context.Context, name string) (core.Skill, error) {
	return l.store.Skill(ctx, strings.TrimSpace(name))
}

// WriteRequest is the agent's change to a skill: its SKILL.md, text files to
// add or replace, and files to remove, based on BaseRevision, zero for a new
// skill.
type WriteRequest struct {
	Markdown     string
	Put          map[string]string
	Delete       []string
	BaseRevision int64
	RunID        string
}

// Write stores the agent's change as a new revision. Writing a retired skill
// brings it back.
func (l *Library) Write(ctx context.Context, req WriteRequest) (core.Skill, error) {
	markdown := normalizeMarkdown(req.Markdown)
	spec, err := validateSkill(markdown)
	if err != nil {
		return core.Skill{}, err
	}
	var files []core.SkillFile
	if req.BaseRevision > 0 {
		if files, err = l.store.SkillFiles(ctx, spec.Name, req.BaseRevision); err != nil {
			return core.Skill{}, err
		}
	}
	for _, p := range req.Delete {
		i := slices.IndexFunc(files, func(f core.SkillFile) bool { return f.Path == p })
		if i < 0 {
			return core.Skill{}, fmt.Errorf("skill %s has no file %q to delete", spec.Name, p)
		}
		files = slices.Delete(files, i, i+1)
	}
	for p, content := range req.Put {
		files = slices.DeleteFunc(files, func(f core.SkillFile) bool { return f.Path == p })
		files = append(files, core.SkillFile{Path: p, Content: []byte(content)})
	}
	if files, err = normalizeFiles(markdown, files); err != nil {
		return core.Skill{}, err
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: spec.Name, Markdown: markdown, Files: files, Author: core.SkillAuthorAgent, RunID: req.RunID, At: l.clock(), BaseRevision: req.BaseRevision})
}

// Retire takes a skill out of use as a new revision; writing it again brings
// it back.
func (l *Library) Retire(ctx context.Context, name string, baseRevision int64, runID string) (core.Skill, error) {
	current, err := l.store.Skill(ctx, strings.TrimSpace(name))
	if err != nil {
		return core.Skill{}, err
	}
	if current.Retired {
		return core.Skill{}, fmt.Errorf("skill %s is already retired", current.ID)
	}
	files, err := l.store.SkillFiles(ctx, current.ID, current.Revision)
	if err != nil {
		return core.Skill{}, err
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: current.ID, Markdown: current.Markdown, Files: files, Retired: true, Author: core.SkillAuthorAgent, RunID: runID, At: l.clock(), BaseRevision: baseRevision})
}

// Stage checks a package read from a directory or zip and keeps it under its
// hash for import.
func (l *Library) Stage(ctx context.Context, entries []core.SkillFile) (core.SkillPackage, error) {
	pkg, err := packageOf(entries)
	if err != nil {
		return core.SkillPackage{}, err
	}
	now := l.clock()
	staged := core.SkillPackage{SHA256: PackageHash(pkg.Markdown, pkg.Files), Name: pkg.Spec.Name, Markdown: pkg.Markdown, Files: pkg.Files, StagedAt: now}
	if err := l.store.StageSkillPackage(ctx, staged, now.Add(-stageLifetime)); err != nil {
		return core.SkillPackage{}, err
	}
	return staged, nil
}

// ImportStaged stores the staged package with that hash, which must hold the
// named skill, as the agent's revision based on baseRevision, zero for a new
// skill.
func (l *Library) ImportStaged(ctx context.Context, name, sha256 string, baseRevision int64, runID string) (core.Skill, error) {
	pkg, err := l.store.StagedSkillPackage(ctx, strings.TrimSpace(sha256))
	if err != nil {
		return core.Skill{}, err
	}
	if pkg.Name != strings.TrimSpace(name) {
		return core.Skill{}, fmt.Errorf("staged package %s holds skill %s, not %s", pkg.SHA256, pkg.Name, name)
	}
	skill, err := l.store.WriteSkill(ctx, core.SkillWrite{ID: pkg.Name, Markdown: pkg.Markdown, Files: pkg.Files, Author: core.SkillAuthorAgent, RunID: runID, At: l.clock(), BaseRevision: baseRevision})
	if err != nil {
		return core.Skill{}, err
	}
	return skill, l.store.DeleteStagedSkillPackage(ctx, sha256)
}

// Import stores a package the owner installs. An existing skill of the same
// name is replaced only when replace is set.
func (l *Library) Import(ctx context.Context, entries []core.SkillFile, replace bool) (core.Skill, error) {
	pkg, err := packageOf(entries)
	if err != nil {
		return core.Skill{}, err
	}
	var base int64
	current, err := l.store.Skill(ctx, pkg.Spec.Name)
	switch {
	case errors.Is(err, core.ErrSkillNotFound):
	case err != nil:
		return core.Skill{}, err
	case !replace:
		return core.Skill{}, fmt.Errorf("skill %s exists at revision %d; replace it explicitly", current.ID, current.Revision)
	default:
		base = current.Revision
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: pkg.Spec.Name, Markdown: pkg.Markdown, Files: pkg.Files, Author: core.SkillAuthorOwner, At: l.clock(), BaseRevision: base})
}

// PackageHost keeps skill packages where the agent can read and run them.
type PackageHost interface {
	// SkillPackages reports the directory packages live in and the hash of
	// each package there.
	SkillPackages(ctx context.Context) (dir string, hashes map[string]string, err error)
	PutSkillPackage(ctx context.Context, name, hash, markdown string, files []core.SkillFile) error
	DeleteSkillPackage(ctx context.Context, name string) error
}

// Sync makes the host hold exactly the eligible skills that carry files, and
// returns the directory they are in.
func (l *Library) Sync(ctx context.Context, host PackageHost, snapshot Snapshot) (string, error) {
	dir, held, err := host.SkillPackages(ctx)
	if err != nil {
		return "", fmt.Errorf("list skill packages: %w", err)
	}
	wanted := map[string]bool{}
	for _, view := range snapshot.Skills {
		if !view.Eligible || len(view.Files) == 0 {
			continue
		}
		wanted[view.Name] = true
		hash := packageHash(view.Markdown, infoSums(view.Files))
		if held[view.Name] == hash {
			continue
		}
		files, err := l.store.SkillFiles(ctx, view.Name, view.Revision)
		if err != nil {
			return "", err
		}
		if err := host.PutSkillPackage(ctx, view.Name, hash, view.Markdown, files); err != nil {
			return "", fmt.Errorf("put skill package %s: %w", view.Name, err)
		}
	}
	for name := range held {
		if wanted[name] {
			continue
		}
		if err := host.DeleteSkillPackage(ctx, name); err != nil {
			return "", fmt.Errorf("delete skill package %s: %w", name, err)
		}
	}
	return dir, nil
}

func validateSkill(markdown string) (Spec, error) {
	spec, err := Parse(markdown)
	if err != nil {
		return Spec{}, err
	}
	if spec.Instruction == "" {
		return Spec{}, fmt.Errorf("skill %s has no instruction", spec.Name)
	}
	return spec, nil
}

func normalizeMarkdown(markdown string) string {
	text := strings.TrimPrefix(strings.ReplaceAll(markdown, "\r\n", "\n"), "\uFEFF")
	return strings.TrimSpace(text) + "\n"
}
