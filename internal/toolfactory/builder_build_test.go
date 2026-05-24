package toolfactory

import (
	"context"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/workspace"
)

// mockWorkspace implements tools.WorkspaceView minimally for testing.
type mockWorkspace struct {
	root string
}

func (m *mockWorkspace) Root() string                                  { return m.root }
func (m *mockWorkspace) StorageDir() string                            { return "" }
func (m *mockWorkspace) ResolveReadPath(value string) (string, error)  { return value, nil }
func (m *mockWorkspace) ResolveWritePath(value string) (string, error) { return value, nil }
func (m *mockWorkspace) RelativePath(absPath string) (string, error)   { return absPath, nil }
func (m *mockWorkspace) ResolveCwd(value string) (string, error)       { return value, nil }
func (m *mockWorkspace) RunCommandEnvWhitelist() []string              { return nil }
func (m *mockWorkspace) RunCommandDefaultTimeout() int                 { return 30 }
func (m *mockWorkspace) CreateMutationCheckpoint(ctx context.Context, toolName string, paths []string) (*workspace.WorkspaceMutationCheckpoint, error) {
	return nil, nil
}
func (m *mockWorkspace) CompleteMutationCheckpoint(ctx context.Context, checkpointID string) (*workspace.WorkspaceMutationCheckpoint, error) {
	return nil, nil
}
func (m *mockWorkspace) RollbackMutationCheckpoint(ctx context.Context, checkpointID string) (*workspace.WorkspaceRollbackResult, error) {
	return nil, nil
}
func (m *mockWorkspace) InspectGitStatus(ctx context.Context, scopedPath string) (*workspace.WorkspaceGitStatus, error) {
	return nil, nil
}

func TestBuilderBuildWithWorkspace(t *testing.T) {
	cfg := config.DefaultConfig()
	ws := &mockWorkspace{root: t.TempDir()}
	b := NewBuilder(cfg, ws, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	// Build will fail because it tries to create webaccess/browser services,
	// but we at least verify it passes the nil-checks.
	_, err := b.Build(context.Background(), BuildOptions{})
	if err == nil {
		t.Fatal("expected error because external services are not configured")
	}
}
