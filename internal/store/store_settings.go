package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ycvk/acorn/internal/core"
)

var _ core.SettingsStore = (*Store)(nil)

// addOwnerSettings (24 to 25) adds the table of the settings the owner saved.
func addOwnerSettings(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, ownerSettingsTable)
	return err
}

// StoredSettings reads every setting the owner saved.
func (s *Store) StoredSettings(ctx context.Context) ([]core.StoredSetting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value, device_id, updated_at FROM owner_settings ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	var settings []core.StoredSetting
	if err := scanRows(rows, func(scan func(...any) error) error {
		var (
			setting core.StoredSetting
			at      string
		)
		if err := scan(&setting.Key, &setting.Value, &setting.DeviceID, &at); err != nil {
			return err
		}
		var err error
		setting.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, at, "owner_settings.updated_at")
		settings = append(settings, setting)
		return err
	}); err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	return settings, nil
}

// SaveSettings writes and resets settings in one transaction, and releases
// the memory jobs and news batches the change asks for: those their budget
// deferred become available at change.At.
func (s *Store) SaveSettings(ctx context.Context, change core.SettingsChange) (err error) {
	if change.At.IsZero() {
		return errors.New("save settings: at is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	defer rollbackOnErr(tx, &err, "save settings")
	for _, setting := range change.Save {
		if setting.Key == "" || setting.UpdatedAt.IsZero() {
			return errors.New("save settings: a setting needs a key and a time")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO owner_settings (key, value, device_id, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, device_id = excluded.device_id, updated_at = excluded.updated_at`,
			setting.Key, setting.Value, setting.DeviceID, formatTimestamp(setting.UpdatedAt)); err != nil {
			return fmt.Errorf("save setting %s: %w", setting.Key, err)
		}
	}
	for _, key := range change.Reset {
		if _, err := tx.ExecContext(ctx, `DELETE FROM owner_settings WHERE key = ?`, key); err != nil {
			return fmt.Errorf("reset setting %s: %w", key, err)
		}
	}
	at := formatTimestamp(change.At)
	if change.ReleaseMemory {
		if _, err := tx.ExecContext(ctx, `UPDATE memory_jobs SET available_at = ? WHERE state = 'pending' AND available_at > ? AND error LIKE ?`,
			at, at, "%"+core.ErrMemoryBudget.Error()+"%"); err != nil {
			return fmt.Errorf("release deferred memory jobs: %w", err)
		}
	}
	if change.ReleaseNews {
		if _, err := tx.ExecContext(ctx, `UPDATE news_claims SET lease_until = ?, error = '' WHERE error = ? AND lease_until > ?`,
			at, core.ErrMemoryBudget.Error(), at); err != nil {
			return fmt.Errorf("release deferred news batches: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
