package memorymodule

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type insight struct {
	ID          string
	Scope       string
	Status      Status
	Title       string
	Tags        []string
	AppliesWhen []string
	SourceRefs  []string
	Body        string
	RelPath     string
	Updated     string
}

type insightHit struct {
	Insight insight
	Score   float64
	Sources []Record
}

type insightSearchRequest struct {
	Query          string
	Scope          string
	Limit          int
	IncludeRetired bool
}

type insightSearchResult struct {
	Hits []insightHit
}

type insightFrontmatter struct {
	ID          string   `yaml:"id"`
	Kind        string   `yaml:"kind"`
	Scope       string   `yaml:"scope"`
	Status      string   `yaml:"status"`
	Title       string   `yaml:"title"`
	Tags        []string `yaml:"tags"`
	AppliesWhen []string `yaml:"applies_when"`
	SourceRefs  []string `yaml:"source_refs"`
	Updated     string   `yaml:"updated"`
}

func (s *LocalService) searchInsights(ctx context.Context, req insightSearchRequest) (*insightSearchResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return &insightSearchResult{}, nil
	}
	insights, err := s.listInsights(ctx)
	if err != nil {
		return nil, err
	}
	terms := queryTerms(query)
	hits := make([]insightHit, 0, len(insights))
	for _, item := range insights {
		if item.Status == StatusRetired && !req.IncludeRetired {
			continue
		}
		if !scopeMatches(req.Scope, item.Scope) {
			continue
		}
		score := scoreInsight(item, terms)
		if score <= 0 {
			continue
		}
		hits = append(hits, insightHit{
			Insight: item,
			Score:   score,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return hits[i].Insight.ID < hits[j].Insight.ID
	})
	limit, err := resolveLimit("insight search", req.Limit, defaultSearchLimit, maxSearchLimit)
	if err != nil {
		return nil, err
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	if err := s.resolveInsightSources(ctx, hits); err != nil {
		return nil, err
	}
	return &insightSearchResult{Hits: hits}, nil
}

func (s *LocalService) listInsights(ctx context.Context) ([]insight, error) {
	root := s.path(".index", "insights")
	items := make([]insight, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		item, err := readInsight(s.root, path)
		if err != nil {
			return err
		}
		items = append(items, *item)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan insight files: %w", err)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].RelPath < items[j].RelPath
	})
	return items, nil
}

func readInsight(root string, path string) (*insight, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read insight file %q: %w", path, err)
	}
	frontmatter, body, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("resolve insight relative path: %w", err)
	}
	item := &insight{
		RelPath: filepath.ToSlash(rel),
		Body:    strings.TrimSpace(body),
	}
	if err := applyInsightFrontmatter(item, frontmatter); err != nil {
		return nil, fmt.Errorf("parse insight frontmatter: %w", err)
	}
	return item, nil
}

func applyInsightFrontmatter(item *insight, frontmatter string) error {
	var meta insightFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return err
	}
	if strings.TrimSpace(meta.Kind) != "insight" {
		return fmt.Errorf("insight kind must be insight")
	}
	item.ID = strings.TrimSpace(meta.ID)
	item.Scope = strings.TrimSpace(meta.Scope)
	item.Status = Status(strings.TrimSpace(meta.Status))
	item.Title = strings.TrimSpace(meta.Title)
	item.Tags = normalizeList(meta.Tags)
	item.AppliesWhen = normalizeList(meta.AppliesWhen)
	item.SourceRefs = normalizeRefs(meta.SourceRefs)
	item.Updated = strings.TrimSpace(meta.Updated)
	if item.ID == "" {
		return fmt.Errorf("insight id is required")
	}
	if item.Title == "" {
		return fmt.Errorf("insight title is required")
	}
	if item.Status != StatusUnverified && item.Status != StatusVerified && item.Status != StatusRetired {
		return fmt.Errorf("status must be unverified, verified, or retired")
	}
	if item.Scope != "" {
		if err := validateScope(item.Scope); err != nil {
			return err
		}
	}
	if item.Status == StatusVerified && len(item.SourceRefs) == 0 {
		return fmt.Errorf("verified insight source_refs are required")
	}
	if len(item.Tags) == 0 && len(item.AppliesWhen) == 0 {
		return fmt.Errorf("insight tags or applies_when are required")
	}
	return nil
}

