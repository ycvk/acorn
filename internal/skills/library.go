package skills

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// seedFS holds the built-in skills that ship with the binary.
//
//go:embed seed/*/SKILL.md
var seedFS embed.FS

const (
	maxSkillBytes        = 32 << 10
	maxSkillSummaryRunes = 200
)

var skillIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Library is the skill collection over the skill store: the built-in seed, the
// agent's writes and the parsed view runs and clients read.
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

// BuiltIn parses the skills that ship with the binary.
func BuiltIn() ([]Spec, error) {
	paths, err := fs.Glob(seedFS, "seed/*/SKILL.md")
	if err != nil {
		return nil, fmt.Errorf("list built-in skills: %w", err)
	}
	specs := make([]Spec, 0, len(paths))
	for _, path := range paths {
		raw, err := seedFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read built-in skill %s: %w", path, err)
		}
		markdown := normalizeMarkdown(string(raw))
		spec, err := Parse(markdown)
		if err != nil {
			return nil, fmt.Errorf("built-in skill %s: %w", path, err)
		}
		spec.Markdown = markdown
		specs = append(specs, spec)
	}
	return specs, nil
}

// Seed writes every built-in skill whose shipped content differs from what a
// release last wrote, as a new revision on top of the current one. A skill
// keeps its retired state.
func (l *Library) Seed(ctx context.Context) error {
	builtIn, err := BuiltIn()
	if err != nil {
		return err
	}
	for _, spec := range builtIn {
		sum := sha256.Sum256([]byte(spec.Markdown))
		seed := hex.EncodeToString(sum[:])
		write := core.SkillWrite{ID: spec.ID, Markdown: spec.Markdown, Author: core.SkillAuthorRelease, SeedSHA256: seed, At: l.clock()}
		current, err := l.store.Skill(ctx, spec.ID)
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
			problems = append(problems, Problem{ID: skill.ID, Error: fmt.Sprintf("parse skill: %v", err)})
			continue
		}
		spec.Revision, spec.Markdown = skill.Revision, skill.Markdown
		items = append(items, spec)
	}
	sortSkills(items)
	items, nameProblems := filterDuplicateSkillNames(items)
	problems = append(problems, nameProblems...)
	sortSkillProblems(problems)
	return &ScanResult{Skills: items, Problems: problems}, nil
}

// Write stores the agent's skill document as a new revision based on
// baseRevision, zero for a new skill. Writing a retired skill brings it back.
func (l *Library) Write(ctx context.Context, markdown string, baseRevision int64, runID string) (core.Skill, error) {
	markdown = normalizeMarkdown(markdown)
	spec, err := validateSkill(markdown)
	if err != nil {
		return core.Skill{}, err
	}
	stored, err := l.store.Skills(ctx)
	if err != nil {
		return core.Skill{}, err
	}
	for _, other := range stored {
		if other.ID == spec.ID || other.Retired {
			continue
		}
		if parsed, err := Parse(other.Markdown); err == nil && parsed.Name == spec.Name {
			return core.Skill{}, fmt.Errorf("skill name %q is already used by %s", spec.Name, other.ID)
		}
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: spec.ID, Markdown: markdown, Author: core.SkillAuthorAgent, RunID: runID, At: l.clock(), BaseRevision: baseRevision})
}

// Retire takes a skill out of use as a new revision; writing it again brings
// it back.
func (l *Library) Retire(ctx context.Context, id string, baseRevision int64, runID string) (core.Skill, error) {
	current, err := l.store.Skill(ctx, strings.TrimSpace(id))
	if err != nil {
		return core.Skill{}, err
	}
	if current.Retired {
		return core.Skill{}, fmt.Errorf("skill %s is already retired", current.ID)
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: current.ID, Markdown: current.Markdown, Retired: true, Author: core.SkillAuthorAgent, RunID: runID, At: l.clock(), BaseRevision: baseRevision})
}

func validateSkill(markdown string) (Spec, error) {
	if len(markdown) > maxSkillBytes {
		return Spec{}, fmt.Errorf("skill markdown is %d bytes; the limit is %d", len(markdown), maxSkillBytes)
	}
	spec, err := Parse(markdown)
	if err != nil {
		return Spec{}, err
	}
	switch {
	case !skillIDPattern.MatchString(spec.ID):
		return Spec{}, fmt.Errorf("skill id %q must be 1-64 lowercase letters, digits, dots, dashes or underscores", spec.ID)
	case spec.Summary == "":
		return Spec{}, fmt.Errorf("skill %s needs a summary that says when to use it", spec.ID)
	case utf8.RuneCountInString(spec.Summary) > maxSkillSummaryRunes:
		return Spec{}, fmt.Errorf("skill %s summary is longer than %d characters", spec.ID, maxSkillSummaryRunes)
	case spec.Instruction == "":
		return Spec{}, fmt.Errorf("skill %s has no instruction", spec.ID)
	}
	return spec, nil
}

func normalizeMarkdown(markdown string) string {
	text := strings.TrimPrefix(strings.ReplaceAll(markdown, "\r\n", "\n"), "\uFEFF")
	return strings.TrimSpace(text) + "\n"
}
