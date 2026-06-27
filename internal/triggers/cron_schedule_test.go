package triggers

import (
	"testing"
	"time"
)

func TestParseCronBasic(t *testing.T) {
	s, err := parseCron("0 9 * * *")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	if !s.minute[0] || len(s.minute) != 1 {
		t.Fatalf("minute = %v, want {0}", s.minute)
	}
	if !s.hour[9] || len(s.hour) != 1 {
		t.Fatalf("hour = %v, want {9}", s.hour)
	}
	// Wildcard fields should match all valid values.
	if len(s.dom) != 31 {
		t.Fatalf("dom len = %d, want 31", len(s.dom))
	}
	if len(s.month) != 12 {
		t.Fatalf("month len = %d, want 12", len(s.month))
	}
	if len(s.dow) != 7 { // 0-6 after normalizing 7→0
		t.Fatalf("dow len = %d, want 7", len(s.dow))
	}
}

func TestParseCronStep(t *testing.T) {
	s, err := parseCron("*/15 * * * *")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	want := []int{0, 15, 30, 45}
	for _, m := range want {
		if !s.minute[m] {
			t.Fatalf("minute missing %d", m)
		}
	}
	if len(s.minute) != 4 {
		t.Fatalf("minute len = %d, want 4", len(s.minute))
	}
}

func TestParseCronRangeWithStep(t *testing.T) {
	s, err := parseCron("1-10/3 * * * *")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	want := []int{1, 4, 7, 10}
	for _, m := range want {
		if !s.minute[m] {
			t.Fatalf("minute missing %d", m)
		}
	}
}

func TestParseCronCommaList(t *testing.T) {
	s, err := parseCron("0,30 * * * *")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	if len(s.minute) != 2 || !s.minute[0] || !s.minute[30] {
		t.Fatalf("minute = %v, want {0,30}", s.minute)
	}
}

func TestParseCronSundayNormalization(t *testing.T) {
	s, err := parseCron("* * * * 7")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	if !s.dow[0] {
		t.Fatal("dow 7 should normalize to 0 (Sunday)")
	}
	if s.dow[7] {
		t.Fatal("dow 7 should be removed after normalization")
	}
}

func TestParseCronInvalidFieldCount(t *testing.T) {
	if _, err := parseCron("0 9 * *"); err == nil {
		t.Fatal("expected error for 4-field expression")
	}
	if _, err := parseCron("0 9 * * * *"); err == nil {
		t.Fatal("expected error for 6-field expression")
	}
}

func TestParseCronOutOfRange(t *testing.T) {
	if _, err := parseCron("60 9 * * *"); err == nil {
		t.Fatal("expected error for minute=60")
	}
	if _, err := parseCron("0 25 * * *"); err == nil {
		t.Fatal("expected error for hour=25")
	}
	if _, err := parseCron("0 9 32 * *"); err == nil {
		t.Fatal("expected error for dom=32")
	}
}

func TestCronNextBasic(t *testing.T) {
	s, _ := parseCron("0 9 * * *") // daily at 09:00
	from := time.Date(2026, 6, 27, 8, 0, 0, 0, time.UTC)
	got := s.next(from)
	want := time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

func TestCronNextAlreadyPast(t *testing.T) {
	s, _ := parseCron("0 9 * * *")
	from := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	got := s.next(from)
	want := time.Date(2026, 6, 28, 9, 0, 0, 0, time.UTC) // next day
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

func TestCronNextWeekly(t *testing.T) {
	// Monday 09:00 (weekday=1)
	s, _ := parseCron("0 9 * * 1")
	// Friday June 27 2026
	from := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	got := s.next(from)
	// Next Monday is June 29 2026
	want := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

func TestCronNextEvery15Minutes(t *testing.T) {
	s, _ := parseCron("*/15 * * * *")
	from := time.Date(2026, 6, 27, 10, 7, 0, 0, time.UTC)
	got := s.next(from)
	want := time.Date(2026, 6, 27, 10, 15, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
}

// TestCronDomDowOrSemantics verifies the standard cron rule: when both dom
// and dow are restricted (non-wildcard), a match on EITHER suffices.
// "0 0 13 * 5" = every 13th of the month OR every Friday at midnight.
func TestCronDomDowOrSemantics(t *testing.T) {
	s, err := parseCron("0 0 13 * 5")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	// June 13, 2026 is a Saturday — matches dom (13), not dow (Fri=5).
	june13 := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	if !s.matches(june13) {
		t.Error("June 13 (dom=13) should match via dom OR rule")
	}
	// June 12, 2026 is a Friday — matches dow (5), not dom (12).
	june12 := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	if !s.matches(june12) {
		t.Error("June 12 (dow=Fri) should match via dow OR rule")
	}
	// June 14, 2026 is a Sunday — matches neither dom (14) nor dow (0, but
	// dow field is {5} not {0}).
	june14 := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	if s.matches(june14) {
		t.Error("June 14 should NOT match (neither dom=13 nor dow=Fri)")
	}
}

// TestCronDomDowAndWhenOneIsWildcard verifies that when either dom or dow
// is wildcard, both must match (AND semantics).
func TestCronDomDowAndWhenOneIsWildcard(t *testing.T) {
	// "0 0 13 * *" — dom restricted, dow wildcard → only dom matters.
	s, _ := parseCron("0 0 13 * *")
	june13 := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	if !s.matches(june13) {
		t.Error("dom=13 with dow=* should match on 13th")
	}
	june14 := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	if s.matches(june14) {
		t.Error("dom=13 with dow=* should NOT match on 14th")
	}

	// "0 0 * * 5" — dom wildcard, dow restricted → only dow matters.
	s2, _ := parseCron("0 0 * * 5")
	june12 := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC) // Friday
	if !s2.matches(june12) {
		t.Error("dom=* with dow=5 should match on Friday")
	}
	june13 = time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) // Saturday
	if s2.matches(june13) {
		t.Error("dom=* with dow=5 should NOT match on Saturday")
	}
}
