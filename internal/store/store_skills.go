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

// WriteSkill stores a new revision of a skill with its whole file set.
func (s *Store) WriteSkill(ctx context.Context, w core.SkillWrite) (core.Skill, error) {
	if strings.TrimSpace(w.ID) == "" || strings.TrimSpace(w.Markdown) == "" || w.At.IsZero() {
		return core.Skill{}, errors.New("write skill: id, markdown and at are required")
	}
	switch w.Author {
	case core.SkillAuthorAgent, core.SkillAuthorOwner, core.SkillAuthorRelease:
	default:
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO skill_revisions (id, revision, markdown, markdown_sha256, retired, author, run_id, at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			skill.ID, skill.Revision, skill.Markdown, hex.EncodeToString(sum[:]), skill.Retired, string(w.Author), w.RunID, formatTimestamp(w.At)); err != nil {
			return err
		}
		for _, f := range w.Files {
			sum := sha256.Sum256(f.Content)
			info := core.SkillFileInfo{Path: f.Path, Size: int64(len(f.Content)), SHA256: hex.EncodeToString(sum[:])}
			if _, err := tx.ExecContext(ctx, `INSERT INTO skill_files (id, revision, path, content, size, sha256) VALUES (?, ?, ?, ?, ?, ?)`,
				skill.ID, skill.Revision, info.Path, f.Content, info.Size, info.SHA256); err != nil {
				return err
			}
			skill.Files = append(skill.Files, info)
		}
		return nil
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
	skills, err := querySkills(ctx, q, ` WHERE s.id = ?`, id)
	if err != nil {
		return core.Skill{}, fmt.Errorf("read skill %s: %w", id, err)
	}
	if len(skills) == 0 {
		return core.Skill{}, fmt.Errorf("%w: %s", core.ErrSkillNotFound, id)
	}
	return skills[0], nil
}

func (s *Store) Skills(ctx context.Context) ([]core.Skill, error) {
	skills, err := querySkills(ctx, s.db, ``)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return skills, nil
}

// querySkills reads skills with the author and the files of each current
// revision; where narrows the skills.
func querySkills(ctx context.Context, q memorySQL, where string, args ...any) ([]core.Skill, error) {
	rows, err := q.QueryContext(ctx, `SELECT s.id, s.markdown, s.revision, s.retired, r.author, s.seed_sha256, s.created_at, s.updated_at
		FROM skills s JOIN skill_revisions r ON r.id = s.id AND r.revision = s.revision`+where+` ORDER BY s.id`, args...)
	if err != nil {
		return nil, err
	}
	skills, err := scanSkills(rows)
	if err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT f.id, f.path, f.size, f.sha256 FROM skill_files f JOIN skills s ON s.id = f.id AND s.revision = f.revision`+where+` ORDER BY f.id, f.path`, args...)
	if err != nil {
		return nil, err
	}
	files := map[string][]core.SkillFileInfo{}
	if err := scanRows(rows, func(scan func(...any) error) error {
		var id string
		var info core.SkillFileInfo
		if err := scan(&id, &info.Path, &info.Size, &info.SHA256); err != nil {
			return err
		}
		files[id] = append(files[id], info)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan skill files: %w", err)
	}
	for i := range skills {
		skills[i].Files = files[skills[i].ID]
	}
	return skills, nil
}

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

// SkillFiles reads the files of one revision of a skill, by path.
func (s *Store) SkillFiles(ctx context.Context, id string, revision int64) ([]core.SkillFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, content FROM skill_files WHERE id = ? AND revision = ? ORDER BY path`, id, revision)
	if err != nil {
		return nil, fmt.Errorf("read skill %s files: %w", id, err)
	}
	return scanSkillFiles(rows)
}

func scanSkillFiles(rows *sql.Rows) ([]core.SkillFile, error) {
	files := []core.SkillFile{}
	err := scanRows(rows, func(scan func(...any) error) error {
		var f core.SkillFile
		if err := scan(&f.Path, &f.Content); err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skill files: %w", err)
	}
	return files, nil
}

// StageSkillPackage keeps a package for import under its hash and drops the
// packages staged before staleBefore.
func (s *Store) StageSkillPackage(ctx context.Context, pkg core.SkillPackage, staleBefore time.Time) error {
	if pkg.SHA256 == "" || pkg.Name == "" || pkg.Markdown == "" || pkg.StagedAt.IsZero() {
		return errors.New("stage skill package: sha256, name, markdown and staged_at are required")
	}
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		stale := `SELECT sha256 FROM skill_package_stages WHERE staged_at < ? OR sha256 = ?`
		if _, err := tx.ExecContext(ctx, `DELETE FROM skill_package_stage_files WHERE sha256 IN (`+stale+`)`, formatTimestamp(staleBefore), pkg.SHA256); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM skill_package_stages WHERE sha256 IN (`+stale+`)`, formatTimestamp(staleBefore), pkg.SHA256); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO skill_package_stages (sha256, name, markdown, staged_at) VALUES (?, ?, ?, ?)`,
			pkg.SHA256, pkg.Name, pkg.Markdown, formatTimestamp(pkg.StagedAt)); err != nil {
			return err
		}
		for _, f := range pkg.Files {
			if _, err := tx.ExecContext(ctx, `INSERT INTO skill_package_stage_files (sha256, path, content) VALUES (?, ?, ?)`, pkg.SHA256, f.Path, f.Content); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("stage skill package %s: %w", pkg.Name, err)
	}
	return nil
}

func (s *Store) StagedSkillPackage(ctx context.Context, sha string) (core.SkillPackage, error) {
	pkg := core.SkillPackage{SHA256: sha}
	var staged string
	err := s.db.QueryRowContext(ctx, `SELECT name, markdown, staged_at FROM skill_package_stages WHERE sha256 = ?`, sha).Scan(&pkg.Name, &pkg.Markdown, &staged)
	if errors.Is(err, sql.ErrNoRows) {
		return core.SkillPackage{}, fmt.Errorf("%w: %s", core.ErrSkillPackageNotStaged, sha)
	}
	if err != nil {
		return core.SkillPackage{}, fmt.Errorf("read staged skill package %s: %w", sha, err)
	}
	if pkg.StagedAt, err = parseTimestamp(fixedTimestampLayout, staged, "skill_package_stages.staged_at"); err != nil {
		return core.SkillPackage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path, content FROM skill_package_stage_files WHERE sha256 = ? ORDER BY path`, sha)
	if err != nil {
		return core.SkillPackage{}, fmt.Errorf("read staged skill package %s files: %w", sha, err)
	}
	if pkg.Files, err = scanSkillFiles(rows); err != nil {
		return core.SkillPackage{}, err
	}
	return pkg, nil
}

func (s *Store) DeleteStagedSkillPackage(ctx context.Context, sha string) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM skill_package_stage_files WHERE sha256 = ?`, sha); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM skill_package_stages WHERE sha256 = ?`, sha)
		return err
	})
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
