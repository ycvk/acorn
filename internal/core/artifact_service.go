package core

import "context"

// ArtifactService is the artifact read/write contract. It is embedded by
// ArtifactStore and consumed directly by tool builders (internal/tools)
// that only need artifact I/O and not summary or OAuth persistence.
type ArtifactService interface {
	WriteArtifact(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error)
	ListByRun(ctx context.Context, runID string) ([]ArtifactRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error)
}
