package sqlite

import storecore "github.com/ycvk/acorn/internal/store"

type RunCreateParams = storecore.RunCreateParams

type OAuthToken = storecore.OAuthToken
type OwnerProfile = storecore.OwnerProfile
type Device = storecore.Device
type DevicePushToken = storecore.DevicePushToken
type PairingCode = storecore.PairingCode
type Notification = storecore.Notification
type NotificationDelivery = storecore.NotificationDelivery

type CreatePendingActionInput = storecore.CreatePendingActionInput

type PlanEvidence = storecore.PlanEvidence
type PlanRepoTarget = storecore.PlanRepoTarget
type VerificationIntent = storecore.VerificationIntent
type PlanStep = storecore.PlanStep
type PlanRecord = storecore.PlanRecord

var (
	ErrRunNotFound              = storecore.ErrRunNotFound
	ErrSessionNotFound          = storecore.ErrSessionNotFound
	ErrSessionMessageNotFound   = storecore.ErrSessionMessageNotFound
	ErrFactNotFound             = storecore.ErrFactNotFound
	ErrPendingActionNotFound    = storecore.ErrPendingActionNotFound
	ErrPendingActionExists      = storecore.ErrPendingActionExists
	ErrPendingActionDecided     = storecore.ErrPendingActionDecided
	ErrUnsupportedStorageSchema = storecore.ErrUnsupportedStorageSchema
	ErrOAuthTokenNotFound       = storecore.ErrOAuthTokenNotFound
	ErrPlanNotFound             = storecore.ErrPlanNotFound
	ErrDeviceNotFound           = storecore.ErrDeviceNotFound
	ErrPairingCodeNotFound      = storecore.ErrPairingCodeNotFound
	ErrPairingCodeUsed          = storecore.ErrPairingCodeUsed
	ErrPairingCodeExpired       = storecore.ErrPairingCodeExpired
	ErrDevicePushTokenNotFound  = storecore.ErrDevicePushTokenNotFound
	ErrNotificationNotFound     = storecore.ErrNotificationNotFound
)
