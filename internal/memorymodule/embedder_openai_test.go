package memorymodule

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatibleEmbedderSendsConfiguredRequest(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotBody openAIEmbeddingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "text-embedding-3-small",
			"data": [
				{"index": 0, "embedding": [0.1, 0.2, 0.3]},
				{"index": 1, "embedding": [0.4, 0.5, 0.6]}
			]
		}`))
	}))
	defer server.Close()

	embedder, err := NewOpenAICompatibleEmbedder(OpenAICompatibleEmbedderConfig{
		BaseURL:        server.URL + "/v1/",
		APIKey:         "sk-test",
		Model:          "text-embedding-3-small",
		Dimensions:     3,
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleEmbedder: %v", err)
	}
	result, err := embedder.Embed(t.Context(), EmbedRequest{Inputs: []EmbedInput{
		{Ref: "facts/a.md#a", Text: "alpha"},
		{Ref: "facts/b.md#b", Text: "beta"},
	}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody.Model != "text-embedding-3-small" || gotBody.Dimensions != 3 || gotBody.EncodingFormat != "float" {
		t.Fatalf("request body = %#v", gotBody)
	}
	if len(gotBody.Input) != 2 || gotBody.Input[0] != "alpha" || gotBody.Input[1] != "beta" {
		t.Fatalf("input = %#v", gotBody.Input)
	}
	if len(result.Vectors) != 2 {
		t.Fatalf("vectors = %d", len(result.Vectors))
	}
	if got, want := result.Vectors[1].Ref, "facts/b.md#b"; got != want {
		t.Fatalf("vector ref = %q, want %q", got, want)
	}
	if got, want := result.Vectors[1].Values[2], float32(0.6); got != want {
		t.Fatalf("vector value = %v, want %v", got, want)
	}
}

func TestOpenAICompatibleEmbedderRejectsMalformedResponse(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantErr  string
	}{
		{
			name:     "missing model",
			response: `{"data":[{"index":0,"embedding":[1,2,3]}]}`,
			wantErr:  "embedding response model is required",
		},
		{
			name:     "missing index",
			response: `{"model":"text-embedding-3-small","data":[{"index":1,"embedding":[1,2,3]}]}`,
			wantErr:  "embedding response index 1 out of range",
		},
		{
			name:     "wrong dimensions",
			response: `{"model":"text-embedding-3-small","data":[{"index":0,"embedding":[1,2]}]}`,
			wantErr:  `embed result vector "facts/a.md#a" dimensions = 2, want 3`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			embedder, err := NewOpenAICompatibleEmbedder(OpenAICompatibleEmbedderConfig{
				BaseURL:        server.URL,
				APIKey:         "sk-test",
				Model:          "text-embedding-3-small",
				Dimensions:     3,
				TimeoutSeconds: 5,
			})
			if err != nil {
				t.Fatalf("NewOpenAICompatibleEmbedder: %v", err)
			}
			_, err = embedder.Embed(t.Context(), EmbedRequest{Inputs: []EmbedInput{
				{Ref: "facts/a.md#a", Text: "alpha"},
			}})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Embed error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestOpenAICompatibleEmbedderRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()
	embedder, err := NewOpenAICompatibleEmbedder(OpenAICompatibleEmbedderConfig{
		BaseURL:        server.URL,
		APIKey:         "sk-test",
		Model:          "text-embedding-3-small",
		Dimensions:     3,
		TimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatibleEmbedder: %v", err)
	}
	_, err = embedder.Embed(t.Context(), EmbedRequest{Inputs: []EmbedInput{
		{Ref: "facts/a.md#a", Text: "alpha"},
	}})
	if err == nil || !strings.Contains(err.Error(), "embedding request failed: status 401") {
		t.Fatalf("Embed error = %v", err)
	}
}
