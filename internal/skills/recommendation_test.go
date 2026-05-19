package skills

import (
	"sort"
	"strings"
	"testing"
)

// --- test helpers ---

func testSpec(id, name, summary string, triggerHints []string) Spec {
	return Spec{
		ID:           id,
		Name:         name,
		Summary:      summary,
		TriggerHints: triggerHints,
	}
}

func testEligibleCtx() EligibilityContext {
	return EligibilityContext{
		AvailableTools:    []string{"read_file", "search_text", "create_file", "replace_span", "apply_unified_patch", "run_command"},
		AvailableToolsets: []string{"filesystem", "shell"},
		GOOS:              "darwin",
		Env:               map[string]string{"HOME": "/tmp", "PATH": "/usr/bin"},
		LookPath:          func(string) (string, error) { return "/usr/bin/tool", nil },
	}
}

// --- TestNormalizeSelectionText ---

func TestNormalizeSelectionText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"whitespace only", "   \t\n  ", ""},
		{"simple lowercase", "Hello World", "hello world"},
		{"non-alphanumeric to space", "foo-bar_baz!qux", "foo bar baz qux"},
		{"trimmed and collapsed spaces", "  Foo  BAR  ", "foo bar"},
		{"mixed case", "GoLaNg Is AwEsOmE", "golang is awesome"},
		{"unicode letters preserved", "café résumé", "café résumé"},
		{"digits preserved", "go1.26 is here", "go1 26 is here"},
		{"multiple punctuation", "a---b!!!c", "a b c"},
		{"leading and trailing punctuation", "!hello world!", "hello world"},
		{"tabs and newlines", "hello\tworld\nfoo", "hello world foo"},
		{"only punctuation", "!!!???", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSelectionText(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSelectionText(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- TestTokenizeSelectionText ---

func TestTokenizeSelectionText(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		got := tokenizeSelectionText("")
		if len(got) != 0 {
			t.Errorf("tokenizeSelectionText(\"\") = %v, want empty map", got)
		}
	})

	t.Run("single word", func(t *testing.T) {
		got := tokenizeSelectionText("hello")
		if len(got) != 1 {
			t.Fatalf("expected 1 term, got %d", len(got))
		}
		if _, ok := got["hello"]; !ok {
			t.Error("expected 'hello' in terms")
		}
	})

	t.Run("multiple words", func(t *testing.T) {
		got := tokenizeSelectionText("hello world foo")
		if len(got) != 3 {
			t.Fatalf("expected 3 terms, got %d", len(got))
		}
		for _, w := range []string{"hello", "world", "foo"} {
			if _, ok := got[w]; !ok {
				t.Errorf("expected %q in terms", w)
			}
		}
	})

	t.Run("duplicate words deduplicated", func(t *testing.T) {
		got := tokenizeSelectionText("hello hello world")
		if len(got) != 2 {
			t.Fatalf("expected 2 terms (deduplicated), got %d", len(got))
		}
		for _, w := range []string{"hello", "world"} {
			if _, ok := got[w]; !ok {
				t.Errorf("expected %q in terms", w)
			}
		}
	})

	t.Run("whitespace between words", func(t *testing.T) {
		got := tokenizeSelectionText("  foo   bar  ")
		if len(got) != 2 {
			t.Fatalf("expected 2 terms, got %d", len(got))
		}
		for _, w := range []string{"foo", "bar"} {
			if _, ok := got[w]; !ok {
				t.Errorf("expected %q in terms", w)
			}
		}
	})

	t.Run("tabs and newlines as separators", func(t *testing.T) {
		got := tokenizeSelectionText("a\tb\nc")
		if len(got) != 3 {
			t.Fatalf("expected 3 terms, got %d", len(got))
		}
	})
}

// --- TestOverlapScore ---

func TestOverlapScore(t *testing.T) {
	t.Run("no overlap", func(t *testing.T) {
		input := tokenizeSelectionText("foo bar")
		candidate := tokenizeSelectionText("baz qux")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 0 {
			t.Errorf("expected score 0, got %d", score)
		}
		if len(matched) != 0 {
			t.Errorf("expected 0 matched terms, got %v", matched)
		}
	})

	t.Run("partial overlap", func(t *testing.T) {
		input := tokenizeSelectionText("foo bar baz")
		candidate := tokenizeSelectionText("bar qux")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 5 {
			t.Errorf("expected score 5, got %d", score)
		}
		if len(matched) != 1 || matched[0] != "bar" {
			t.Errorf("expected matched [bar], got %v", matched)
		}
	})

	t.Run("full overlap", func(t *testing.T) {
		input := tokenizeSelectionText("foo bar")
		candidate := tokenizeSelectionText("foo bar")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 10 {
			t.Errorf("expected score 10, got %d", score)
		}
		sort.Strings(matched)
		if len(matched) != 2 || matched[0] != "bar" || matched[1] != "foo" {
			t.Errorf("expected matched [bar foo], got %v", matched)
		}
	})

	t.Run("empty input terms", func(t *testing.T) {
		input := tokenizeSelectionText("")
		candidate := tokenizeSelectionText("foo bar")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 0 {
			t.Errorf("expected score 0, got %d", score)
		}
	})

	t.Run("empty candidate terms", func(t *testing.T) {
		input := tokenizeSelectionText("foo bar")
		candidate := tokenizeSelectionText("")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 0 {
			t.Errorf("expected score 0, got %d", score)
		}
	})

	t.Run("weight 5 applied correctly", func(t *testing.T) {
		input := tokenizeSelectionText("a b c")
		candidate := tokenizeSelectionText("a b c")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 15 {
			t.Errorf("expected score 15 (3*5), got %d", score)
		}
	})

	t.Run("weight 4 applied correctly", func(t *testing.T) {
		input := tokenizeSelectionText("a b")
		candidate := tokenizeSelectionText("a b")
		var matched []string
		score := overlapScore(input, candidate, 4, &matched)
		if score != 8 {
			t.Errorf("expected score 8 (2*4), got %d", score)
		}
	})

	t.Run("weight 2 applied correctly", func(t *testing.T) {
		input := tokenizeSelectionText("a")
		candidate := tokenizeSelectionText("a")
		var matched []string
		score := overlapScore(input, candidate, 2, &matched)
		if score != 2 {
			t.Errorf("expected score 2 (1*2), got %d", score)
		}
	})

	t.Run("matched terms collected", func(t *testing.T) {
		input := tokenizeSelectionText("alpha beta gamma")
		candidate := tokenizeSelectionText("beta gamma delta")
		var matched []string
		score := overlapScore(input, candidate, 5, &matched)
		if score != 10 {
			t.Errorf("expected score 10, got %d", score)
		}
		sort.Strings(matched)
		if len(matched) != 2 {
			t.Fatalf("expected 2 matched terms, got %d", len(matched))
		}
		if matched[0] != "beta" || matched[1] != "gamma" {
			t.Errorf("expected [beta gamma], got %v", matched)
		}
	})
}

// --- TestScoreSkillMatch ---

func TestScoreSkillMatch(t *testing.T) {
	t.Run("trigger hint exact match", func(t *testing.T) {
		input := normalizeSelectionText("inspect repo")
		inputTerms := tokenizeSelectionText(input)
		skill := testSpec("skill.inspect.repo", "Inspect Repo", "Inspect a repository", []string{"inspect repo"})
		score, matched, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if score < 100 {
			t.Errorf("expected score >= 100 for trigger hint match, got %d", score)
		}
		if !triggerMatched {
			t.Error("expected triggerMatched true")
		}
		found := false
		for _, m := range matched {
			if m == "inspect repo" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected 'inspect repo' in matched terms, got %v", matched)
		}
	})

	t.Run("trigger hint partial match (input contains hint)", func(t *testing.T) {
		input := normalizeSelectionText("please inspect the repo for me")
		inputTerms := tokenizeSelectionText(input)
		skill := testSpec("skill.inspect", "Inspect", "Inspect things", []string{"inspect"})
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if score < 100 {
			t.Errorf("expected score >= 100 for partial trigger hint match, got %d", score)
		}
		if !triggerMatched {
			t.Error("expected triggerMatched true for partial match")
		}
	})

	t.Run("multiple trigger hints match", func(t *testing.T) {
		input := normalizeSelectionText("debug panic in backend")
		inputTerms := tokenizeSelectionText(input)
		skill := testSpec("skill.debug", "Debug", "Debug things", []string{"debug", "panic"})
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if score < 200 {
			t.Errorf("expected score >= 200 for two trigger hint matches, got %d", score)
		}
		if !triggerMatched {
			t.Error("expected triggerMatched true")
		}
	})

	t.Run("name match", func(t *testing.T) {
		input := normalizeSelectionText("use the inspect repo tool")
		inputTerms := tokenizeSelectionText(input)
		skill := testSpec("skill.inspect.repo", "Inspect Repo", "A tool", nil)
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if score < 80 {
			t.Errorf("expected score >= 80 for name match, got %d", score)
		}
		if !triggerMatched {
			t.Error("expected triggerMatched true for name match")
		}
	})

	t.Run("summary overlap", func(t *testing.T) {
		input := normalizeSelectionText("inspect a git repository")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:           "skill.test",
			Name:         "Test Skill",
			Summary:      "inspect a git repository quickly",
			TriggerHints: nil,
		}
		score, matched, _ := scoreSkillMatch(input, inputTerms, skill)
		// "inspect", "a", "git", "repository" overlap → 4 * 5 = 20
		summaryOverlap := 0
		for _, m := range matched {
			for _, w := range []string{"inspect", "a", "git", "repository"} {
				if m == w {
					summaryOverlap += 5
				}
			}
		}
		if summaryOverlap == 0 {
			t.Errorf("expected summary overlap contribution, got score=%d matched=%v", score, matched)
		}
	})

	t.Run("tags overlap", func(t *testing.T) {
		input := normalizeSelectionText("debug panic")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:           "skill.debug",
			Name:         "Debug Tool",
			Summary:      "Something else",
			Tags:         []string{"debug", "panic", "backend"},
			TriggerHints: nil,
		}
		score, matched, _ := scoreSkillMatch(input, inputTerms, skill)
		// "debug" and "panic" overlap → 2 * 4 = 8
		tagOverlap := 0
		for _, m := range matched {
			if m == "debug" || m == "panic" {
				tagOverlap += 4
			}
		}
		if tagOverlap != 8 {
			t.Errorf("expected tag overlap 8, got %d (score=%d matched=%v)", tagOverlap, score, matched)
		}
	})

	t.Run("ID overlap", func(t *testing.T) {
		input := normalizeSelectionText("debug panic tool")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:           "skill.debug.panic",
			Name:         "Panic Helper",
			Summary:      "Unrelated summary",
			TriggerHints: nil,
		}
		score, matched, _ := scoreSkillMatch(input, inputTerms, skill)
		// ID normalized: "skill debug panic" → "debug" and "panic" overlap → 2 * 2 = 4
		idOverlap := 0
		for _, m := range matched {
			if m == "debug" || m == "panic" {
				idOverlap += 2
			}
		}
		if idOverlap != 4 {
			t.Errorf("expected ID overlap 4, got %d (score=%d matched=%v)", idOverlap, score, matched)
		}
	})

	t.Run("no matches", func(t *testing.T) {
		input := normalizeSelectionText("deploy to production")
		inputTerms := tokenizeSelectionText(input)
		skill := testSpec("skill.inspect.repo", "Inspect Repo", "Inspect a repository", []string{"inspect"})
		score, matched, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if score != 0 {
			t.Errorf("expected score 0 for no matches, got %d", score)
		}
		if triggerMatched {
			t.Error("expected triggerMatched false for no matches")
		}
		if len(matched) != 0 {
			t.Errorf("expected no matched terms, got %v", matched)
		}
	})

	t.Run("combined matches add up", func(t *testing.T) {
		input := normalizeSelectionText("debug panic in backend")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:           "skill.debug.panic",
			Name:         "Debug Panic",
			Summary:      "debug panic errors in backend services",
			Tags:         []string{"debug", "panic", "backend"},
			TriggerHints: []string{"debug panic"},
		}
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if !triggerMatched {
			t.Error("expected triggerMatched true")
		}
		// trigger hint "debug panic" → +100
		// name "debug panic" → +80
		// summary overlap: "debug", "panic", "in", "backend" → at least "debug", "panic", "backend" = 3*5=15
		// tags overlap: "debug", "panic", "backend" → 3*4=12
		// ID overlap: "debug", "panic" → 2*2=4
		// minimum expected: 100 + 80 + 15 + 12 + 4 = 211
		if score < 211 {
			t.Errorf("expected score >= 211 for combined matches, got %d", score)
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		input := normalizeSelectionText("DEBUG PANIC")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:           "skill.debug",
			Name:         "Debug Tool",
			Summary:      "debug panic errors",
			Tags:         []string{"debug"},
			TriggerHints: []string{"debug"},
		}
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if !triggerMatched {
			t.Error("expected triggerMatched true for case-insensitive match")
		}
		if score < 100 {
			t.Errorf("expected score >= 100 for case-insensitive trigger match, got %d", score)
		}
	})

	t.Run("empty trigger hint skipped", func(t *testing.T) {
		input := normalizeSelectionText("hello")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:           "skill.test",
			Name:         "Test",
			Summary:      "A test skill",
			TriggerHints: []string{"", "   "},
		}
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if triggerMatched {
			t.Error("expected triggerMatched false when all hints are empty")
		}
		_ = score
	})
}

