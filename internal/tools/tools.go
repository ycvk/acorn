package tools

import (
	"encoding/gob"
	"errors"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/domain"
)

const defaultVerificationPreviewBytes = 2000

type ReadFileInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type ReadFileOutput struct {
	Path       string `json:"path"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	TotalLines int    `json:"total_lines"`
	Bytes      int    `json:"bytes"`
	Truncated  bool   `json:"truncated,omitempty"`
	Content    string `json:"content"`
}

type ListFilesInput struct {
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type ListFileEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir,omitempty"`
	Size  int64  `json:"size,omitempty"`
}

type ListFilesOutput struct {
	RootPath  string          `json:"root_path"`
	Path      string          `json:"path,omitempty"`
	Pattern   string          `json:"pattern,omitempty"`
	Total     int             `json:"total"`
	Truncated bool            `json:"truncated,omitempty"`
	Entries   []ListFileEntry `json:"entries"`
}

type SearchTextInput struct {
	Query         string `json:"query"`
	Path          string `json:"path,omitempty"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type SearchTextMatch struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	LineText string `json:"line_text"`
}

type SearchTextOutput struct {
	Query                  string            `json:"query"`
	Path                   string            `json:"path,omitempty"`
	Regex                  bool              `json:"regex,omitempty"`
	ScannedFileCount       int               `json:"scanned_file_count"`
	SkippedBinaryFileCount int               `json:"skipped_binary_file_count,omitempty"`
	Truncated              bool              `json:"truncated,omitempty"`
	Matches                []SearchTextMatch `json:"matches"`
}

type InspectGitStatusInput struct {
	Path string `json:"path,omitempty"`
}

type GitStatusEntry struct {
	Path           string `json:"path"`
	IndexStatus    string `json:"index_status"`
	WorktreeStatus string `json:"worktree_status"`
}

type InspectGitStatusOutput struct {
	RootPath string           `json:"root_path"`
	Branch   string           `json:"branch,omitempty"`
	Clean    bool             `json:"clean"`
	Entries  []GitStatusEntry `json:"entries"`
}

type InspectGitDiffInput struct {
	Path         string `json:"path,omitempty"`
	Cached       bool   `json:"cached,omitempty"`
	ContextLines int    `json:"context_lines,omitempty"`
	MaxBytes     int    `json:"max_bytes,omitempty"`
}

type InspectGitDiffOutput struct {
	RootPath  string `json:"root_path"`
	Path      string `json:"path,omitempty"`
	Cached    bool   `json:"cached,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Diff      string `json:"diff"`
}

type CreateFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type CreateFileOutput struct {
	Path                  string   `json:"path"`
	Bytes                 int      `json:"bytes"`
	Message               string   `json:"message"`
	CheckpointID          string   `json:"checkpoint_id,omitempty"`
	CheckpointPaths       []string `json:"checkpoint_paths,omitempty"`
	VerifiedBytes         int      `json:"verified_bytes"`
	VerifiedContent       string   `json:"verified_content,omitempty"`
	VerificationTruncated bool     `json:"verification_truncated,omitempty"`
}

type ReplaceSpanInput struct {
	Path        string `json:"path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Replacement string `json:"replacement"`
}

type ReplaceSpanOutput struct {
	Path                  string   `json:"path"`
	StartLine             int      `json:"start_line"`
	EndLine               int      `json:"end_line"`
	Bytes                 int      `json:"bytes"`
	Message               string   `json:"message"`
	CheckpointID          string   `json:"checkpoint_id,omitempty"`
	CheckpointPaths       []string `json:"checkpoint_paths,omitempty"`
	VerifiedBytes         int      `json:"verified_bytes"`
	VerifiedContent       string   `json:"verified_content,omitempty"`
	VerificationTruncated bool     `json:"verification_truncated,omitempty"`
}

type ApplyUnifiedPatchInput struct {
	Patch string   `json:"patch"`
	Paths []string `json:"paths"`
}

type ApplyUnifiedPatchOutput struct {
	Paths            []string `json:"paths"`
	Message          string   `json:"message"`
	CheckpointID     string   `json:"checkpoint_id,omitempty"`
	CheckpointPaths  []string `json:"checkpoint_paths,omitempty"`
	VerifiedDiffStat string   `json:"verified_diff_stat,omitempty"`
}

type MultiEditInput struct {
	Edits []MultiEditSpan `json:"edits"`
}

type MultiEditSpan struct {
	Path        string `json:"path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Replacement string `json:"replacement"`
}

type MultiEditAppliedSpan struct {
	Path             string `json:"path"`
	StartLine        int    `json:"start_line"`
	EndLine          int    `json:"end_line"`
	ReplacementBytes int    `json:"replacement_bytes"`
}

