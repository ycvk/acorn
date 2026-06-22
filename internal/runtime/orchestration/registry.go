package orchestration

import "encoding/gob"

// RegisterTypes registers all types required for orchestration serialization.
// This replaces the former init() registration and must be called once during
// application bootstrap before any orchestration operations.
func RegisterTypes() {
	gob.Register(&DirectResponseInterruptData{})
}
