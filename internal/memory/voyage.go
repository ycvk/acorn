package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/cloudwego/eino/components/embedding"
)

type VoyageConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	Dimensions int
	Client     *http.Client
}

type Voyage struct{ cfg VoyageConfig }

type embeddingCallOptions struct {
	InputType string
	Usage     *int
}

func embeddingOptions(inputType string, usage *int) embedding.Option {
	return embedding.WrapImplSpecificOptFn(func(o *embeddingCallOptions) { o.InputType = inputType; o.Usage = usage })
}

func NewVoyage(cfg VoyageConfig) (*Voyage, error) {
	if cfg.Client == nil || cfg.BaseURL == "" || cfg.Model == "" || cfg.Dimensions <= 0 {
		return nil, errors.New("voyage requires client, base_url, model and dimensions")
	}
	return &Voyage{cfg: cfg}, nil
}

func (v *Voyage) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	if strings.TrimSpace(v.cfg.APIKey) == "" || strings.Contains(v.cfg.APIKey, "${") {
		return nil, errors.New("memory.embedding.api_key is required; set VOYAGE_API_KEY")
	}
	if len(texts) == 0 || len(texts) > 1000 {
		return nil, errors.New("voyage input must contain 1 to 1000 texts")
	}
	common := embedding.GetCommonOptions(nil, opts...)
	if common.Model != nil && *common.Model != v.cfg.Model {
		return nil, errors.New("voyage call model must match the configured index")
	}
	call := embedding.GetImplSpecificOptions(&embeddingCallOptions{InputType: "document"}, opts...)
	if call.InputType != "query" && call.InputType != "document" {
		return nil, errors.New("voyage input_type must be query or document")
	}
	body, err := json.Marshal(map[string]any{"input": texts, "model": v.cfg.Model, "input_type": call.InputType, "output_dimension": v.cfg.Dimensions, "output_dtype": "float", "truncation": false})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(v.cfg.BaseURL, "/")+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voyage request: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 40*1024*1024+1))
	if err != nil {
		return nil, fmt.Errorf("voyage response: %w", err)
	}
	if len(data) > 40*1024*1024 {
		return nil, errors.New("voyage response exceeds 40 MiB")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voyage response status %d", resp.StatusCode)
	}
	var result struct {
		Model string `json:"model"`
		Data  []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage struct {
			TotalTokens *int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode voyage: %w", err)
	}
	if result.Model != "" && result.Model != v.cfg.Model {
		return nil, fmt.Errorf("voyage returned model %q, expected %q", result.Model, v.cfg.Model)
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("voyage returned %d vectors for %d inputs", len(result.Data), len(texts))
	}
	vectors := make([][]float64, len(texts))
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(vectors) || vectors[item.Index] != nil {
			return nil, errors.New("voyage returned invalid or repeated vector index")
		}
		if len(item.Embedding) != v.cfg.Dimensions {
			return nil, fmt.Errorf("voyage returned %d dimensions, expected %d", len(item.Embedding), v.cfg.Dimensions)
		}
		var norm float64
		for _, n := range item.Embedding {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, errors.New("voyage returned non-finite vector")
			}
			norm += n * n
		}
		if norm == 0 {
			return nil, errors.New("voyage returned zero vector")
		}
		vectors[item.Index] = item.Embedding
	}
	if call.Usage != nil && result.Usage.TotalTokens != nil {
		*call.Usage = *result.Usage.TotalTokens
	}
	return vectors, nil
}
