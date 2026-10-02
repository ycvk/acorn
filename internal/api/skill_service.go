package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

type SkillService struct {
	cfg     *config.Config
	scanner *skills.Loader
}

var ErrSkillNotFound = errors.New("skill not found")

func NewSkillService(cfg *config.Config, scanner *skills.Loader) *SkillService {
	return &SkillService{cfg: cfg, scanner: scanner}
}

func (s *SkillService) Snapshot(ctx context.Context) (*skills.Snapshot, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	scan, err := s.scanner.ScanSkills(ctx)
	if err != nil {
		return nil, err
	}
	snapshot, err := skills.BuildSnapshot(*scan, staticSkillEligibilityContext(s.cfg))
	if err != nil {
		return nil, err
	}
	copied := skills.CopySnapshot(snapshot)
	return &copied, nil
}

func (s *SkillService) Health(ctx context.Context) (*skills.HealthReport, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	scan, err := s.scanner.ScanSkills(ctx)
	if err != nil {
		return nil, err
	}
	report, err := skills.BuildHealthReport(*scan, staticSkillEligibilityContext(s.cfg))
	if err != nil {
		return nil, err
	}
	copied := skills.CopyHealthReport(*report)
	return &copied, nil
}

type SkillListFilter struct {
	Limit  int
	Offset int
}

type SkillFileView struct {
	SkillID string `json:"skill_id"`
	Path    string `json:"path"`
	Content string `json:"content"`
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

func (s *SkillService) Get(ctx context.Context, id string) (*skills.View, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, fmt.Errorf("skill id is required")
	}
	for _, item := range snapshot.Skills {
		if item.ID == trimmedID {
			return new(skills.CopyView(item)), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, trimmedID)
}

func (s *SkillService) ReadFile(ctx context.Context, id, relativePath string) (*SkillFileView, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	content, err := s.scanner.ReadSkillFile(ctx, id, relativePath)
	if err != nil {
		return nil, translateSkillStoreError(err)
	}
	path := strings.TrimSpace(relativePath)
	if path == "" {
		path = "SKILL.md"
	}
	return &SkillFileView{SkillID: strings.TrimSpace(id), Path: path, Content: content}, nil
}

func translateSkillStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, skills.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrSkillNotFound, err)
	default:
		return err
	}
}

func environmentMap() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		env[key] = value
	}
	return env
}

func localEligibilityToolNames(cfg *config.Config) []string {
	specs := tools.ConfiguredLocalSpecs()
	names := make([]string, 0, len(specs)+11)
	seen := make(map[string]struct{}, len(specs)+11)
	for _, spec := range specs {
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
	availableToolsets := make([]string, 0, 1)
	if cfg != nil {
		for _, provider := range cfg.MCP.Providers {
			if !provider.Enabled {
				continue
			}
			availableToolsets = append(availableToolsets, provider.Name)
			availableTools = append(availableTools, provider.ToolNames...)
		}
	}
	return skills.EligibilityContext{
		AvailableTools:    availableTools,
		AvailableToolsets: availableToolsets,
		Env:               environmentMap(),
	}
}
