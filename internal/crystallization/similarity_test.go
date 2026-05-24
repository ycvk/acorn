package crystallization

import (
	"testing"
)

func TestSimilarityCheckerThreshold(t *testing.T) {
	c := NewSimilarityChecker(0.75)
	if got := c.Threshold(); got != 0.75 {
		t.Fatalf("Threshold() = %v, want 0.75", got)
	}
}

func TestSimilarityCheckerDefaultThreshold(t *testing.T) {
	c := NewSimilarityChecker(0)
	if got := c.Threshold(); got != 0.85 {
		t.Fatalf("Threshold() = %v, want 0.85", got)
	}

	c2 := NewSimilarityChecker(1.5)
	if got := c2.Threshold(); got != 0.85 {
		t.Fatalf("Threshold() = %v, want 0.85", got)
	}
}

func TestSimilarityCheckerNilThreshold(t *testing.T) {
	var c *DefaultSimilarityChecker
	if got := c.Threshold(); got != 0.85 {
		t.Fatalf("nil Threshold() = %v, want 0.85", got)
	}
}

func TestSimilarityFindMostSimilarEmpty(t *testing.T) {
	c := NewSimilarityChecker(0.85)
	id, score := c.FindMostSimilar("pattern", nil)
	if id != "" || score != 0 {
		t.Fatalf("FindMostSimilar(nil) = (%q, %v), want (\"\", 0)", id, score)
	}

	id, score = c.FindMostSimilar("pattern", []IndexEntry{})
	if id != "" || score != 0 {
		t.Fatalf("FindMostSimilar(empty) = (%q, %v), want (\"\", 0)", id, score)
	}
}

func TestSimilarityFindMostSimilarNilChecker(t *testing.T) {
	var c *DefaultSimilarityChecker
	id, score := c.FindMostSimilar("pattern", []IndexEntry{{SkillID: "s1", TaskPattern: "pattern"}})
	if id != "" || score != 0 {
		t.Fatalf("nil FindMostSimilar = (%q, %v), want (\"\", 0)", id, score)
	}
}

func TestSimilarityFindMostSimilarEmptyPattern(t *testing.T) {
	c := NewSimilarityChecker(0.85)
	id, score := c.FindMostSimilar("", []IndexEntry{{SkillID: "s1", TaskPattern: "pattern"}})
	if id != "" || score != 0 {
		t.Fatalf("FindMostSimilar(\"\") = (%q, %v), want (\"\", 0)", id, score)
	}
}

func TestSimilarityFindMostSimilarExactMatch(t *testing.T) {
	c := NewSimilarityChecker(0.5)
	entries := []IndexEntry{
		{SkillID: "s1", TaskPattern: "search and replace", Keywords: []string{"search", "replace"}},
		{SkillID: "s2", TaskPattern: "completely different", Keywords: []string{"different"}},
	}
	id, score := c.FindMostSimilar("search and replace", entries)
	if id != "s1" {
		t.Fatalf("id = %q, want s1", id)
	}
	if score <= 0 {
		t.Fatalf("score = %v, want > 0", score)
	}
}

func TestSimilarityFindMostSimilarPartialMatch(t *testing.T) {
	c := NewSimilarityChecker(0.1)
	entries := []IndexEntry{
		{SkillID: "s1", TaskPattern: "read file and search text", Keywords: []string{"read", "file", "search"}},
		{SkillID: "s2", TaskPattern: "write file and edit content", Keywords: []string{"write", "file", "edit"}},
	}
	id, score := c.FindMostSimilar("search text in files", entries)
	if id != "s1" {
		t.Fatalf("id = %q, want s1", id)
	}
	if score <= 0 {
		t.Fatalf("score = %v, want > 0", score)
	}
}

func TestOverlapScore(t *testing.T) {
	cases := []struct {
		name string
		a    map[string]struct{}
		b    map[string]struct{}
		want float64
	}{
		{
			name: "identical",
			a:    map[string]struct{}{"foo": {}, "bar": {}},
			b:    map[string]struct{}{"foo": {}, "bar": {}},
			want: 1.0,
		},
		{
			name: "no overlap",
			a:    map[string]struct{}{"foo": {}},
			b:    map[string]struct{}{"bar": {}},
			want: 0,
		},
		{
			name: "partial",
			a:    map[string]struct{}{"foo": {}, "bar": {}, "baz": {}},
			b:    map[string]struct{}{"foo": {}, "bar": {}},
			want: 2.0 / 3.0,
		},
		{
			name: "empty a",
			a:    map[string]struct{}{},
			b:    map[string]struct{}{"foo": {}},
			want: 0,
		},
		{
			name: "empty b",
			a:    map[string]struct{}{"foo": {}},
			b:    map[string]struct{}{},
			want: 0,
		},
		{
			name: "both empty",
			a:    map[string]struct{}{},
			b:    map[string]struct{}{},
			want: 0,
		},
		{
			name: "subset",
			a:    map[string]struct{}{"foo": {}},
			b:    map[string]struct{}{"foo": {}, "bar": {}, "baz": {}},
			want: 1.0 / 3.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := overlapScore(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("overlapScore = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeTerms(t *testing.T) {
	terms := normalizeTerms("Hello, World! Test-Case_Example")
	// Note: '!' is not a delimiter, so "world!" is kept as-is
	expected := map[string]struct{}{"hello": {}, "world!": {}, "test": {}, "case": {}, "example": {}}
	if len(terms) != len(expected) {
		t.Fatalf("len(terms) = %d, want %d", len(terms), len(expected))
	}
	for k := range expected {
		if _, ok := terms[k]; !ok {
			t.Fatalf("missing term %q", k)
		}
	}
}

func TestNormalizeTermsFiltersShortWords(t *testing.T) {
	terms := normalizeTerms("a ab abc abcd")
	if _, ok := terms["a"]; ok {
		t.Fatalf("should filter out single-char words")
	}
	// Two-char words are kept (filter is < 2, not <= 2)
	if _, ok := terms["ab"]; !ok {
		t.Fatalf("should keep two-char words")
	}
	if _, ok := terms["abc"]; !ok {
		t.Fatalf("should keep three-char words")
	}
	if _, ok := terms["abcd"]; !ok {
		t.Fatalf("should keep four-char words")
	}
}

func TestNormalizeTermsEmpty(t *testing.T) {
	terms := normalizeTerms("")
	if len(terms) != 0 {
		t.Fatalf("len(terms) = %d, want 0", len(terms))
	}
}
