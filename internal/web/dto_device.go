package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

type PairDeviceRequest struct {
	PairingCode string `json:"pairing_code"`
	DeviceName  string `json:"device_name"`
	Platform    string `json:"platform"`
}

type PairDeviceResponse struct {
	Device      DeviceDTO `json:"device"`
	AccessToken string    `json:"access_token"`
}

type DeviceDTO struct {
	DeviceID   string  `json:"device_id"`
	Name       string  `json:"name"`
	Platform   string  `json:"platform"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt string  `json:"last_seen_at"`
	RevokedAt  *string `json:"revoked_at,omitempty"`
}

type DeviceListResponse struct {
	Items []DeviceDTO `json:"items"`
}

func deviceDTOFromView(view app.DeviceView) DeviceDTO {
	return DeviceDTO{
		DeviceID:   view.DeviceID,
		Name:       view.Name,
		Platform:   view.Platform,
		CreatedAt:  view.CreatedAt.UTC().Format(time.RFC3339Nano),
		LastSeenAt: view.LastSeenAt.UTC().Format(time.RFC3339Nano),
		RevokedAt:  optionalDeviceTime(view.RevokedAt),
	}
}
