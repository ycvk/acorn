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

// knowledgeVault is the part of the knowledge base the client API uses.
type knowledgeVault interface {
	Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error)
	Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error)
	Read(ctx context.Context, path string) (core.KnowledgeNote, error)
	ReadAttachment(ctx context.Context, path string) (core.Attachment, error)
	Delete(ctx context.Context, path string, revision int64) error
}

// noteForgetter forgets a note the owner deletes.
type noteForgetter interface {
	ForgetNoteForOwner(ctx context.Context, path, title string) (core.MemoryForgotten, error)
}

// KnowledgeService serves the knowledge base to clients: reading notes and
// deleting them on the owner's request.
type KnowledgeService struct {
	vault     knowledgeVault
	forgetter noteForgetter
	barrier   func(context.Context, core.MemoryExclusion, string) error
}

// Deleting a note forgets it through forgetter, then waits at barrier for
// the runs that used it to stop before the note is removed.
func NewKnowledgeService(vault knowledgeVault, forgetter noteForgetter, barrier func(context.Context, core.MemoryExclusion, string) error) (*KnowledgeService, error) {
	if vault == nil || forgetter == nil || barrier == nil {
		return nil, errors.New("knowledge service: vault, forgetter and barrier are required")
	}
	return &KnowledgeService{vault: vault, forgetter: forgetter, barrier: barrier}, nil
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

// DeleteNote deletes a note and makes Acorn forget it: every revision is
// excluded from memory before the note is removed. Images the note links
// stay, since the conversation that shared them may show them too.
func (s *KnowledgeService) DeleteNote(ctx context.Context, path string) error {
	note, err := s.vault.Read(ctx, path)
	if err != nil {
		return err
	}
	forgotten, err := s.forgetter.ForgetNoteForOwner(ctx, note.Path, note.Title)
	if err != nil {
		return err
	}
	if err := s.barrier(ctx, forgotten.Exclusion, ""); err != nil {
		return err
	}
	return s.vault.Delete(ctx, note.Path, note.Revision)
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
