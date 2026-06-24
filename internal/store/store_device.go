package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) SavePairingCode(ctx context.Context, code *core.PairingCode) error {
	if code == nil {
		return errors.New("pairing code is nil")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pairing_codes(code_hash, expires_at, used_at, created_at)
		 VALUES(?, ?, ?, ?)`,
		code.CodeHash,
		formatTimestamp(code.ExpiresAt),
		formatOptionalTimestamp(code.UsedAt),
		formatTimestamp(code.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("save pairing code: %w", err)
	}
	return nil
}

func (s *Store) LoadPairingCode(ctx context.Context, codeHash string) (*core.PairingCode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT code_hash, expires_at, used_at, created_at
		 FROM pairing_codes
		 WHERE code_hash = ?`,
		codeHash,
	)
	code, err := scanPairingCode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPairingCodeNotFound
		}
		return nil, fmt.Errorf("load pairing code: %w", err)
	}
	return code, nil
}

func (s *Store) ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*core.PairingCode, error) {
	code, err := s.LoadPairingCode(ctx, codeHash)
	if err != nil {
		return nil, err
	}
	if code.UsedAt != nil {
		return nil, ErrPairingCodeUsed
	}
	if !now.Before(code.ExpiresAt) {
		return nil, ErrPairingCodeExpired
	}
	usedAt := now.UTC()
	result, err := s.db.ExecContext(ctx,
		`UPDATE pairing_codes SET used_at = ? WHERE code_hash = ? AND used_at = ''`,
		formatTimestamp(usedAt),
		codeHash,
	)
	if err != nil {
		return nil, fmt.Errorf("consume pairing code: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("consume pairing code rows affected: %w", err)
	}
	if affected == 0 {
		return nil, ErrPairingCodeUsed
	}
	code.UsedAt = &usedAt
	return code, nil
}

func (s *Store) SaveDevice(ctx context.Context, device *core.Device) error {
	if device == nil {
		return errors.New("device is nil")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO devices(device_id, name, platform, token_hash, created_at, last_seen_at, revoked_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		device.DeviceID,
		device.Name,
		device.Platform,
		device.TokenHash,
		formatTimestamp(device.CreatedAt),
		formatTimestamp(device.LastSeenAt),
		formatOptionalTimestamp(device.RevokedAt),
	)
	if err != nil {
		return fmt.Errorf("save device: %w", err)
	}
	return nil
}

func (s *Store) LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*core.Device, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT device_id, name, platform, token_hash, created_at, last_seen_at, revoked_at
		 FROM devices
		 WHERE token_hash = ?`,
		tokenHash,
	)
	device, err := scanDevice(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeviceNotFound
		}
		return nil, fmt.Errorf("load device by token hash: %w", err)
	}
	return device, nil
}

func (s *Store) LoadDevice(ctx context.Context, deviceID string) (*core.Device, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT device_id, name, platform, token_hash, created_at, last_seen_at, revoked_at
		 FROM devices
		 WHERE device_id = ?`,
		deviceID,
	)
	device, err := scanDevice(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDeviceNotFound
		}
		return nil, fmt.Errorf("load device: %w", err)
	}
	return device, nil
}

func (s *Store) ListDevices(ctx context.Context) ([]core.Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, name, platform, token_hash, created_at, last_seen_at, revoked_at
		 FROM devices
		 ORDER BY created_at ASC, device_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	var devices []core.Device
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, *device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list devices rows: %w", err)
	}
	return devices, nil
}

func (s *Store) TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE devices SET last_seen_at = ? WHERE device_id = ?`,
		formatTimestamp(seenAt),
		deviceID,
	)
	if err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch device rows affected: %w", err)
	}
	if affected == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

func (s *Store) RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE devices SET revoked_at = ? WHERE device_id = ?`,
		formatTimestamp(revokedAt),
		deviceID,
	)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke device rows affected: %w", err)
	}
	if affected == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

func scanPairingCode(scanner interface{ Scan(dest ...any) error }) (*core.PairingCode, error) {
	var (
		code      core.PairingCode
		expiresAt string
		usedAt    string
		createdAt string
	)
	if err := scanner.Scan(&code.CodeHash, &expiresAt, &usedAt, &createdAt); err != nil {
		return nil, err
	}
	parsedExpiresAt, err := parseTimestamp(fixedTimestampLayout, expiresAt, "pairing_code.expires_at")
	if err != nil {
		return nil, err
	}
	parsedUsedAt, err := parseOptionalTimestamp(usedAt, "pairing_code.used_at")
	if err != nil {
		return nil, err
	}
	parsedCreatedAt, err := parseTimestamp(fixedTimestampLayout, createdAt, "pairing_code.created_at")
	if err != nil {
		return nil, err
	}
	code.ExpiresAt = parsedExpiresAt
	code.UsedAt = parsedUsedAt
	code.CreatedAt = parsedCreatedAt
	return &code, nil
}

func scanDevice(scanner interface{ Scan(dest ...any) error }) (*core.Device, error) {
	var (
		device     core.Device
		createdAt  string
		lastSeenAt string
		revokedAt  string
	)
	if err := scanner.Scan(&device.DeviceID, &device.Name, &device.Platform, &device.TokenHash, &createdAt, &lastSeenAt, &revokedAt); err != nil {
		return nil, err
	}
	parsedCreatedAt, err := parseTimestamp(fixedTimestampLayout, createdAt, "device.created_at")
	if err != nil {
		return nil, err
	}
	parsedLastSeenAt, err := parseTimestamp(fixedTimestampLayout, lastSeenAt, "device.last_seen_at")
	if err != nil {
		return nil, err
	}
	parsedRevokedAt, err := parseOptionalTimestamp(revokedAt, "device.revoked_at")
	if err != nil {
		return nil, err
	}
	device.CreatedAt = parsedCreatedAt
	device.LastSeenAt = parsedLastSeenAt
	device.RevokedAt = parsedRevokedAt
	return &device, nil
}

func parseOptionalTimestamp(value, field string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := parseTimestamp(fixedTimestampLayout, value, field)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatOptionalTimestamp(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatTimestamp(*value)
}
