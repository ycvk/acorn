package runtime

import (
	"encoding/gob"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
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
		gob.Register(ElicitationInterruptState{})
		gob.Register(toolApprovalState{})
	})
}

type ElicitationInterruptInfo struct {
	Kind            string
	ActionID        string
	Message         string
	RequestedSchema any
}

type ElicitationInterruptState struct {
	ActionID string
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
	WorldStateUpdater tools.WorldStateUpdater
	ExtraLocalTools   []einotool.BaseTool
	Handlers          []adk.ChatModelAgentMiddleware
	ToolRegistry      core.ToolRegistry
}
