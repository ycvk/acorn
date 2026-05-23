package tools

import (
	"context"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/browser"
	"github.com/ycvk/acorn/internal/terminalsession"
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
	Write(ctx context.Context, req artifacts.WriteRequest) (artifacts.Record, error)
	ReadRange(ctx context.Context, req artifacts.ReadRangeRequest) (artifacts.ReadRangeResult, error)
	ListByRun(ctx context.Context, runID string) ([]artifacts.Record, error)
	ListBySession(ctx context.Context, sessionID string) ([]artifacts.Record, error)
}

// TerminalService is the subset of terminal session operations required by tool builders.
type TerminalService interface {
	Start(ctx context.Context, req terminalsession.StartRequest) (terminalsession.SessionRecord, error)
	Write(ctx context.Context, req terminalsession.WriteRequest) (terminalsession.WriteResult, error)
	Read(ctx context.Context, req terminalsession.ReadRequest) (terminalsession.ReadResult, error)
	Signal(ctx context.Context, req terminalsession.SignalRequest) (terminalsession.SessionRecord, error)
	Close(ctx context.Context, terminalSessionID string, force bool) (terminalsession.SessionRecord, error)
	ListByRun(ctx context.Context, runID string) ([]terminalsession.SessionRecord, error)
	Load(ctx context.Context, terminalSessionID string) (terminalsession.SessionRecord, error)
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
	Status(ctx context.Context) (browser.Status, error)
	Open(ctx context.Context, rawURL string) (browser.NavigateResult, error)
	Tabs(ctx context.Context) ([]browser.Tab, error)
	Scan(ctx context.Context, req browser.ScanRequest) (browser.ScanResult, error)
	Snapshot(ctx context.Context) (browser.SnapshotResult, error)
	Click(ctx context.Context, ref, selector string) (browser.NavigateResult, error)
	Fill(ctx context.Context, ref, selector, text string) (browser.NavigateResult, error)
	Press(ctx context.Context, ref, selector, key string) (browser.NavigateResult, error)
	Select(ctx context.Context, ref, selector, value string) (browser.NavigateResult, error)
	Screenshot(ctx context.Context, fullPage bool) ([]byte, error)
	ConsoleStart(ctx context.Context) (browser.ConsoleResult, error)
	ConsoleList() browser.ConsoleResult
	ConsoleStop() browser.ConsoleResult
	NetworkStart(ctx context.Context) (browser.NetworkResult, error)
	NetworkList() browser.NetworkResult
	NetworkStop() browser.NetworkResult
	Close() error
}
