package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestOAuthTokenGetNotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	_, err := store.GetOAuthToken(ctx, "nonexistent")
	if !errors.Is(err, ErrOAuthTokenNotFound) {
		t.Fatalf("expected ErrOAuthTokenNotFound, got %v", err)
	}
}

func TestOAuthTokenSaveAndGetRoundTrip(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	expiry := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	token := core.OAuthToken{
		ProviderName: "test-provider",
		AccessToken:  "access-abc",
		RefreshToken: "refresh-xyz",
		Expiry:       expiry,
		UpdatedAt:    time.Now().UTC(),
	}

	if err := store.SaveOAuthToken(ctx, token); err != nil {
		t.Fatalf("save oauth token: %v", err)
	}

	got, err := store.GetOAuthToken(ctx, "test-provider")
	if err != nil {
		t.Fatalf("get oauth token: %v", err)
	}

	if got.ProviderName != token.ProviderName {
		t.Fatalf("provider_name = %q, want %q", got.ProviderName, token.ProviderName)
	}
	if got.AccessToken != token.AccessToken {
		t.Fatalf("access_token = %q, want %q", got.AccessToken, token.AccessToken)
	}
	if got.RefreshToken != token.RefreshToken {
		t.Fatalf("refresh_token = %q, want %q", got.RefreshToken, token.RefreshToken)
	}
	if got.Expiry.Unix() != token.Expiry.Unix() {
		t.Fatalf("expiry = %v, want %v", got.Expiry, token.Expiry)
	}
}

func TestOAuthTokenSaveUpserts(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token1 := core.OAuthToken{
		ProviderName: "upsert-provider",
		AccessToken:  "first-access",
		RefreshToken: "first-refresh",
		Expiry:       time.Now().UTC().Add(time.Hour),
	}

	if err := store.SaveOAuthToken(ctx, token1); err != nil {
		t.Fatalf("first save: %v", err)
	}

	token2 := core.OAuthToken{
		ProviderName: "upsert-provider",
		AccessToken:  "second-access",
		RefreshToken: "second-refresh",
		Expiry:       time.Now().UTC().Add(2 * time.Hour),
	}

	if err := store.SaveOAuthToken(ctx, token2); err != nil {
		t.Fatalf("second save (upsert): %v", err)
	}

	got, err := store.GetOAuthToken(ctx, "upsert-provider")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}

	if got.AccessToken != "second-access" {
		t.Fatalf("access_token after upsert = %q, want %q", got.AccessToken, "second-access")
	}
	if got.RefreshToken != "second-refresh" {
		t.Fatalf("refresh_token after upsert = %q, want %q", got.RefreshToken, "second-refresh")
	}
}

func TestOAuthTokenDelete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	token := core.OAuthToken{
		ProviderName: "delete-provider",
		AccessToken:  "to-delete",
		RefreshToken: "to-delete-refresh",
		Expiry:       time.Now().UTC().Add(time.Hour),
	}

	if err := store.SaveOAuthToken(ctx, token); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := store.DeleteOAuthToken(ctx, "delete-provider"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := store.GetOAuthToken(ctx, "delete-provider")
	if !errors.Is(err, ErrOAuthTokenNotFound) {
		t.Fatalf("expected ErrOAuthTokenNotFound after delete, got %v", err)
	}
}

func TestOAuthTokenDeleteNotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	err := store.DeleteOAuthToken(ctx, "nonexistent")
	if !errors.Is(err, ErrOAuthTokenNotFound) {
		t.Fatalf("expected ErrOAuthTokenNotFound on delete of missing token, got %v", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
