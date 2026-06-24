package api

import (
	"github.com/ycvk/acorn/internal/core"
)

// StoreView is the store contract required by app-facing services.
// It composes the session, identity, and artifact store capabilities with
// the message convenience methods that the client/inbox/pending-action/run
// services depend on.
type StoreView interface {
	core.SessionStore
	core.IdentityStore
	core.ArtifactStore
}
