package memorymodule

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// CreateFact writes a new fact record from minimal structured input, generating
// the Record V2 frontmatter on the backend and auto-stamping created/updated/
// status/scope. It is the structured alternative to hand-authoring raw markdown
// frontmatter via memory_create_file: the `remember` tool supplies only
// title/body/tags/scope and never touches YAML, dates, or status — eliminating
// the date-format / scope-format / unknown-key reject classes for the model.
// Validation and noop-duplicate detection still flow through PlanMemoryMutation.
func (s *LocalService) CreateFact(ctx context.Context, req CreateFactRequest) (*Record, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format("2006-01-02")
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "user"
	}
	record := Record{
		Kind:    KindFact,
		Title:   strings.TrimSpace(req.Title),
		Scope:   scope,
		Tags:    normalizeList(req.Tags),
		Status:  StatusUnverified,
		Body:    strings.TrimSpace(req.Body),
		Created: now,
		Updated: now,
	}
	if err := validateCreateFactRecord(record); err != nil {
		return nil, err
	}
	slug := sanitizeProcedureSlug(record.Title)
	if slug == "" {
		return nil, fmt.Errorf("fact title cannot produce a file name")
	}
	relPath := factRelPath(scope, slug)
	path := filepath.Join(s.root, filepath.FromSlash(relPath))
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("fact file already exists: %s", relPath)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat fact file %s: %w", relPath, err)
	}
	content, err := renderFactFile(record)
	if err != nil {
		return nil, err
	}
	plan, err := s.PlanMemoryMutation(ctx, PlanMemoryMutationRequest{Path: relPath, Content: content})
	if err != nil {
		return nil, err
	}
	if plan.Action != MemoryMutationCreate {
		return nil, fmt.Errorf("fact mutation plan action %q: %s", plan.Action, plan.Reason)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create fact directory: %w", err)
	}
	if err := writeNewMemoryFile(path, []byte(content)); err != nil {
		return nil, fmt.Errorf("write fact file %s: %w", relPath, err)
	}
	memoryRecord, err := readMemoryRecord(s.root, KindFact, path)
	if err != nil {
		return nil, err
	}
	if err := s.BuildIndex(ctx); err != nil {
		return nil, fmt.Errorf("refresh index after create fact: %w", err)
	}
	return memoryRecord, nil
}

func validateCreateFactRecord(record Record) error {
	if strings.TrimSpace(record.Title) == "" {
		return fmt.Errorf("fact title is required")
	}
	if strings.TrimSpace(record.Body) == "" {
		return fmt.Errorf("fact body is required")
	}
	if err := validateScope(record.Scope); err != nil {
		return err
	}
	return validateCommon(&record)
}

// factRelPath places user facts under facts/user/ and workspace facts under
// facts/workspaces/{slug}/ to avoid cross-workspace title collisions. The scope
// itself remains authoritative in frontmatter; the path is organizational.
func factRelPath(scope string, slug string) string {
	if raw, ok := strings.CutPrefix(scope, "workspace:"); ok {
		if ws := sanitizeName(raw); ws != "" {
			return filepath.ToSlash(filepath.Join("facts", "workspaces", ws, slug+".md"))
		}
	}
	return filepath.ToSlash(filepath.Join("facts", "user", slug+".md"))
}

func renderFactFile(record Record) (string, error) {
	meta := factFrontmatter{
		Scope:        record.Scope,
		Tags:         append([]string(nil), record.Tags...),
		Status:       string(record.Status),
		Created:      record.Created,
		Updated:      record.Updated,
		ValidFrom:    record.ValidFrom,
		ValidUntil:   record.ValidUntil,
		SourceRun:    record.SourceRun,
		SourceRefs:   append([]string(nil), record.SourceRefs...),
		EvidenceRefs: append([]string(nil), record.EvidenceRefs...),
		Relations:    relationFrontmatterFromDomain(record.Relations),
	}
	frontmatter, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal fact frontmatter: %w", err)
	}
	return "---\n" + string(frontmatter) + "---\n\n# " + strings.TrimSpace(record.Title) + "\n\n" + strings.TrimSpace(record.Body) + "\n", nil
}
