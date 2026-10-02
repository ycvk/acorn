package runtime

import (
	"encoding/gob"
	"strings"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
)

func compactText(value string, limit int) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return trimmed, false
	}
	return string(runes[:limit]) + "...", true
}

var registerOnce sync.Once

func RegisterTypes() {
	registerOnce.Do(func() {
		gob.Register(toolApprovalState{})
	})
}

// RuntimeStore is the store contract required by the runtime.
// It composes session persistence with artifact and OAuth token storage.
type RuntimeStore interface {
	core.SessionStore
	core.ArtifactStore
}

type RuntimeDeps struct {
	Config            *config.Config
	Store             RuntimeStore
	Loader            *skills.Loader
	MemoryModule      memory.Service
	ContextPlane      *ContextPlane
	MCPPendingActions core.SessionStore
	ArtifactService   core.ArtifactService
	ToolRegistry      core.ToolRegistry
	Presence          core.PresenceStore
	Clock             func() time.Time
	Location          *time.Location
}
