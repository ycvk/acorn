package crystallization

import "testing"

func TestSimilarityCheckerFindMostSimilar(t *testing.T) {
	checker := NewSimilarityChecker(0.5)
	entries := []IndexEntry{
		{SkillID: "s1", TaskPattern: "search and replace", Keywords: []string{"search", "replace"}},
		{SkillID: "s2", TaskPattern: "completely different", Keywords: []string{"different"}},
	}

	id, score := checker.FindMostSimilar("search and replace", entries)
	if id != "s1" {
		t.Fatalf("id = %q, want s1", id)
	}
	if score <= 0 {
		t.Fatalf("score = %v, want > 0", score)
	}
}

func TestSimilarityCheckerDefaultThreshold(t *testing.T) {
	for _, threshold := range []float64{0, -1, 1.5} {
		checker := NewSimilarityChecker(threshold)
		if got := checker.Threshold(); got != 0.85 {
			t.Fatalf("Threshold(%v) = %v, want 0.85", threshold, got)
		}
	}
}

func TestOverlapScore(t *testing.T) {
	got := overlapScore(
		map[string]struct{}{"foo": {}, "bar": {}, "baz": {}},
		map[string]struct{}{"foo": {}, "bar": {}},
	)
	if got != 2.0/3.0 {
		t.Fatalf("overlapScore = %v, want %v", got, 2.0/3.0)
	}
}
