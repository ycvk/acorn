package skills

import (
	"fmt"
	"strings"
)

type Origin string

const (
	OriginHuman     Origin = "human"
	OriginDistilled Origin = "distilled"
)

type Source string

const (
	SourceBuiltin   Source = "builtin"
	SourceWorkspace Source = "workspace"
	SourceGenerated Source = "generated"
	SourceUser      Source = "user"
)

type LifecycleStatus string

const (
	LifecycleDraft      LifecycleStatus = "draft"
	LifecycleVerified   LifecycleStatus = "verified"
	LifecycleUnverified LifecycleStatus = "unverified"
	LifecycleNeedsEval  LifecycleStatus = "needs_eval"
	LifecycleRetired    LifecycleStatus = "retired"
)

type ResourceSpec struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
}

type CreatorOutput struct {
	SkillID       string         `json:"skill_id"`
	Directory     string         `json:"directory"`
	SkillMarkdown string         `json:"skill_markdown"`
	Description   string         `json:"description"`
	TriggerHints  []string       `json:"trigger_hints,omitempty"`
	Resources     []ResourceSpec `json:"resources,omitempty"`
	Scripts       []ResourceSpec `json:"scripts,omitempty"`
	SourceRunID   string         `json:"source_run_id,omitempty"`
	EvidenceRefs  []string       `json:"evidence_refs,omitempty"`
}

type AssessmentVerdict string

const (
	AssessmentVerified  AssessmentVerdict = "verified"
	AssessmentNeedsEval AssessmentVerdict = "needs_eval"
	AssessmentRetired   AssessmentVerdict = "retired"
)

type SkillAssessment struct {
	AssessmentID    string            `json:"assessment_id"`
	SkillID         string            `json:"skill_id"`
	Verdict         AssessmentVerdict `json:"verdict"`
	Reason          string            `json:"reason"`
	SourceRunID     string            `json:"source_run_id,omitempty"`
	EvidenceRefs    []string          `json:"evidence_refs,omitempty"`
	ChangesRequired []string          `json:"changes_required,omitempty"`
	Applied         bool              `json:"applied"`
}

type LifecycleEvent struct {
	SkillID         string           `json:"skill_id"`
	Action          string           `json:"action"`
	Status          string           `json:"status,omitempty"`
	Verdict         string           `json:"verdict,omitempty"`
	Reason          string           `json:"reason,omitempty"`
	EvidenceRefs    []string         `json:"evidence_refs,omitempty"`
	AssessmentID    string           `json:"assessment_id,omitempty"`
	ChangesRequired []string         `json:"changes_required,omitempty"`
	Applied         bool             `json:"applied"`
	Assessment      *SkillAssessment `json:"assessment,omitempty"`
}

type Spec struct {
	ID              string
	Name            string
	Version         string
	Category        string
	Summary         string
	Description     string
	Instruction     string
	PromotedFrom    string
	Source          string
	Origin          Origin
	LifecycleStatus LifecycleStatus
	TaskPattern     string
	Path            string
	Scripts         []string
	Files           []string
	Tags            []string
	Platforms       []string
	TriggerHints    []string
	Requires        Requirements
	CreatedByRunID  string
	UpdatedByRunID  string
	EvidenceRefs    []string
	Replaces        []string
	ReplacedBy      []string
}

type Requirements struct {
	Tools    []string `json:"tools,omitempty"`
	Toolsets []string `json:"toolsets,omitempty"`
	Bins     []string `json:"bins,omitempty"`
	Env      []string `json:"env,omitempty"`
}

type Problem struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Source string `json:"source,omitempty"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ScanResult struct {
	Skills   []Spec    `json:"skills,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

type View struct {
	Spec
	Eligible        bool     `json:"eligible"`
	DisabledReasons []string `json:"disabled_reasons,omitempty"`
}

