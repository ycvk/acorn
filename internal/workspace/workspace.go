package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ToolCreateFile         = "create_file"
	ToolReplaceSpan        = "replace_span"
	ToolApplyUnifiedPatch  = "apply_unified_patch"
	ToolMultiEdit          = "multi_edit"
	ToolRollbackCheckpoint = "rollback_workspace_checkpoint"
	ToolRunCommand         = "run_command"
)

var defaultMutationDenylist = []string{
	".ssh", ".aws", ".gnupg", ".config/gh",
	".netrc", ".env", ".credentials",
}

type Config struct {
	RootDir                  string
	StorageDir               string
	MutationDenylist         []string
	RunCommandDefaultTimeout int
	RunCommandEnvWhitelist   []string
}

type Workspace struct {
	rootDir                  string
	storageDir               string
	mutationDenylist         []string
	runCommandDefaultTimeout int
	runCommandEnvWhitelist   []string
}

func New(cfg Config) (*Workspace, error) {
	rootDir := strings.TrimSpace(cfg.RootDir)
	if rootDir == "" {
		return nil, errors.New("workspace root dir is required")
	}
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root dir: %w", err)
	}

	storageDir := strings.TrimSpace(cfg.StorageDir)
	if storageDir != "" {
		storageDir, err = filepath.Abs(storageDir)
		if err != nil {
			return nil, fmt.Errorf("resolve workspace storage dir: %w", err)
		}
	}

	denylist := append([]string(nil), cfg.MutationDenylist...)
	if len(denylist) == 0 {
		denylist = append([]string(nil), defaultMutationDenylist...)
	}

	w := &Workspace{
		rootDir:                  filepath.Clean(absRoot),
		storageDir:               filepath.Clean(storageDir),
		mutationDenylist:         denylist,
		runCommandDefaultTimeout: cfg.RunCommandDefaultTimeout,
		runCommandEnvWhitelist:   append([]string(nil), cfg.RunCommandEnvWhitelist...),
	}
	return w, nil
}

func (w *Workspace) Root() string {
	if w == nil {
		return ""
	}
	return w.rootDir
}

func (w *Workspace) StorageDir() string {
	if w == nil {
		return ""
	}
	return w.storageDir
}

func (w *Workspace) MutationDenylist() []string {
	if w == nil {
		return nil
	}
	return append([]string(nil), w.mutationDenylist...)
}

func (w *Workspace) RunCommandDefaultTimeout() int {
	if w == nil {
		return 0
	}
	return w.runCommandDefaultTimeout
}

func (w *Workspace) RunCommandEnvWhitelist() []string {
	if w == nil {
		return nil
	}
	return append([]string(nil), w.runCommandEnvWhitelist...)
}

func (w *Workspace) ResolveReadPath(value string) (string, error) {
	return w.resolvePath(value, nil)
}

func (w *Workspace) ResolveWritePath(value string) (string, error) {
	return w.resolvePath(value, w.mutationDenylist)
}

func (w *Workspace) ResolveCwd(value string) (string, error) {
	if w == nil {
		return "", errors.New("workspace is required")
	}
	if strings.TrimSpace(value) == "" {
		return w.rootDir, nil
	}
	return w.resolvePath(value, nil)
}

func (w *Workspace) NormalizeRelativePath(value string) (string, error) {
	resolved, err := w.ResolveReadPath(value)
	if err != nil {
		return "", err
	}
	return w.RelativePath(resolved)
}

func (w *Workspace) RelativePath(absPath string) (string, error) {
	if w == nil {
		return "", errors.New("workspace is required")
	}
	trimmed := strings.TrimSpace(absPath)
	if trimmed == "" {
		return "", errors.New("path is required")
	}
	cleaned := filepath.Clean(trimmed)
	if !filepath.IsAbs(cleaned) {
		cleaned = filepath.Join(w.rootDir, cleaned)
	}
	if !isUnderRoot(cleaned, w.rootDir) {
		return "", fmt.Errorf("path %q escapes workspace root %q", absPath, w.rootDir)
	}
	rel, err := filepath.Rel(w.rootDir, cleaned)
	if err != nil {
		return "", fmt.Errorf("compute relative path: %w", err)
	}
	if rel == "." {
		return "", errors.New("path resolves to workspace root, not a file path")
	}
	return filepath.Clean(rel), nil
}

func (w *Workspace) resolvePath(value string, denylist []string) (string, error) {
	if w == nil {
		return "", errors.New("workspace is required")
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("path is required")
	}
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", value)
	}

	cleaned := filepath.Clean(value)
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if component == ".." {
			return "", fmt.Errorf("path traversal (..) is not allowed: %q", value)
		}
	}

	joined := filepath.Clean(filepath.Join(w.rootDir, cleaned))

	rootEval, rootEvalErr := filepath.EvalSymlinks(w.rootDir)
	if rootEvalErr != nil {
		if !os.IsNotExist(rootEvalErr) {
			return "", fmt.Errorf("evaluate workspace root symlinks: %w", rootEvalErr)
		}
		rootEval = w.rootDir
	}
	evaluated, evalErr := filepath.EvalSymlinks(joined)
	if evalErr != nil && !os.IsNotExist(evalErr) {
		return "", fmt.Errorf("evaluate workspace path symlinks: %w", evalErr)
	}
	if evalErr != nil {
		evaluated = resolveExistingPrefix(joined)
	}
	if !isUnderRoot(evaluated, rootEval) {
		return "", fmt.Errorf("resolved path escapes workspace root: %q", value)
	}

	rel, relErr := filepath.Rel(rootEval, evaluated)
	if relErr == nil && rel != "." {
		if denied, matched := matchesDenylist(rel, denylist); matched {
			return "", fmt.Errorf("path contains denied pattern %q: %q", denied, value)
		}
	}

	return joined, nil
}

func matchesDenylist(relPath string, denylist []string) (string, bool) {
	for _, denied := range denylist {
		normalized := strings.TrimSpace(filepath.Clean(denied))
		if normalized == "" || normalized == "." {
			continue
		}
		if relPath == normalized {
			return denied, true
		}
		prefix := normalized + string(filepath.Separator)
		if strings.HasPrefix(relPath, prefix) {
			return denied, true
		}
	}
	return "", false
}

func isUnderRoot(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func resolveExistingPrefix(path string) string {
	dir := path
	for {
		evaluated, err := filepath.EvalSymlinks(dir)
		if err == nil {
			suffix := strings.TrimPrefix(path, dir)
			if suffix != "" && suffix[0] == filepath.Separator {
				suffix = suffix[1:]
			}
			if suffix == "" {
				return evaluated
			}
			return filepath.Join(evaluated, suffix)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		dir = parent
	}
}
