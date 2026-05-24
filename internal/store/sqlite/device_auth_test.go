package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"
)

func TestDeviceAuthOwnerProfileExistsOnOpen(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	profile, err := store.LoadOwnerProfile(ctx)
	if err != nil {
		t.Fatalf("load owner profile: %v", err)
	}
	if profile.OwnerID != "owner" {
		t.Fatalf("owner id = %q, want owner", profile.OwnerID)
	}
	if profile.CreatedAt.IsZero() {
		t.Fatal("owner created_at should be set")
	}
}

func TestDeviceAuthPairingCodeLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	code := &storecore.PairingCode{
		CodeHash:  "hash-code",
		ExpiresAt: now.Add(10 * time.Minute),
		CreatedAt: now,
	}

	if err := store.SavePairingCode(ctx, code); err != nil {
		t.Fatalf("save pairing code: %v", err)
	}
	got, err := store.LoadPairingCode(ctx, "hash-code")
	if err != nil {
		t.Fatalf("load pairing code: %v", err)
	}
	if got.CodeHash != "hash-code" || !got.ExpiresAt.Equal(code.ExpiresAt) || got.UsedAt != nil {
		t.Fatalf("unexpected pairing code: %#v", got)
	}

	consumed, err := store.ConsumePairingCode(ctx, "hash-code", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("consume pairing code: %v", err)
	}
	if consumed.UsedAt == nil || !consumed.UsedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("used_at = %v, want %v", consumed.UsedAt, now.Add(time.Minute))
	}
	if _, err := store.ConsumePairingCode(ctx, "hash-code", now.Add(2*time.Minute)); !errors.Is(err, storecore.ErrPairingCodeUsed) {
		t.Fatalf("second consume error = %v, want storecore.ErrPairingCodeUsed", err)
	}
}

func TestDeviceAuthPairingCodeExpiryAndMissing(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)

	if _, err := store.LoadPairingCode(ctx, "missing"); !errors.Is(err, storecore.ErrPairingCodeNotFound) {
		t.Fatalf("load missing code error = %v, want storecore.ErrPairingCodeNotFound", err)
	}
	if err := store.SavePairingCode(ctx, &storecore.PairingCode{
		CodeHash:  "expired",
		ExpiresAt: now.Add(-time.Second),
		CreatedAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("save expired code: %v", err)
	}
	if _, err := store.ConsumePairingCode(ctx, "expired", now); !errors.Is(err, storecore.ErrPairingCodeExpired) {
		t.Fatalf("consume expired code error = %v, want storecore.ErrPairingCodeExpired", err)
	}
}

func TestDeviceAuthDeviceLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
	device := &storecore.Device{
		DeviceID:   "device_1",
		Name:       "iPhone",
		Platform:   "ios",
		TokenHash:  "token-hash",
		CreatedAt:  now,
		LastSeenAt: now,
	}

	if err := store.SaveDevice(ctx, device); err != nil {
		t.Fatalf("save device: %v", err)
	}
	got, err := store.LoadDeviceByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("load device by token: %v", err)
	}
	if got.DeviceID != "device_1" || got.TokenHash != "token-hash" || got.RevokedAt != nil {
		t.Fatalf("unexpected device: %#v", got)
	}

	touchedAt := now.Add(5 * time.Minute)
	if err := store.TouchDevice(ctx, "device_1", touchedAt); err != nil {
		t.Fatalf("touch device: %v", err)
	}
	got, err = store.LoadDeviceByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("load touched device: %v", err)
	}
	if !got.LastSeenAt.Equal(touchedAt) {
		t.Fatalf("last_seen_at = %v, want %v", got.LastSeenAt, touchedAt)
	}

	devices, err := store.ListDevices(ctx)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "device_1" {
		t.Fatalf("unexpected devices: %#v", devices)
	}

	revokedAt := now.Add(10 * time.Minute)
	if err := store.RevokeDevice(ctx, "device_1", revokedAt); err != nil {
		t.Fatalf("revoke device: %v", err)
	}
	got, err = store.LoadDeviceByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("load revoked device: %v", err)
	}
	if got.RevokedAt == nil || !got.RevokedAt.Equal(revokedAt) {
		t.Fatalf("revoked_at = %v, want %v", got.RevokedAt, revokedAt)
	}
}

func TestDeviceAuthDeviceNotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if _, err := store.LoadDeviceByTokenHash(ctx, "missing"); !errors.Is(err, storecore.ErrDeviceNotFound) {
		t.Fatalf("load missing device error = %v, want storecore.ErrDeviceNotFound", err)
	}
	if err := store.TouchDevice(ctx, "missing", time.Now().UTC()); !errors.Is(err, storecore.ErrDeviceNotFound) {
		t.Fatalf("touch missing device error = %v, want storecore.ErrDeviceNotFound", err)
	}
	if err := store.RevokeDevice(ctx, "missing", time.Now().UTC()); !errors.Is(err, storecore.ErrDeviceNotFound) {
		t.Fatalf("revoke missing device error = %v, want storecore.ErrDeviceNotFound", err)
	}
}
