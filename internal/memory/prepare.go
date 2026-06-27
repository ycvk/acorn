package memory

import (
	"context"
	"fmt"
	"strings"
)

func (s *LocalService) Prepare(ctx context.Context, req PrepareRequest) (*PrepareResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}

	// Active Memory is always loaded, regardless of query. It is the frozen
	// snapshot of persistent user facts injected into every run.
	activeFacts, err := s.loadActiveFacts(ctx, req.ActiveCharLimit)
	if err != nil {
		return nil, err
	}

	query := strings.TrimSpace(req.UserInput)
	if query == "" {
		return &PrepareResult{
			SkillTree:   s.GetSkillTree(),
			ActiveFacts: activeFacts,
		}, nil
	}
	maxNudges, err := resolveLimit("prepare nudges", req.MaxNudges, defaultMaxNudges, maxPrepareNudges)
	if err != nil {
		return nil, err
	}
	maxEntries, err := resolveLimit("prepare entries", req.MaxEntries, defaultMaxEntries, maxPrepareEntries)
	if err != nil {
		return nil, err
	}

	// Search uses keyword matching; when the caller asked for Explain
	// (eval / replay / debug; the run hot path never does), the search explain
	// is forwarded.
	search, err := s.Search(ctx, SearchRequest{
		Query:   query,
		Scope:   WorkspaceScope(req.WorkspaceSlug),
		Limit:   maxNudges + maxEntries + 4,
		Explain: req.Explain,
	})
	if err != nil {
		return nil, err
	}
	items := search.Items

	// Build a ref set of active facts so search-matched Entries skip facts
	// already in the Active Memory snapshot — no point showing the same fact
	// twice.
	activeRefs := make(map[string]struct{}, len(activeFacts))
	for _, f := range activeFacts {
		activeRefs[f.Ref] = struct{}{}
	}

	result := &PrepareResult{
		Nudges:      make([]Nudge, 0, maxNudges),
		Entries:     make([]Entry, 0, maxEntries),
		SkillTree:   s.GetSkillTree(),
		ActiveFacts: activeFacts,
	}
	if req.Explain {
		var stages []SearchStageExplain
		if search.Explain != nil {
			stages = append(stages, search.Explain.Stages...)
		}
		result.Explain = buildSearchExplain(query, WorkspaceScope(req.WorkspaceSlug), items, stages)
	}

	for _, item := range items {
		if len(result.Nudges) < maxNudges {
			result.Nudges = append(result.Nudges, Nudge{
				Ref:    item.Ref,
				Kind:   item.Kind,
				Title:  item.Title,
				Status: item.Status,
				Reason: fmt.Sprintf("matched %q", strings.TrimSpace(req.UserInput)),
			})
		}
		if len(result.Entries) < maxEntries && item.Score >= 3 {
			if item.Kind != string(KindSkill) {
				// Skip facts already in Active Memory — they're already
				// visible to the agent, injecting twice wastes tokens.
				if _, dup := activeRefs[item.Ref]; dup {
					continue
				}
				record, err := s.GetRecordByRef(ctx, item.Ref)
				if err != nil {
					return nil, fmt.Errorf("load memory record %q for entry: %w", item.Ref, err)
				}
				if prepareRecordInjectable(*record) {
					result.Entries = append(result.Entries, Entry{
						Ref:     record.Ref,
						Kind:    string(record.Kind),
						Title:   record.Title,
						Content: record.Body,
					})
				}
			}
		}
	}
	return result, nil
}

func prepareRecordInjectable(record Record) bool {
	if record.Status != StatusVerified {
		return false
	}
	if record.Kind == KindSkill {
		return false
	}
	return true
}
