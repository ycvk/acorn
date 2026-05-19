package orchestrationmode

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
		want Mode
	}{
		{"direct response", DirectResponse, DirectResponse},
		{"single agent", SingleAgent, SingleAgent},
		{"plan execute", PlanExecute, PlanExecute},
		{"unknown mode", "unknown", "unknown"},
		{"empty mode", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Normalize(tc.mode)
			if got != tc.want {
				t.Fatalf("Normalize(%q) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}
