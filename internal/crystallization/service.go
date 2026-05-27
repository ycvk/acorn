package crystallization

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/memorymodule"
)

type Service interface {
	Crystallize(ctx context.Context, req CrystallizationRequest) (*CrystallizationResult, error)
	BuildIndexEntry(ctx context.Context, skillID string) (*IndexEntry, error)
	QueryIndex(ctx context.Context, input string, limit int) ([]IndexEntry, error)
}

type IndexStore interface {
	Upsert(ctx context.Context, entry *IndexEntry) error
	Query(ctx context.Context, input string, limit int) ([]IndexEntry, error)
	Delete(ctx context.Context, skillID string) error
}

type DefaultService struct {
	memorySvc  memorymodule.Service
	indexStore IndexStore
	similarity SimilarityChecker
	scorer     QualityScorer
	summarizer Summarizer
}

func NewDefaultService(memorySvc memorymodule.Service, indexStore IndexStore) *DefaultService {
	return &DefaultService{
		memorySvc:  memorySvc,
		indexStore: indexStore,
		similarity: NewSimilarityChecker(0.85),
		scorer:     NewQualityScorer(indexStore),
		summarizer: NewSummarizer(),
	}
}

func (s *DefaultService) Crystallize(ctx context.Context, req CrystallizationRequest) (*CrystallizationResult, error) {
	if s == nil || s.memorySvc == nil || s.indexStore == nil {
		return nil, fmt.Errorf("crystallization service is not initialized")
	}
	if len(req.ToolNames) == 0 || onlyReadTools(req.ToolNames) {
		return &CrystallizationResult{Verdict: VerdictInsufficientValue, Reason: "no meaningful tool sequence"}, nil
	}
	if len(req.EvidenceRefs) == 0 {
		return &CrystallizationResult{Verdict: VerdictInsufficientValue, Reason: "no verified tool evidence"}, nil
	}

	candidate := s.generateCandidate(req)

	existing, err := s.indexStore.Query(ctx, candidate.TaskPattern, 100)
	if err != nil {
		return nil, fmt.Errorf("query insight index for similarity: %w", err)
	}
	similarID, score := s.similarity.FindMostSimilar(candidate.TaskPattern, existing)
	if score > s.similarity.Threshold() {
		return &CrystallizationResult{
			Verdict:   VerdictTooSimilar,
			SimilarTo: similarID,
			Reason:    fmt.Sprintf("similarity %.2f > threshold %.2f", score, s.similarity.Threshold()),
		}, nil
	}

	record, err := s.memorySvc.CreateProcedure(ctx, memorymodule.CreateProcedureRequest{
		Title:        candidate.Title,
		Body:         candidate.Body,
		TaskPattern:  candidate.TaskPattern,
		SourceRun:    req.RunID,
		SourceRefs:   []string{req.RunID},
		EvidenceRefs: append([]string(nil), req.EvidenceRefs...),
	})
	if err != nil {
		return nil, fmt.Errorf("create procedure: %w", err)
	}

	if _, err := s.BuildIndexEntry(ctx, record.Ref); err != nil {
		return nil, fmt.Errorf("build insight index entry: %w", err)
	}

	return &CrystallizationResult{
		Verdict: VerdictCrystallized,
		SkillID: record.Ref,
		Reason:  candidate.Reason,
	}, nil
}

func (s *DefaultService) BuildIndexEntry(ctx context.Context, skillID string) (*IndexEntry, error) {
	if s == nil || s.memorySvc == nil || s.indexStore == nil {
		return nil, fmt.Errorf("service not initialized")
	}
	if s.scorer == nil {
		return nil, fmt.Errorf("quality scorer is not initialized")
	}

	skills, err := s.memorySvc.ListSkills(ctx, memorymodule.RecordSelection{})
	if err != nil {
		return nil, err
	}

	var target *memorymodule.Record
	for i := range skills {
		if skills[i].Ref == skillID {
			target = &skills[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("skill %s not found", skillID)
	}

	summary := s.summarizer.Summarize(target.Title, target.Body, target.TaskPattern, nil)
	keywords := extractKeywords(target.TaskPattern, target.Title, target.Body)
	quality, err := s.scorer.Score(ctx, skillID)
	if err != nil {
		return nil, err
	}

	createdAt, err := parseIndexTime(target.Created)
	if err != nil {
		return nil, fmt.Errorf("parse skill created timestamp for %s: %w", skillID, err)
	}
	entry := &IndexEntry{
		SkillID:      skillID,
		SkillName:    target.Title,
		Summary:      summary,
		Keywords:     keywords,
		TaskPattern:  target.TaskPattern,
		QualityScore: quality,
		Source:       target.Origin,
		CreatedAt:    createdAt,
		UpdatedAt:    time.Now().UTC(),
	}

	if err := s.indexStore.Upsert(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *DefaultService) QueryIndex(ctx context.Context, input string, limit int) ([]IndexEntry, error) {
	if s == nil || s.indexStore == nil {
		return nil, fmt.Errorf("service not initialized")
	}
	return s.indexStore.Query(ctx, input, limit)
}

func (s *DefaultService) generateCandidate(req CrystallizationRequest) *SkillCandidate {
	title := fmt.Sprintf("Auto: %s", truncate(req.Input, 40))
	body := fmt.Sprintf("Tool sequence: %s\nInput: %s\nOutput: %s",
		strings.Join(req.ToolNames, " -> "), req.Input, req.Output)
	taskPattern := fmt.Sprintf("When asked to %s", truncate(req.Input, 60))
	return &SkillCandidate{
		Title:       title,
		Body:        body,
		TaskPattern: taskPattern,
		Reason:      fmt.Sprintf("crystallized from run %s with %d tools", req.RunID, len(req.ToolNames)),
	}
}

func onlyReadTools(toolNames []string) bool {
	for _, name := range toolNames {
		lower := strings.ToLower(name)
		if !strings.Contains(lower, "read") && !strings.Contains(lower, "list") && !strings.Contains(lower, "get") && !strings.Contains(lower, "search") {
			return false
		}
	}
	return len(toolNames) > 0
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func extractKeywords(taskPattern, title, body string) []string {
	text := strings.ToLower(taskPattern + " " + title + " " + body)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' || r == ';' || r == ':' || r == '-' || r == '_'
	})
	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || len(w) < 3 || seen[w] || isStopWord(w) {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}
	if len(keywords) > 20 {
		keywords = keywords[:20]
	}
	return keywords
}

func isStopWord(w string) bool {
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "that": true,
		"this": true, "from": true, "you": true, "can": true, "use": true,
		"will": true, "have": true, "are": true, "was": true, "not": true,
	}
	return stopWords[w]
}

func parseIndexTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("timestamp is required")
	}
	t, err := time.Parse("2006-01-02", s)
	if err == nil && !t.IsZero() {
		return t, nil
	}
	t, err = time.Parse(time.RFC3339, s)
	if err == nil && !t.IsZero() {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", s)
}
