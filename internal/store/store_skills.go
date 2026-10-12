package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// WriteSkill stores a new revision of a skill.
func (s *Store) WriteSkill(ctx context.Context, w core.SkillWrite) (core.Skill, error) {
	if strings.TrimSpace(w.ID) == "" || strings.TrimSpace(w.Markdown) == "" || w.At.IsZero() {
		return core.Skill{}, errors.New("write skill: id, markdown and at are required")
	}
	if w.Author != core.SkillAuthorAgent && w.Author != core.SkillAuthorRelease {
		return core.Skill{}, fmt.Errorf("write skill %s: unknown author %q", w.ID, w.Author)
	}
	var skill core.Skill
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		current, err := loadSkill(ctx, tx, w.ID)
		exists := err == nil
		if err != nil && !errors.Is(err, core.ErrSkillNotFound) {
			return err
		}
		if current.Revision != w.BaseRevision {
			return fmt.Errorf("%w: %s is at revision %d, not %d", core.ErrSkillConflict, w.ID, current.Revision, w.BaseRevision)
		}
		skill = core.Skill{ID: w.ID, Markdown: w.Markdown, Revision: current.Revision + 1, Retired: w.Retired, Author: w.Author, SeedSHA256: current.SeedSHA256, CreatedAt: w.At, UpdatedAt: w.At}
		if exists {
			skill.CreatedAt = current.CreatedAt
		}
		if w.SeedSHA256 != "" {
			skill.SeedSHA256 = w.SeedSHA256
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO skills (id, markdown, revision, retired, seed_sha256, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET markdown = excluded.markdown, revision = excluded.revision, retired = excluded.retired, seed_sha256 = excluded.seed_sha256, updated_at = excluded.updated_at`,
			skill.ID, skill.Markdown, skill.Revision, skill.Retired, skill.SeedSHA256, formatTimestamp(skill.CreatedAt), formatTimestamp(skill.UpdatedAt)); err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(skill.Markdown))
		_, err = tx.ExecContext(ctx, `INSERT INTO skill_revisions (id, revision, markdown, markdown_sha256, retired, author, run_id, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			skill.ID, skill.Revision, skill.Markdown, hex.EncodeToString(sum[:]), skill.Retired, string(w.Author), w.RunID, formatTimestamp(w.At))
		return err
	})
	if err != nil {
		return core.Skill{}, fmt.Errorf("write skill %s: %w", w.ID, err)
	}
	return skill, nil
}

func (s *Store) Skill(ctx context.Context, id string) (core.Skill, error) {
	return loadSkill(ctx, s.db, id)
}

func loadSkill(ctx context.Context, q memorySQL, id string) (core.Skill, error) {
	rows, err := q.QueryContext(ctx, skillColumns+` WHERE s.id = ?`, id)
	if err != nil {
		return core.Skill{}, fmt.Errorf("read skill %s: %w", id, err)
	}
	skills, err := scanSkills(rows)
	if err != nil {
		return core.Skill{}, err
	}
	if len(skills) == 0 {
		return core.Skill{}, fmt.Errorf("%w: %s", core.ErrSkillNotFound, id)
	}
	return skills[0], nil
}

func (s *Store) Skills(ctx context.Context) ([]core.Skill, error) {
	rows, err := s.db.QueryContext(ctx, skillColumns+` ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return scanSkills(rows)
}

// skillColumns selects the rows scanSkills reads, with the author of each
// current revision.
const skillColumns = `SELECT s.id, s.markdown, s.revision, s.retired, r.author, s.seed_sha256, s.created_at, s.updated_at
	FROM skills s JOIN skill_revisions r ON r.id = s.id AND r.revision = s.revision`

func scanSkills(rows *sql.Rows) ([]core.Skill, error) {
	defer func() { _ = rows.Close() }()
	skills := []core.Skill{}
	for rows.Next() {
		var (
			skill                    core.Skill
			author, created, updated string
		)
		if err := rows.Scan(&skill.ID, &skill.Markdown, &skill.Revision, &skill.Retired, &author, &skill.SeedSHA256, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}
		skill.Author = core.SkillAuthor(author)
		var err error
		if skill.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "skills.created_at"); err != nil {
			return nil, err
		}
		if skill.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "skills.updated_at"); err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan skills: %w", err)
	}
	return skills, nil
}

// SkillLoadsSince counts the skill tool calls since a time by the skill they
// load. Calls whose arguments name no skill are left out.
func (s *Store) SkillLoadsSince(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT skill, COUNT(*) FROM (
			SELECT CASE WHEN json_valid(json_extract(payload_json, '$.arguments')) THEN json_extract(json_extract(payload_json, '$.arguments'), '$.skill') END AS skill
			FROM events WHERE kind = 'tool.call.started' AND json_extract(payload_json, '$.tool_name') = 'skill' AND created_at > ?)
		WHERE skill IS NOT NULL GROUP BY skill`, formatTimestamp(since))
	if err != nil {
		return nil, fmt.Errorf("count skill loads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	loads := map[string]int{}
	for rows.Next() {
		var (
			id string
			n  int
		)
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("scan skill loads: %w", err)
		}
		loads[id] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan skill loads: %w", err)
	}
	return loads, nil
}
