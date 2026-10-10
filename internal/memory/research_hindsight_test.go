package memory_test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

type researchHindsight struct {
	base, key, bank string
	client          *http.Client
	config          map[string]any
	sources         map[string]bool
}

func newResearchHindsight(t *testing.T, version, split string) *researchHindsight {
	t.Helper()
	base := os.Getenv("ACORN_MEMORY_HINDSIGHT_URL")
	key, err := os.ReadFile(os.Getenv("ACORN_MEMORY_HINDSIGHT_KEY_FILE"))
	if err != nil || base == "" {
		t.Fatal("Hindsight URL and key file are required")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	h := &researchHindsight{base: strings.TrimRight(base, "/"), key: strings.TrimSpace(string(key)), bank: "acorn-eval-20261010-" + split + "-" + hex.EncodeToString(nonce[:]), client: &http.Client{Timeout: 3 * time.Minute}, sources: map[string]bool{}}
	if err := h.call("PUT", "", map[string]any{"name": version + " " + split, "retain_mission": "Remember the supplied synthetic conversation with original attribution, temporal changes, conditional preferences and source documents. Owner statements are evidence about the owner. Assistant proposals and external claims are not owner agreement. Distinguish proposed, authorized, tested and completed actions. Do not invent facts.", "enable_observations": true, "enable_reranking": false}, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("created isolated bank %s", h.bank)
	// Register cleanup immediately after creating the uniquely owned bank.
	t.Cleanup(func() {
		if err := h.call("DELETE", "", nil, nil); err != nil {
			t.Errorf("remove isolated bank %s: %v", h.bank, err)
		}
	})
	var cfg struct {
		Config map[string]any `json:"config"`
	}
	if err := h.call("GET", "/config", nil, &cfg); err != nil {
		t.Fatal(err)
	}
	h.config = map[string]any{}
	for _, name := range []string{"llm_provider", "llm_model", "llm_reasoning_effort", "embeddings_provider", "embeddings_litellm_sdk_model", "embeddings_dimensions", "retain_extraction_mode", "consolidation_llm_batch_size", "enable_observations", "enable_reranking", "enable_graph_retrieval", "enable_temporal_retrieval", "enable_text_search"} {
		h.config[name] = cfg.Config[name]
	}
	return h
}

func (h *researchHindsight) call(method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, h.base+"/v1/default/banks/"+url.PathEscape(h.bank)+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Hindsight %s %s returned %d: %.1500s", method, path, resp.StatusCode, data)
	}
	if output != nil {
		return json.Unmarshal(data, output)
	}
	return nil
}

func (h *researchHindsight) settled() error {
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		var out struct {
			Operations []struct {
				Status string `json:"status"`
				Error  any    `json:"error"`
			} `json:"operations"`
		}
		if err := h.call("GET", "/operations?limit=100", nil, &out); err != nil {
			return err
		}
		pending := false
		for _, op := range out.Operations {
			if op.Status == "failed" || op.Status == "cancelled" {
				return fmt.Errorf("Hindsight operation %s: %v", op.Status, op.Error)
			}
			if op.Status != "completed" {
				pending = true
			}
		}
		if !pending {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("Hindsight operations did not settle within 15 minutes")
}

func (h *researchHindsight) ingest(s researchSource) error {
	var out map[string]any
	err := h.call("POST", "/memories", map[string]any{"async": true, "items": []any{map[string]any{"content": s.Text, "timestamp": s.At, "document_id": s.ID, "context": "speaker=" + s.Speaker + "; conversation=" + s.Thread, "metadata": map[string]string{"source_id": s.ID, "speaker": s.Speaker, "thread": s.Thread}}}}, &out)
	if err != nil {
		return err
	}
	h.sources[s.ID] = true
	return h.settled()
}

type hindsightResearchFact struct {
	ID, Text, Type, Context string
	DocumentID              string            `json:"document_id"`
	SourceFacts             []string          `json:"source_fact_ids"`
	Metadata                map[string]string `json:"metadata"`
}

func (h *researchHindsight) recall(q researchQuery) ([]researchEvidence, error) {
	input := map[string]any{"query": q.Query, "budget": "mid", "max_tokens": 8192, "include": map[string]any{"entities": nil, "source_facts": map[string]int{"max_tokens": 8192}}}
	if q.KnownAt != "" {
		input["query_timestamp"] = q.KnownAt
	} else {
		input["query_timestamp"] = "2026-10-01T12:00:00Z"
	}
	if q.AsOf != "" {
		input["temporal_window"] = map[string]string{"start": q.AsOf, "end": q.AsOf}
	}
	var out struct {
		Results []hindsightResearchFact          `json:"results"`
		Facts   map[string]hindsightResearchFact `json:"source_facts"`
	}
	if err := h.call("POST", "/memories/recall", input, &out); err != nil {
		return nil, err
	}
	var result []researchEvidence
	for _, fact := range out.Results[:min(len(out.Results), 5)] {
		ids := []string{}
		add := func(f hindsightResearchFact) {
			if h.sources[f.DocumentID] {
				ids = append(ids, f.DocumentID)
			}
			if h.sources[f.Metadata["source_id"]] {
				ids = append(ids, f.Metadata["source_id"])
			}
		}
		add(fact)
		for _, id := range fact.SourceFacts {
			add(out.Facts[id])
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		result = append(result, researchEvidence{ID: fact.ID, Kind: fact.Type, Content: fact.Text, Scope: fact.Context, Sources: ids})
	}
	return result, nil
}

func (h *researchHindsight) forget(ids []string) error {
	for _, id := range ids {
		if !h.sources[id] {
			return fmt.Errorf("cannot delete unowned document %s", id)
		}
		if err := h.call("DELETE", "/documents/"+url.PathEscape(id), nil, nil); err != nil {
			return err
		}
	}
	return h.settled()
}

func (h *researchHindsight) diagnostics() (any, error) {
	var stats map[string]any
	if err := h.call("GET", "/stats", nil, &stats); err != nil {
		return nil, err
	}
	var traces []map[string]any
	for offset := 0; ; offset += 100 {
		var out map[string]json.RawMessage
		if err := h.call("GET", fmt.Sprintf("/llm-requests?limit=100&offset=%d", offset), nil, &out); err != nil {
			return nil, err
		}
		var rows []map[string]any
		for _, key := range []string{"items", "requests"} {
			if raw, ok := out[key]; ok {
				if err := json.Unmarshal(raw, &rows); err != nil {
					return nil, err
				}
				break
			}
		}
		for _, row := range rows {
			trace := map[string]any{}
			for _, key := range []string{"model", "provider", "operation", "status", "duration_ms", "input_tokens", "output_tokens", "total_tokens", "error", "reasoning_effort"} {
				if v, ok := row[key]; ok {
					trace[key] = v
				}
			}
			traces = append(traces, trace)
		}
		if len(rows) < 100 {
			break
		}
	}
	return map[string]any{"bank": h.bank, "config": h.config, "stats": stats, "llm_requests": traces}, nil
}
