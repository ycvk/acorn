package runtime

import (
	"context"
	"encoding/gob"
	"strings"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
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

// SkillScanner reads the skills in use.
type SkillScanner interface {
	Scan(ctx context.Context) (*skills.ScanResult, error)
}

type RuntimeDeps struct {
	Config             *config.Config
	Store              RuntimeStore
	Skills             SkillScanner
	MCPPendingActions  core.SessionStore
	ArtifactService    core.ArtifactService
	ToolRegistry       core.ToolRegistry
	PhoneNotifications core.PhoneNotificationStore
	Activity           core.ActivityStore
	MemoryStore        core.MemoryStore
	Memory             MemoryContextService
	Commitments        core.CommitmentStore
	Attachments        core.AttachmentReader
	Workstation        tools.WorkstationToolsConfig
	Reviewer           ActionReviewer
	Clock              func() time.Time
	Location           *time.Location
}
