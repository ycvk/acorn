package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// NewRunID generates a unique run identifier.
func NewRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

// NewSessionID generates a unique session identifier.
func NewSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

// Midnight is the start of now's day in loc.
func Midnight(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// NewActionID generates a pending action identifier. Clients address actions
// as /v1/pending-actions/{action_id}:decide, so the ID holds only [a-z0-9_].
func NewActionID() string {
	var b [8]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never returns an error.
	return "action_" + hex.EncodeToString(b[:])
}

// InterruptPayloadFromStream converts a StreamInterrupt into a serializable
// map suitable for event payloads. Returns nil if interrupt is nil.
func InterruptPayloadFromStream(interrupt *StreamInterrupt) map[string]any {
	if interrupt == nil {
		return nil
	}
	payload := map[string]any{"context_count": interrupt.ContextCount}
	contexts := make([]map[string]any, 0, len(interrupt.Contexts))
	for _, item := range interrupt.Contexts {
		contexts = append(contexts, map[string]any{
			"id":            item.ID,
			"address":       item.Address,
			"info":          item.Info,
			"is_root_cause": item.IsRootCause,
		})
	}
	payload["contexts"] = contexts
	return payload
}
