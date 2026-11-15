package skills

import (
	"context"
	"errors"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// Change is what a write by the agent would do to the stored skills.
type Change struct {
	// Name and Description are those of the skill the write stores. Name is
	// empty when its SKILL.md does not parse.
	Name        string
	Description string
	// Base is the revision the write builds on, zero for a new skill.
	Base int64
	// Current is the stored skill of that name, nil when there is none.
	Current *core.Skill
	// Files are the files the write adds, changes and removes, SKILL.md
	// first: against the stored package, or all of the package, as added,
	// for a skill that is not stored. Empty for a write that would fail.
	Files []FileDiff
	// Fails is why the write would be refused before it reaches the store.
	Fails error
}

// Stale reports a write that builds on a revision the skill is not at, which
// the store refuses.
func (c Change) Stale() bool {
	var revision int64
	if c.Current != nil {
		revision = c.Current.Revision
	}
	return c.Name != "" && revision != c.Base
}

// refused is c for a write the library would refuse, with why.
func (c Change) refused(why error) Change {
	c.Fails = why
	return c
}

// WriteChange is what Write would do with req. It stores nothing.
func (l *Library) WriteChange(ctx context.Context, req WriteRequest) (Change, error) {
	markdown := normalizeMarkdown(req.Markdown)
	spec, err := validateSkill(markdown)
	if err != nil {
		return Change{Base: req.BaseRevision}.refused(err), nil
	}
	change, stored, err := l.against(ctx, spec.Name, spec.Description, req.BaseRevision)
	if err != nil || change.Stale() {
		return change, err
	}
	files, err := l.written(spec.Name, markdown, stored, req)
	if err != nil {
		return change.refused(err), nil
	}
	change.Files = changedFiles(change.Current, stored, markdown, files)
	return change, nil
}

// ImportChange is what ImportStaged would do with the staged package of
// that hash. It stores nothing and leaves the package staged.
func (l *Library) ImportChange(ctx context.Context, name, sha256 string, baseRevision int64) (Change, error) {
	name = strings.TrimSpace(name)
	pkg, err := l.store.StagedSkillPackage(ctx, strings.TrimSpace(sha256))
	switch {
	case errors.Is(err, core.ErrSkillPackageNotStaged):
		return Change{Name: name, Base: baseRevision}.refused(err), nil
	case err != nil:
		return Change{}, err
	}
	spec, err := validateSkill(pkg.Markdown)
	if err == nil {
		err = l.importable(name, pkg)
	}
	if err != nil {
		return Change{Name: name, Base: baseRevision}.refused(err), nil
	}
	change, stored, err := l.against(ctx, pkg.Name, spec.Description, baseRevision)
	if err != nil || change.Stale() {
		return change, err
	}
	change.Files = changedFiles(change.Current, stored, pkg.Markdown, pkg.Files)
	return change, nil
}

// against starts the Change of a write of the named skill on base: the
// stored skill and, when the write builds on its revision, its files.
func (l *Library) against(ctx context.Context, name, description string, base int64) (Change, []core.SkillFile, error) {
	change := Change{Name: name, Description: description, Base: base}
	current, err := l.store.Skill(ctx, name)
	switch {
	case errors.Is(err, core.ErrSkillNotFound):
		return change, nil, nil
	case err != nil:
		return Change{}, nil, err
	}
	change.Current = &current
	if change.Stale() {
		return change, nil, nil
	}
	stored, err := l.store.SkillFiles(ctx, current.ID, current.Revision)
	if err != nil {
		return Change{}, nil, err
	}
	return change, stored, nil
}

// changedFiles are the files of a package that differ from the stored one.
// With no stored skill, they are all of the package, as added.
func changedFiles(current *core.Skill, stored []core.SkillFile, markdown string, files []core.SkillFile) []FileDiff {
	next := Version{Revision: core.SkillRevision{Markdown: markdown}, Files: files}
	if current == nil {
		diffs := Diff(Version{}, next)
		for i := range diffs {
			diffs[i].Change = FileAdded
		}
		return diffs
	}
	return Diff(Version{Revision: core.SkillRevision{Markdown: current.Markdown}, Files: stored}, next)
}
