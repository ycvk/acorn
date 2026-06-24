package domain

import "github.com/ycvk/acorn/internal/core"

// The following types are aliases for their canonical definitions in
// internal/core. The store package predates core; its methods are typed
// against these domain names. Aliasing (rather than duplicating) lets a single
// *store.Store satisfy both the legacy port.*Repo interfaces (typed against
// domain) and the new core.SessionStore / core.IdentityStore / core.ArtifactStore
// interfaces (typed against core), because the two names now denote the same type.

type RunCreateParams = core.RunCreateParams

type PendingActionInput = core.PendingActionInput

type PairingCode = core.PairingCode

type Device = core.Device

type OAuthToken = core.OAuthToken

type ArtifactWriteRequest = core.ArtifactWriteRequest

type ArtifactRecord = core.ArtifactRecord

type ArtifactReadRangeRequest = core.ArtifactReadRangeRequest

type ArtifactReadRangeResult = core.ArtifactReadRangeResult
