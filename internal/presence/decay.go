// Package presence holds the deterministic rules of working memory: how items
// decay, how "the present" is rendered for the model, and the owner-editable
// persona.
package presence

import (
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// activeTTL is how long an untouched item stays active before it rests.
var activeTTL = map[core.MemoryKind]time.Duration{
	core.MemorySaid:     7 * 24 * time.Hour,
	core.MemoryThought:  48 * time.Hour,
	core.MemoryTendency: 30 * 24 * time.Hour,
	core.MemoryRuler:    30 * 24 * time.Hour,
}

// restingTTL is how long a resting item stays before it sinks.
const restingTTL = 14 * 24 * time.Hour

// NewExpiry returns the expiry for an item of kind that becomes active or is
// renewed at now. Commitments do not expire; they get the zero time.
func NewExpiry(kind core.MemoryKind, now time.Time) time.Time {
	ttl, ok := activeTTL[kind]
	if !ok {
		return time.Time{}
	}
	return now.Add(ttl)
}

// Decay returns copies of the items whose status changes at now: active items
// past their expiry start resting, resting items past their expiry sink.
// Commitments and items in any other state never decay.
func Decay(items []core.MemoryItem, now time.Time) []core.MemoryItem {
	var changed []core.MemoryItem
	for _, item := range items {
		if item.Kind == core.MemoryCommitment || item.ExpiresAt.IsZero() || !now.After(item.ExpiresAt) {
			continue
		}
		switch item.Status {
		case core.MemoryActive:
			item.Status = core.MemoryResting
			item.ExpiresAt = now.Add(restingTTL)
		case core.MemoryResting:
			item.Status = core.MemorySunk
			item.ExpiresAt = time.Time{}
		default:
			continue
		}
		changed = append(changed, item)
	}
	return changed
}
