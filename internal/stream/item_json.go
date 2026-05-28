package stream

import (
	"encoding/json"
	"fmt"
	"time"
)

// StreamItem is a single event in the run stream.
type StreamItem struct {
	RunID     string         `json:"run_id"`
	Sequence  int64          `json:"sequence,omitempty"`
	Kind      StreamItemKind `json:"kind"`
	CreatedAt time.Time      `json:"created_at"`
	Payload   map[string]any `json:"-"`
}

// MarshalJSON serializes StreamItem with payload fields flattened into the
// top-level object. The "kind" field acts as the discriminator.
func (item StreamItem) MarshalJSON() ([]byte, error) {
	obj := map[string]any{
		"run_id":     item.RunID,
		"kind":       string(item.Kind),
		"created_at": item.CreatedAt,
	}
	if item.Sequence != 0 {
		obj["sequence"] = item.Sequence
	}
	for k, v := range item.Payload {
		obj[k] = v
	}
	return json.Marshal(obj)
}

// UnmarshalJSON deserializes flat StreamItem JSON, extracting common fields
// and keeping the remaining keys as the payload map.
func (item *StreamItem) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	runID, ok := raw["run_id"].(string)
	if !ok {
		return fmt.Errorf("stream item run_id must be a string")
	}
	kindStr, ok := raw["kind"].(string)
	if !ok {
		return fmt.Errorf("stream item kind must be a string")
	}

	var sequence int64
	if seq, ok := raw["sequence"]; ok {
		switch v := seq.(type) {
		case float64:
			sequence = int64(v)
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return fmt.Errorf("parse stream item sequence: %w", err)
			}
			sequence = n
		}
	}

	var createdAt time.Time
	if ca, ok := raw["created_at"]; ok {
		if caStr, ok := ca.(string); ok {
			t, err := time.Parse(time.RFC3339Nano, caStr)
			if err != nil {
				t, err = time.Parse(time.RFC3339, caStr)
				if err != nil {
					return fmt.Errorf("parse created_at: %w", err)
				}
			}
			createdAt = t
		}
	}

	item.RunID = runID
	item.Kind = StreamItemKind(kindStr)
	item.Sequence = sequence
	item.CreatedAt = createdAt

	delete(raw, "run_id")
	delete(raw, "kind")
	delete(raw, "sequence")
	delete(raw, "created_at")
	item.Payload = raw

	return nil
}
