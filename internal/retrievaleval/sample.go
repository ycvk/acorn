package retrievaleval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Kind string

const (
	KindMemorySearch  Kind = "memory_search"
	KindMemoryPrepare Kind = "memory_prepare"
	KindSkillRouting  Kind = "skill_routing"
)

type Sample struct {
	ID            string        `json:"id,omitempty"`
	Kind          Kind          `json:"kind"`
	RunID         string        `json:"run_id,omitempty"`
	Query         string        `json:"query"`
	Scope         string        `json:"scope,omitempty"`
	ReturnedRefs  []string      `json:"returned_refs,omitempty"`
	ExplainDigest ExplainDigest `json:"explain_digest,omitempty"`
	LatencyMS     int64         `json:"latency_ms,omitempty"`
	CapturedAt    time.Time     `json:"captured_at"`
}

type ExplainDigest struct {
	Stages []StageDigest `json:"stages,omitempty"`
	Items  []ItemDigest  `json:"items,omitempty"`
}

type StageDigest struct {
	Name           string `json:"name"`
	CandidateCount int    `json:"candidate_count"`
}

type ItemDigest struct {
	Ref               string   `json:"ref"`
	FinalScore        float64  `json:"final_score"`
	ContributionCount int      `json:"contribution_count"`
	Stages            []string `json:"stages,omitempty"`
}

type Sink interface {
	Capture(ctx context.Context, sample Sample) error
}

type FileSink struct {
	path string
	now  func() time.Time
}

func NewFileSink(path string) (*FileSink, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, fmt.Errorf("capture sink path is required")
	}
	return &FileSink{
		path: trimmed,
		now:  time.Now,
	}, nil
}

func (s *FileSink) Capture(ctx context.Context, sample Sample) error {
	if s == nil {
		return fmt.Errorf("capture sink is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := NormalizeSample(sample, s.now())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create capture sink directory: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open capture sink: %w", err)
	}
	defer file.Close()
	body, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("marshal capture sample: %w", err)
	}
	if _, err := file.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write capture sample: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close capture sink: %w", err)
	}
	return nil
}

func NormalizeSample(sample Sample, fallbackTime time.Time) (Sample, error) {
	sample.ID = strings.TrimSpace(sample.ID)
	sample.RunID = strings.TrimSpace(sample.RunID)
	sample.Query = strings.TrimSpace(sample.Query)
	sample.Scope = strings.TrimSpace(sample.Scope)
	sample.ReturnedRefs = uniqueNonEmpty(sample.ReturnedRefs)
	if sample.Kind == "" {
		return Sample{}, fmt.Errorf("capture sample kind is required")
	}
	if sample.Query == "" {
		return Sample{}, fmt.Errorf("capture sample query is required")
	}
	if sample.CapturedAt.IsZero() {
		if fallbackTime.IsZero() {
			fallbackTime = time.Now()
		}
		sample.CapturedAt = fallbackTime.UTC()
	} else {
		sample.CapturedAt = sample.CapturedAt.UTC()
	}
	return sample, nil
}

func uniqueNonEmpty(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
