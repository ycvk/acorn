package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/config"
)

const (
	BuiltinScope   = string(SourceBuiltin)
	WorkspaceScope = string(SourceWorkspace)
	UserScope      = string(SourceUser)
)

var ErrNotFound = errors.New("skill not found")

type Loader struct {
	builtinDir   string
	workspaceDir string
	userDir      string
}

type sourceRoot struct {
	scope    string
	root     string
	priority int
}

type loadedSkill struct {
	spec     Spec
	scope    string
	root     string
	priority int
}

func NewLoader(cfg *config.Config) *Loader {
	if cfg == nil {
		return &Loader{}
	}
	workspaceRoot := strings.TrimSpace(cfg.WorkspaceRoot())
	builtinDir := ""
	workspaceDir := ""
	if workspaceRoot != "" {
		builtinDir = filepath.Join(workspaceRoot, "skills")
		workspaceDir = filepath.Join(workspaceRoot, ".acorn", "skills", "workspace")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &Loader{
			builtinDir:   builtinDir,
			workspaceDir: workspaceDir,
		}
	}
	return &Loader{
		builtinDir:   builtinDir,
		workspaceDir: workspaceDir,
		userDir:      filepath.Join(homeDir, ".acorn", "skills"),
	}
}
