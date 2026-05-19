package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/providerusage"
)

func (s *Store) AppendProviderUsage(ctx context.Context, record providerusage.Record) error {
	normalized, err := providerusage.NormalizeRecord(record)
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

func (s *Store) ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error) {
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
	defer rows.Close()

	var items []providerusage.Record
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

type providerUsageScanner interface {
	Scan(dest ...any) error
}

func scanProviderUsage(scanner providerUsageScanner) (providerusage.Record, error) {
	var record providerusage.Record
	var createdAt string
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
		&createdAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return providerusage.Record{}, err
		}
		return providerusage.Record{}, err
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "provider_usage.created_at")
	if err != nil {
		return providerusage.Record{}, err
	}
	record.CreatedAt = parsed
	return record, nil
}
