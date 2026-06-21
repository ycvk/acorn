package runtime

import (
	"encoding/gob"
	"sync"

	"github.com/ycvk/acorn/internal/orchestration"
)

var registerOnce sync.Once

// RegisterTypes registers all types required for runtime serialization.
// This replaces the former scattered init() registrations and must be called
// once during application bootstrap before any runtime operations.
// Safe to call multiple times; subsequent calls are no-ops.
func RegisterTypes() {
	registerOnce.Do(func() {
		gob.Register(ElicitationInterruptState{})
		orchestration.RegisterTypes()
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