// --- TestRecommend ---

func TestRecommend(t *testing.T) {
	ctx := testEligibleCtx()

	t.Run("empty items list", func(t *testing.T) {
		result, err := Recommend("test", ctx, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("expected empty result, got %d items", len(result))
		}
	})

	t.Run("single matching skill", func(t *testing.T) {
		skill := testSpec("skill.inspect", "Inspect", "Inspect a repository", []string{"inspect"})
		result, err := Recommend("inspect repo", ctx, []Spec{skill})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		if result[0].Skill.ID != "skill.inspect" {
			t.Errorf("expected skill ID skill.inspect, got %s", result[0].Skill.ID)
		}
		if result[0].Score == 0 {
			t.Error("expected non-zero score for matching skill")
		}
	})

	t.Run("multiple skills sorted by score descending", func(t *testing.T) {
		skillA := testSpec("skill.a", "Alpha", "deploy to production", []string{"deploy"})
		skillB := testSpec("skill.b", "Bravo", "inspect a repository", []string{"inspect"})
		skillC := testSpec("skill.c", "Charlie", "debug panic errors", []string{"debug"})
		result, err := Recommend("deploy", ctx, []Spec{skillA, skillB, skillC})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 results, got %d", len(result))
		}
		if result[0].Skill.ID != "skill.a" {
			t.Errorf("expected highest-scoring skill first, got %s (score=%d)", result[0].Skill.ID, result[0].Score)
		}
		for i := 1; i < len(result); i++ {
			if result[i].Score > result[i-1].Score {
				t.Errorf("results not sorted by score descending: [%d]=%d > [%d]=%d", i, result[i].Score, i-1, result[i-1].Score)
			}
		}
	})

	t.Run("ineligible skills sorted after eligible", func(t *testing.T) {
		eligibleSkill := testSpec("skill.good", "Good Skill", "inspect things", []string{"inspect"})
		// This skill requires a tool that doesn't exist in context
		ineligibleSkill := testSpec("skill.bad", "Bad Skill", "inspect things", []string{"inspect"})
		ineligibleSkill.Requires.Tools = []string{"nonexistent_tool"}

		result, err := Recommend("inspect", ctx, []Spec{eligibleSkill, ineligibleSkill})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 results, got %d", len(result))
		}
		// eligible first
		if result[0].FilteredReason != "" {
			t.Errorf("expected first result to be eligible, got FilteredReason=%q", result[0].FilteredReason)
		}
		if result[1].FilteredReason == "" {
			t.Error("expected second result to be ineligible (have FilteredReason)")
		}
	})

	t.Run("same score sorted by name then ID", func(t *testing.T) {
		skillA := testSpec("skill.z", "Alpha", "unrelated", nil)
		skillB := testSpec("skill.a", "Alpha", "unrelated", nil)
		skillC := testSpec("skill.m", "Bravo", "unrelated", nil)
		result, err := Recommend("nothing matches", ctx, []Spec{skillC, skillA, skillB})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 results, got %d", len(result))
		}
		// All have score 0, so sorted by name then ID
		// "Alpha" < "Bravo", and within Alpha: "skill.a" < "skill.z"
		if result[0].Skill.Name != "Alpha" || result[0].Skill.ID != "skill.a" {
			t.Errorf("expected first=Alpha/skill.a, got %s/%s", result[0].Skill.Name, result[0].Skill.ID)
		}
		if result[1].Skill.Name != "Alpha" || result[1].Skill.ID != "skill.z" {
			t.Errorf("expected second=Alpha/skill.z, got %s/%s", result[1].Skill.Name, result[1].Skill.ID)
		}
		if result[2].Skill.Name != "Bravo" {
			t.Errorf("expected third=Bravo, got %s", result[2].Skill.Name)
		}
	})

	t.Run("empty input string", func(t *testing.T) {
		skill := testSpec("skill.test", "Test", "A test skill", []string{"test"})
		result, err := Recommend("", ctx, []Spec{skill})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 result, got %d", len(result))
		}
		// empty input → no matches, score 0
		if result[0].Score != 0 {
			t.Errorf("expected score 0 for empty input, got %d", result[0].Score)
		}
	})

	t.Run("evaluate error propagated", func(t *testing.T) {
		// Spec with empty ID will fail NormalizeSpec inside Evaluate
		badSkill := Spec{Name: "No ID"}
		_, err := Recommend("test", ctx, []Spec{badSkill})
		if err == nil {
			t.Error("expected error for invalid spec, got nil")
		}
		if !strings.Contains(err.Error(), "normalize skill") {
			t.Errorf("expected error containing 'normalize skill', got %v", err)
		}
	})
}

