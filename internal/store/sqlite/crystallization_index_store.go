package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/crystallization"
)

type CrystallizationIndexStore struct {
	db *sql.DB
}

func OpenCrystallizationIndexStore(dbPath string) (*CrystallizationIndexStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open insight index db: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &CrystallizationIndexStore{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *CrystallizationIndexStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *CrystallizationIndexStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS insight_index (
		skill_id TEXT PRIMARY KEY,
		skill_name TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		keywords TEXT NOT NULL DEFAULT '',
		task_pattern TEXT NOT NULL DEFAULT '',
		quality_score INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_insight_keywords ON insight_index(keywords);
	CREATE INDEX IF NOT EXISTS idx_insight_task_pattern ON insight_index(task_pattern);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *CrystallizationIndexStore) Upsert(ctx context.Context, entry *crystallization.IndexEntry) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("index store not initialized")
	}
	if entry == nil {
		return fmt.Errorf("index entry is required")
	}
	if entry.CreatedAt.IsZero() {
		return fmt.Errorf("index entry created_at is required")
	}
	if entry.UpdatedAt.IsZero() {
		return fmt.Errorf("index entry updated_at is required")
	}
	keywords := strings.Join(entry.Keywords, ",")
	createdAt := entry.CreatedAt.Format(time.RFC3339)
	updatedAt := entry.UpdatedAt.Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO insight_index (skill_id, skill_name, summary, keywords, task_pattern, quality_score, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(skill_id) DO UPDATE SET
			skill_name = excluded.skill_name,
			summary = excluded.summary,
			keywords = excluded.keywords,
			task_pattern = excluded.task_pattern,
			quality_score = excluded.quality_score,
			source = excluded.source,
			updated_at = excluded.updated_at
	`, entry.SkillID, entry.SkillName, entry.Summary, keywords, entry.TaskPattern, entry.QualityScore, entry.Source, createdAt, updatedAt)
	return err
}

func (s *CrystallizationIndexStore) Query(ctx context.Context, input string, limit int) ([]crystallization.IndexEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("index store not initialized")
	}
	if limit <= 0 {
		limit = 20
	}
	terms := extractCrystallizationTerms(input)
	if len(terms) == 0 {
		return nil, nil
	}

	var args []any
	var conditions []string
	for _, term := range terms {
		conditions = append(conditions, "(skill_id LIKE ? OR skill_name LIKE ? OR summary LIKE ? OR keywords LIKE ? OR task_pattern LIKE ?)")
		likeTerm := "%" + term + "%"
		for i := 0; i < 5; i++ {
			args = append(args, likeTerm)
		}
	}

	query := fmt.Sprintf(
		"SELECT skill_id, skill_name, summary, keywords, task_pattern, quality_score, source, created_at, updated_at FROM insight_index WHERE %s ORDER BY quality_score DESC LIMIT ?",
		strings.Join(conditions, " OR "),
	)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query insight index: %w", err)
	}
	defer rows.Close()

	var results []crystallization.IndexEntry
	for rows.Next() {
		var entry crystallization.IndexEntry
		var keywordsStr, createdStr, updatedStr string
		if err := rows.Scan(&entry.SkillID, &entry.SkillName, &entry.Summary, &keywordsStr, &entry.TaskPattern, &entry.QualityScore, &entry.Source, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("scan insight index row: %w", err)
		}
		if keywordsStr != "" {
			entry.Keywords = strings.Split(keywordsStr, ",")
		}
		entry.CreatedAt, err = parseCrystallizationIndexTime(createdStr)
		if err != nil {
			return nil, fmt.Errorf("parse insight index created_at for %s: %w", entry.SkillID, err)
		}
		entry.UpdatedAt, err = parseCrystallizationIndexTime(updatedStr)
		if err != nil {
			return nil, fmt.Errorf("parse insight index updated_at for %s: %w", entry.SkillID, err)
		}
		results = append(results, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate insight index rows: %w", err)
	}
	return results, nil
}

func (s *CrystallizationIndexStore) Delete(ctx context.Context, skillID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("index store not initialized")
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM insight_index WHERE skill_id = ?", skillID)
	return err
}

func extractCrystallizationTerms(input string) []string {
	input = strings.ToLower(input)
	words := strings.FieldsFunc(input, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' || r == ';' || r == ':' || r == '?' || r == '!'
	})
	var terms []string
	seen := make(map[string]struct{})
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || len(w) < 2 {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		terms = append(terms, w)
	}
	return terms
}

func parseCrystallizationIndexTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("timestamp is required")
	}
	t, err := time.Parse("2006-01-02", value)
	if err == nil && !t.IsZero() {
		return t, nil
	}
	t, err = time.Parse(time.RFC3339, value)
	if err == nil && !t.IsZero() {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}
