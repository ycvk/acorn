package knowledge

import (
	"context"
	"errors"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// MemoryView applies owner exclusions to agent reads. The owner's knowledge
// API continues to read notes through Vault.
type MemoryView struct {
	*Vault
	Store core.MemoryStore
}

// excludedQuotes returns the excluded fragments of one note revision; an
// excluded whole revision is core.ErrMemoryExcluded.
func (v *MemoryView) excludedQuotes(ctx context.Context, path string, revision int64) ([]string, error) {
	ex, err := v.Store.MemoryExclusions(ctx)
	if err != nil {
		return nil, err
	}
	id := core.KnowledgeSourceID(path, revision)
	var quotes []string
	for _, f := range ex.Fragments {
		if f.SourceID == id {
			if f.Quote == "" {
				return nil, core.ErrMemoryExcluded
			}
			quotes = append(quotes, f.Quote)
		}
	}
	return quotes, nil
}

func redactKnowledge(text string, quotes []string) string {
	for _, quote := range quotes {
		text = strings.ReplaceAll(text, quote, "[owner excluded this memory]")
	}
	return text
}

func (v *MemoryView) Read(ctx context.Context, path string) (core.KnowledgeNote, error) {
	note, err := v.Vault.Read(ctx, path)
	if err != nil {
		return note, err
	}
	quotes, err := v.excludedQuotes(ctx, note.Path, note.Revision)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	note.Body = redactKnowledge(note.Body, quotes)
	note.Title = redactKnowledge(note.Title, quotes)
	note.Source = redactKnowledge(note.Source, quotes)
	note.Tags = append([]string(nil), note.Tags...)
	for i, tag := range note.Tags {
		note.Tags[i] = redactKnowledge(tag, quotes)
	}
	return note, nil
}

func (v *MemoryView) filterHits(ctx context.Context, hits []core.KnowledgeHit) ([]core.KnowledgeHit, error) {
	out := make([]core.KnowledgeHit, 0, len(hits))
	for _, hit := range hits {
		quotes, err := v.excludedQuotes(ctx, hit.Path, hit.Revision)
		if errors.Is(err, core.ErrMemoryExcluded) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// A search snippet can cut an excluded quote in half. Regenerate it from
		// the filtered body whenever this note has partial exclusions.
		if len(quotes) > 0 {
			note, err := v.Read(ctx, hit.Path)
			if err != nil {
				return nil, err
			}
			hit.Title = note.Title
			hit.Tags = note.Tags
			runes := []rune(note.Body)
			hit.Snippet = string(runes[:min(len(runes), 240)])
		}
		out = append(out, hit)
	}
	return out, nil
}

func (v *MemoryView) Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error) {
	hits, err := v.Vault.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	return v.filterHits(ctx, hits)
}

func (v *MemoryView) Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) {
	hits, err := v.Vault.Recent(ctx, prefix, limit)
	if err != nil {
		return nil, err
	}
	return v.filterHits(ctx, hits)
}
