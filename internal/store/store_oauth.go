package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// LoadOAuthToken implements core.OAuthRepo.
func (s *Store) GetOAuthToken(ctx context.Context, providerName string) (*core.OAuthToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT provider_name, access_token, refresh_token, expiry, updated_at
     FROM mcp_oauth_tokens
     WHERE provider_name = ?`,
		providerName,
	)
	var (
		token   core.OAuthToken
		expiry  string
		updated string
	)
	if err := row.Scan(&token.ProviderName, &token.AccessToken, &token.RefreshToken, &expiry, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, core.ErrOAuthTokenNotFound
		}
		return nil, fmt.Errorf("get oauth token: %w", err)
	}
	expiryAt, err := parseTimestamp(fixedTimestampLayout, expiry, "mcp_oauth_token.expiry")
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimestamp(fixedTimestampLayout, updated, "mcp_oauth_token.updated_at")
	if err != nil {
		return nil, err
	}
	token.Expiry = expiryAt
	token.UpdatedAt = updatedAt
	return &token, nil
}

// SaveOAuthToken implements core.OAuthRepo.
func (s *Store) SaveOAuthToken(ctx context.Context, token core.OAuthToken) error {
	now := formatTimestamp(time.Now())
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO mcp_oauth_tokens(provider_name, access_token, refresh_token, expiry, updated_at)
     VALUES(?, ?, ?, ?, ?)
     ON CONFLICT(provider_name) DO UPDATE SET
        access_token = excluded.access_token,
        refresh_token = excluded.refresh_token,
        expiry = excluded.expiry,
        updated_at = excluded.updated_at`,
		token.ProviderName,
		token.AccessToken,
		token.RefreshToken,
		formatTimestamp(token.Expiry),
		now,
	)
	if err != nil {
		return fmt.Errorf("save oauth token: %w", err)
	}
	return nil
}

func (s *Store) DeleteOAuthToken(ctx context.Context, providerName string) error {
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM mcp_oauth_tokens WHERE provider_name = ?`,
		providerName,
	)
	if err != nil {
		return fmt.Errorf("delete oauth token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete oauth token rows affected: %w", err)
	}
	if affected == 0 {
		return core.ErrOAuthTokenNotFound
	}
	return nil
}
