package core

import "context"

// ArtifactService is the interface for artifact read/write operations.
// store.ArtifactService implements this interface; runtime and tools
// depend on this interface to avoid importing internal/store directly.
type ArtifactService interface {
	WriteArtifact(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error)
	ListByRun(ctx context.Context, runID string) ([]ArtifactRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error)
}
