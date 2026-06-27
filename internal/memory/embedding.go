package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// EmbeddingClient calls an OpenAI-compatible /v1/embeddings endpoint to
// generate vectors for memory records. It reuses the primary provider's
// base_url + api_key, so no separate credential is needed.
type EmbeddingClient struct {
	baseURL string
	apiKey  string
	model   string
	dims    int
	client  *http.Client
}

// EmbeddingConfig is the minimal config for constructing an EmbeddingClient.
// BaseURL and APIKey come from the enabled provider; Model and Dimensions
// come from MemoryEmbeddingConfig.
type EmbeddingConfig struct {
	BaseURL    string
	APIKey     string
	Model      string
	Dimensions int
}

// NewEmbeddingClient constructs a client. An empty baseURL or apiKey yields a
// nil client (semantic retrieval is disabled; search falls back to keyword).
func NewEmbeddingClient(cfg EmbeddingConfig) *EmbeddingClient {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil
	}
	dims := cfg.Dimensions
	if dims <= 0 {
		dims = 1536
	}
	return &EmbeddingClient{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		dims:    dims,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Dims returns the embedding dimension the client was configured for.
func (c *EmbeddingClient) Dims() int {
	if c == nil {
		return 0
	}
	return c.dims
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed generates a vector for a single text input. Returns the raw float32
// vector. The caller is responsible for storing it via VectorIndex.
func (c *EmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	if c == nil {
		return nil, fmt.Errorf("embedding client is not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("embedding input text is empty")
	}
	reqBody := embeddingRequest{Model: c.model, Input: []string{text}}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}
	url := c.baseURL + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call embedding endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embedding endpoint returned %d: %s", resp.StatusCode, string(raw))
	}
	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) == 0 {
		return nil, fmt.Errorf("embedding response has no data")
	}
	return result.Data[0].Embedding, nil
}

// Enabled reports whether the client is configured and ready.
func (c *EmbeddingClient) Enabled() bool {
	return c != nil
}
