package sqlite

import (
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
)

func scanRunRecord(scanner interface{ Scan(dest ...any) error }) (*events.RunRecord, error) {
	var (
		rec                  events.RunRecord
		status               string
		orchestrationModeRaw string
		created              string
		updated              string
	)
	if err := scanner.Scan(&rec.RunID, &rec.SessionID, &rec.TurnIndex, &status, &rec.Input, &rec.Output, &rec.Error, &rec.CheckpointID, &orchestrationModeRaw, &rec.ParentRunID, &rec.Depth, &created, &updated); err != nil {
		return nil, err
	}
	rec.Status = events.RunStatus(status)
	rec.OrchestrationMode = events.OrchestrationMode(orchestrationModeRaw).Normalize()
	createdAt, err := parseTimestamp(time.RFC3339Nano, created, "run.created_at")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimestamp(time.RFC3339Nano, updated, "run.updated_at")
	if err != nil {
		return nil, err
	}
	rec.CreatedAt = createdAt
	rec.UpdatedAt = updatedAt
	return &rec, nil
}

func scanPendingActionRecord(scanner interface{ Scan(dest ...any) error }) (*events.PendingActionRecord, error) {
	var (
		record     events.PendingActionRecord
		kind       string
		mode       string
		status     string
		payload    string
		decision   string
		subject    string
		interrupt  string
		createdAt  string
		decidedAt  string
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
		&mode,
		&record.Reason,
		&record.Rule,
		&decision,
		&createdAt,
		&decidedAt,
		&resolvedAt,
	); err != nil {
		return nil, err
	}
	record.InterruptID = interrupt
	record.Kind = events.PendingActionKind(kind)
	record.Subject = subject
	record.PayloadJSON = payload
	record.Mode = events.PendingActionDecisionMode(mode)
	record.Status = events.PendingActionStatus(status)
	record.DecisionJSON = decision
	createdParsed, err := parseTimestamp(fixedTimestampLayout, createdAt, "pending_action.created_at")
	if err != nil {
		return nil, err
	}
	record.CreatedAt = createdParsed
	if strings.TrimSpace(decidedAt) != "" {
		parsedDecision, err := time.Parse(fixedTimestampLayout, decidedAt)
		if err != nil {
			return nil, fmt.Errorf("parse pending action decided_at: %w", err)
		}
		record.DecidedAt = &parsedDecision
	}
	if strings.TrimSpace(resolvedAt) != "" {
		parsedResolvedAt, err := time.Parse(fixedTimestampLayout, resolvedAt)
		if err != nil {
			return nil, fmt.Errorf("parse pending action resolved_at: %w", err)
		}
		record.ResolvedAt = &parsedResolvedAt
	}
	return &record, nil
}
