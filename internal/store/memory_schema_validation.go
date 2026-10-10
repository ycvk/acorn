package store

var memoryRequiredTables = map[string][]string{
	"memory_sources":           {"id", "kind", "object_id", "version", "speaker", "session_id", "run_id", "body", "recorded_at", "occurred_at"},
	"memory_records":           {"id", "kind", "content", "scope", "state", "basis", "revision", "recorded_at", "updated_at", "valid_from", "valid_to", "needs_review", "pinned", "excluded"},
	"memory_revisions":         {"record_id", "revision", "kind", "content", "scope", "state", "basis", "valid_from", "valid_to", "needs_review", "pinned", "at", "reason"},
	"memory_revision_evidence": {"record_id", "revision", "source_id", "quote", "relation"},
	"memory_revision_entities": {"record_id", "revision", "entity"},
	"memory_revision_parents":  {"record_id", "revision", "parent_id"},
	"memory_revision_sources":  {"record_id", "revision", "source_id"},
	"memory_links":             {"from_id", "to_id", "relation"},
	"memory_entities":          {"id", "name"},
	"memory_mentions":          {"record_id", "entity_id"},
	"memory_entity_aliases":    {"record_id", "revision", "canonical", "alias", "scope", "source_id", "quote"},
	"memory_concerns":          {"id", "title", "state", "revision", "source_id", "data"},
	"memory_concern_links":     {"concern_id", "record_id"},
	"memory_concern_revisions": {"concern_id", "revision", "data"},
	"commitments":              {"id", "content", "state", "session_id", "source_run_id", "concern_id", "wake_at", "recurrence", "created_at", "updated_at"},
	"commitment_occurrences":   {"id", "commitment_id", "due_at", "state", "run_id", "evidence_source_id", "updated_at"},
	"memory_embeddings":        {"record_id", "revision", "generation", "dimensions", "vector"},
	"memory_vector_sketches":   {"generation", "record_id", "revision", "dimensions", "scale", "codes"},
	"memory_index_state":       {"id", "generation", "model", "dimensions", "state"},
	"memory_jobs":              {"id", "operation", "object_id", "version", "processor_version", "state", "token", "epoch", "attempts", "cursor", "lease_until", "available_at", "error", "created_at", "completed_at"},
	"memory_state":             {"id", "epoch", "request_run_id"},
	"memory_exclusions":        {"id", "epoch", "source_id", "quote", "request_source_id", "reason", "created_at"},
	"memory_source_links":      {"source_id", "parent_id"},
	"thread_summaries":         {"session_id", "through_message_id", "epoch", "content", "sources_json", "updated_at"},
	"context_snapshot_refs":    {"hash", "run_id", "source_id", "record_id", "epoch"},
	"memory_usage":             {"id", "operation", "job_id", "run_id", "budget", "model", "input_tokens", "output_tokens", "reported", "created_at"},
}

func (s *Store) validateMemorySchema() error {
	for table, columns := range memoryRequiredTables {
		if err := s.requireColumns(table, columns); err != nil {
			return err
		}
	}
	return nil
}
