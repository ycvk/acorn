package compaction

import (
	"testing"
)

func TestComputeSummaryBudget(t *testing.T) {
	tests := []struct {
		name    string
		tokens  int
		wantMin int
		wantMax int
	}{
		{"small input uses floor 2000", 100, 2000, 2000},
		{"medium input uses 20%", 15000, 3000, 3000},
		{"large input uses ceiling 12000", 100000, 12000, 12000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			budget := computeSummaryBudget(tc.tokens)
			if budget < tc.wantMin {
				t.Fatalf("budget = %d, want >= %d", budget, tc.wantMin)
			}
			if budget > tc.wantMax {
				t.Fatalf("budget = %d, want <= %d", budget, tc.wantMax)
			}
		})
	}
}
