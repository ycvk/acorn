package skills

import (
	"strings"
)

type CandidateQuery struct {
	Input           string
	ExplicitSkillID string
	Eligibility     EligibilityContext
}

type CandidateResult struct {
	Candidates []Recommendation
}

func RetrieveCandidates(query CandidateQuery, items []Spec) (*CandidateResult, error) {
	if strings.TrimSpace(query.ExplicitSkillID) != "" {
		return retrieveExplicit(query, items)
	}
	return retrieveRanked(query, items)
}

func retrieveExplicit(query CandidateQuery, items []Spec) (*CandidateResult, error) {
	_, candidates, err := ActivateExplicit(query.ExplicitSkillID, query.Eligibility, items)
	if err != nil {
		return &CandidateResult{Candidates: candidates}, err
	}
	return &CandidateResult{Candidates: candidates}, nil
}

func retrieveRanked(query CandidateQuery, items []Spec) (*CandidateResult, error) {
	candidates, err := Recommend(query.Input, query.Eligibility, items)
	if err != nil {
		return nil, err
	}
	return &CandidateResult{Candidates: candidates}, nil
}
