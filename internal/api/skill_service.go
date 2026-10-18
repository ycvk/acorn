package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
	"github.com/ycvk/acorn/internal/tools"
)

// SkillLibrary is the skill collection as the owner reads and changes it:
// the skills in use, every stored skill with its history, and the owner's
// own changes, which no approval gates.
type SkillLibrary interface {
	Scan(ctx context.Context) (*skills.ScanResult, error)
	Entries(ctx context.Context) ([]skills.Entry, error)
	Entry(ctx context.Context, name string) (skills.Entry, error)
	Revisions(ctx context.Context, name string) ([]core.SkillRevision, error)
	Version(ctx context.Context, name string, revision int64) (skills.Version, error)
	MissingSecrets(ctx context.Context, spec skills.Spec) ([]string, error)
	Import(ctx context.Context, entries []core.SkillFile, replace bool) (core.Skill, error)
	RetireForOwner(ctx context.Context, name string, baseRevision int64) (core.Skill, error)
	Restore(ctx context.Context, name string, baseRevision int64) (core.Skill, error)
	Rollback(ctx context.Context, name string, revision, baseRevision int64) (core.Skill, error)
	SetSecret(ctx context.Context, name, secret, value string) error
	ClearSecret(ctx context.Context, name, secret string) error
}

// PackageStatus reads how the packages of the skills in use run.
type PackageStatus interface {
	Servers(ctx context.Context) ([]skills.ServerReport, error)
	Triggers(ctx context.Context) ([]skills.TriggerReport, error)
}

type SkillService struct {
	cfg     *config.Config
	library SkillLibrary
	status  PackageStatus
}

func NewSkillService(cfg *config.Config, library SkillLibrary, status PackageStatus) (*SkillService, error) {
	if library == nil || status == nil {
		return nil, errors.New("skill service: library and package status are required")
	}
	return &SkillService{cfg: cfg, library: library, status: status}, nil
}

