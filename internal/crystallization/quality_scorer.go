package crystallization

import (
	"context"
	"fmt"
	"time"
)

type QualityScorer interface {
	Score(ctx context.Context, skillID string) (int, error)
}

type DefaultQualityScorer struct {
	indexStore IndexStore
}

func NewQualityScorer(store IndexStore) *DefaultQualityScorer {
	return &DefaultQualityScorer{indexStore: store}
}

func (s *DefaultQualityScorer) Score(ctx context.Context, skillID string) (int, error) {
	if s == nil || s.indexStore == nil {
		return 0, fmt.Errorf("quality scorer is not initialized")
	}

	entries, err := s.indexStore.Query(ctx, skillID, 1)
	if err != nil {
		return 0, fmt.Errorf("query insight index for quality score: %w", err)
	}
	if len(entries) == 0 {
		return baseScoreForNewSkill(), nil
	}

	entry := entries[0]
	score := entry.QualityScore
	if score == 0 {
		score = baseScoreForNewSkill()
	}

	age := time.Since(entry.CreatedAt)
	if age > 30*24*time.Hour {
		score += 10
	}
	if age > 90*24*time.Hour {
		score += 10
	}

	if len(entry.Keywords) > 5 {
		score += 5
	}

	if score > 100 {
		score = 100
	}
	return score, nil
}

func baseScoreForNewSkill() int {
	return 50
}

func (s *DefaultQualityScorer) String() string {
	return "DefaultQualityScorer"
}