type Snapshot struct {
	Skills   []View    `json:"skills,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

func CopyRequirements(item Requirements) Requirements {
	return Requirements{
		Tools:    append([]string(nil), item.Tools...),
		Toolsets: append([]string(nil), item.Toolsets...),
		Bins:     append([]string(nil), item.Bins...),
		Env:      append([]string(nil), item.Env...),
	}
}

func NormalizeSpec(item Spec) (Spec, error) {
	item.ID = strings.TrimSpace(item.ID)
	item.Name = strings.TrimSpace(item.Name)
	item.Version = strings.TrimSpace(item.Version)
	item.Category = strings.TrimSpace(item.Category)
	item.Summary = strings.TrimSpace(item.Summary)
	item.Description = strings.TrimSpace(item.Description)
	item.Instruction = strings.TrimSpace(item.Instruction)
	item.PromotedFrom = strings.TrimSpace(item.PromotedFrom)
	item.Source = strings.TrimSpace(item.Source)
	item.Origin = normalizeOrigin(item.Origin)
	item.LifecycleStatus = normalizeLifecycleStatus(item.LifecycleStatus)
	item.TaskPattern = strings.TrimSpace(item.TaskPattern)
	item.Path = strings.TrimSpace(item.Path)
	item.Scripts = uniqueNonEmpty(item.Scripts)
	item.Files = uniqueNonEmpty(item.Files)
	item.Tags = uniqueNonEmpty(item.Tags)
	item.Platforms = uniqueLowerNonEmpty(item.Platforms)
	item.TriggerHints = uniqueNonEmpty(item.TriggerHints)
	item.Requires = NormalizeRequirements(item.Requires)
	item.CreatedByRunID = strings.TrimSpace(item.CreatedByRunID)
	item.UpdatedByRunID = strings.TrimSpace(item.UpdatedByRunID)
	item.EvidenceRefs = uniqueNonEmpty(item.EvidenceRefs)
	item.Replaces = uniqueNonEmpty(item.Replaces)
	item.ReplacedBy = uniqueNonEmpty(item.ReplacedBy)
	if item.ID == "" {
		return Spec{}, fmt.Errorf("skill id is required")
	}
	if item.Name == "" {
		return Spec{}, fmt.Errorf("skill %s name is required", item.ID)
	}
	if item.Version == "" {
		item.Version = "v1"
	}
	if item.Source == "" {
		item.Source = "unknown"
	}
	if item.LifecycleStatus == "" {
		item.LifecycleStatus = defaultLifecycleStatus(item.Source)
	}
	if err := validateLifecycleStatus(item.LifecycleStatus); err != nil {
		return Spec{}, fmt.Errorf("skill %s %w", item.ID, err)
	}
	if item.LifecycleStatus == LifecycleVerified && item.Source != string(SourceBuiltin) && len(item.EvidenceRefs) == 0 {
		return Spec{}, fmt.Errorf("skill %s lifecycle_status verified requires evidence_refs", item.ID)
	}
	switch item.Origin {
	case "", OriginHuman:
		item.Origin = OriginHuman
	case OriginDistilled:
		if item.TaskPattern == "" {
			return Spec{}, fmt.Errorf("skill %s task_pattern is required for distilled origin", item.ID)
		}
	default:
		return Spec{}, fmt.Errorf("skill %s origin %q is invalid", item.ID, item.Origin)
	}
	return item, nil
}

func normalizeOrigin(origin Origin) Origin {
	return Origin(strings.TrimSpace(string(origin)))
}

func normalizeLifecycleStatus(status LifecycleStatus) LifecycleStatus {
	return LifecycleStatus(strings.TrimSpace(string(status)))
}

func defaultLifecycleStatus(source string) LifecycleStatus {
	switch strings.TrimSpace(source) {
	case string(SourceBuiltin):
		return LifecycleVerified
	case string(SourceGenerated):
		return LifecycleDraft
	default:
		return LifecycleUnverified
	}
}

func validateLifecycleStatus(status LifecycleStatus) error {
	switch status {
	case LifecycleDraft, LifecycleVerified, LifecycleUnverified, LifecycleNeedsEval, LifecycleRetired:
		return nil
	default:
		return fmt.Errorf("lifecycle_status %q is invalid", status)
	}
}

func NormalizeRequirements(item Requirements) Requirements {
	return Requirements{
		Tools:    uniqueNonEmpty(item.Tools),
		Toolsets: uniqueNonEmpty(item.Toolsets),
		Bins:     uniqueNonEmpty(item.Bins),
		Env:      uniqueNonEmpty(item.Env),
	}
}

func CopySpec(item Spec) Spec {
	copy := item
	copy.Scripts = append([]string(nil), item.Scripts...)
	copy.Files = append([]string(nil), item.Files...)
	copy.Tags = append([]string(nil), item.Tags...)
	copy.Platforms = append([]string(nil), item.Platforms...)
	copy.TriggerHints = append([]string(nil), item.TriggerHints...)
	copy.Requires = CopyRequirements(item.Requires)
	copy.EvidenceRefs = append([]string(nil), item.EvidenceRefs...)
	copy.Replaces = append([]string(nil), item.Replaces...)
	copy.ReplacedBy = append([]string(nil), item.ReplacedBy...)
	return copy
}

func CopyView(item View) View {
	copy := item
	copy.Spec = CopySpec(item.Spec)
	copy.DisabledReasons = append([]string(nil), item.DisabledReasons...)
	return copy
}

func CopySnapshot(item Snapshot) Snapshot {
	copy := Snapshot{
		Skills:   make([]View, 0, len(item.Skills)),
		Problems: make([]Problem, 0, len(item.Problems)),
	}
	for _, current := range item.Skills {
		copy.Skills = append(copy.Skills, CopyView(current))
	}
	copy.Problems = append(copy.Problems, item.Problems...)
	return copy
}

func uniqueNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func uniqueLowerNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.ToLower(strings.TrimSpace(item))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
