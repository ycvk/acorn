package api

import (
	"encoding/json"

	"github.com/cloudwego/eino/compose"
)

// JSONSerializer is a simple JSON serializer for eino compose graphs.
type JSONSerializer struct{}

func (j *JSONSerializer) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (j *JSONSerializer) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

var _ compose.Serializer = (*JSONSerializer)(nil)
