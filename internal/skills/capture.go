package skills

import (
	"time"

	"github.com/ycvk/acorn/internal/memorymodule"
)

type CaptureMetadata struct {
	ID         string
	RunID      string
	Latency    time.Duration
	CapturedAt time.Time
}

func CandidateCaptureSample(query CandidateQuery, result *CandidateResult, meta CaptureMetadata) memorymodule.EvalSample {
	return memorymodule.EvalSample{
		ID:           meta.ID,
		Kind:         memorymodule.EvalKindSkillRouting,
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
