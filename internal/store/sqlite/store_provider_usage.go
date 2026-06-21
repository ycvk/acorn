package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/providers"
)

func (s *Store) AppendProviderUsage(ctx context.Context, record providers.UsageRecord) error {
	normalized, err := providers.NormalizeUsageRecord(record)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO provider_usages (
			usage_id, run_id, session_id, call_site, provider_name, model_name,
			prompt_tokens, completion_tokens, total_tokens, cached_tokens,
			reasoning_tokens, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, normalized.UsageID, normalized.RunID, normalized.SessionID, normalized.CallSite,
		normalized.ProviderName, normalized.ModelName, normalized.PromptTokens,
		normalized.CompletionTokens, normalized.TotalTokens, normalized.CachedTokens,
		normalized.ReasoningTokens, formatTimestamp(normalized.CreatedAt))
	if err != nil {
		return fmt.Errorf("append provider usage %s: %w", normalized.UsageID, err)
	}
	return nil
}

func (s *Store) ListProviderUsagesByRun(ctx context.Context, runID string) ([]providers.UsageRecord, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("provider usage run_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT usage_id, run_id, session_id, call_site, provider_name, model_name,
		       prompt_tokens, completion_tokens, total_tokens, cached_tokens,
		       reasoning_tokens, created_at
		FROM provider_usages
		WHERE run_id = ?
		ORDER BY created_at ASC, usage_id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("list provider usages for run %s: %w", runID, err)
	}
	return scanProviderUsageRows(rows, runID)
}

// scanProviderUsageRows scans all rows from a provider_usages query into a
// slice, closing the rows and wrapping the iteration error with the run id.
func scanProviderUsageRows(rows *sql.Rows, runID string) ([]providers.UsageRecord, error) {
	defer rows.Close()
	var items []providers.UsageRecord
	for rows.Next() {
		record, err := scanProviderUsage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider usages for run %s: %w", runID, err)
	}
	return items, nil
}

func scanProviderUsage(scanner interface{ Scan(dest ...any) error }) (providers.UsageRecord, error) {
	var record providers.UsageRecord
	var createdAt string
	if err := scanProviderUsageFields(scanner, &record, &createdAt); err != nil {
		return providers.UsageRecord{}, err
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "provider_usage.created_at")
	if err != nil {
		return providers.UsageRecord{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}

func scanProviderUsageFields(scanner interface{ Scan(dest ...any) error }, record *providers.UsageRecord, createdAt *string) error {
	if err := scanner.Scan(
		&record.UsageID,
		&record.RunID,
		&record.SessionID,
		&record.CallSite,
		&record.ProviderName,
		&record.ModelName,
		&record.PromptTokens,
		&record.CompletionTokens,
		&record.TotalTokens,
		&record.CachedTokens,
		&record.ReasoningTokens,
		createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return err
	}
	return nil
}
