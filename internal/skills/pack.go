package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type PackManifest struct {
	PackID       string
	Version      string
	Skills       []PackSkill
	Dependencies []string
}

type PackSkill struct {
	ID    string
	Files map[string]string
}

type PackTargetSource string

const (
	PackTargetWorkspace PackTargetSource = PackTargetSource(WorkspaceScope)
	PackTargetGenerated PackTargetSource = PackTargetSource(GeneratedScope)
	PackTargetUser      PackTargetSource = PackTargetSource(UserScope)
)

type PackApplyOptions struct {
	Destructive bool
	Now         time.Time
}

type PackPlan struct {
	PackID  string
	Version string
	Target  string
	Actions []PackFileAction
}

type PackFileAction struct {
	Action  PackAction
	SkillID string
	Path    string
	SHA256  string
	Managed bool
}

type PackAction string

const (
	PackActionCreate PackAction = "create"
	PackActionUpdate PackAction = "update"
	PackActionNoop   PackAction = "noop"
)

type PackReceipt struct {
	PackID      string            `json:"pack_id"`
	Version     string            `json:"version"`
	Target      string            `json:"target"`
	InstalledAt time.Time         `json:"installed_at"`
	Files       []PackReceiptFile `json:"files"`
	Skills      []string          `json:"skills"`
}

type PackReceiptFile struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Managed bool   `json:"managed"`
}

func (l *Loader) PlanSkillPack(ctx context.Context, manifest PackManifest, target PackTargetSource) (*PackPlan, error) {
	normalized, err := normalizePackManifest(manifest)
	if err != nil {
		return nil, err
	}
	root, err := l.packTargetRoot(target)
	if err != nil {
		return nil, err
	}
	if err := l.validatePackDependencyClosure(ctx, normalized); err != nil {
		return nil, err
	}
	plan := &PackPlan{
		PackID:  normalized.PackID,
		Version: normalized.Version,
		Target:  string(target),
		Actions: make([]PackFileAction, 0),
	}
	for _, skill := range normalized.Skills {
		for _, rel := range sortedPackFilePaths(skill.Files) {
			content := skill.Files[rel]
			targetRel := filepath.ToSlash(filepath.Join(skill.ID, rel))
			targetPath := filepath.Join(root, filepath.FromSlash(targetRel))
			hash := sha256Text(content)
			action := PackActionCreate
			if existing, err := os.ReadFile(targetPath); err == nil {
				if sha256Text(string(existing)) == hash {
					action = PackActionNoop
				} else {
					action = PackActionUpdate
				}
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read skill pack target %s: %w", targetRel, err)
			}
			plan.Actions = append(plan.Actions, PackFileAction{
				Action:  action,
				SkillID: skill.ID,
				Path:    targetRel,
				SHA256:  hash,
				Managed: true,
			})
		}
	}
	return plan, nil
}

func (l *Loader) ApplySkillPack(ctx context.Context, manifest PackManifest, target PackTargetSource, options PackApplyOptions) (*PackReceipt, error) {
	normalized, err := normalizePackManifest(manifest)
	if err != nil {
		return nil, err
	}
	root, err := l.packTargetRoot(target)
	if err != nil {
		return nil, err
	}
	plan, err := l.PlanSkillPack(ctx, normalized, target)
	if err != nil {
		return nil, err
	}
	if err := l.validatePackOverwrites(root, *plan, options.Destructive); err != nil {
		return nil, err
	}
	for _, skill := range normalized.Skills {
		for _, rel := range sortedPackFilePaths(skill.Files) {
			targetPath := filepath.Join(root, skill.ID, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
				return nil, fmt.Errorf("create skill pack parent: %w", err)
			}
			if err := os.WriteFile(targetPath, []byte(strings.ReplaceAll(skill.Files[rel], "\r\n", "\n")), 0o644); err != nil {
				return nil, fmt.Errorf("write skill pack file %s/%s: %w", skill.ID, rel, err)
			}
		}
		if _, problem := loadSkillDir(filepath.Join(root, skill.ID), string(target)); problem != nil {
			return nil, fmt.Errorf("validate installed skill %s: %s", skill.ID, problem.Error)
		}
	}
	receipt := &PackReceipt{
		PackID:      normalized.PackID,
		Version:     normalized.Version,
		Target:      string(target),
		InstalledAt: options.Now,
		Skills:      packSkillIDs(normalized.Skills),
		Files:       make([]PackReceiptFile, 0, len(plan.Actions)),
	}
	if receipt.InstalledAt.IsZero() {
		receipt.InstalledAt = time.Now().UTC()
	} else {
		receipt.InstalledAt = receipt.InstalledAt.UTC()
	}
	for _, action := range plan.Actions {
		receipt.Files = append(receipt.Files, PackReceiptFile{
			Path:    action.Path,
			SHA256:  action.SHA256,
			Managed: action.Managed,
		})
	}
	if err := writePackReceipt(root, *receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func (l *Loader) validatePackDependencyClosure(ctx context.Context, manifest PackManifest) error {
	if len(manifest.Dependencies) == 0 {
		return nil
	}
	scan, err := l.ScanSkills(ctx)
	if err != nil {
		return fmt.Errorf("scan skills for dependency closure: %w", err)
	}
	available := make(map[string]struct{}, len(scan.Skills)+len(manifest.Skills))
	for _, item := range scan.Skills {
		available[item.ID] = struct{}{}
	}
	for _, item := range manifest.Skills {
		available[item.ID] = struct{}{}
	}
	for _, dep := range manifest.Dependencies {
		if _, exists := available[dep]; exists {
			continue
		}
		return fmt.Errorf("skill pack dependency %q is not available", dep)
	}
	return nil
}

func (l *Loader) validatePackOverwrites(root string, plan PackPlan, destructive bool) error {
	if destructive {
		return nil
	}
	receipt, err := readPackReceipt(root, plan.PackID)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	managed := make(map[string]string)
	if receipt != nil {
		for _, file := range receipt.Files {
			managed[file.Path] = file.SHA256
		}
	}
	for _, action := range plan.Actions {
		if action.Action != PackActionUpdate {
			continue
		}
		existingHash := managed[action.Path]
		if existingHash == "" {
			return fmt.Errorf("skill pack file %s already exists without managed receipt; rerun with destructive mode to overwrite", action.Path)
		}
		current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(action.Path)))
		if err != nil {
			return fmt.Errorf("read managed skill pack file %s: %w", action.Path, err)
		}
		if sha256Text(string(current)) != existingHash {
			return fmt.Errorf("skill pack file %s has unmanaged changes; rerun with destructive mode to overwrite", action.Path)
		}
	}
	return nil
}

