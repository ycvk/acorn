package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Embedder converts text inputs into dense float32 vectors via an external
// embedding model (e.g. an OpenAI-compatible embeddings API).
type Embedder interface {
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error)
}

type EmbedRequest struct {
	Inputs []EmbedInput
}

type EmbedInput struct {
	Ref  string
	Text string
}

type EmbedResult struct {
	Model      string
	Dimensions int
	Vectors    []EmbeddingVector
}

type EmbeddingVector struct {
	Ref    string
	Values []float32
}

const SemanticSchemaMemoryRecordsV1 = "memory_records_v1"

func ValidateEmbedResult(req EmbedRequest, result *EmbedResult, dimensions int) error {
	if result == nil {
		return errors.New("embed result is required")
	}
	if len(result.Vectors) != len(req.Inputs) {
		return fmt.Errorf("embed result vector count = %d, want %d", len(result.Vectors), len(req.Inputs))
	}
	if result.Dimensions != dimensions {
		return fmt.Errorf("embed result dimensions = %d, want %d", result.Dimensions, dimensions)
	}
	if strings.TrimSpace(result.Model) == "" {
		return errors.New("embed result model is required")
	}
	for i, vector := range result.Vectors {
		if strings.TrimSpace(vector.Ref) == "" {
			return fmt.Errorf("embed result vectors[%d].ref is required", i)
		}
		if i < len(req.Inputs) && vector.Ref != req.Inputs[i].Ref {
			return fmt.Errorf("embed result vectors[%d].ref = %q, want %q", i, vector.Ref, req.Inputs[i].Ref)
		}
		if len(vector.Values) != dimensions {
			return fmt.Errorf("embed result vector %q dimensions = %d, want %d", vector.Ref, len(vector.Values), dimensions)
		}
	}
	return nil
}