type MultiEditOutput struct {
	Paths            []string               `json:"paths"`
	Edits            []MultiEditAppliedSpan `json:"edits"`
	Message          string                 `json:"message"`
	CheckpointID     string                 `json:"checkpoint_id,omitempty"`
	CheckpointPaths  []string               `json:"checkpoint_paths,omitempty"`
	VerifiedDiffStat string                 `json:"verified_diff_stat,omitempty"`
}

type RollbackWorkspaceCheckpointInput struct {
	CheckpointID string `json:"checkpoint_id"`
}

type RollbackWorkspaceCheckpointOutput struct {
	CheckpointID  string   `json:"checkpoint_id"`
	RollbackID    string   `json:"rollback_id,omitempty"`
	Status        string   `json:"status"`
	RestoredPaths []string `json:"restored_paths,omitempty"`
	ConflictPaths []string `json:"conflict_paths,omitempty"`
	Error         string   `json:"error,omitempty"`
	Message       string   `json:"message"`
}

type RunCommandInput struct {
	Command         []string `json:"command"`
	Cwd             string   `json:"cwd,omitempty"`
	TimeoutSeconds  int      `json:"timeout_seconds,omitempty"`
	PauseBeforeExec bool     `json:"pause_before_exec,omitempty"`
}

type RunCommandOutput struct {
	Command  []string `json:"command"`
	Cwd      string   `json:"cwd"`
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

type RunVerificationInput struct {
	Kind           string   `json:"kind" jsonschema:"description=Verification kind: test, lint, build, format_check, or custom."`
	Command        []string `json:"command"`
	Cwd            string   `json:"cwd,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	Paths          []string `json:"paths,omitempty"`
}

type RunVerificationOutput struct {
	Kind             string          `json:"kind"`
	Status           string          `json:"status"`
	Command          []string        `json:"command"`
	Cwd              string          `json:"cwd"`
	ExitCode         int             `json:"exit_code"`
	TimedOut         bool            `json:"timed_out,omitempty"`
	DurationMS       int64           `json:"duration_ms"`
	Paths            []string        `json:"paths,omitempty"`
	Summary          string          `json:"summary"`
	StdoutArtifactID string          `json:"stdout_artifact_id"`
	StderrArtifactID string          `json:"stderr_artifact_id"`
	StdoutArtifact   ArtifactSummary `json:"stdout_artifact"`
	StderrArtifact   ArtifactSummary `json:"stderr_artifact"`
}

type GitSummaryInput struct {
	Path         string `json:"path,omitempty"`
	IncludeDiff  bool   `json:"include_diff,omitempty"`
	Cached       bool   `json:"cached,omitempty"`
	ContextLines int    `json:"context_lines,omitempty"`
}

type GitSummaryOutput struct {
	RootPath       string           `json:"root_path"`
	Path           string           `json:"path,omitempty"`
	Branch         string           `json:"branch,omitempty"`
	Clean          bool             `json:"clean"`
	Entries        []GitStatusEntry `json:"entries"`
	ChangedPaths   []string         `json:"changed_paths"`
	DiffStat       string           `json:"diff_stat,omitempty"`
	DiffArtifactID string           `json:"diff_artifact_id,omitempty"`
	DiffArtifact   *ArtifactSummary `json:"diff_artifact,omitempty"`
}

type CatalogConfig struct {
	Workspace         WorkspaceView
	MutationEnabled   bool
	RunCommandEnabled bool
	ArtifactService   ArtifactService
	ArtifactContext   domain.ToolCallContextBridge
	OperatorStore     OperatorQuestionStore
	OperatorContext   domain.ToolCallContextBridge
	WebFetchService   WebFetchService
	WebSearchService  WebSearchService
	BrowserService    BrowserService
}

type Catalog struct {
	Tools []einotool.BaseTool
}

func init() {
	gob.Register(RunCommandInput{})
	gob.Register(AskOperatorState{})
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

func BuildCatalog(cfg CatalogConfig, extraTools []einotool.BaseTool) (*Catalog, error) {
	if cfg.Workspace == nil && (cfg.MutationEnabled || cfg.RunCommandEnabled) {
		return nil, errors.New("workspace is required when mutation or run_command tools are enabled")
	}
	items := make([]einotool.BaseTool, 0, 10+len(extraTools))
	groups := []func() ([]einotool.BaseTool, error){
		func() ([]einotool.BaseTool, error) { return buildWorkspaceTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildMutationTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildRunCommandTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildArtifactServiceTools(cfg) },
		func() ([]einotool.BaseTool, error) { return buildOperatorTool(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWebFetchToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildWebSearchToolEntry(cfg) },
		func() ([]einotool.BaseTool, error) { return buildBrowserToolEntry(cfg) },
	}
	for _, group := range groups {
		built, err := group()
		if err != nil {
			return nil, err
		}
		items = append(items, built...)
	}
	items = append(items, extraTools...)
	return &Catalog{Tools: items}, nil
}
