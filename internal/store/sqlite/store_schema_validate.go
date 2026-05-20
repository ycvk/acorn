package sqlite

import (
	"database/sql"
	"fmt"
)

func (s *Store) validateSchema() error {
	requiredTables := map[string][]string{
		"runs":                    {"run_id", "session_id", "turn_index", "status", "input_text", "output_text", "error_text", "checkpoint_id", "orchestration_mode", "created_at", "updated_at", "parent_run_id", "depth"},
		"events":                  {"sequence", "run_id", "kind", "payload_json", "created_at"},
		"checkpoints":             {"checkpoint_id", "run_id", "payload", "created_at", "updated_at"},
		"sessions":                {"session_id", "title", "created_at", "updated_at"},
		"session_messages":        {"id", "session_id", "turn_index", "role", "content", "content_parts", "run_id", "created_at"},
		"pending_actions":         {"action_id", "run_id", "interrupt_id", "kind", "subject", "payload_json", "status", "mode", "reason", "rule", "decision_json", "created_at", "decided_at", "resolved_at"},
		"mcp_oauth_tokens":        {"provider_name", "access_token", "refresh_token", "expiry", "updated_at"},
		"owner_profile":           {"owner_id", "created_at"},
		"devices":                 {"device_id", "name", "platform", "token_hash", "created_at", "last_seen_at", "revoked_at"},
		"device_push_tokens":      {"push_token_id", "device_id", "provider", "platform", "token_value", "token_hash", "created_at", "updated_at", "revoked_at"},
		"notifications":           {"notification_id", "kind", "run_id", "action_id", "created_at"},
		"notification_deliveries": {"delivery_id", "notification_id", "device_id", "push_token_id", "provider", "status", "error", "created_at", "updated_at"},
		"pairing_codes":           {"code_hash", "expires_at", "used_at", "created_at"},
		"conversation_segments":   {"id", "session_id", "run_id", "user_content", "assistant_content", "run_status", "created_at"},
		"working_checkpoints":     {"session_id", "content", "related_skill_id", "updated_at"},
		"run_context_snapshots":   {"run_id", "working_checkpoint_content", "working_checkpoint_skill_id", "decision_profile_hash", "decision_action", "decision_skill_id", "created_at"},
		"context_boundaries":      {"boundary_id", "session_id", "run_id", "sequence", "turn_index", "mode", "trigger", "first_index", "last_index", "covered_first_message_id", "covered_last_message_id", "previous_boundary_id", "summary_message_id", "transcript_ref", "preserved_from_index", "preserved_to_index", "preserved_head_message_id", "preserved_anchor_message_id", "preserved_tail_message_id", "tokens_before", "tokens_after", "effective_window_tokens", "summary", "summary_snippet", "created_at"},
		"tool_results":            {"result_ref", "run_id", "session_id", "turn_index", "call_id", "tool_name", "arguments_json", "status", "error_reason", "preview", "full_text", "token_estimate", "side_effects_json", "evidence_refs_json", "created_at"},
		"artifacts":               {"artifact_id", "run_id", "session_id", "source_tool_result_ref", "kind", "title", "mime_type", "relative_path", "size_bytes", "sha256", "created_at"},
		"terminal_sessions":       {"terminal_session_id", "run_id", "session_id", "label", "command_json", "cwd", "interactive", "pty", "status", "pid", "process_group_id", "exit_code", "signal", "stdout_artifact_id", "stderr_artifact_id", "pty_artifact_id", "started_at", "ended_at", "created_at", "updated_at"},
		"terminal_session_logs":   {"log_id", "terminal_session_id", "stream", "artifact_id", "start_offset", "size_bytes", "created_at"},
		"provider_usages":         {"usage_id", "run_id", "session_id", "call_site", "provider_name", "model_name", "prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "reasoning_tokens", "created_at"},
		"run_decisions":           {"run_id", "session_id", "action", "intent", "selected_skill_id", "decision_reason", "decision_profile_hash", "created_at"},
		"run_archives":            {"run_id", "session_id", "input_excerpt", "output_excerpt", "touched_paths_json", "tool_names_json", "run_status", "created_at"},
		"session_summaries":       {"session_id", "source_run_id", "run_status", "summary", "updated_at"},
		"schema_migrations":       {"version", "applied_at"},
		"plan_steps":              {"plan_id", "session_id", "run_id", "steps_json", "created_at", "updated_at"},
	}
	for table, columns := range requiredTables {
		if err := s.requireColumns(table, columns); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) requireColumns(table string, columns []string) error {
	existing, err := s.tableColumns(table)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if _, ok := existing[column]; ok {
			continue
		}
		return fmt.Errorf("%w: table %s is missing required column %s; rebuild the local database with a clean current storage directory", ErrUnsupportedStorageSchema, table, column)
	}
	return nil
}

func (s *Store) tableColumns(table string) (map[string]struct{}, error) {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("table info %s: %w", table, err)
	}
	defer rows.Close()

	result := make(map[string]struct{})
	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultV   sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultV, &primaryKey); err != nil {
			return nil, fmt.Errorf("scan table info %s: %w", table, err)
		}
		result[name] = struct{}{}
	}
	return result, rows.Err()
}
