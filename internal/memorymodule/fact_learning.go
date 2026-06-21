package memorymodule

import (
	"context"
	"fmt"
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
	scope, err := canonicalFactScope(req.Scope)
	if err != nil {
		return nil, err
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
	slug := sanitizeMemorySlug(record.Title)
	if slug == "" {
		return nil, fmt.Errorf("fact title cannot produce a file name")
	}
	content, err := renderFactFile(record)
	if err != nil {
		return nil, err
	}
	memoryRecord, _, err := s.applyNewMemoryRecord(ctx, factRelPath(scope, slug), content, KindFact)
	if err != nil {
		return nil, err
	}
	return memoryRecord, nil
}

// canonicalFactScope normalizes a requested scope so the stored value matches the
// scope queries use. An empty scope defaults to "user". A workspace scope is run
// through WorkspaceScope (which sanitizes the slug) so a non-canonical input like
// "workspace:Acorn Prod" is stored as "workspace:acorn-prod" — otherwise the fact
// would be written but never matched by current-workspace recall (exact scope ==).
func canonicalFactScope(raw string) (string, error) {
	scope := strings.TrimSpace(raw)
	if scope == "" {
		return "user", nil
	}
	if rawWorkspace, ok := strings.CutPrefix(scope, "workspace:"); ok {
		canonical := WorkspaceScope(rawWorkspace)
		if canonical == "" {
			return "", fmt.Errorf("workspace scope must include a non-empty slug")
		}
		return canonical, nil
	}
	return scope, nil
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
		Scope:      record.Scope,
		Tags:       append([]string(nil), record.Tags...),
		Status:     string(record.Status),
		Created:    record.Created,
		Updated:    record.Updated,
		SourceRun:  record.SourceRun,
		SourceRefs: append([]string(nil), record.SourceRefs...),
	}
	frontmatter, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal fact frontmatter: %w", err)
	}
	return "---\n" + string(frontmatter) + "---\n\n# " + strings.TrimSpace(record.Title) + "\n\n" + strings.TrimSpace(record.Body) + "\n", nil
}

// sanitizeMemorySlug derives a filesystem-safe slug from a record title.
func sanitizeMemorySlug(value string) string {
	slug := strings.ToLower(sanitizeName(value))
	slug = strings.Trim(slug, ".-_")
	return filepath.Base(slug)
}
