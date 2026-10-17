package skills

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// seedFS holds the built-in skills that ship with the binary, one directory
// per skill named after it.
//
//go:embed seed
var seedFS embed.FS

// stageLifetime is how long a package read for import waits for approval.
const stageLifetime = 7 * 24 * time.Hour

// Library is the skill collection over the skill store: the built-in seed, the
// agent's and the owner's writes, and the parsed view runs and clients read.
type Library struct {
	store core.SkillStore
	clock func() time.Time
	// providers are the names the configured MCP providers' tools are
	// namespaced under; a package's MCP server cannot take one.
	providers []string
}

func NewLibrary(store core.SkillStore, clock func() time.Time, providers []string) (*Library, error) {
	switch {
	case store == nil:
		return nil, errors.New("skill library: store is required")
	case clock == nil:
		return nil, errors.New("skill library: clock is required")
	}
	return &Library{store: store, clock: clock, providers: slices.Clone(providers)}, nil
}

// Package is a skill with the files it carries.
type Package struct {
	Spec     Spec
	Markdown string
	Files    []core.SkillFile
}

// BuiltIn reads the skills that ship with the binary.
func BuiltIn() ([]Package, error) {
	dirs, err := fs.ReadDir(seedFS, "seed")
	if err != nil {
		return nil, fmt.Errorf("list built-in skills: %w", err)
	}
	var packages []Package
	for _, dir := range dirs {
		root := path.Join("seed", dir.Name())
		var entries []core.SkillFile
		err := fs.WalkDir(seedFS, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			content, err := seedFS.ReadFile(p)
			if err != nil {
				return err
			}
			entries = append(entries, core.SkillFile{Path: strings.TrimPrefix(p, root+"/"), Content: content})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read built-in skill %s: %w", dir.Name(), err)
		}
		pkg, err := packageOf(entries)
		if err != nil {
			return nil, fmt.Errorf("built-in skill %s: %w", dir.Name(), err)
		}
		if pkg.Spec.Name != dir.Name() {
			return nil, fmt.Errorf("built-in skill directory %s holds skill %s", dir.Name(), pkg.Spec.Name)
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// packageOf checks the files read from a skill's directory or archive.
func packageOf(entries []core.SkillFile) (Package, error) {
	markdown, files, err := FromEntries(entries)
	if err != nil {
		return Package{}, err
	}
	markdown = normalizeMarkdown(markdown)
	spec, err := validateSkill(markdown)
	if err != nil {
		return Package{}, err
	}
	files, err = normalizeFiles(markdown, files)
	if err != nil {
		return Package{}, err
	}
	if spec.MCP, err = mcpOf(files); err != nil {
		return Package{}, err
	}
	return Package{Spec: spec, Markdown: markdown, Files: files}, nil
}

// mcpOf is the MCP server a package's manifest declares, nil without a
// manifest.
func mcpOf(files []core.SkillFile) (*MCPServer, error) {
	m, ok, err := ManifestOf(files)
	if err != nil || !ok {
		return nil, err
	}
	return m.MCP, nil
}

// checkProvider rejects a package whose MCP server would take the name of a
// configured MCP provider, which would give two servers the same tool names.
func (l *Library) checkProvider(name string, server *MCPServer) error {
	if server != nil && slices.Contains(l.providers, name) {
		return fmt.Errorf("skill %s declares an MCP server, and a provider in mcp.providers already names its tools %s*; rename the skill or the provider", name, MCPToolPrefix(name))
	}
	return nil
}

// CheckProviders fails when a skill in use declares an MCP server under the
// name of a configured MCP provider, as when a provider is added to the
// config after the skill.
func (l *Library) CheckProviders(ctx context.Context) error {
	scan, err := l.Scan(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, spec := range scan.Skills {
		errs = append(errs, l.checkProvider(spec.Name, spec.MCP))
	}
	return errors.Join(errs...)
}

// Seed writes every built-in skill whose shipped package differs from what a
// release last wrote, as a new revision on top of the current one. A skill
// keeps its retired state.
func (l *Library) Seed(ctx context.Context) error {
	builtIn, err := BuiltIn()
	if err != nil {
		return err
	}
	for _, pkg := range builtIn {
		seed := PackageHash(pkg.Markdown, pkg.Files)
		write := core.SkillWrite{ID: pkg.Spec.Name, Markdown: pkg.Markdown, Files: pkg.Files, Author: core.SkillAuthorRelease, SeedSHA256: seed, At: l.clock()}
		current, err := l.store.Skill(ctx, pkg.Spec.Name)
		switch {
		case errors.Is(err, core.ErrSkillNotFound):
		case err != nil:
			return err
		case current.SeedSHA256 == seed:
			continue
		default:
			write.BaseRevision, write.Retired = current.Revision, current.Retired
		}
		if _, err := l.store.WriteSkill(ctx, write); err != nil {
			return err
		}
	}
	return nil
}

// Scan parses the skills in use. A stored skill that no longer parses is a
// problem, not a skill.
func (l *Library) Scan(ctx context.Context) (*ScanResult, error) {
	stored, err := l.store.Skills(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Spec, 0, len(stored))
	problems := []Problem{}
	for _, skill := range stored {
		if skill.Retired {
			continue
		}
		spec, err := Parse(skill.Markdown)
		if err != nil {
			problems = append(problems, Problem{Name: skill.ID, Error: fmt.Sprintf("parse skill: %v", err)})
			continue
		}
		spec.Revision, spec.Markdown, spec.Files = skill.Revision, skill.Markdown, slices.Clone(skill.Files)
		if slices.ContainsFunc(skill.Files, func(f core.SkillFileInfo) bool { return f.Path == ManifestFileName }) {
			content, err := l.store.SkillFile(ctx, skill.ID, skill.Revision, ManifestFileName)
			if err != nil {
				return nil, err
			}
			m, err := ParseManifest(content)
			if err != nil {
				problems = append(problems, Problem{Name: skill.ID, Error: err.Error()})
				continue
			}
			spec.MCP = m.MCP
		}
		items = append(items, spec)
	}
	sortSkills(items)
	sortSkillProblems(problems)
	return &ScanResult{Skills: items, Problems: problems}, nil
}

// Skill reads a stored skill, retired or not.
func (l *Library) Skill(ctx context.Context, name string) (core.Skill, error) {
	return l.store.Skill(ctx, strings.TrimSpace(name))
}

// WriteRequest is the agent's change to a skill: its SKILL.md, text files to
// add or replace, and files to remove, based on BaseRevision, zero for a new
// skill.
type WriteRequest struct {
	Markdown     string
	Put          map[string]string
	Delete       []string
	BaseRevision int64
	RunID        string
}

// Write stores the agent's change as a new revision. Writing a retired skill
// brings it back.
func (l *Library) Write(ctx context.Context, req WriteRequest) (core.Skill, error) {
	markdown := normalizeMarkdown(req.Markdown)
	spec, err := validateSkill(markdown)
	if err != nil {
		return core.Skill{}, err
	}
	var files []core.SkillFile
	if req.BaseRevision > 0 {
		if files, err = l.store.SkillFiles(ctx, spec.Name, req.BaseRevision); err != nil {
			return core.Skill{}, err
		}
	}
	for _, p := range req.Delete {
		i := slices.IndexFunc(files, func(f core.SkillFile) bool { return f.Path == p })
		if i < 0 {
			return core.Skill{}, fmt.Errorf("skill %s has no file %q to delete", spec.Name, p)
		}
		files = slices.Delete(files, i, i+1)
	}
	for p, content := range req.Put {
		files = slices.DeleteFunc(files, func(f core.SkillFile) bool { return f.Path == p })
		files = append(files, core.SkillFile{Path: p, Content: []byte(content)})
	}
	if files, err = normalizeFiles(markdown, files); err != nil {
		return core.Skill{}, err
	}
	server, err := mcpOf(files)
	if err != nil {
		return core.Skill{}, err
	}
	if err := l.checkProvider(spec.Name, server); err != nil {
		return core.Skill{}, err
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: spec.Name, Markdown: markdown, Files: files, Author: core.SkillAuthorAgent, RunID: req.RunID, At: l.clock(), BaseRevision: req.BaseRevision})
}

// Retire takes a skill out of use as a new revision; writing it again brings
// it back.
func (l *Library) Retire(ctx context.Context, name string, baseRevision int64, runID string) (core.Skill, error) {
	current, err := l.store.Skill(ctx, strings.TrimSpace(name))
	if err != nil {
		return core.Skill{}, err
	}
	if current.Retired {
		return core.Skill{}, fmt.Errorf("skill %s is already retired", current.ID)
	}
	files, err := l.store.SkillFiles(ctx, current.ID, current.Revision)
	if err != nil {
		return core.Skill{}, err
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: current.ID, Markdown: current.Markdown, Files: files, Retired: true, Author: core.SkillAuthorAgent, RunID: runID, At: l.clock(), BaseRevision: baseRevision})
}

// Stage checks a package read from a directory or zip and keeps it under its
// hash for import.
func (l *Library) Stage(ctx context.Context, entries []core.SkillFile) (core.SkillPackage, error) {
	pkg, err := packageOf(entries)
	if err != nil {
		return core.SkillPackage{}, err
	}
	if err := l.checkProvider(pkg.Spec.Name, pkg.Spec.MCP); err != nil {
		return core.SkillPackage{}, err
	}
	now := l.clock()
	staged := core.SkillPackage{SHA256: PackageHash(pkg.Markdown, pkg.Files), Name: pkg.Spec.Name, Markdown: pkg.Markdown, Files: pkg.Files, StagedAt: now}
	if err := l.store.StageSkillPackage(ctx, staged, now.Add(-stageLifetime)); err != nil {
		return core.SkillPackage{}, err
	}
	return staged, nil
}

// ImportStaged stores the staged package with that hash, which must hold the
// named skill, as the agent's revision based on baseRevision, zero for a new
// skill.
func (l *Library) ImportStaged(ctx context.Context, name, sha256 string, baseRevision int64, runID string) (core.Skill, error) {
	pkg, err := l.store.StagedSkillPackage(ctx, strings.TrimSpace(sha256))
	if err != nil {
		return core.Skill{}, err
	}
	if pkg.Name != strings.TrimSpace(name) {
		return core.Skill{}, fmt.Errorf("staged package %s holds skill %s, not %s", pkg.SHA256, pkg.Name, name)
	}
	server, err := mcpOf(pkg.Files)
	if err != nil {
		return core.Skill{}, err
	}
	if err := l.checkProvider(pkg.Name, server); err != nil {
		return core.Skill{}, err
	}
	skill, err := l.store.WriteSkill(ctx, core.SkillWrite{ID: pkg.Name, Markdown: pkg.Markdown, Files: pkg.Files, Author: core.SkillAuthorAgent, RunID: runID, At: l.clock(), BaseRevision: baseRevision})
	if err != nil {
		return core.Skill{}, err
	}
	return skill, l.store.DeleteStagedSkillPackage(ctx, sha256)
}

// Import stores a package the owner installs. An existing skill of the same
// name is replaced only when replace is set.
func (l *Library) Import(ctx context.Context, entries []core.SkillFile, replace bool) (core.Skill, error) {
	pkg, err := packageOf(entries)
	if err != nil {
		return core.Skill{}, err
	}
	if err := l.checkProvider(pkg.Spec.Name, pkg.Spec.MCP); err != nil {
		return core.Skill{}, err
	}
	var base int64
	current, err := l.store.Skill(ctx, pkg.Spec.Name)
	switch {
	case errors.Is(err, core.ErrSkillNotFound):
	case err != nil:
		return core.Skill{}, err
	case !replace:
		return core.Skill{}, fmt.Errorf("skill %s exists at revision %d; replace it explicitly", current.ID, current.Revision)
	default:
		base = current.Revision
	}
	return l.store.WriteSkill(ctx, core.SkillWrite{ID: pkg.Spec.Name, Markdown: pkg.Markdown, Files: pkg.Files, Author: core.SkillAuthorOwner, At: l.clock(), BaseRevision: base})
}

// PackageHost keeps skill packages where the agent can read and run them.
type PackageHost interface {
	// SkillPackages reports the directory packages live in and the hash of
	// each package there.
	SkillPackages(ctx context.Context) (dir string, hashes map[string]string, err error)
	PutSkillPackage(ctx context.Context, name, hash, markdown string, files []core.SkillFile) error
	DeleteSkillPackage(ctx context.Context, name string) error
}

// Sync makes the host hold exactly the eligible skills that carry files, and
// returns the directory they are in.
func (l *Library) Sync(ctx context.Context, host PackageHost, snapshot Snapshot) (string, error) {
	dir, held, err := host.SkillPackages(ctx)
	if err != nil {
		return "", fmt.Errorf("list skill packages: %w", err)
	}
	wanted := map[string]bool{}
	for _, view := range snapshot.Skills {
		if !view.Eligible || len(view.Files) == 0 {
			continue
		}
		wanted[view.Name] = true
		hash := view.Hash()
		if held[view.Name] == hash {
			continue
		}
		files, err := l.store.SkillFiles(ctx, view.Name, view.Revision)
		if err != nil {
			return "", err
		}
		if err := host.PutSkillPackage(ctx, view.Name, hash, view.Markdown, files); err != nil {
			return "", fmt.Errorf("put skill package %s: %w", view.Name, err)
		}
	}
	for name := range held {
		if wanted[name] {
			continue
		}
		if err := host.DeleteSkillPackage(ctx, name); err != nil {
			return "", fmt.Errorf("delete skill package %s: %w", name, err)
		}
	}
	return dir, nil
}

// SetSecret stores the value of a secret the MCP server of a skill's current
// revision declares.
func (l *Library) SetSecret(ctx context.Context, name, secret, value string) error {
	skill, err := l.store.Skill(ctx, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(skill.Files, func(f core.SkillFileInfo) bool { return f.Path == ManifestFileName }) {
		return fmt.Errorf("skill %s has no %s and takes no secrets", skill.ID, ManifestFileName)
	}
	content, err := l.store.SkillFile(ctx, skill.ID, skill.Revision, ManifestFileName)
	if err != nil {
		return err
	}
	m, err := ParseManifest(content)
	if err != nil {
		return err
	}
	if !slices.Contains(m.MCP.Secrets, secret) {
		return fmt.Errorf("skill %s's MCP server declares the secrets %v, not %s", skill.ID, m.MCP.Secrets, secret)
	}
	if value == "" {
		return fmt.Errorf("the value of %s is empty", secret)
	}
	return l.store.SetSkillSecret(ctx, skill.ID, secret, value, l.clock())
}

// MissingSecrets lists the secrets a skill's MCP server declares that have no
// value.
func (l *Library) MissingSecrets(ctx context.Context, spec Spec) ([]string, error) {
	_, missing, err := l.secretEnv(ctx, spec)
	return missing, err
}

// secretEnv is the environment of a skill's MCP server, from the values of
// its secrets, and the secrets that have none.
func (l *Library) secretEnv(ctx context.Context, spec Spec) (map[string]string, []string, error) {
	if spec.MCP == nil {
		return nil, nil, nil
	}
	values, err := l.store.SkillSecrets(ctx, spec.Name)
	if err != nil {
		return nil, nil, err
	}
	env := map[string]string{}
	var missing []string
	for _, name := range spec.MCP.Secrets {
		value, ok := values[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		env[name] = value
	}
	return env, missing, nil
}

// MCPServerRun is a package's MCP server as a host runs it: from the package
// it holds under Name at Hash, with Env from the package's secrets.
type MCPServerRun struct {
	Name string
	Hash string
	Env  map[string]string
}

// ServerHost runs the MCP servers of the packages it holds.
type ServerHost interface {
	// RunMCPServers makes the host run exactly these servers, stopping any
	// other, and returns why each of them that does not serve fails, by name.
	RunMCPServers(ctx context.Context, servers []MCPServerRun) (map[string]string, error)
}

// Serve has the host run the MCP server of every eligible skill that declares
// one, with its secrets, and stop the others. A skill whose secrets are not
// all set, or whose server does not serve, leaves the snapshot with the
// reason.
func (l *Library) Serve(ctx context.Context, host ServerHost, snapshot Snapshot) (Snapshot, error) {
	out := CopySnapshot(snapshot)
	runs := []MCPServerRun{}
	index := map[string]int{}
	for i, view := range out.Skills {
		if !view.Eligible || view.MCP == nil {
			continue
		}
		env, missing, err := l.secretEnv(ctx, view.Spec)
		if err != nil {
			return Snapshot{}, err
		}
		if len(missing) > 0 {
			out.Skills[i] = Disable(view, "missing_secrets:"+strings.Join(missing, ","))
			continue
		}
		runs = append(runs, MCPServerRun{Name: view.Name, Hash: view.Hash(), Env: env})
		index[view.Name] = i
	}
	failed, err := host.RunMCPServers(ctx, runs)
	if err != nil {
		return Snapshot{}, fmt.Errorf("run MCP servers: %w", err)
	}
	for name, reason := range failed {
		if i, ok := index[name]; ok {
			out.Skills[i] = Disable(out.Skills[i], "mcp_server_failed:"+reason)
		}
	}
	return out, nil
}

// Disable takes a skill out of a run for the reason.
func Disable(view View, reason string) View {
	out := CopyView(view)
	out.Eligible = false
	out.DisabledReasons = append(out.DisabledReasons, reason)
	return out
}

func validateSkill(markdown string) (Spec, error) {
	spec, err := Parse(markdown)
	if err != nil {
		return Spec{}, err
	}
	if spec.Instruction == "" {
		return Spec{}, fmt.Errorf("skill %s has no instruction", spec.Name)
	}
	return spec, nil
}

func normalizeMarkdown(markdown string) string {
	text := strings.TrimPrefix(strings.ReplaceAll(markdown, "\r\n", "\n"), "\uFEFF")
	return strings.TrimSpace(text) + "\n"
}
