package skills

import "testing"

func TestRetrieveCandidatesNoMatch(t *testing.T) {
	ctx := testEligibleCtx()
	items := []Spec{
		{ID: "skill.inspect", Name: "Inspect", Source: "workspace", TriggerHints: []string{"inspect repo"}},
	}
	result, err := RetrieveCandidates(CandidateQuery{
		Input:       "deploy to production",
		Eligibility: ctx,
	}, items)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	if result.Candidates[0].Score != 0 {
		t.Fatalf("score = %d, want 0 for no-match input", result.Candidates[0].Score)
	}
	if result.Candidates[0].TriggerMatched {
		t.Fatal("trigger matched = true, want false for no-match input")
	}
}

func TestRetrieveCandidatesTaskPatternScoringInMainPath(t *testing.T) {
	ctx := testEligibleCtx()
	items := []Spec{
		{
			ID:           "sop.fix-sqlite-rows",
			Name:         "SQLite Rows Error Handling",
			Source:       WorkspaceScope,
			Origin:       OriginDistilled,
			TaskPattern:  "fix sqlite rows error handling",
			TriggerHints: []string{"sqlite rows"},
			Requires:     Requirements{Tools: []string{"read_file"}},
		},
	}
	result, err := RetrieveCandidates(CandidateQuery{
		Input:       "fix sqlite rows error handling in query loop",
		Eligibility: ctx,
	}, items)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	candidate := result.Candidates[0]
	if !candidate.TriggerMatched {
		t.Fatal("expected trigger matched for task_pattern substring match")
	}
	if candidate.Score < 120 {
		t.Fatalf("score = %d, want >= 120 (task_pattern substring contribution)", candidate.Score)
	}
	found := false
	for _, term := range candidate.MatchedTerms {
		if term == "fix sqlite rows error handling" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected task_pattern in matched terms, got %v", candidate.MatchedTerms)
	}
}

func TestRetrieveCandidatesEligibilityFilterBeforePublish(t *testing.T) {
	ctx := EligibilityContext{
		AvailableTools: []string{"read_file"},
	}
	items := []Spec{
		{
			ID:       "skill.write-code",
			Name:     "Write Code",
			Source:   "workspace",
			Requires: Requirements{Tools: []string{"create_file"}},
		},
		{
			ID:           "skill.read-code",
			Name:         "Read Code",
			Source:       "workspace",
			TriggerHints: []string{"read code"},
			Requires:     Requirements{Tools: []string{"read_file"}},
		},
	}
	result, err := RetrieveCandidates(CandidateQuery{
		Input:       "read and write code",
		Eligibility: ctx,
	}, items)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(result.Candidates))
	}
	if result.Candidates[0].FilteredReason != "" {
		t.Fatalf("expected first candidate to be eligible, got filtered_reason=%q", result.Candidates[0].FilteredReason)
	}
	if result.Candidates[1].FilteredReason == "" {
		t.Fatal("expected second candidate to be ineligible")
	}
}

func TestRetrieveCandidatesExplicitSkill(t *testing.T) {
	ctx := testEligibleCtx()
	items := []Spec{
		{
			ID:           "skill.explicit",
			Name:         "Explicit Skill",
			Source:       "workspace",
			TriggerHints: []string{"explicit"},
			Requires:     Requirements{Tools: []string{"read_file"}},
		},
	}
	result, err := RetrieveCandidates(CandidateQuery{
		Input:           "unrelated input",
		ExplicitSkillID: "skill.explicit",
		Eligibility:     ctx,
	}, items)
	if err != nil {
		t.Fatalf("RetrieveCandidates: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(result.Candidates))
	}
	if result.Candidates[0].Score != 1000 {
		t.Fatalf("score = %d, want 1000 (explicit skill, no SOP boost)", result.Candidates[0].Score)
	}
}

func TestScoreSkillMatchTaskPattern(t *testing.T) {
	t.Run("task_pattern substring match", func(t *testing.T) {
		input := normalizeSelectionText("fix sqlite rows error handling in query loop")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:          "sop.fix-sqlite-rows",
			Name:        "SQLite Rows SOP",
			TaskPattern: "fix sqlite rows error handling",
			Origin:      OriginDistilled,
		}
		score, matched, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if !triggerMatched {
			t.Fatal("expected triggerMatched for task_pattern substring match")
		}
		if score < 120 {
			t.Fatalf("score = %d, want >= 120 for task_pattern substring match", score)
		}
		found := false
		for _, m := range matched {
			if m == "fix sqlite rows error handling" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected task_pattern in matched terms, got %v", matched)
		}
	})

	t.Run("task_pattern overlap", func(t *testing.T) {
		input := normalizeSelectionText("fix query loop error handling")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:          "sop.fix-query",
			Name:        "Query Fix SOP",
			TaskPattern: "fix sqlite query loop error",
			Origin:      OriginDistilled,
		}
		score, _, _ := scoreSkillMatch(input, inputTerms, skill)
		if score < 32 {
			t.Fatalf("score = %d, want >= 32 for task_pattern overlap (4 terms * 8)", score)
		}
	})

	t.Run("human skill without task_pattern gets no task_pattern score", func(t *testing.T) {
		input := normalizeSelectionText("fix query loop")
		inputTerms := tokenizeSelectionText(input)
		skill := Spec{
			ID:     "skill.human",
			Name:   "Human Skill",
			Origin: OriginHuman,
		}
		score, _, triggerMatched := scoreSkillMatch(input, inputTerms, skill)
		if triggerMatched {
			t.Fatal("expected triggerMatched false for human skill without task_pattern")
		}
		if score != 0 {
			t.Fatalf("score = %d, want 0 for no-matching human skill", score)
		}
	})
}