// --- TestActivateExplicit ---

func TestActivateExplicit(t *testing.T) {
	ctx := testEligibleCtx()

	t.Run("valid ID found and eligible", func(t *testing.T) {
		skill := testSpec("skill.inspect", "Inspect", "Inspect things", nil)
		activated, recs, err := ActivateExplicit("skill.inspect", ctx, []Spec{skill})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if activated == nil {
			t.Fatal("expected non-nil Activated")
		}
		if activated.Skill.ID != "skill.inspect" {
			t.Errorf("expected skill ID skill.inspect, got %s", activated.Skill.ID)
		}
		if !activated.Explicit {
			t.Error("expected Explicit=true")
		}
		if activated.Score != 1000 {
			t.Errorf("expected score 1000, got %d", activated.Score)
		}
		if len(recs) != 1 {
			t.Fatalf("expected 1 recommendation, got %d", len(recs))
		}
	})

	t.Run("valid ID found but ineligible", func(t *testing.T) {
		skill := testSpec("skill.bad", "Bad Skill", "Needs missing tool", nil)
		skill.Requires.Tools = []string{"nonexistent_tool"}
		activated, recs, err := ActivateExplicit("skill.bad", ctx, []Spec{skill})
		if err == nil {
			t.Fatal("expected error for ineligible skill")
		}
		if activated != nil {
			t.Error("expected nil Activated for ineligible skill")
		}
		if !strings.Contains(err.Error(), "ineligible") {
			t.Errorf("expected error containing 'ineligible', got %v", err)
		}
		if len(recs) != 1 {
			t.Fatalf("expected 1 recommendation, got %d", len(recs))
		}
		if recs[0].FilteredReason == "" {
			t.Error("expected FilteredReason on ineligible recommendation")
		}
	})

	t.Run("invalid/missing ID", func(t *testing.T) {
		skill := testSpec("skill.exists", "Exists", "A skill", nil)
		activated, _, err := ActivateExplicit("skill.nonexistent", ctx, []Spec{skill})
		if err == nil {
			t.Fatal("expected error for missing ID")
		}
		if activated != nil {
			t.Error("expected nil Activated for missing ID")
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected error containing 'not found', got %v", err)
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		activated, _, err := ActivateExplicit("", ctx, nil)
		if err == nil {
			t.Fatal("expected error for empty ID")
		}
		if activated != nil {
			t.Error("expected nil Activated for empty ID")
		}
		if !strings.Contains(err.Error(), "required") {
			t.Errorf("expected error containing 'required', got %v", err)
		}
	})

	t.Run("whitespace-only ID", func(t *testing.T) {
		activated, _, err := ActivateExplicit("   ", ctx, nil)
		if err == nil {
			t.Fatal("expected error for whitespace-only ID")
		}
		if activated != nil {
			t.Error("expected nil Activated for whitespace-only ID")
		}
		if !strings.Contains(err.Error(), "required") {
			t.Errorf("expected error containing 'required', got %v", err)
		}
	})

	t.Run("ID with surrounding whitespace trimmed", func(t *testing.T) {
		skill := testSpec("skill.test", "Test", "A test skill", nil)
		activated, _, err := ActivateExplicit("  skill.test  ", ctx, []Spec{skill})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if activated == nil {
			t.Fatal("expected non-nil Activated")
		}
		if activated.Skill.ID != "skill.test" {
			t.Errorf("expected skill ID skill.test, got %s", activated.Skill.ID)
		}
	})

	t.Run("evaluate error on invalid spec", func(t *testing.T) {
		badSkill := Spec{Name: "No ID"}
		_, _, err := ActivateExplicit("some-id", ctx, []Spec{badSkill})
		// The bad skill has empty ID so it won't match "some-id", returns "not found"
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
