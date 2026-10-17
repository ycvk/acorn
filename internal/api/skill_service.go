package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

// SkillLibrary reads the skills in use, installs the owner's packages and
// keeps the secrets they declare.
type SkillLibrary interface {
	Scan(ctx context.Context) (*skills.ScanResult, error)
	Import(ctx context.Context, entries []core.SkillFile, replace bool) (core.Skill, error)
	SetSecret(ctx context.Context, name, secret, value string) error
}

type SkillService struct {
	cfg     *config.Config
	library SkillLibrary
}

var ErrSkillNotFound = errors.New("skill not found")

func NewSkillService(cfg *config.Config, library SkillLibrary) *SkillService {
	return &SkillService{cfg: cfg, library: library}
}

// Import installs a package the owner read from a directory or zip. An
// existing skill of the same name is replaced only when replace is set.
func (s *SkillService) Import(ctx context.Context, entries []core.SkillFile, replace bool) (core.Skill, error) {
	return s.library.Import(ctx, entries, replace)
}

// SetSecret stores the value of a secret a skill's MCP server declares; the
// server gets it as an environment variable from the next run on.
func (s *SkillService) SetSecret(ctx context.Context, name, secret, value string) error {
	return s.library.SetSecret(ctx, name, secret, value)
}

func (s *SkillService) Snapshot(ctx context.Context) (*skills.Snapshot, error) {
	scan, err := s.library.Scan(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := skills.BuildSnapshot(*scan, staticSkillEligibilityContext(s.cfg))
	return &snapshot, nil
}

func (s *SkillService) Health(ctx context.Context) (*skills.HealthReport, error) {
	scan, err := s.library.Scan(ctx)
	if err != nil {
		return nil, err
	}
	report := skills.BuildHealthReport(*scan, staticSkillEligibilityContext(s.cfg))
	return &report, nil
}

type SkillListFilter struct {
	Limit  int
	Offset int
}

func (s *SkillService) ListFiltered(ctx context.Context, filter SkillListFilter) ([]skills.View, int, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, 0, err
	}
	items := make([]skills.View, 0, len(snapshot.Skills))
	for _, item := range snapshot.Skills {
		items = append(items, skills.CopyView(item))
	}
	filtered := make([]skills.View, 0, len(items))
	filtered = append(filtered, items...)
	total := len(filtered)
	if filter.Limit > 0 {
		end := filter.Offset + filter.Limit
		if end > total {
			end = total
		}
		if filter.Offset >= total {
			filtered = []skills.View{}
		} else {
			filtered = filtered[filter.Offset:end]
		}
	}
	return filtered, total, nil
}

func (s *SkillService) List(ctx context.Context, limit int) ([]skills.View, error) {
	items, _, err := s.ListFiltered(ctx, SkillListFilter{Limit: limit})
	return items, err
}

func (s *SkillService) Get(ctx context.Context, name string) (*skills.View, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	for _, item := range snapshot.Skills {
		if item.Name == name {
			return new(skills.CopyView(item)), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, name)
}

func localEligibilityToolNames(cfg *config.Config) []string {
	specs := tools.ConfiguredLocalSpecs()
	names := make([]string, 0, len(specs)+11)
	seen := make(map[string]struct{}, len(specs)+11)
	workstation := cfg != nil && strings.TrimSpace(cfg.Workstation.URL) != ""
	for _, spec := range specs {
		if spec.Loading.Reason == "workstation" && !workstation {
			continue
		}
		if spec.Enabled() {
			if _, ok := seen[spec.Name]; ok {
				continue
			}
			seen[spec.Name] = struct{}{}
			names = append(names, spec.Name)
		}
	}
	for _, name := range tools.BuiltinToolNames() {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func staticSkillEligibilityContext(cfg *config.Config) skills.EligibilityContext {
	availableTools := localEligibilityToolNames(cfg)
	if cfg != nil {
		for _, provider := range cfg.MCP.Providers {
			if provider.Enabled {
				availableTools = append(availableTools, provider.ToolNames...)
			}
		}
	}
	return skills.EligibilityContext{AvailableTools: availableTools}
}
