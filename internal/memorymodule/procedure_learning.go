package memorymodule

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (s *LocalService) CreateProcedure(ctx context.Context, req CreateProcedureRequest) (*ProcedureRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service is nil")
	}
	if err := s.EnsureLayout(ctx); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format("2006-01-02")
	title := strings.TrimSpace(req.Title)
	body := strings.TrimSpace(req.Body)
	record := Record{
		Kind:         KindSkill,
		Title:        title,
		Status:       StatusVerified,
		Origin:       string(ProcedureOriginActionVerified),
		TaskPattern:  strings.TrimSpace(req.TaskPattern),
		Body:         body,
		SourceRun:    strings.TrimSpace(req.SourceRun),
		SourceRefs:   normalizeRefList(req.SourceRefs),
		EvidenceRefs: normalizeRefList(req.EvidenceRefs),
		Tags:         taskPatternTags(req.TaskPattern),
		Created:      now,
		Updated:      now,
	}
	if err := validateCreateProcedureRecord(record); err != nil {
		return nil, err
	}
	slug := sanitizeMemorySlug(title)
	if slug == "" {
		return nil, fmt.Errorf("procedure title cannot produce a file name")
	}
	content, err := renderProcedureFile(record)
	if err != nil {
		return nil, err
	}
	relPath := filepath.ToSlash(filepath.Join("skills", "learned", slug+".md"))
	memoryRecord, plan, err := s.applyNewMemoryRecord(ctx, relPath, content, KindSkill)
	if err != nil {
		return nil, err
	}
	procedure, err := ProcedureRecordFromMemoryRecord(*memoryRecord)
	if err != nil {
		return nil, err
	}
	procedure.MutationPlan = plan
	return procedure, nil
}

func validateCreateProcedureRecord(record Record) error {
	if strings.TrimSpace(record.Title) == "" {
		return fmt.Errorf("procedure title is required")
	}
	if strings.TrimSpace(record.TaskPattern) == "" {
		return fmt.Errorf("procedure task_pattern is required")
	}
	if strings.TrimSpace(record.Body) == "" {
		return fmt.Errorf("procedure body is required")
	}
	return validateProcedureRecord(record)
}

func renderProcedureFile(record Record) (string, error) {
	meta := skillFrontmatter{
		Origin:       record.Origin,
		TaskPattern:  record.TaskPattern,
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
		return "", fmt.Errorf("marshal procedure frontmatter: %w", err)
	}
	return "---\n" + string(frontmatter) + "---\n\n# " + strings.TrimSpace(record.Title) + "\n\n" + strings.TrimSpace(record.Body) + "\n", nil
}

// sanitizeMemorySlug derives a filesystem-safe slug from a record title, shared by
// the structured fact and procedure writers.
func sanitizeMemorySlug(value string) string {
	slug := strings.ToLower(sanitizeName(value))
	slug = strings.Trim(slug, ".-_")
	return filepath.Base(slug)
}
