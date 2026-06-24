package tools

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/webaccess"
	"github.com/ycvk/acorn/internal/workspace"
)

// WorkspaceView is the subset of workspace operations required by tool builders.
type WorkspaceView interface {
	Root() string
	StorageDir() string
	ResolveReadPath(value string) (string, error)
	ResolveWritePath(value string) (string, error)
	RelativePath(absPath string) (string, error)
	ResolveCwd(value string) (string, error)
	RunCommandEnvWhitelist() []string
	RunCommandDefaultTimeout() int
	CreateMutationCheckpoint(ctx context.Context, toolName string, paths []string) (*workspace.WorkspaceMutationCheckpoint, error)
	CompleteMutationCheckpoint(ctx context.Context, checkpointID string) (*workspace.WorkspaceMutationCheckpoint, error)
	RollbackMutationCheckpoint(ctx context.Context, checkpointID string) (*workspace.WorkspaceRollbackResult, error)
	InspectGitStatus(ctx context.Context, scopedPath string) (*workspace.WorkspaceGitStatus, error)
}

// ArtifactService is the subset of artifact operations required by tool builders.
type ArtifactService interface {
	WriteArtifact(ctx context.Context, req core.ArtifactWriteRequest) (core.ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req core.ArtifactReadRangeRequest) (core.ArtifactReadRangeResult, error)
	ListByRun(ctx context.Context, runID string) ([]core.ArtifactRecord, error)
	ListBySession(ctx context.Context, sessionID string) ([]core.ArtifactRecord, error)
}

// WebFetchService is the subset of web fetch operations required by tool builders.
type WebFetchService interface {
	Fetch(ctx context.Context, req webaccess.FetchRequest) (webaccess.FetchResult, error)
}

// WebSearchService is the subset of web search operations required by tool builders.
type WebSearchService interface {
	Search(ctx context.Context, req webaccess.SearchRequest) (webaccess.SearchResult, error)
}

// BrowserService is the subset of browser automation operations required by tool builders.
type BrowserService interface {
	Status(ctx context.Context) (Status, error)
	Open(ctx context.Context, rawURL string) (NavigateResult, error)
	Tabs(ctx context.Context) ([]Tab, error)
	Scan(ctx context.Context, req ScanRequest) (ScanResult, error)
	Snapshot(ctx context.Context) (SnapshotResult, error)
	Click(ctx context.Context, ref, selector string) (NavigateResult, error)
	Fill(ctx context.Context, ref, selector, text string) (NavigateResult, error)
	Press(ctx context.Context, ref, selector, key string) (NavigateResult, error)
	Select(ctx context.Context, ref, selector, value string) (NavigateResult, error)
	Screenshot(ctx context.Context, fullPage bool) ([]byte, error)
	ConsoleStart(ctx context.Context) (ConsoleResult, error)
	ConsoleList() ConsoleResult
	ConsoleStop() ConsoleResult
	NetworkStart(ctx context.Context) (NetworkResult, error)
	NetworkList() NetworkResult
	NetworkStop() NetworkResult
	Close() error
}
