package core

import (
	"context"
	"time"
)

// KnowledgeNote is the indexed form of one markdown note in the knowledge
// directory. The file is the truth; the index can be rebuilt from it.
type KnowledgeNote struct {
	Path      string // relative to the knowledge directory, slash-separated
	Title     string
	Tags      []string
	Body      string
	MTimeNS   int64
	Size      int64
	UpdatedAt time.Time
}

// KnowledgeFileStat is what the index knows about a file, used to find notes
// that changed on disk.
type KnowledgeFileStat struct {
	Path    string
	MTimeNS int64
	Size    int64
}

// KnowledgeHit is one note in a search or listing result.
type KnowledgeHit struct {
	Path      string
	Title     string
	Tags      []string
	Snippet   string
	UpdatedAt time.Time
}

// KnowledgeStore persists the full-text index of the knowledge directory.
type KnowledgeStore interface {
	UpsertKnowledgeNote(ctx context.Context, note KnowledgeNote) error
	DeleteKnowledgeNote(ctx context.Context, path string) error
	ListKnowledgeFileStats(ctx context.Context) ([]KnowledgeFileStat, error)
	// SearchKnowledge ranks by relevance; queries shorter than three
	// characters match by substring and rank by recency.
	SearchKnowledge(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
	// RecentKnowledge lists notes under prefix by update time, newest first.
	RecentKnowledge(ctx context.Context, prefix string, limit int) ([]KnowledgeHit, error)
}
