package memorymodule

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatibleEmbedderConfig struct {
	BaseURL        string
	APIKey         string
	Model          string
	Dimensions     int
	TimeoutSeconds int
	HTTPClient     *http.Client
}

type OpenAICompatibleEmbedder struct {
	endpoint   string
	apiKey     string
	model      string
	dimensions int
	client     *http.Client
}

func NewOpenAICompatibleEmbedder(cfg OpenAICompatibleEmbedderConfig) (*OpenAICompatibleEmbedder, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		return nil, errors.New("embedding base_url is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("embedding api_key is required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("embedding model is required")
	}
	if cfg.Dimensions <= 0 {
		return nil, errors.New("embedding dimensions must be > 0")
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		return nil, errors.New("embedding timeout_seconds must be > 0")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: time.Duration(timeout) * time.Second}
	}
	return &OpenAICompatibleEmbedder{
		endpoint:   baseURL + "/embeddings",
		apiKey:     strings.TrimSpace(cfg.APIKey),
		model:      strings.TrimSpace(cfg.Model),
		dimensions: cfg.Dimensions,
		client:     client,
	}, nil
}

func (e *OpenAICompatibleEmbedder) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	if e == nil {
		return nil, errors.New("openai-compatible embedder is nil")
	}
	if len(req.Inputs) == 0 {
		return &EmbedResult{Model: e.model, Dimensions: e.dimensions}, nil
	}
	input := make([]string, 0, len(req.Inputs))
	for i, item := range req.Inputs {
		if strings.TrimSpace(item.Ref) == "" {
			return nil, fmt.Errorf("embed input %d ref is required", i)
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			return nil, fmt.Errorf("embed input %q text is required", item.Ref)
		}
		input = append(input, text)
	}
	body, err := json.Marshal(openAIEmbeddingRequest{
		Model:          e.model,
		Input:          input,
		Dimensions:     e.dimensions,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build embedding request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil {
			return nil, fmt.Errorf("read embedding error response: %w", err)
		}
		return nil, fmt.Errorf("embedding request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var decoded openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	result, err := e.decodeEmbeddingResponse(req, decoded)
	if err != nil {
		return nil, err
	}
	if err := ValidateEmbedResult(req, result, e.dimensions); err != nil {
		return nil, err
	}
	return result, nil
}

func (e *OpenAICompatibleEmbedder) decodeEmbeddingResponse(req EmbedRequest, decoded openAIEmbeddingResponse) (*EmbedResult, error) {
	if strings.TrimSpace(decoded.Model) == "" {
		return nil, errors.New("embedding response model is required")
	}
	if len(decoded.Data) != len(req.Inputs) {
		return nil, fmt.Errorf("embedding response data count = %d, want %d", len(decoded.Data), len(req.Inputs))
	}
	byIndex := make(map[int]openAIEmbeddingData, len(decoded.Data))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(req.Inputs) {
			return nil, fmt.Errorf("embedding response index %d out of range", item.Index)
		}
		if _, exists := byIndex[item.Index]; exists {
			return nil, fmt.Errorf("embedding response duplicate index %d", item.Index)
		}
		byIndex[item.Index] = item
	}
	vectors := make([]EmbeddingVector, 0, len(req.Inputs))
	for index, input := range req.Inputs {
		item, exists := byIndex[index]
		if !exists {
			return nil, fmt.Errorf("embedding response missing index %d", index)
		}
		values := make([]float32, 0, len(item.Embedding))
		for _, value := range item.Embedding {
			values = append(values, float32(value))
		}
		vectors = append(vectors, EmbeddingVector{Ref: input.Ref, Values: values})
	}
	return &EmbedResult{
		Model:      strings.TrimSpace(decoded.Model),
		Dimensions: e.dimensions,
		Vectors:    vectors,
	}, nil
}

type openAIEmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	Dimensions     int      `json:"dimensions"`
	EncodingFormat string   `json:"encoding_format"`
}

type openAIEmbeddingResponse struct {
	Data  []openAIEmbeddingData `json:"data"`
	Model string                `json:"model"`
}

type openAIEmbeddingData struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}
