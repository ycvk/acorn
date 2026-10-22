package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const watchItemColumns = `id, watch_id, item_key, title, url, summary, published_at, status, run_id, seen_at`

func (s *Store) PendingBriefingItems(ctx context.Context, limit int) (core.PendingBriefing, error) {
	if limit <= 0 {
		return core.PendingBriefing{}, errors.New("pending briefing items: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+watchItemColumns+` FROM (
		   SELECT i.*, ROW_NUMBER() OVER (PARTITION BY i.watch_id ORDER BY i.id) AS turn
		   FROM watch_items i JOIN watches w ON w.id = i.watch_id
		   WHERE i.status = ? AND w.mode <> ?)
		 ORDER BY turn, watch_id LIMIT ?`,
		string(core.WatchItemPending), string(core.WatchModeNews), limit)
	if err != nil {
		return core.PendingBriefing{}, fmt.Errorf("pending briefing items: %w", err)
	}
	items, err := scanWatchItems(rows)
	if err != nil {
		return core.PendingBriefing{}, fmt.Errorf("pending briefing items: %w", err)
	}
	counts, err := s.pendingCounts(ctx, `w.mode <> ?`, string(core.WatchModeNews))
	if err != nil {
		return core.PendingBriefing{}, fmt.Errorf("pending briefing items: %w", err)
	}
	return core.PendingBriefing{Items: items, Counts: counts}, nil
}

// pendingCounts counts the pending items of each watch matching where, a
// condition on the watch w with one argument.
func (s *Store) pendingCounts(ctx context.Context, where string, arg any) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.watch_id, COUNT(*) FROM watch_items i JOIN watches w ON w.id = i.watch_id
		 WHERE i.status = ? AND `+where+` GROUP BY i.watch_id`,
		string(core.WatchItemPending), arg)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var watchID int64
		var count int
		if err := scan(&watchID, &count); err != nil {
			return err
		}
		counts[watchID] = count
		return nil
	})
	return counts, err
}

func (s *Store) ListNewsSections(ctx context.Context) ([]core.NewsSection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, position, preference, created_at, updated_at FROM news_sections ORDER BY position, name`)
	if err != nil {
		return nil, fmt.Errorf("list news sections: %w", err)
	}
	var sections []core.NewsSection
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			section          core.NewsSection
			created, updated string
		)
		if err := scan(&section.Name, &section.Position, &section.Preference, &created, &updated); err != nil {
			return err
		}
		var err error
		if section.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "news_sections.created_at"); err != nil {
			return err
		}
		section.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "news_sections.updated_at")
		sections = append(sections, section)
		return err
	}); err != nil {
		return nil, fmt.Errorf("list news sections: %w", err)
	}
	return sections, nil
}

func (s *Store) SaveNewsSection(ctx context.Context, section core.NewsSection) (core.NewsSection, error) {
	section.Name = strings.TrimSpace(section.Name)
	switch {
	case section.Name == "":
		return core.NewsSection{}, errors.New("save news section: name is required")
	case section.UpdatedAt.IsZero():
		return core.NewsSection{}, errors.New("save news section: updated_at is required")
	}
	if section.CreatedAt.IsZero() {
		section.CreatedAt = section.UpdatedAt
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO news_sections (name, position, preference, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET position = excluded.position, preference = excluded.preference, updated_at = excluded.updated_at`,
		section.Name, section.Position, section.Preference, formatTimestamp(section.CreatedAt), formatTimestamp(section.UpdatedAt)); err != nil {
		return core.NewsSection{}, fmt.Errorf("save news section %q: %w", section.Name, err)
	}
	sections, err := s.ListNewsSections(ctx)
	if err != nil {
		return core.NewsSection{}, err
	}
	for _, saved := range sections {
		if saved.Name == section.Name {
			return saved, nil
		}
	}
	return core.NewsSection{}, fmt.Errorf("save news section %q: not found after saving", section.Name)
}

func (s *Store) RenameNewsSection(ctx context.Context, from, to string, at time.Time) (err error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return errors.New("rename news section: from and to are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("rename news section %q: %w", from, err)
	}
	defer rollbackOnErr(tx, &err, "rename news section")
	result, err := tx.ExecContext(ctx, `UPDATE news_sections SET name = ?, updated_at = ? WHERE name = ?`, to, formatTimestamp(at), from)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return fmt.Errorf("rename news section %q: a section named %q exists", from, to)
		}
		return fmt.Errorf("rename news section %q: %w", from, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rename news section %q: %w", from, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %q", core.ErrNewsSectionNotFound, from)
	}
	// Stories keep their section by name; the watches follow by foreign key.
	if _, err := tx.ExecContext(ctx, `UPDATE news_stories SET section = ? WHERE section = ?`, to, from); err != nil {
		return fmt.Errorf("rename news section %q: %w", from, err)
	}
	return tx.Commit()
}

func (s *Store) DeleteNewsSection(ctx context.Context, name string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM watches WHERE section = ? ORDER BY id`, name)
	if err != nil {
		return fmt.Errorf("delete news section %q: %w", name, err)
	}
	var watches []string
	if err := scanRows(rows, func(scan func(...any) error) error {
		var watch string
		if err := scan(&watch); err != nil {
			return err
		}
		watches = append(watches, watch)
		return nil
	}); err != nil {
		return fmt.Errorf("delete news section %q: %w", name, err)
	}
	if len(watches) > 0 {
		return fmt.Errorf("%w: %q has %s", core.ErrNewsSectionInUse, name, strings.Join(watches, ", "))
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM news_sections WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete news section %q: %w", name, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete news section %q: %w", name, err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: %q", core.ErrNewsSectionNotFound, name)
	}
	return nil
}
