package skills

import (
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestCandidateCaptureSampleIncludesCandidateRefs(t *testing.T) {
	items := []Spec{
		{
			ID:           "skill.inspect.repo",
			Name:         "Inspect Repo",
			Source:       WorkspaceScope,
			Summary:      "Inspect repository structure.",
			TriggerHints: []string{"inspect repo"},
		},
	}
	query := CandidateQuery{Input: "inspect repo", Eligibility: testEligibleCtx()}
	result, err := RetrieveCandidates(query, items)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	sample := CandidateCaptureSample(query, result, CaptureMetadata{
		ID:      "skill-sample",
		RunID:   "run_skill",
		Latency: 5 * time.Millisecond,
	})
	if sample.Kind != memorymodule.EvalKindSkillRouting || sample.Query != "inspect repo" {
		t.Fatalf("sample = %#v", sample)
	}
	if sample.RunID != "run_skill" || sample.LatencyMS != 5 {
		t.Fatalf("run/latency = %q/%d", sample.RunID, sample.LatencyMS)
	}
	if len(sample.ReturnedRefs) != 1 || sample.ReturnedRefs[0] != "skill.inspect.repo" {
		t.Fatalf("returned refs = %#v", sample.ReturnedRefs)
	}
}
