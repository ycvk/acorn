package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"
)

func TestDeviceAuthPairingCreatesHashedCodeAndToken(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	service := NewDeviceAuthService(store)
	service.now = fixedDeviceAuthNow
	service.rand = strings.NewReader(strings.Repeat("a", 128))

	code, err := service.CreatePairingCode(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("create pairing code: %v", err)
	}
	if code.Code == "" || code.ExpiresAt.IsZero() {
		t.Fatalf("unexpected pairing code view: %#v", code)
	}
	if strings.Contains(code.Code, "a") {
		t.Fatalf("pairing code should be encoded, got %q", code.Code)
	}
	storedCode, err := store.LoadPairingCode(ctx, hashSecret(normalizePairingCode(code.Code)))
	if err != nil {
		t.Fatalf("load stored code: %v", err)
	}
	if storedCode.CodeHash == normalizePairingCode(code.Code) {
		t.Fatal("pairing code stored raw code instead of hash")
	}

	result, err := service.PairDevice(ctx, PairDeviceInput{
		PairingCode: code.Code,
		DeviceName:  "iPhone",
		Platform:    "ios",
	})
	if err != nil {
		t.Fatalf("pair device: %v", err)
	}
	if result.AccessToken == "" || !strings.HasPrefix(result.AccessToken, "acorn_dev_") {
		t.Fatalf("unexpected access token %q", result.AccessToken)
	}
	if result.Device.DeviceID == "" || result.Device.Name != "iPhone" || result.Device.Platform != "ios" {
		t.Fatalf("unexpected device result: %#v", result.Device)
	}
	if _, err := store.LoadDeviceByTokenHash(ctx, result.AccessToken); !errors.Is(err, storecore.ErrDeviceNotFound) {
		t.Fatalf("raw token lookup error = %v, want ErrDeviceNotFound", err)
	}
	if _, err := store.LoadDeviceByTokenHash(ctx, hashSecret(result.AccessToken)); err != nil {
		t.Fatalf("hashed token lookup: %v", err)
	}
}

func TestDeviceAuthPairingCodeIsOneTimeAndExpires(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	service := NewDeviceAuthService(store)
	service.now = fixedDeviceAuthNow
	service.rand = strings.NewReader(strings.Repeat("b", 256))

	code, err := service.CreatePairingCode(ctx, time.Minute)
	if err != nil {
		t.Fatalf("create pairing code: %v", err)
	}
	if _, err := service.PairDevice(ctx, PairDeviceInput{PairingCode: code.Code, DeviceName: "first", Platform: "ios"}); err != nil {
		t.Fatalf("first pair: %v", err)
	}
	if _, err := service.PairDevice(ctx, PairDeviceInput{PairingCode: code.Code, DeviceName: "second", Platform: "ios"}); !errors.Is(err, ErrInvalidPairingCode) {
		t.Fatalf("second pair error = %v, want ErrInvalidPairingCode", err)
	}

	service.rand = strings.NewReader(strings.Repeat("c", 128))
	expired, err := service.CreatePairingCode(ctx, time.Minute)
	if err != nil {
		t.Fatalf("create expiring code: %v", err)
	}
	service.now = func() time.Time { return fixedDeviceAuthNow().Add(2 * time.Minute) }
	if _, err := service.PairDevice(ctx, PairDeviceInput{PairingCode: expired.Code, DeviceName: "late", Platform: "ios"}); !errors.Is(err, ErrInvalidPairingCode) {
		t.Fatalf("expired pair error = %v, want ErrInvalidPairingCode", err)
	}
}

func TestDeviceAuthAuthenticateAndRevoke(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	service := NewDeviceAuthService(store)
	service.now = fixedDeviceAuthNow
	service.rand = strings.NewReader(strings.Repeat("d", 256))

	code, err := service.CreatePairingCode(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("create pairing code: %v", err)
	}
	paired, err := service.PairDevice(ctx, PairDeviceInput{PairingCode: code.Code, DeviceName: "iPad", Platform: "ios"})
	if err != nil {
		t.Fatalf("pair device: %v", err)
	}

	service.now = func() time.Time { return fixedDeviceAuthNow().Add(5 * time.Minute) }
	auth, err := service.Authenticate(ctx, paired.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if auth.Device.DeviceID != paired.Device.DeviceID {
		t.Fatalf("auth device = %q, want %q", auth.Device.DeviceID, paired.Device.DeviceID)
	}
	if !auth.Device.LastSeenAt.Equal(fixedDeviceAuthNow().Add(5 * time.Minute)) {
		t.Fatalf("last_seen_at = %v", auth.Device.LastSeenAt)
	}

	devices, err := service.ListDevices(ctx)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 || devices[0].DeviceID != paired.Device.DeviceID {
		t.Fatalf("unexpected devices: %#v", devices)
	}

	if err := service.RevokeDevice(ctx, paired.Device.DeviceID); err != nil {
		t.Fatalf("revoke device: %v", err)
	}
	if _, err := service.Authenticate(ctx, paired.AccessToken); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("revoked auth error = %v, want ErrDeviceRevoked", err)
	}
}

func TestDeviceAuthRejectsInvalidInputs(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	service := NewDeviceAuthService(store)
	service.now = fixedDeviceAuthNow
	service.rand = strings.NewReader(strings.Repeat("e", 128))

	if _, err := service.CreatePairingCode(ctx, 0); err == nil {
		t.Fatal("CreatePairingCode should reject non-positive ttl")
	}
	if _, err := service.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty auth error = %v, want ErrUnauthenticated", err)
	}
	if _, err := service.Authenticate(ctx, "missing-token"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing token auth error = %v, want ErrUnauthenticated", err)
	}
	if _, err := service.PairDevice(ctx, PairDeviceInput{PairingCode: "", DeviceName: "phone", Platform: "ios"}); !errors.Is(err, ErrInvalidPairingCode) {
		t.Fatalf("empty pairing code error = %v, want ErrInvalidPairingCode", err)
	}
	if _, err := service.PairDevice(ctx, PairDeviceInput{PairingCode: "missing", DeviceName: "", Platform: "ios"}); err == nil || err.Error() != "device name is required" {
		t.Fatalf("missing device name error = %v", err)
	}
	if err := service.RevokeDevice(ctx, "missing"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("missing revoke error = %v, want ErrDeviceNotFound", err)
	}
}

func fixedDeviceAuthNow() time.Time {
	return time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
}
