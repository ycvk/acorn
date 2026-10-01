package tools

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

type GitStatusEntry struct {
	Path           string `json:"path"`
	IndexStatus    string `json:"index_status"`
	WorktreeStatus string `json:"worktree_status"`
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
