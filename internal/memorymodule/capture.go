package memorymodule

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

func SearchCaptureSample(req SearchRequest, result *SearchResult, meta CaptureMetadata) retrievaleval.Sample {
	return retrievaleval.Sample{
		ID:            meta.ID,
		Kind:          retrievaleval.KindMemorySearch,
		RunID:         meta.RunID,
		Query:         req.Query,
		Scope:         req.Scope,
		ReturnedRefs:  searchItemRefs(result),
		ExplainDigest: explainDigest(result),
		LatencyMS:     meta.Latency.Milliseconds(),
		CapturedAt:    meta.CapturedAt,
	}
}

func PrepareCaptureSample(req PrepareRequest, result *PrepareResult, meta CaptureMetadata) retrievaleval.Sample {
	return retrievaleval.Sample{
		ID:            meta.ID,
		Kind:          retrievaleval.KindMemoryPrepare,
		RunID:         firstNonEmptyString(meta.RunID, req.RunID),
		Query:         req.UserInput,
		Scope:         WorkspaceScope(req.WorkspaceSlug),
		ReturnedRefs:  prepareResultRefs(result),
		ExplainDigest: prepareExplainDigest(result),
		LatencyMS:     meta.Latency.Milliseconds(),
		CapturedAt:    meta.CapturedAt,
	}
}

func searchItemRefs(result *SearchResult) []string {
	if result == nil {
		return nil
	}
	refs := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		refs = append(refs, item.Ref)
	}
	return refs
}

func prepareResultRefs(result *PrepareResult) []string {
	if result == nil {
		return nil
	}
	refs := make([]string, 0, len(result.Nudges)+len(result.Entries))
	for _, item := range result.Nudges {
		refs = append(refs, item.Ref)
	}
	for _, item := range result.Entries {
		refs = append(refs, item.Ref)
	}
	return refs
}

func prepareExplainDigest(result *PrepareResult) retrievaleval.ExplainDigest {
	if result == nil {
		return retrievaleval.ExplainDigest{}
	}
	return digestSearchExplain(result.Explain)
}

func explainDigest(result *SearchResult) retrievaleval.ExplainDigest {
	if result == nil {
		return retrievaleval.ExplainDigest{}
	}
	return digestSearchExplain(result.Explain)
}

func digestSearchExplain(explain *SearchExplain) retrievaleval.ExplainDigest {
	if explain == nil {
		return retrievaleval.ExplainDigest{}
	}
	digest := retrievaleval.ExplainDigest{
		Stages: make([]retrievaleval.StageDigest, 0, len(explain.Stages)),
		Items:  make([]retrievaleval.ItemDigest, 0, len(explain.Items)),
	}
	for _, stage := range explain.Stages {
		digest.Stages = append(digest.Stages, retrievaleval.StageDigest{
			Name:           stage.Name,
			CandidateCount: stage.CandidateCount,
		})
	}
	for _, item := range explain.Items {
		digest.Items = append(digest.Items, retrievaleval.ItemDigest{
			Ref:               item.Ref,
			FinalScore:        item.FinalScore,
			ContributionCount: len(item.Contributions),
			Stages:            contributionStages(item.Contributions),
		})
	}
	return digest
}

func contributionStages(items []ScoreContribution) []string {
	seen := make(map[string]struct{}, len(items))
	stages := make([]string, 0, len(items))
	for _, item := range items {
		if item.Stage == "" {
			continue
		}
		if _, exists := seen[item.Stage]; exists {
			continue
		}
		seen[item.Stage] = struct{}{}
		stages = append(stages, item.Stage)
	}
	return stages
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
