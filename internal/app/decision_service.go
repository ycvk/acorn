package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/decision"
)

type DecisionService struct {
	profiles *decision.ProfileService
	store    decisionStore
}

func NewDecisionService(profiles *decision.ProfileService, store decisionStore) *DecisionService {
	return &DecisionService{profiles: profiles, store: store}
}

func (s *DecisionService) Profile(ctx context.Context) (*decision.ParsedProfile, error) {
	_ = ctx
	if s == nil || s.profiles == nil {
		return nil, fmt.Errorf("decision profile service is nil")
	}
	return s.profiles.Load()
}

func (s *DecisionService) InspectRun(ctx context.Context, runID string) (*decision.Record, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("decision store is nil")
	}
	return s.store.LoadRunDecision(ctx, strings.TrimSpace(runID))
}
