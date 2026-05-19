package memorymodule

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
	query := strings.TrimSpace(req.UserInput)
	if query == "" {
		return &PrepareResult{}, nil
	}
	maxNudges, err := resolveLimit("prepare nudges", req.MaxNudges, defaultMaxNudges, maxPrepareNudges)
	if err != nil {
		return nil, err
	}
	maxEntries, err := resolveLimit("prepare entries", req.MaxEntries, defaultMaxEntries, maxPrepareEntries)
	if err != nil {
		return nil, err
	}

	search, err := s.Search(ctx, SearchRequest{
		Query:   query,
		Scope:   WorkspaceScope(req.WorkspaceSlug),
		Limit:   maxNudges + maxEntries + 4,
		Explain: req.Explain,
	})
	if err != nil {
		return nil, err
	}
	insights, err := s.searchInsights(ctx, insightSearchRequest{
		Query: query,
		Scope: WorkspaceScope(req.WorkspaceSlug),
		Limit: maxNudges + maxEntries + 4,
	})
	if err != nil {
		return nil, err
	}
	scope := WorkspaceScope(req.WorkspaceSlug)
	insightItems, insightContributions := searchItemsFromInsightHitsWithContributions(insights.Hits, scope)
	items, contributions := mergeSearchItemsWithContributions(
		contributionMapFromExplain(search.Explain),
		search.Items,
		insightItems,
	)
	mergeContributionMaps(contributions, insightContributions)

	result := &PrepareResult{
		Nudges:    make([]Nudge, 0, maxNudges),
		Entries:   make([]Entry, 0, maxEntries),
		SkillTree: s.GetSkillTree(),
	}
	if req.Explain {
		stages := make([]SearchStageExplain, 0, len(search.Explain.Stages)+1)
		if search.Explain != nil {
			stages = append(stages, search.Explain.Stages...)
		}
		stages = append(stages, SearchStageExplain{
			Name:           searchStageInsightSource,
			CandidateCount: len(insightItems),
		})
		result.Explain = buildSearchExplain(query, scope, items, stages, contributions)
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
			if item.Kind == string(KindSkill) {
				record, err := s.GetRecordByRef(ctx, item.Ref)
				if err != nil {
					return nil, fmt.Errorf("load skill record %q for activation: %w", item.Ref, err)
				}
				result.ProcedureActivations = append(result.ProcedureActivations, ProcedureActivationFromRecord(
					req.RunID,
					req.SessionID,
					*record,
					ProcedureActivationMatched,
					"matched_prepare_query",
					item.Score,
				))
			}
		}
		if len(result.Entries) < maxEntries && item.Score >= 3 {
			if item.Kind != string(KindSkill) {
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
		return procedureInjectable(record)
	}
	return true
}
