package store

import (
	"context"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) UsageReport(ctx context.Context, since time.Time) (core.UsageReport, error) {
	var report core.UsageReport
	err := s.db.QueryRowContext(ctx, `SELECT
	COALESCE(SUM(CASE WHEN EXISTS(SELECT 1 FROM events w WHERE w.run_id=u.run_id AND w.kind=?) THEN COALESCE(json_extract(u.payload_json,'$.total_tokens'),0) ELSE 0 END),0),
	COALESCE(SUM(COALESCE(json_extract(u.payload_json,'$.total_tokens'),0)),0),
	COALESCE(SUM(CASE WHEN json_extract(u.payload_json,'$.reported')=0 THEN 1 ELSE 0 END),0)
	FROM events u WHERE u.kind=? AND u.created_at >= ?`, core.EventWakeFired, core.EventModelUsage, formatTimestamp(since)).Scan(&report.AutonomousTokens, &report.TotalTokens, &report.UnreportedCalls)
	if err != nil {
		return report, fmt.Errorf("model usage report: %w", err)
	}
	var autonomous, total, unreported int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN budget='autonomous' THEN input_tokens+output_tokens ELSE 0 END),0),COALESCE(SUM(input_tokens+output_tokens),0),COALESCE(SUM(CASE WHEN reported=0 THEN 1 ELSE 0 END),0) FROM memory_usage WHERE created_at>=?`, formatTimestamp(since)).Scan(&autonomous, &total, &unreported); err != nil {
		return report, fmt.Errorf("memory usage report: %w", err)
	}
	report.AutonomousTokens += autonomous
	report.TotalTokens += total
	report.UnreportedCalls += unreported
	return report, nil
}

func (s *Store) SumAutonomousTokensSince(ctx context.Context, since time.Time) (int, error) {
	report, err := s.UsageReport(ctx, since)
	return report.AutonomousTokens, err
}
