package store

import (
	"errors"

	"github.com/ycvk/acorn/internal/domain"
)

var (
	ErrRunNotFound              = errors.New("run not found")
	ErrSessionNotFound          = errors.New("session not found")
	ErrSessionMessageNotFound   = errors.New("session message not found")
	ErrFactNotFound             = errors.New("fact not found")
	ErrPendingActionNotFound    = errors.New("pending action not found")
	ErrPendingActionExists      = errors.New("pending action already exists")
	ErrPendingActionDecided     = errors.New("pending action already decided")
	ErrUnsupportedStorageSchema = errors.New("unsupported storage schema")
	ErrOAuthTokenNotFound       = errors.New("oauth token not found")
	ErrDeviceNotFound           = errors.New("device not found")
	ErrPairingCodeNotFound      = errors.New("pairing code not found")
	ErrPairingCodeUsed          = errors.New("pairing code already used")
	ErrPairingCodeExpired       = errors.New("pairing code expired")
)

// Types — aliases to domain (temporary during migration; removed in Phase 3)
type RunCreateParams = domain.RunCreateParams
type CreatePendingActionInput = domain.PendingActionInput
type OAuthToken = domain.OAuthToken
type OwnerProfile = domain.OwnerProfile
type Device = domain.Device
type PairingCode = domain.PairingCode
