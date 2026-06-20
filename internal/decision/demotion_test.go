package decision

import (
	"context"
	"strings"
	"testing"
)

// T-003 RED: after demotion, Engine must not classify intent by substring nor
// route by intent. These fail today (intent routing is live) and are made
// green by T-004. Skill selection must come only from recommendation, not from
// a keyword classifier layered on top of skills.RetrieveCandidates.

func TestEngineDoesNotClassifyIntentBySubstring(t *testing.T) {
	engine := NewEngine(DefaultProfile())
	record, err := engine.Decide(context.Background(), DecideInput{
		Input:             "inspect the repo for problems",
		HasWorkingContext: true,
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.Intent == "inspect" {
		t.Fatalf("Engine must not classify intent by substring; got Intent=%q (no intent routing)", record.Intent)
	}
}

func TestEngineDoesNotRouteByIntent(t *testing.T) {
	engine := NewEngine(DefaultProfile())
	record, err := engine.Decide(context.Background(), DecideInput{
		Input: "inspect the repo for problems",
		AvailableSkills: []RecommendedSkill{
			{ID: "skill.inspect.repo", Name: "Inspect Repo", Score: 10, TriggerMatched: true},
		},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if record.DecisionReason == "profile_route" || strings.HasPrefix(record.DecisionReason, "profile_route_") {
		t.Fatalf("Engine must not route by intent; got DecisionReason=%q (skill selection must come from recommendation)", record.DecisionReason)
	}
}