func normalizeRefs(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func scoreInsight(item insight, terms []string) float64 {
	var score float64
	id := strings.ToLower(item.ID)
	title := strings.ToLower(item.Title)
	body := strings.ToLower(item.Body)
	tags := strings.ToLower(strings.Join(item.Tags, " "))
	appliesWhen := strings.ToLower(strings.Join(item.AppliesWhen, " "))
	for _, term := range terms {
		switch {
		case term == "":
			continue
		case strings.Contains(tags, term):
			score += 5
		case strings.Contains(appliesWhen, term):
			score += 4
		case strings.Contains(id, term):
			score += 3
		}
		if strings.Contains(title, term) {
			score += 3
		}
		if strings.Contains(body, term) {
			score += 1
		}
	}
	if item.Status == StatusVerified {
		score += 0.5
	}
	return score
}

func (s *LocalService) resolveInsightSources(ctx context.Context, hits []insightHit) error {
	if len(hits) == 0 {
		return nil
	}
	records, err := s.allRecords(ctx)
	if err != nil {
		return err
	}
	byRef := make(map[string]Record, len(records))
	for _, record := range records {
		byRef[record.Ref] = record
	}
	for i := range hits {
		sources := make([]Record, 0, len(hits[i].Insight.SourceRefs))
		for _, ref := range hits[i].Insight.SourceRefs {
			record, ok := byRef[ref]
			if !ok {
				return fmt.Errorf("resolve insight %q source ref %q: memory record not found", hits[i].Insight.ID, ref)
			}
			sources = append(sources, record)
		}
		hits[i].Sources = sources
	}
	return nil
}

func searchItemsFromInsightHitsWithContributions(hits []insightHit, scope string) ([]SearchItem, map[string][]ScoreContribution) {
	items := make([]SearchItem, 0)
	contributions := make(map[string][]ScoreContribution)
	for _, hit := range hits {
		for _, source := range hit.Sources {
			if source.Status == StatusRetired {
				continue
			}
			if !scopeMatches(scope, source.Scope) {
				continue
			}
			items = append(items, SearchItemFromRecord(source, hit.Score+sourceStatusScore(source)))
			appendContribution(contributions, source.Ref, ScoreContribution{
				Stage:      searchStageInsightSource,
				Delta:      hit.Score,
				Reason:     fmt.Sprintf("insight %s resolved source", hit.Insight.ID),
				SourceRefs: []string{source.Ref},
			})
			if statusScore := sourceStatusScore(source); statusScore > 0 {
				appendContribution(contributions, source.Ref, ScoreContribution{
					Stage:      searchStageInsightSource,
					Delta:      statusScore,
					Reason:     "resolved source status/kind boost",
					SourceRefs: []string{source.Ref},
				})
			}
		}
	}
	return items, contributions
}

func mergeSearchItemsWithContributions(seed map[string][]ScoreContribution, groups ...[]SearchItem) ([]SearchItem, map[string][]ScoreContribution) {
	byRef := make(map[string]SearchItem)
	contributions := make(map[string][]ScoreContribution)
	mergeContributionMaps(contributions, seed)
	for _, group := range groups {
		for _, item := range group {
			existing, exists := byRef[item.Ref]
			if !exists {
				byRef[item.Ref] = item
				continue
			}
			existing.Score += item.Score
			if existing.Snippet == "" {
				existing.Snippet = item.Snippet
			}
			byRef[item.Ref] = existing
		}
	}
	result := make([]SearchItem, 0, len(byRef))
	for _, item := range byRef {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return result[i].Ref < result[j].Ref
	})
	return result, contributions
}

func sourceStatusScore(record Record) float64 {
	var score float64
	if record.Status == StatusVerified {
		score += 0.5
	}
	if record.Kind == KindSkill {
		score += 0.25
	}
	return score
}

func scopeMatches(requestScope string, itemScope string) bool {
	scope := strings.TrimSpace(requestScope)
	if scope == "" {
		return true
	}
	item := strings.TrimSpace(itemScope)
	return item == "" || item == scope
}
