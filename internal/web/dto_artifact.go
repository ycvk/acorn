package web

import (
	"time"

	"github.com/ycvk/acorn/internal/app"
)

type ArtifactSummaryDTO struct {
	ArtifactID          string    `json:"artifact_id"`
	RunID               string    `json:"run_id"`
	SessionID           string    `json:"session_id,omitempty"`
	SourceToolResultRef string    `json:"source_tool_result_ref,omitempty"`
	Kind                string    `json:"kind"`
	Title               string    `json:"title,omitempty"`
	MIMEType            string    `json:"mime_type,omitempty"`
	SizeBytes           int64     `json:"size_bytes"`
	SHA256              string    `json:"sha256"`
	CreatedAt           time.Time `json:"created_at"`
}

func artifactSummaryDTOsFromDomain(items []app.ArtifactSummary) []ArtifactSummaryDTO {
	return DefaultConverter.artifactSummaryDTOsFromDomain(items)
}
