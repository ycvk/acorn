package memorymodule

import "strings"

const (
	searchStageInsightSource      = "insight_source"
	searchStageSourceRefBacklink  = "source_ref_backlink"
	searchStageSemanticVector     = "semantic_vector"
	searchStageSemanticFTS        = "semantic_fts"
	searchStageSemanticHybrid     = "semantic_hybrid"
	searchStageRelationSupports   = "relation_supports"
	searchStageRelationDerived    = "relation_derived_from"
	searchStageRelationSupersede  = "relation_supersedes"
	searchStageRelationContradict = "relation_contradicts"
)

func buildSearchExplain(query string, scope string, items []SearchItem, stages []SearchStageExplain, contributions map[string][]ScoreContribution) *SearchExplain {
	explainItems := make([]SearchItemExplain, 0, len(items))
	for _, item := range items {
		itemContributions := append([]ScoreContribution(nil), contributions[item.Ref]...)
		explainItems = append(explainItems, SearchItemExplain{
			Ref:           item.Ref,
			FinalScore:    item.Score,
			Contributions: itemContributions,
		})
	}
	return &SearchExplain{
		Query:  strings.TrimSpace(query),
		Scope:  strings.TrimSpace(scope),
		Stages: append([]SearchStageExplain(nil), stages...),
		Items:  explainItems,
	}
}

func contributionMapFromExplain(explain *SearchExplain) map[string][]ScoreContribution {
	result := make(map[string][]ScoreContribution)
	if explain == nil {
		return result
	}
	for _, item := range explain.Items {
		if item.Ref == "" {
			continue
		}
		result[item.Ref] = append(result[item.Ref], item.Contributions...)
	}
	return result
}

func appendContribution(target map[string][]ScoreContribution, ref string, contribution ScoreContribution) {
	if target == nil || strings.TrimSpace(ref) == "" || contribution.Delta == 0 {
		return
	}
	contribution.Stage = strings.TrimSpace(contribution.Stage)
	contribution.Reason = strings.TrimSpace(contribution.Reason)
	contribution.SourceRefs = normalizeRefs(contribution.SourceRefs)
	target[ref] = append(target[ref], contribution)
}

func appendContributions(target map[string][]ScoreContribution, ref string, contributions []ScoreContribution) {
	for _, contribution := range contributions {
		appendContribution(target, ref, contribution)
	}
}

func mergeContributionMaps(target map[string][]ScoreContribution, source map[string][]ScoreContribution) {
	if target == nil {
		return
	}
	for ref, contributions := range source {
		appendContributions(target, ref, contributions)
	}
}
