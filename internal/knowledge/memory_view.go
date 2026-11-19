package knowledge

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/redact"
)

// MemoryView applies owner exclusions to agent reads. The owner's knowledge
// API continues to read notes through Vault.
type MemoryView struct {
	*Vault
	Store core.MemoryStore
}

// noteExclusions are the owner's exclusions by source id: the quotes excluded
// from a note revision, or the whole revision.
type noteExclusions struct {
	quotes map[string][]string
	whole  map[string]bool
}

func (v *MemoryView) exclusions(ctx context.Context) (noteExclusions, error) {
	ex, err := v.Store.MemoryExclusions(ctx)
	if err != nil {
		return noteExclusions{}, err
	}
	out := noteExclusions{quotes: map[string][]string{}, whole: map[string]bool{}}
	for _, f := range ex.Fragments {
		if f.Quote == "" {
			out.whole[f.SourceID] = true
		} else {
			out.quotes[f.SourceID] = append(out.quotes[f.SourceID], f.Quote)
		}
	}
	return out, nil
}

// of returns the excluded quotes of one note revision; an excluded whole
// revision is core.ErrMemoryExcluded.
func (e noteExclusions) of(path string, revision int64) ([]string, error) {
	id := core.KnowledgeSourceID(path, revision)
	if e.whole[id] {
		return nil, core.ErrMemoryExcluded
	}
	return e.quotes[id], nil
}

// redactNote hides the excluded quotes the way memory sources do, so a quote
// the owner took from memory's redacted text, or quotes that overlap, hide
// the same words here.
func redactNote(note core.KnowledgeNote, quotes []string) core.KnowledgeNote {
	note.Body = redact.Render(note.Body, quotes, false)
	note.Title = redact.Render(note.Title, quotes, false)
	note.Source = redact.Render(note.Source, quotes, false)
	tags := make([]string, len(note.Tags))
	for i, tag := range note.Tags {
		tags[i] = redact.Render(tag, quotes, false)
	}
	note.Tags = tags
	return note
}

func (v *MemoryView) Read(ctx context.Context, path string) (core.KnowledgeNote, error) {
	note, err := v.Vault.Read(ctx, path)
	if err != nil {
		return note, err
	}
	ex, err := v.exclusions(ctx)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	quotes, err := ex.of(note.Path, note.Revision)
	if err != nil {
		return core.KnowledgeNote{}, err
	}
	return redactNote(note, quotes), nil
}

func (v *MemoryView) filterHits(ctx context.Context, hits []core.KnowledgeHit) ([]core.KnowledgeHit, error) {
	ex, err := v.exclusions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.KnowledgeHit, 0, len(hits))
	for _, hit := range hits {
		quotes, err := ex.of(hit.Path, hit.Revision)
		if errors.Is(err, core.ErrMemoryExcluded) {
			continue
		}
		// A search snippet can cut an excluded quote in half. Regenerate it from
		// the filtered body whenever this note has partial exclusions.
		if len(quotes) > 0 {
			note, err := v.Vault.Read(ctx, hit.Path)
			if err != nil {
				return nil, err
			}
			quotes, err := ex.of(note.Path, note.Revision)
			if err != nil {
				return nil, err
			}
			note = redactNote(note, quotes)
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
