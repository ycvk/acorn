package crystallization

import (
	"fmt"
	"strings"
)

type SimilarityChecker interface {
	FindMostSimilar(taskPattern string, existing []IndexEntry) (skillID string, score float64)
	Threshold() float64
}

type DefaultSimilarityChecker struct {
	threshold float64
}

func NewSimilarityChecker(threshold float64) *DefaultSimilarityChecker {
	if threshold <= 0 || threshold > 1 {
		threshold = 0.85
	}
	return &DefaultSimilarityChecker{threshold: threshold}
}

func (c *DefaultSimilarityChecker) FindMostSimilar(taskPattern string, existing []IndexEntry) (string, float64) {
	if c == nil || len(existing) == 0 {
		return "", 0
	}
	inputTerms := normalizeTerms(taskPattern)
	if len(inputTerms) == 0 {
		return "", 0
	}

	var bestID string
	var bestScore float64

	for _, entry := range existing {
		entryTerms := normalizeTerms(entry.TaskPattern + " " + strings.Join(entry.Keywords, " "))
		score := overlapScore(inputTerms, entryTerms)
		if score > bestScore {
			bestScore = score
			bestID = entry.SkillID
		}
	}

	return bestID, bestScore
}

func normalizeTerms(text string) map[string]struct{} {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' || r == ';' || r == ':' || r == '-' || r == '_'
	})
	terms := make(map[string]struct{})
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" || len(w) < 2 {
			continue
		}
		terms[w] = struct{}{}
	}
	return terms
}

func overlapScore(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var intersection int
	for term := range a {
		if _, ok := b[term]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func (c *DefaultSimilarityChecker) Threshold() float64 {
	if c == nil {
		return 0.85
	}
	return c.threshold
}

func (c *DefaultSimilarityChecker) String() string {
	return fmt.Sprintf("DefaultSimilarityChecker(threshold=%.2f)", c.Threshold())
}
