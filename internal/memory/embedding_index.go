package memory

import (
	"context"
	"log/slog"
	"strings"
)

// indexEmbedding generates and stores an embedding for a memory record. It is
// called after a record is written to disk (mutation, fact creation, history
// append). When embedding is not configured or the vector index is absent,
// this is a no-op. Errors are logged but never returned: a failed embedding
// must not break the write path — the record is already on disk and the
// keyword index is already rebuilt. The next search will simply not find this
// record via semantic matching until the embedding is retried.
func (s *LocalService) indexEmbedding(ctx context.Context, record Record) {
	if s.embedding == nil || s.vectors == nil {
		return
	}
	// Embed title + body together: the title carries the most signal, the body
	// adds context. Truncate to avoid excessive token usage on large records.
	text := strings.TrimSpace(record.Title + "\n\n" + record.Body)
	if text == "" {
		return
	}
	const maxEmbedText = 8000
	if len(text) > maxEmbedText {
		text = text[:maxEmbedText]
	}
	embedding, err := s.embedding.Embed(ctx, text)
	if err != nil {
		slog.Warn("embedding generation failed; record remains keyword-searchable",
			"ref", record.Ref, "kind", record.Kind, "err", err)
		return
	}
	scope := record.Scope
	if scope == "" {
		scope = "user"
	}
	if err := s.vectors.UpsertEmbedding(ctx, record.Ref, string(record.Kind), record.Title, scope, embedding); err != nil {
		slog.Warn("vector index upsert failed; record remains keyword-searchable",
			"ref", record.Ref, "kind", record.Kind, "err", err)
	}
}
