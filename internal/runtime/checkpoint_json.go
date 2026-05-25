package runtime

import (
	"encoding/json"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// jsonSerializer implements compose.Serializer using encoding/json.
// Unlike gob, JSON preserves pointer type information for *schema.Message,
// which is critical for compose.Graph checkpoint round-trips.
type jsonSerializer struct{}

func (j *jsonSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (j *jsonSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// Verify interface compliance.
var _ compose.Serializer = (*jsonSerializer)(nil)

type schemaMessageWrapper struct {
	Type  string          `json:"__type"`
	Value *schema.Message `json:"value"`
}