// Import installs a package the owner read from a directory or zip. An
// existing skill of the same name is replaced only when replace is set.
func (s *SkillService) Import(ctx context.Context, entries []core.SkillFile, replace bool) (core.Skill, error) {
	return s.library.Import(ctx, entries, replace)
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

// List is the skills in use as a run sees them, at most limit of them when
// limit is positive.
func (s *SkillService) List(ctx context.Context, limit int) ([]skills.View, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	items := snapshot.Skills
	if limit > 0 && limit < len(items) {
		items = items[:limit]
	}
	return items, nil
}

// Get is one skill in use as a run sees it.
func (s *SkillService) Get(ctx context.Context, name string) (*skills.View, error) {
	snapshot, err := s.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	for _, item := range snapshot.Skills {
		if item.Name == name {
			return new(skills.CopyView(item)), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", core.ErrSkillNotFound, name)
}

// SkillOverview is a stored skill with whether the next run lists it, and
// why not. A retired skill or one that does not parse is never listed.
type SkillOverview struct {
	Entry    skills.Entry
	Eligible bool
	Reasons  []string
}

// Overview lists every stored skill, retired ones and ones that no longer
// parse included.
func (s *SkillService) Overview(ctx context.Context) ([]SkillOverview, error) {
	entries, err := s.library.Entries(ctx)
	if err != nil {
		return nil, err
	}
	eligibility := staticSkillEligibilityContext(s.cfg)
	out := make([]SkillOverview, 0, len(entries))
	for _, entry := range entries {
		overview, err := s.overview(ctx, entry, eligibility)
		if err != nil {
			return nil, err
		}
		out = append(out, overview)
	}
	return out, nil
}

// overview says whether the next run lists a skill: it is in use, parses,
// has the tools it requires and the values of its package's secrets.
func (s *SkillService) overview(ctx context.Context, entry skills.Entry, eligibility skills.EligibilityContext) (SkillOverview, error) {
	out := SkillOverview{Entry: entry}
	if entry.Skill.Retired || entry.Problem != "" {
		return out, nil
	}
	view := skills.Evaluate(entry.Spec, eligibility)
	missing, err := s.library.MissingSecrets(ctx, entry.Spec)
	if err != nil {
		return SkillOverview{}, err
	}
	if len(missing) > 0 {
		view = skills.Disable(view, "missing_secrets:"+strings.Join(missing, ","))
	}
	out.Eligible, out.Reasons = view.Eligible, view.DisabledReasons
	return out, nil
}

func (s *SkillService) overviewOf(ctx context.Context, name string) (SkillOverview, error) {
	entry, err := s.library.Entry(ctx, name)
	if err != nil {
		return SkillOverview{}, err
	}
	return s.overview(ctx, entry, staticSkillEligibilityContext(s.cfg))
}

// SkillPage is a skill as its page shows it: the overview, the content of
// its current revision, and its package when the skill has a manifest that
// parses.
type SkillPage struct {
	Overview SkillOverview
	Current  skills.Version
	Package  *SkillPackageState
}

// SkillPackageState is what a skill's package declares and how it runs.
// Server and Triggers report how the package runs only while the skill is in
// use; Secrets says which declared secrets have a value.
type SkillPackageState struct {
	Spec     skills.Spec
	Server   *skills.ServerReport
	Triggers []skills.TriggerReport
	Secrets  []SkillSecretState
}

// SkillSecretState says whether a secret a package declares has a value.
type SkillSecretState struct {
	Name string
	Set  bool
}

// Page reads a skill with its current content and how its package runs.
func (s *SkillService) Page(ctx context.Context, name string) (SkillPage, error) {
	overview, err := s.overviewOf(ctx, name)
	if err != nil {
		return SkillPage{}, err
	}
	entry := overview.Entry
	current, err := s.library.Version(ctx, entry.Skill.ID, entry.Skill.Revision)
	if err != nil {
		return SkillPage{}, err
	}
	page := SkillPage{Overview: overview, Current: current}
	if entry.Problem != "" || !skills.HasManifest(entry.Skill.Files) {
		return page, nil
	}
	state := &SkillPackageState{Spec: entry.Spec}
	if state.Secrets, err = s.secretStates(ctx, entry.Spec); err != nil {
		return SkillPage{}, err
	}
	if !entry.Skill.Retired {
		if entry.Spec.MCP != nil {
			servers, err := s.status.Servers(ctx)
			if err != nil {
				return SkillPage{}, err
			}
			if i := slices.IndexFunc(servers, func(r skills.ServerReport) bool { return r.Skill == entry.Skill.ID }); i >= 0 {
				state.Server = &servers[i]
			}
		}
		if len(entry.Spec.Triggers) > 0 {
			triggers, err := s.status.Triggers(ctx)
			if err != nil {
				return SkillPage{}, err
			}
			for _, t := range triggers {
				if t.Skill == entry.Skill.ID {
					state.Triggers = append(state.Triggers, t)
				}
			}
		}
	}
	page.Package = state
	return page, nil
}

func (s *SkillService) secretStates(ctx context.Context, spec skills.Spec) ([]SkillSecretState, error) {
	missing, err := s.library.MissingSecrets(ctx, spec)
	if err != nil {
		return nil, err
	}
	out := make([]SkillSecretState, 0, len(spec.Secrets))
	for _, name := range spec.Secrets {
		out = append(out, SkillSecretState{Name: name, Set: !slices.Contains(missing, name)})
	}
	return out, nil
}

// Revisions lists a skill's revisions, newest first.
func (s *SkillService) Revisions(ctx context.Context, name string) ([]core.SkillRevision, error) {
	return s.library.Revisions(ctx, name)
}

// Revision reads one revision of a skill with its files.
func (s *SkillService) Revision(ctx context.Context, name string, revision int64) (skills.Version, error) {
	return s.library.Version(ctx, name, revision)
}

// Diff compares two revisions of a skill file by file.
func (s *SkillService) Diff(ctx context.Context, name string, from, to int64) ([]skills.FileDiff, error) {
	older, err := s.library.Version(ctx, name, from)
	if err != nil {
		return nil, err
	}
	newer, err := s.library.Version(ctx, name, to)
	if err != nil {
		return nil, err
	}
	return skills.Diff(older, newer), nil
}

// Rollback writes the content of an earlier revision as the owner's
// revision on top of baseRevision; the skill is in use after it.
func (s *SkillService) Rollback(ctx context.Context, name string, revision, baseRevision int64) (SkillOverview, error) {
	skill, err := s.library.Rollback(ctx, name, revision, baseRevision)
	if err != nil {
		return SkillOverview{}, err
	}
	return s.overviewOf(ctx, skill.ID)
}

// Retire takes a skill out of use as the owner's revision on top of
// baseRevision.
func (s *SkillService) Retire(ctx context.Context, name string, baseRevision int64) (SkillOverview, error) {
	skill, err := s.library.RetireForOwner(ctx, name, baseRevision)
	if err != nil {
		return SkillOverview{}, err
	}
	return s.overviewOf(ctx, skill.ID)
}

// Restore brings a retired skill back into use as the owner's revision on
// top of baseRevision.
func (s *SkillService) Restore(ctx context.Context, name string, baseRevision int64) (SkillOverview, error) {
	skill, err := s.library.Restore(ctx, name, baseRevision)
	if err != nil {
		return SkillOverview{}, err
	}
	return s.overviewOf(ctx, skill.ID)
}

// SetSecret stores the value of a secret a skill's package declares; its
// MCP server and trigger commands get it from the next run on. It returns
// which declared secrets have a value, never a value.
func (s *SkillService) SetSecret(ctx context.Context, name, secret, value string) ([]SkillSecretState, error) {
	if err := s.library.SetSecret(ctx, name, secret, value); err != nil {
		return nil, err
	}
	return s.secretsOf(ctx, name)
}

// ClearSecret removes the value of a secret a skill's package declares. It
// returns which declared secrets have a value.
func (s *SkillService) ClearSecret(ctx context.Context, name, secret string) ([]SkillSecretState, error) {
	if err := s.library.ClearSecret(ctx, name, secret); err != nil {
		return nil, err
	}
	return s.secretsOf(ctx, name)
}

func (s *SkillService) secretsOf(ctx context.Context, name string) ([]SkillSecretState, error) {
	entry, err := s.library.Entry(ctx, name)
	if err != nil {
		return nil, err
	}
	return s.secretStates(ctx, entry.Spec)
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
