package store

import (
	"errors"
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
