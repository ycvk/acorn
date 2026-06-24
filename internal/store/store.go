package store

import "github.com/ycvk/acorn/internal/core"

// Sentinel errors are aliased to core for cross-package use without
// importing store. Store-internal code uses these directly.
var (
	ErrRunNotFound              = core.ErrRunNotFound
	ErrSessionNotFound          = core.ErrSessionNotFound
	ErrSessionMessageNotFound   = core.ErrSessionMessageNotFound
	ErrFactNotFound             = core.ErrFactNotFound
	ErrPendingActionNotFound    = core.ErrPendingActionNotFound
	ErrPendingActionExists      = core.ErrPendingActionExists
	ErrPendingActionDecided     = core.ErrPendingActionDecided
	ErrUnsupportedStorageSchema = core.ErrUnsupportedStorageSchema
	ErrOAuthTokenNotFound       = core.ErrOAuthTokenNotFound
	ErrDeviceNotFound           = core.ErrDeviceNotFound
	ErrPairingCodeNotFound      = core.ErrPairingCodeNotFound
	ErrPairingCodeUsed          = core.ErrPairingCodeUsed
	ErrPairingCodeExpired       = core.ErrPairingCodeExpired
)
