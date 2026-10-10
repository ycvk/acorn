package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	defaultKnowledgeListLimit = 50
	maxKnowledgeListLimit     = 100
)

// knowledgeReader is the part of the knowledge base the client API reads.
type knowledgeReader interface {
	Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error)
	Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error)
	Read(ctx context.Context, path string) (core.KnowledgeNote, error)
	ReadAttachment(ctx context.Context, path string) (core.Attachment, error)
}

// KnowledgeService serves the knowledge base to clients, read-only.
type KnowledgeService struct {
	vault knowledgeReader
}

func NewKnowledgeService(vault knowledgeReader) *KnowledgeService {
	return &KnowledgeService{vault: vault}
}

// KnowledgeNoteSummaryDTO is one note in a listing.
type KnowledgeNoteSummaryDTO struct {
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	Tags      []string  `json:"tags"`
	Snippet   string    `json:"snippet"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KnowledgeNoteListResponse is GET /v1/knowledge/notes.
type KnowledgeNoteListResponse struct {
	Notes []KnowledgeNoteSummaryDTO `json:"notes"`
}

// KnowledgeNoteDTO is GET /v1/knowledge/note.
type KnowledgeNoteDTO struct {
	Path      string     `json:"path"`
	Title     string     `json:"title"`
	Tags      []string   `json:"tags"`
	Source    string     `json:"source,omitempty"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at"`
	Body      string     `json:"body"`
}

// ListNotes searches with query, or lists notes under prefix by update time.
func (s *KnowledgeService) ListNotes(ctx context.Context, query, prefix string, limit int) (KnowledgeNoteListResponse, error) {
	if s == nil || s.vault == nil {
		return KnowledgeNoteListResponse{}, errors.New("knowledge service is not initialized")
	}
	var (
		hits []core.KnowledgeHit
		err  error
	)
	if query = strings.TrimSpace(query); query != "" {
		hits, err = s.vault.Search(ctx, query, limit)
	} else {
		hits, err = s.vault.Recent(ctx, strings.TrimSpace(prefix), limit)
	}
	if err != nil {
		return KnowledgeNoteListResponse{}, err
	}
	out := KnowledgeNoteListResponse{Notes: make([]KnowledgeNoteSummaryDTO, 0, len(hits))}
	for _, hit := range hits {
		out.Notes = append(out.Notes, KnowledgeNoteSummaryDTO{
			Path:      hit.Path,
			Title:     hit.Title,
			Tags:      nonNilTags(hit.Tags),
			Snippet:   hit.Snippet,
			UpdatedAt: hit.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

// GetNote reads one note.
func (s *KnowledgeService) GetNote(ctx context.Context, path string) (KnowledgeNoteDTO, error) {
	if s == nil || s.vault == nil {
		return KnowledgeNoteDTO{}, errors.New("knowledge service is not initialized")
	}
	note, err := s.vault.Read(ctx, path)
	if err != nil {
		return KnowledgeNoteDTO{}, err
	}
	created := note.CreatedAt.UTC()
	dto := KnowledgeNoteDTO{
		Path:      note.Path,
		Title:     note.Title,
		Tags:      nonNilTags(note.Tags),
		Source:    note.Source,
		CreatedAt: &created,
		UpdatedAt: note.UpdatedAt.UTC(),
		Body:      note.Body,
	}
	return dto, nil
}

// GetAttachment reads one image a capture stored.
func (s *KnowledgeService) GetAttachment(ctx context.Context, path string) (core.Attachment, error) {
	if s == nil || s.vault == nil {
		return core.Attachment{}, errors.New("knowledge service is not initialized")
	}
	return s.vault.ReadAttachment(ctx, path)
}

func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}
