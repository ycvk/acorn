package model

import "time"

type RunArchive struct {
	RunID         string    `json:"run_id"`
	SessionID     string    `json:"session_id"`
	InputExcerpt  string    `json:"input_excerpt"`
	OutputExcerpt string    `json:"output_excerpt"`
	TouchedPaths  []string  `json:"touched_paths"`
	ToolNames     []string  `json:"tool_names"`
	RunStatus     string    `json:"run_status"`
	CreatedAt     time.Time `json:"created_at"`
}

type SessionSummary struct {
	SessionID   string    `json:"session_id"`
	SourceRunID string    `json:"source_run_id"`
	RunStatus   string    `json:"run_status"`
	Summary     string    `json:"summary"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type HistoryHit struct {
	SegmentID int64     `json:"segment_id"`
	SessionID string    `json:"session_id"`
	RunID     string    `json:"run_id"`
	RunStatus string    `json:"run_status"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Rank      float64   `json:"rank"`
}

type RunContextSnapshot struct {
	RunID                    string    `json:"run_id"`
	WorkingCheckpointContent string    `json:"working_checkpoint_content"`
	WorkingCheckpointSkillID string    `json:"working_checkpoint_skill_id"`
	CreatedAt                time.Time `json:"created_at"`
}

type ContextBoundary struct {
	BoundaryID               string    `json:"boundary_id"`
	SessionID                string    `json:"session_id"`
	RunID                    string    `json:"run_id"`
	Sequence                 int       `json:"sequence"`
	TurnIndex                int       `json:"turn_index"`
	Mode                     string    `json:"mode"`
	Trigger                  string    `json:"trigger"`
	FirstIndex               int       `json:"first_index"`
	LastIndex                int       `json:"last_index"`
	CoveredFirstMessageID    string    `json:"covered_first_message_id"`
	CoveredLastMessageID     string    `json:"covered_last_message_id"`
	PreviousBoundaryID       string    `json:"previous_boundary_id"`
	SummaryMessageID         string    `json:"summary_message_id"`
	TranscriptRef            string    `json:"transcript_ref"`
	PreservedFromIndex       int       `json:"preserved_from_index"`
	PreservedToIndex         int       `json:"preserved_to_index"`
	PreservedHeadMessageID   string    `json:"preserved_head_message_id"`
	PreservedAnchorMessageID string    `json:"preserved_anchor_message_id"`
	PreservedTailMessageID   string    `json:"preserved_tail_message_id"`
	TokensBefore             int       `json:"tokens_before"`
	TokensAfter              int       `json:"tokens_after"`
	EffectiveWindowTokens    int       `json:"effective_window_tokens"`
	Summary                  string    `json:"summary"`
	SummarySnippet           string    `json:"summary_snippet"`
	CreatedAt                time.Time `json:"created_at"`
}
