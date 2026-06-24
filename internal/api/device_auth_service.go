package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/store"
)

var (
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrDeviceRevoked      = errors.New("device revoked")
	ErrInvalidPairingCode = errors.New("invalid pairing code")
	ErrDeviceNotFound     = errors.New("device not found")
)

type PairingCodeView struct {
	Code      string
	ExpiresAt time.Time
}

type PairDeviceInput struct {
	PairingCode string
	DeviceName  string
	Platform    string
}

type PairDeviceResult struct {
	Device      DeviceView
	AccessToken string
}

type DeviceAuthContext struct {
	Device DeviceView
}

type DeviceView struct {
	DeviceID   string
	Name       string
	Platform   string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

type DeviceAuthService struct {
	store StoreView
	now   func() time.Time
	rand  io.Reader
}

func NewDeviceAuthService(store StoreView) *DeviceAuthService {
	return &DeviceAuthService{
		store: store,
		now:   func() time.Time { return time.Now().UTC() },
		rand:  rand.Reader,
	}
}

func (s *DeviceAuthService) CreatePairingCode(ctx context.Context, ttl time.Duration) (*PairingCodeView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	if ttl <= 0 {
		return nil, errors.New("pairing code ttl must be positive")
	}
	code, err := generatePairingCode(s.rand)
	if err != nil {
		return nil, err
	}
	now := s.now()
	record := &core.PairingCode{
		CodeHash:  hashSecret(normalizePairingCode(code)),
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
	if err := s.store.SavePairingCode(ctx, record); err != nil {
		return nil, err
	}
	return &PairingCodeView{Code: code, ExpiresAt: record.ExpiresAt}, nil
}

func (s *DeviceAuthService) PairDevice(ctx context.Context, input PairDeviceInput) (*PairDeviceResult, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	code := normalizePairingCode(input.PairingCode)
	if code == "" {
		return nil, ErrInvalidPairingCode
	}
	name := strings.TrimSpace(input.DeviceName)
	if name == "" {
		return nil, errors.New("device name is required")
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		return nil, errors.New("device platform is required")
	}
	now := s.now()
	if _, err := s.store.ConsumePairingCode(ctx, hashSecret(code), now); err != nil {
		if errors.Is(err, store.ErrPairingCodeNotFound) || errors.Is(err, store.ErrPairingCodeUsed) || errors.Is(err, store.ErrPairingCodeExpired) {
			return nil, ErrInvalidPairingCode
		}
		return nil, err
	}
	deviceID, err := generatePrefixedID(s.rand, "device", 16)
	if err != nil {
		return nil, err
	}
	token, err := generateDeviceToken(s.rand)
	if err != nil {
		return nil, err
	}
	device := &core.Device{
		DeviceID:   deviceID,
		Name:       name,
		Platform:   platform,
		TokenHash:  hashSecret(token),
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if err := s.store.SaveDevice(ctx, device); err != nil {
		return nil, err
	}
	return &PairDeviceResult{
		Device:      deviceViewFromRecord(*device),
		AccessToken: token,
	}, nil
}

func (s *DeviceAuthService) Authenticate(ctx context.Context, rawToken string) (*DeviceAuthContext, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return nil, ErrUnauthenticated
	}
	device, err := s.store.LoadDeviceByTokenHash(ctx, hashSecret(token))
	if err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			return nil, ErrUnauthenticated
		}
		return nil, err
	}
	if device.RevokedAt != nil {
		return nil, ErrDeviceRevoked
	}
	now := s.now()
	if err := s.store.TouchDevice(ctx, device.DeviceID, now); err != nil {
		return nil, err
	}
	device.LastSeenAt = now
	return &DeviceAuthContext{Device: deviceViewFromRecord(*device)}, nil
}

func (s *DeviceAuthService) ListDevices(ctx context.Context) ([]DeviceView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("device auth store is nil")
	}
	records, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices := make([]DeviceView, 0, len(records))
	for _, record := range records {
		devices = append(devices, deviceViewFromRecord(record))
	}
	return devices, nil
}

func (s *DeviceAuthService) RevokeDevice(ctx context.Context, deviceID string) error {
	if s == nil || s.store == nil {
		return errors.New("device auth store is nil")
	}
	trimmed := strings.TrimSpace(deviceID)
	if trimmed == "" {
		return ErrDeviceNotFound
	}
	if err := s.store.RevokeDevice(ctx, trimmed, s.now()); err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			return ErrDeviceNotFound
		}
		return err
	}
	return nil
}

func deviceViewFromRecord(record core.Device) DeviceView {
	return DeviceView{
		DeviceID:   record.DeviceID,
		Name:       record.Name,
		Platform:   record.Platform,
		CreatedAt:  record.CreatedAt,
		LastSeenAt: record.LastSeenAt,
		RevokedAt:  record.RevokedAt,
	}
}

func normalizePairingCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func generatePairingCode(reader io.Reader) (string, error) {
	raw := make([]byte, 10)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate pairing code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	if len(encoded) < 16 {
		return "", errors.New("generated pairing code is too short")
	}
	encoded = encoded[:16]
	return encoded[0:4] + "-" + encoded[4:8] + "-" + encoded[8:12] + "-" + encoded[12:16], nil
}

func generateDeviceToken(reader io.Reader) (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate device token: %w", err)
	}
	return "acorn_dev_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func generatePrefixedID(reader io.Reader, prefix string, bytesLen int) (string, error) {
	raw := make([]byte, bytesLen)
	if _, err := io.ReadFull(reader, raw); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(raw), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
