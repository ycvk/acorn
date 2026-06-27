package memory

import (
	"context"
	"log/slog"
	"strings"
)

// embedOutcome signals what happened in an embedding attempt.
type embedOutcome int

const (
	embedSkipped embedOutcome = iota // embedding not configured, empty text, or record skipped
	embedIndexed                     // embedding generated and stored
	embedFailed                      // embedding or upsert failed (logged)
)

// indexEmbedding generates and stores an embedding for a memory record. It is
// called after a record is written to disk (mutation, fact creation, history
// append). When embedding is not configured or the vector index is absent,
// this is a no-op returning embedSkipped. Errors are logged but never
// returned: a failed embedding must not break the write path — the record is
// already on disk and the keyword index is already rebuilt.
func (s *LocalService) indexEmbedding(ctx context.Context, record Record) embedOutcome {
	if s.embedding == nil || s.vectors == nil {
		return embedSkipped
	}
	// Embed title + body together: the title carries the most signal, the body
	// adds context.
	text := strings.TrimSpace(record.Title + "\n\n" + record.Body)
	if text == "" {
		return embedSkipped
	}
	embedding, err := s.embedding.Embed(ctx, text)
	if err != nil {
		slog.Warn("embedding generation failed; record remains keyword-searchable",
			"ref", record.Ref, "kind", record.Kind, "err", err)
		return embedFailed
	}
	scope := record.Scope
	if scope == "" {
		scope = "user"
	}
	if err := s.vectors.UpsertEmbedding(ctx, record.Ref, string(record.Kind), record.Title, scope, embedding); err != nil {
		slog.Warn("vector index upsert failed; record remains keyword-searchable",
			"ref", record.Ref, "kind", record.Kind, "err", err)
		return embedFailed
	}
	return embedIndexed
}
