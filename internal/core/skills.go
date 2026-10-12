package core

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSkillNotFound = errors.New("skill not found")
	// ErrSkillConflict means the skill is not at the revision a write was
	// based on.
	ErrSkillConflict = errors.New("skill changed concurrently")
)

// SkillAuthor says who wrote a skill revision.
type SkillAuthor string

const (
	SkillAuthorAgent   SkillAuthor = "agent"
	SkillAuthorRelease SkillAuthor = "release"
)

// Skill is the current revision of one skill. Markdown is the whole skill
// document, frontmatter included, and Author wrote the current revision.
// SeedSHA256 is the hash of the built-in content last written by a release; it
// is empty for skills no release ships.
type Skill struct {
	ID         string
	Markdown   string
	Revision   int64
	Retired    bool
	Author     SkillAuthor
	SeedSHA256 string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SkillWrite stores a new revision of a skill. BaseRevision must equal the
// stored revision, zero for a skill that does not exist yet. A non-empty
// SeedSHA256 replaces the recorded built-in hash.
type SkillWrite struct {
	ID           string
	Markdown     string
	Retired      bool
	Author       SkillAuthor
	RunID        string
	SeedSHA256   string
	At           time.Time
	BaseRevision int64
}

// SkillStore holds the skills. Every write is a new revision.
type SkillStore interface {
	WriteSkill(ctx context.Context, write SkillWrite) (Skill, error)
	Skill(ctx context.Context, id string) (Skill, error)
	// Skills lists every skill, retired ones included, by id.
	Skills(ctx context.Context) ([]Skill, error)
	// SkillLoadsSince counts the skill tool's loads per skill id since a time.
	SkillLoadsSince(ctx context.Context, since time.Time) (map[string]int, error)
}
