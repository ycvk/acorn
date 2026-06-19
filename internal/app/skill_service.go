package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/skills"
)

type SkillService struct {
	cfg     *config.Config
	scanner *skills.Loader
}

var (
	ErrSkillAlreadyExists = errors.New("skill already exists")
	ErrSkillNotFound      = errors.New("skill not found")
)

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

func (s *SkillService) Health(ctx context.Context, fixtures []skills.RoutingFixture) (*skills.HealthReport, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	scan, err := s.scanner.ScanSkills(ctx)
	if err != nil {
		return nil, err
	}
	report, err := skills.BuildHealthReport(*scan, staticSkillEligibilityContext(s.cfg), fixtures)
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

type CreateSkillInput struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Version      string              `json:"version,omitempty"`
	Category     string              `json:"category,omitempty"`
	Summary      string              `json:"summary,omitempty"`
	PromotedFrom string              `json:"promoted_from,omitempty"`
	Origin       skills.Origin       `json:"origin,omitempty"`
	TaskPattern  string              `json:"task_pattern,omitempty"`
	Instruction  string              `json:"instruction"`
	Tags         []string            `json:"tags,omitempty"`
	Platforms    []string            `json:"platforms,omitempty"`
	TriggerHints []string            `json:"trigger_hints,omitempty"`
	Requires     skills.Requirements `json:"requirements,omitempty"`
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

func (s *SkillService) Create(ctx context.Context, input CreateSkillInput) (*skills.View, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	spec, err := s.scanner.CreateSkill(ctx, skills.CreateInput{
		ID:           input.ID,
		Name:         input.Name,
		Version:      input.Version,
		Category:     input.Category,
		Summary:      input.Summary,
		PromotedFrom: input.PromotedFrom,
		Origin:       input.Origin,
		TaskPattern:  input.TaskPattern,
		Instruction:  input.Instruction,
		Tags:         append([]string(nil), input.Tags...),
		Platforms:    append([]string(nil), input.Platforms...),
		TriggerHints: append([]string(nil), input.TriggerHints...),
		Requires:     skills.CopyRequirements(input.Requires),
	})
	if err != nil {
		return nil, translateSkillStoreError(err)
	}
	view, err := skills.Evaluate(*spec, staticSkillEligibilityContext(s.cfg))
	if err != nil {
		return nil, err
	}
	copied := skills.CopyView(view)
	return &copied, nil
}

func (s *SkillService) Patch(ctx context.Context, id, content, source string) (*skills.View, error) {
	if s == nil || s.scanner == nil {
		return nil, errors.New("stable skill scanner is nil")
	}
	if err := s.scanner.PatchSkillWithSource(ctx, id, content, source); err != nil {
		return nil, translateSkillStoreError(err)
	}
	return s.Get(ctx, id)
}

func (s *SkillService) Delete(ctx context.Context, id string) error {
	if s == nil || s.scanner == nil {
		return errors.New("stable skill scanner is nil")
	}
	return translateSkillStoreError(s.scanner.DeleteSkill(ctx, id))
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
	case errors.Is(err, skills.ErrAlreadyExists):
		return fmt.Errorf("%w: %v", ErrSkillAlreadyExists, err)
	case errors.Is(err, skills.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrSkillNotFound, err)
	default:
		return err
	}
}
