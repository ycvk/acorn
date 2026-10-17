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
	// ErrSkillPackageNotStaged means no package with that hash waits for
	// import.
	ErrSkillPackageNotStaged = errors.New("skill package not staged")
)

// SkillAuthor says who wrote a skill revision.
type SkillAuthor string

const (
	SkillAuthorAgent   SkillAuthor = "agent"
	SkillAuthorOwner   SkillAuthor = "owner"
	SkillAuthorRelease SkillAuthor = "release"
)

// SkillFile is one file a skill carries besides SKILL.md, at a slash-separated
// path relative to the skill's directory.
type SkillFile struct {
	Path    string
	Content []byte
}

// SkillFileInfo describes a stored skill file.
type SkillFileInfo struct {
	Path   string
	Size   int64
	SHA256 string
}

// Skill is the current revision of one skill. ID is the skill's name and
// Markdown its whole SKILL.md; Author wrote the current revision and Files
// are the files it carries. SeedSHA256 is the package hash of the built-in
// content last written by a release; it is empty for skills no release ships.
type Skill struct {
	ID         string
	Markdown   string
	Revision   int64
	Retired    bool
	Author     SkillAuthor
	SeedSHA256 string
	Files      []SkillFileInfo
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SkillWrite stores a new revision of a skill with its whole file set.
// BaseRevision must equal the stored revision, zero for a skill that does not
// exist yet. A non-empty SeedSHA256 replaces the recorded built-in hash.
type SkillWrite struct {
	ID           string
	Markdown     string
	Files        []SkillFile
	Retired      bool
	Author       SkillAuthor
	RunID        string
	SeedSHA256   string
	At           time.Time
	BaseRevision int64
}

// SkillPackage is a skill package read for import, kept under its package
// hash until it is imported.
type SkillPackage struct {
	SHA256   string
	Name     string
	Markdown string
	Files    []SkillFile
	StagedAt time.Time
}

// SkillStore holds the skills. Every write is a new revision.
type SkillStore interface {
	WriteSkill(ctx context.Context, write SkillWrite) (Skill, error)
	Skill(ctx context.Context, id string) (Skill, error)
	// Skills lists every skill, retired ones included, by id.
	Skills(ctx context.Context) ([]Skill, error)
	// SkillFiles reads the files of one revision of a skill.
	SkillFiles(ctx context.Context, id string, revision int64) ([]SkillFile, error)
	// SkillFile reads one file of one revision of a skill.
	SkillFile(ctx context.Context, id string, revision int64, path string) ([]byte, error)
	// SkillLoadsSince counts the skill tool's loads per skill id since a time.
	SkillLoadsSince(ctx context.Context, since time.Time) (map[string]int, error)
	// StageSkillPackage keeps a package for import and drops packages staged
	// before staleBefore.
	StageSkillPackage(ctx context.Context, pkg SkillPackage, staleBefore time.Time) error
	StagedSkillPackage(ctx context.Context, sha256 string) (SkillPackage, error)
	DeleteStagedSkillPackage(ctx context.Context, sha256 string) error
	// SetSkillSecret stores the value of a secret a skill's package declares;
	// SkillSecrets reads them by name. Values only go to the package's own
	// processes.
	SetSkillSecret(ctx context.Context, skill, name, value string, at time.Time) error
	SkillSecrets(ctx context.Context, skill string) (map[string]string, error)
}
