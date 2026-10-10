package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestCommitmentClaimOnceAndRetainOccurrence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := memoryTestNow
	rule, err := s.AddCommitment(ctx, core.Commitment{Content: "看论文", State: "scheduled", WakeAt: now, CreatedAt: now, Recurrence: "0 9 * * *"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var claims []core.CommitmentOccurrence
	for range 8 {
		wg.Go(func() {
			occ, err := s.ClaimCommitment(ctx, rule.ID, now)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				claims = append(claims, occ)
			} else if !errors.Is(err, core.ErrCommitmentNotDue) {
				t.Errorf("claim: %v", err)
			}
		})
	}
	wg.Wait()
	if len(claims) != 1 {
		t.Fatalf("claims=%+v", claims)
	}
	if err := s.StartCommitmentOccurrence(ctx, claims[0], "wake_run", now.Add(24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	next, err := s.LoadCommitment(ctx, rule.ID)
	if err != nil || next.State != "scheduled" || !next.WakeAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("next=%+v %v", next, err)
	}
	due, err := s.ListDueOccurrences(ctx)
	if err != nil || len(due) != 1 || due[0].RunID != "wake_run" {
		t.Fatalf("occurrences=%+v %v", due, err)
	}
	evidence := seedMemorySource(t, s, "owner:done", "owner", "论文已读完")
	if err := s.SettleCommitment(ctx, core.CommitmentSettlement{CommitmentID: rule.ID, OccurrenceID: claims[0].ID, Action: "done", SourceID: evidence.ID, Now: now}); err != nil {
		t.Fatal(err)
	}
	due, err = s.ListDueOccurrences(ctx)
	if err != nil || len(due) != 0 {
		t.Fatalf("settled=%+v %v", due, err)
	}
	next, err = s.LoadCommitment(ctx, rule.ID)
	if err != nil || next.State != "scheduled" {
		t.Fatalf("recurrence lost=%+v %v", next, err)
	}
}

func TestCommitmentRetryAndCompletionEvidence(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := memoryTestNow
	rule, err := s.AddCommitment(ctx, core.Commitment{Content: "看论文", State: "scheduled", WakeAt: now, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ClaimCommitment(ctx, rule.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RetryCommitment(ctx, first, now.Add(5*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimCommitment(ctx, rule.ID, now); !errors.Is(err, core.ErrCommitmentNotDue) {
		t.Fatalf("early claim: %v", err)
	}
	second, err := s.ClaimCommitment(ctx, rule.ID, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StartCommitmentOccurrence(ctx, second, "wake_run", time.Time{}, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.SettleCommitment(ctx, core.CommitmentSettlement{CommitmentID: rule.ID, OccurrenceID: second.ID, Action: "done", Now: now}); err == nil {
		t.Fatal("completion without evidence accepted")
	}
	guessed := seedMemorySource(t, s, "assistant:done", "assistant", "应该完成了")
	if err := s.SettleCommitment(ctx, core.CommitmentSettlement{CommitmentID: rule.ID, OccurrenceID: second.ID, Action: "done", SourceID: guessed.ID, Now: now}); err == nil {
		t.Fatal("assistant guess accepted as outcome")
	}
}

func TestCommitmentClaimRecoveryFencesLateStarter(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	c, err := s.AddCommitment(ctx, core.Commitment{Content: "提醒", WakeAt: memoryTestNow, CreatedAt: memoryTestNow})
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.ClaimCommitment(ctx, c.ID, memoryTestNow)
	if err != nil {
		t.Fatal(err)
	}
	now := memoryTestNow.Add(6 * time.Minute)
	if err := s.RecoverCommitmentClaims(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err := s.StartCommitmentOccurrence(ctx, old, "late-run", time.Time{}, now); !errors.Is(err, core.ErrCommitmentNotDue) {
		t.Fatalf("late starter: %v", err)
	}
	next, err := s.ClaimCommitment(ctx, c.ID, now)
	if err != nil || next.ID == old.ID {
		t.Fatalf("recovered claim %+v %v", next, err)
	}
	if err := s.StartCommitmentOccurrence(ctx, next, "new-run", time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverCommitmentClaims(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	occurrences, err := s.ListDueOccurrences(ctx)
	if err != nil || len(occurrences) != 1 || occurrences[0].RunID != "new-run" {
		t.Fatalf("bound occurrence changed %+v %v", occurrences, err)
	}
}
