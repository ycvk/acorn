package core

type MemoryMigrationReport struct {
	Ready             bool           `json:"ready"`
	Schema            string         `json:"schema"`
	Integrity         string         `json:"integrity"`
	ActiveRuns        int            `json:"active_runs"`
	InterruptedRuns   int            `json:"interrupted_runs"`
	PendingActions    int            `json:"pending_actions"`
	LegacyEntries     int            `json:"legacy_entries"`
	LegacyCommitments map[string]int `json:"legacy_commitments"`
	Messages          int            `json:"messages"`
	ToolOutcomes      int            `json:"tool_outcomes"`
	Reason            string         `json:"reason,omitempty"`
}
