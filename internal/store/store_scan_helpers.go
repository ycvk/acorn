package store

import (
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func scanRunRecord(scanner interface{ Scan(dest ...any) error }) (*core.RunRecord, error) {
	var (
		rec      core.RunRecord
		status   string
		created  string
		finished string
	)
	if err := scanner.Scan(&rec.RunID, &rec.SessionID, &rec.TurnIndex, &status, &rec.Input, &rec.Output, &rec.Error, &created, &finished); err != nil {
		return nil, err
	}
	rec.Status = core.RunStatus(status)
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "run.created_at")
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = createdAt
	if strings.TrimSpace(finished) != "" {
		finishedAt, err := parseTimestamp(time.RFC3339Nano, finished, "run.finished_at")
		if err != nil {
			return nil, err
		}
		rec.FinishedAt = finishedAt
	}
	return &rec, nil
}

func scanPendingActionRecord(scanner interface{ Scan(dest ...any) error }) (*core.PendingActionRecord, error) {
	var (
		record     core.PendingActionRecord
		kind       string
		status     string
		payload    string
		decision   string
		subject    string
		interrupt  string
		createdAt  string
		resolvedAt string
	)
	if err := scanner.Scan(
		&record.ActionID,
		&record.RunID,
		&interrupt,
		&kind,
		&subject,
		&payload,
		&status,
		&record.Reason,
		&decision,
		&createdAt,
		&resolvedAt,
	); err != nil {
		return nil, err
	}
	record.InterruptID = interrupt
	record.Kind = core.PendingActionKind(kind)
	record.Subject = subject
	record.PayloadJSON = payload
	record.Status = core.PendingActionStatus(status)
	record.DecisionJSON = decision
	createdParsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "pending_action.created_at")
	if err != nil {
		return nil, err
	}
	record.CreatedAt = createdParsed
	if strings.TrimSpace(resolvedAt) != "" {
		parsedResolvedAt, err := time.Parse(fixedTimestampLayout, resolvedAt)
		if err != nil {
			return nil, fmt.Errorf("parse pending action resolved_at: %w", err)
		}
		record.ResolvedAt = &parsedResolvedAt
	}
	return &record, nil
}
