package memorymodule

import "time"

type CaptureMetadata struct {
	ID         string
	RunID      string
	Latency    time.Duration
	CapturedAt time.Time
}

func SearchCaptureSample(req SearchRequest, result *SearchResult, meta CaptureMetadata) EvalSample {
	return EvalSample{
		ID:            meta.ID,
		Kind:          EvalKindMemorySearch,
		RunID:         meta.RunID,
		Query:         req.Query,
		Scope:         req.Scope,
		ReturnedRefs:  searchItemRefs(result),
		ExplainDigest: explainDigest(result),
		LatencyMS:     meta.Latency.Milliseconds(),
		CapturedAt:    meta.CapturedAt,
	}
}

func PrepareCaptureSample(req PrepareRequest, result *PrepareResult, meta CaptureMetadata) EvalSample {
	return EvalSample{
		ID:            meta.ID,
		Kind:          EvalKindMemoryPrepare,
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

func prepareExplainDigest(result *PrepareResult) EvalExplainDigest {
	if result == nil {
		return EvalExplainDigest{}
	}
	return digestSearchExplain(result.Explain)
}

func explainDigest(result *SearchResult) EvalExplainDigest {
	if result == nil {
		return EvalExplainDigest{}
	}
	return digestSearchExplain(result.Explain)
}

func digestSearchExplain(explain *SearchExplain) EvalExplainDigest {
	if explain == nil {
		return EvalExplainDigest{}
	}
	digest := EvalExplainDigest{
		Stages: make([]EvalStageDigest, 0, len(explain.Stages)),
		Items:  make([]EvalItemDigest, 0, len(explain.Items)),
	}
	for _, stage := range explain.Stages {
		digest.Stages = append(digest.Stages, EvalStageDigest(stage))
	}
	for _, item := range explain.Items {
		digest.Items = append(digest.Items, EvalItemDigest(item))
	}
	return digest
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