func (l *Loader) packTargetRoot(target PackTargetSource) (string, error) {
	switch target {
	case PackTargetWorkspace:
		if strings.TrimSpace(l.workspaceDir) == "" {
			return "", fmt.Errorf("workspace skill directory is not configured")
		}
		return l.workspaceDir, nil
	case PackTargetGenerated:
		if strings.TrimSpace(l.generatedDir) == "" {
			return "", fmt.Errorf("generated skill directory is not configured")
		}
		return l.generatedDir, nil
	case PackTargetUser:
		if strings.TrimSpace(l.userDir) == "" {
			return "", fmt.Errorf("user skill directory is not configured")
		}
		return l.userDir, nil
	default:
		return "", fmt.Errorf("skill pack target %q is not mutable", target)
	}
}

func normalizePackManifest(manifest PackManifest) (PackManifest, error) {
	manifest.PackID = strings.TrimSpace(manifest.PackID)
	manifest.Version = strings.TrimSpace(manifest.Version)
	manifest.Dependencies = uniqueNonEmpty(manifest.Dependencies)
	if manifest.PackID == "" {
		return PackManifest{}, fmt.Errorf("skill pack id is required")
	}
	if manifest.Version == "" {
		return PackManifest{}, fmt.Errorf("skill pack version is required")
	}
	if len(manifest.Skills) == 0 {
		return PackManifest{}, fmt.Errorf("skill pack requires at least one skill")
	}
	normalizedSkills := make([]PackSkill, 0, len(manifest.Skills))
	seenSkills := make(map[string]struct{}, len(manifest.Skills))
	for _, skill := range manifest.Skills {
		skill.ID = strings.TrimSpace(skill.ID)
		if _, err := normalizeSkillDirID(skill.ID); err != nil {
			return PackManifest{}, fmt.Errorf("skill pack skill id: %w", err)
		}
		if _, exists := seenSkills[skill.ID]; exists {
			return PackManifest{}, fmt.Errorf("duplicate skill pack skill %q", skill.ID)
		}
		seenSkills[skill.ID] = struct{}{}
		if len(skill.Files) == 0 {
			return PackManifest{}, fmt.Errorf("skill pack skill %s requires files", skill.ID)
		}
		files := make(map[string]string, len(skill.Files))
		for rel, content := range skill.Files {
			clean, err := normalizeSkillRelativePath(rel)
			if err != nil {
				return PackManifest{}, fmt.Errorf("skill pack skill %s file: %w", skill.ID, err)
			}
			if strings.TrimSpace(content) == "" {
				return PackManifest{}, fmt.Errorf("skill pack skill %s file %s content is empty", skill.ID, clean)
			}
			files[filepath.ToSlash(clean)] = strings.ReplaceAll(content, "\r\n", "\n")
		}
		if _, ok := files["SKILL.md"]; !ok {
			return PackManifest{}, fmt.Errorf("skill pack skill %s requires SKILL.md", skill.ID)
		}
		skill.Files = files
		normalizedSkills = append(normalizedSkills, skill)
	}
	slices.SortFunc(normalizedSkills, func(left, right PackSkill) int {
		return strings.Compare(left.ID, right.ID)
	})
	manifest.Skills = normalizedSkills
	return manifest, nil
}

func sortedPackFilePaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	return paths
}

func packSkillIDs(items []PackSkill) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func sha256Text(text string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

func packReceiptPath(root string, packID string) string {
	return filepath.Join(root, ".acorn-pack-receipts", packID+".json")
}

func writePackReceipt(root string, receipt PackReceipt) error {
	path := packReceiptPath(root, receipt.PackID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create skill pack receipt directory: %w", err)
	}
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill pack receipt: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write skill pack receipt: %w", err)
	}
	return nil
}

func readPackReceipt(root string, packID string) (*PackReceipt, error) {
	body, err := os.ReadFile(packReceiptPath(root, packID))
	if err != nil {
		return nil, err
	}
	var receipt PackReceipt
	if err := json.Unmarshal(body, &receipt); err != nil {
		return nil, fmt.Errorf("parse skill pack receipt: %w", err)
	}
	return &receipt, nil
}
