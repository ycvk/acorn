package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/config"
)

const (
	BuiltinScope   = string(SourceBuiltin)
	WorkspaceScope = string(SourceWorkspace)
	GeneratedScope = string(SourceGenerated)
	UserScope      = string(SourceUser)
)

var ErrAlreadyExists = errors.New("skill already exists")
var ErrNotFound = errors.New("skill not found")

type Loader struct {
	builtinDir   string
	workspaceDir string
	generatedDir string
	userDir      string
}

// SkillLoader is the subset of Loader used by tool builders.
type SkillLoader interface {
	ScanSkills(context.Context) (*ScanResult, error)
	CreateSkill(context.Context, CreateInput) (*Spec, error)
	WriteSkillFile(context.Context, string, string, string) error
}

type CreateInput struct {
	ID             string
	Name           string
	Version        string
	Category       string
	Summary        string
	PromotedFrom   string
	Origin         Origin
	TaskPattern    string
	Instruction    string
	Tags           []string
	Platforms      []string
	TriggerHints   []string
	Requires       Requirements
	CreatedByRunID string
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
	generatedDir := ""
	if strings.TrimSpace(cfg.Runtime.StorageDir) != "" {
		generatedDir = filepath.Join(cfg.Runtime.StorageDir, "skills", "generated")
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return &Loader{
			builtinDir:   builtinDir,
			workspaceDir: workspaceDir,
			generatedDir: generatedDir,
		}
	}
	return &Loader{
		builtinDir:   builtinDir,
		workspaceDir: workspaceDir,
		generatedDir: generatedDir,
		userDir:      filepath.Join(homeDir, ".acorn", "skills"),
	}
}
