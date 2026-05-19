package skills

import (
	"time"

	"github.com/ycvk/acorn/internal/retrievaleval"
)

type CaptureMetadata struct {
	ID         string
	RunID      string
	Latency    time.Duration
	CapturedAt time.Time
}

func CandidateCaptureSample(query CandidateQuery, result *CandidateResult, meta CaptureMetadata) retrievaleval.Sample {
	return retrievaleval.Sample{
		ID:           meta.ID,
		Kind:         retrievaleval.KindSkillRouting,
		RunID:        meta.RunID,
		Query:        query.Input,
		ReturnedRefs: candidateResultRefs(result),
		LatencyMS:    meta.Latency.Milliseconds(),
		CapturedAt:   meta.CapturedAt,
	}
}

func candidateResultRefs(result *CandidateResult) []string {
	if result == nil {
		return nil
	}
	refs := make([]string, 0, len(result.Candidates))
	for _, item := range result.Candidates {
		refs = append(refs, item.Skill.ID)
	}
	return refs
}
