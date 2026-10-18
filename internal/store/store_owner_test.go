package store

import (
	"testing"
	"time"
)

func TestOwnerNameIsUnsetUntilSavedAndKeepsTheLatest(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := s.OwnerName(ctx)
	if err != nil || owner.Name != "" || !owner.UpdatedAt.IsZero() {
		t.Fatalf("fresh owner = %+v, %v", owner, err)
	}
	if _, err := s.SetOwnerName(ctx, "温遥", time.Time{}); err == nil {
		t.Fatal("a name without its time must be rejected")
	}
	first := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	if _, err := s.SetOwnerName(ctx, "温遥", first); err != nil {
		t.Fatal(err)
	}
	later := first.Add(time.Hour)
	if _, err := s.SetOwnerName(ctx, "遥遥", later); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	owner, err = reopened.OwnerName(ctx)
	if err != nil || owner.Name != "遥遥" || !owner.UpdatedAt.Equal(later) {
		t.Fatalf("owner after reopen = %+v, %v", owner, err)
	}
	if _, err := reopened.SetOwnerName(ctx, "", later.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if owner, err = reopened.OwnerName(ctx); err != nil || owner.Name != "" || owner.UpdatedAt.IsZero() {
		t.Fatalf("cleared owner = %+v, %v", owner, err)
	}
}
