package api

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type deviceAuthServiceStore struct {
	unimplementedStore
	stub         *deviceAuthHandlerStub
	pairedDevice *core.Device
}

func newDeviceAuthTestService(stub *deviceAuthHandlerStub) *DeviceAuthService {
	if stub == nil {
		stub = &deviceAuthHandlerStub{}
	}
	return &DeviceAuthService{
		store: &deviceAuthServiceStore{stub: stub},
		now:   func() time.Time { return time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC) },
		rand:  bytes.NewReader(bytes.Repeat([]byte{0x11}, 128)),
	}
}

func (s *deviceAuthServiceStore) stubOrErr() (*deviceAuthHandlerStub, error) {
	if s == nil || s.stub == nil {
		return nil, errUnexpectedClientStoreCall
	}
	return s.stub, nil
}

func (s *deviceAuthServiceStore) SavePairingCode(context.Context, *core.PairingCode) error {
	return nil
}

func (s *deviceAuthServiceStore) ConsumePairingCode(_ context.Context, _ string, _ time.Time) (*core.PairingCode, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if stub.pairErr != nil {
		return nil, stub.pairErr
	}
	return &core.PairingCode{}, nil
}

func (s *deviceAuthServiceStore) SaveDevice(_ context.Context, device *core.Device) error {
	if s == nil || device == nil {
		return errUnexpectedClientStoreCall
	}
	deviceCopy := *device
	s.pairedDevice = &deviceCopy
	return nil
}

func (s *deviceAuthServiceStore) LoadDeviceByTokenHash(_ context.Context, _ string) (*core.Device, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if stub.authErr != nil {
		if errors.Is(stub.authErr, ErrDeviceRevoked) {
			now := time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)
			return &core.Device{DeviceID: "device_test", Name: "Test device", Platform: "test", RevokedAt: &now}, nil
		}
		return nil, core.ErrDeviceNotFound
	}
	if s.pairedDevice != nil {
		deviceCopy := *s.pairedDevice
		return &deviceCopy, nil
	}
	return &core.Device{
		DeviceID:   "device_test",
		Name:       "Test device",
		Platform:   "test",
		CreatedAt:  time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		LastSeenAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s *deviceAuthServiceStore) ListDevices(context.Context) ([]core.Device, error) {
	stub, err := s.stubOrErr()
	if err != nil {
		return nil, err
	}
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	items := make([]core.Device, 0, len(stub.devices))
	for _, item := range stub.devices {
		items = append(items, core.Device{
			DeviceID:   item.DeviceID,
			Name:       item.Name,
			Platform:   item.Platform,
			CreatedAt:  item.CreatedAt,
			LastSeenAt: item.LastSeenAt,
			RevokedAt:  item.RevokedAt,
		})
	}
	if s.pairedDevice != nil {
		items = append(items, *s.pairedDevice)
	}
	return items, nil
}

func (s *deviceAuthServiceStore) TouchDevice(context.Context, string, time.Time) error {
	return nil
}

func (s *deviceAuthServiceStore) RevokeDevice(_ context.Context, deviceID string, _ time.Time) error {
	stub, err := s.stubOrErr()
	if err != nil {
		return err
	}
	if stub.revokeErr != nil {
		return stub.revokeErr
	}
	stub.revokedDeviceID = deviceID
	return nil
}
