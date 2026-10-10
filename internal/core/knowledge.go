package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"
)

var (
	ErrKnowledgeNoteNotFound = errors.New("knowledge note not found")
	// ErrKnowledgeConflict means the note changed after the revision a write
	// was based on.
	ErrKnowledgeConflict = errors.New("knowledge note changed concurrently")
)

// KnowledgeNote is the current revision of one note. Path is relative to the
// knowledge base and slash-separated.
type KnowledgeNote struct {
	Path      string
	Title     string
	Tags      []string
	Source    string
	Body      string
	Revision  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// KnowledgeWrite replaces a note's content as a new revision. BaseRevision,
// when positive, must match the stored revision. RunID attributes the write to
// the agent run that made it.
type KnowledgeWrite struct {
	Path         string
	Title        string
	Tags         []string
	Source       string
	Body         string
	RunID        string
	At           time.Time
	BaseRevision int64
}

// KnowledgeHit is one note in a search or listing result.
type KnowledgeHit struct {
	Path      string
	Title     string
	Tags      []string
	Snippet   string
	Revision  int64
	UpdatedAt time.Time
}

// KnowledgeSourceID names the memory source of one note revision.
func KnowledgeSourceID(path string, revision int64) string {
	sum := sha256.Sum256([]byte(path + "\x00" + strconv.FormatInt(revision, 10)))
	return "knowledge:" + hex.EncodeToString(sum[:16])
}

// KnowledgeStore holds the knowledge base. Every write is a new revision and
// registers that revision as a memory source in the same transaction.
type KnowledgeStore interface {
	WriteKnowledgeNote(ctx context.Context, write KnowledgeWrite) (KnowledgeNote, error)
	KnowledgeNote(ctx context.Context, path string) (KnowledgeNote, error)
	CountKnowledgeNotes(ctx context.Context) (int, error)
	// SearchKnowledge ranks by relevance; queries shorter than three
	// characters match by substring and rank by recency.
	SearchKnowledge(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
	// RecentKnowledge lists notes under prefix by update time, newest first.
	RecentKnowledge(ctx context.Context, prefix string, limit int) ([]KnowledgeHit, error)
}
