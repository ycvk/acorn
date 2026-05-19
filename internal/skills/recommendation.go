package skills

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

type Recommendation struct {
	Skill          Spec
	Score          int
	MatchedTerms   []string
	TriggerMatched bool
	FilteredReason string
}

type Activated struct {
	Skill        Spec
	Score        int
	MatchedTerms []string
	Explicit     bool
}

func Recommend(input string, ctx EligibilityContext, items []Spec) ([]Recommendation, error) {
	normalizedInput := normalizeSelectionText(input)
	inputTerms := tokenizeSelectionText(normalizedInput)
	ctx = normalizeEligibilityContext(ctx)
	matches := make([]Recommendation, 0, len(items))
	for _, item := range items {
		view, err := Evaluate(item, ctx)
		if err != nil {
			return nil, fmt.Errorf("normalize skill %q for recommend: %w", item.ID, err)
		}
		match := Recommendation{Skill: view.Spec}
		match.Score, match.MatchedTerms, match.TriggerMatched = scoreSkillMatch(normalizedInput, inputTerms, view.Spec)
		if len(view.DisabledReasons) > 0 {
			match.FilteredReason = strings.Join(view.DisabledReasons, ";")
		}
		matches = append(matches, match)
	}
	slices.SortFunc(matches, func(left, right Recommendation) int {
		if left.FilteredReason == "" && right.FilteredReason != "" {
			return -1
		}
		if left.FilteredReason != "" && right.FilteredReason == "" {
			return 1
		}
		if left.Score != right.Score {
			if left.Score > right.Score {
				return -1
			}
			return 1
		}
		if left.Skill.Name != right.Skill.Name {
			return strings.Compare(left.Skill.Name, right.Skill.Name)
		}
		return strings.Compare(left.Skill.ID, right.Skill.ID)
	})
	return matches, nil
}

func ActivateExplicit(id string, ctx EligibilityContext, items []Spec) (*Activated, []Recommendation, error) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, nil, fmt.Errorf("explicit skill id is required")
	}
	for _, item := range items {
		if item.ID != trimmedID {
			continue
		}
		view, err := Evaluate(item, ctx)
		if err != nil {
			return nil, nil, err
		}
		match := Recommendation{Skill: view.Spec, Score: 1000, TriggerMatched: true}
		if len(view.DisabledReasons) > 0 {
			match.FilteredReason = strings.Join(view.DisabledReasons, ";")
			return nil, []Recommendation{match}, fmt.Errorf("explicit skill %q is ineligible: %s", trimmedID, match.FilteredReason)
		}
		return &Activated{Skill: view.Spec, Score: match.Score, Explicit: true}, []Recommendation{match}, nil
	}
	return nil, nil, fmt.Errorf("explicit skill %q not found", trimmedID)
}

func scoreSkillMatch(input string, inputTerms map[string]struct{}, skill Spec) (int, []string, bool) {
	score := 0
	matched := make([]string, 0)
	triggerMatched := false
	for _, hint := range skill.TriggerHints {
		normalizedHint := normalizeSelectionText(hint)
		if normalizedHint == "" {
			continue
		}
		if strings.Contains(input, normalizedHint) {
			score += 100
			matched = append(matched, hint)
			triggerMatched = true
		}
	}
	if phrase := normalizeSelectionText(skill.Name); phrase != "" && strings.Contains(input, phrase) {
		score += 80
		matched = append(matched, skill.Name)
		triggerMatched = true
	}
	score += overlapScore(inputTerms, tokenizeSelectionText(normalizeSelectionText(skill.Summary)), 5, &matched)
	score += overlapScore(inputTerms, tokenizeSelectionText(normalizeSelectionText(strings.Join(skill.Tags, " "))), 4, &matched)
	score += overlapScore(inputTerms, tokenizeSelectionText(normalizeSelectionText(skill.ID)), 2, &matched)
	if pattern := normalizeSelectionText(skill.TaskPattern); pattern != "" {
		if strings.Contains(input, pattern) {
			score += 120
			matched = append(matched, skill.TaskPattern)
			triggerMatched = true
		}
		score += overlapScore(inputTerms, tokenizeSelectionText(pattern), 8, &matched)
	}
	return score, uniqueNonEmpty(matched), triggerMatched
}

func normalizeSelectionText(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(strings.TrimSpace(input)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func tokenizeSelectionText(input string) map[string]struct{} {
	terms := make(map[string]struct{})
	for _, item := range strings.Fields(input) {
		if item == "" {
			continue
		}
		terms[item] = struct{}{}
	}
	return terms
}

func overlapScore(inputTerms map[string]struct{}, candidateTerms map[string]struct{}, weight int, matched *[]string) int {
	score := 0
	for term := range candidateTerms {
		if _, ok := inputTerms[term]; !ok {
			continue
		}
		score += weight
		*matched = append(*matched, term)
	}
	return score
}
