package core

import "time"

type MemoryJob struct {
	ID         int64     `json:"id"`
	Operation  string    `json:"operation"`
	ObjectID   string    `json:"object_id"`
	Version    string    `json:"version"`
	State      string    `json:"state"`
	Token      int64     `json:"token"`
	Epoch      int64     `json:"epoch"`
	Attempts   int       `json:"attempts"`
	Cursor     int       `json:"cursor"`
	LeaseUntil time.Time `json:"lease_until"`
	Error      string    `json:"error,omitempty"`
}

type MemoryProcessingStatus struct {
	TokensToday     int         `json:"tokens_today"`
	DailyTokenLimit int         `json:"daily_token_limit"`
	Pending         int         `json:"pending"`
	Failed          int         `json:"failed"`
	Oldest          time.Time   `json:"oldest,omitempty"`
	LastCompleted   time.Time   `json:"last_completed,omitempty"`
	LastError       string      `json:"last_error,omitempty"`
	Index           MemoryIndex `json:"index"`
}

type MemoryIndex struct {
	Generation int64  `json:"generation"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	State      string `json:"state"`
}

type MemoryVector struct {
	ID         string
	Revision   int64
	Generation int64
	Values     []float32
}

// MemoryVectorSketch is a per-vector scaled int8 candidate index. The stored
// float32 vector supplies the final cosine score.
type MemoryVectorSketch struct {
	ID       string
	Revision int64
	Codes    []byte
	Scale    float64
}

type ThreadSummary struct {
	SessionID        string    `json:"session_id"`
	ThroughMessageID int64     `json:"through_message_id"`
	Content          string    `json:"content"`
	SourceIDs        []string  `json:"source_ids"`
	Epoch            int64     `json:"epoch"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type MemoryUsage struct {
	ID           string    `json:"id"`
	Operation    string    `json:"operation"`
	JobID        int64     `json:"job_id,omitempty"`
	RunID        string    `json:"run_id,omitempty"`
	Budget       string    `json:"budget"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Reported     bool      `json:"reported"`
	CreatedAt    time.Time `json:"created_at"`
}
