package memorymodule

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var nonAnchorChars = regexp.MustCompile(`[^a-z0-9]+`)

type factFrontmatter struct {
	Scope        string                `yaml:"scope"`
	Tags         []string              `yaml:"tags"`
	Status       string                `yaml:"status"`
	Created      string                `yaml:"created"`
	Updated      string                `yaml:"updated"`
	ValidFrom    string                `yaml:"valid_from"`
	ValidUntil   string                `yaml:"valid_until"`
	SourceRun    string                `yaml:"source_run"`
	SourceRefs   []string              `yaml:"source_refs"`
	EvidenceRefs []string              `yaml:"evidence_refs"`
	Relations    []relationFrontmatter `yaml:"relations"`
}

type skillFrontmatter struct {
	Origin       string                `yaml:"origin"`
	TaskPattern  string                `yaml:"task_pattern"`
	Status       string                `yaml:"status"`
	Created      string                `yaml:"created"`
	Updated      string                `yaml:"updated"`
	ValidFrom    string                `yaml:"valid_from"`
	ValidUntil   string                `yaml:"valid_until"`
	SourceRun    string                `yaml:"source_run"`
	SourceRefs   []string              `yaml:"source_refs"`
	EvidenceRefs []string              `yaml:"evidence_refs"`
	Relations    []relationFrontmatter `yaml:"relations"`
}

type relationFrontmatter struct {
	Type   string `yaml:"type"`
	Target string `yaml:"target"`
	Reason string `yaml:"reason"`
}

func readMemoryRecord(root string, kind Kind, path string) (*Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read memory file %q: %w", path, err)
	}
	return parseMemoryRecord(root, kind, path, string(data))
}

func parseMemoryRecord(root string, kind Kind, path string, text string) (*Record, error) {
	frontmatter, body, err := splitFrontmatter(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("resolve memory relative path: %w", err)
	}
	rel = filepath.ToSlash(rel)
	record := &Record{
		Kind:     kind,
		RootPath: root,
		RelPath:  rel,
		Body:     strings.TrimSpace(body),
		Title:    titleFromBody(body),
	}
	if record.Title == "" {
		record.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	record.Ref = rel + "#" + anchor(record.Title)
	switch kind {
	case KindFact:
		if err := applyFactFrontmatter(record, frontmatter); err != nil {
			return nil, fmt.Errorf("parse fact frontmatter: %w", err)
		}
	case KindSkill:
		if err := applySkillFrontmatter(record, frontmatter); err != nil {
			return nil, fmt.Errorf("parse skill frontmatter: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported frontmatter kind %q", kind)
	}
	return record, nil
}

func splitFrontmatter(text string) (string, string, error) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", fmt.Errorf("frontmatter is required")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end == -1 {
		return "", "", fmt.Errorf("closing frontmatter marker is required")
	}
	end += 4
	return text[4:end], text[end+5:], nil
}

func applyFactFrontmatter(record *Record, frontmatter string) error {
	var meta factFrontmatter
	if err := decodeKnownFrontmatter(frontmatter, &meta); err != nil {
		return err
	}
	record.Scope = strings.TrimSpace(meta.Scope)
	record.Tags = normalizeList(meta.Tags)
	record.Status = Status(strings.TrimSpace(meta.Status))
	record.Created = strings.TrimSpace(meta.Created)
	record.Updated = strings.TrimSpace(meta.Updated)
	record.ValidFrom = strings.TrimSpace(meta.ValidFrom)
	record.ValidUntil = strings.TrimSpace(meta.ValidUntil)
	record.SourceRun = strings.TrimSpace(meta.SourceRun)
	record.SourceRefs = normalizeRefList(meta.SourceRefs)
	record.EvidenceRefs = normalizeRefList(meta.EvidenceRefs)
	relations, err := normalizeRelations(meta.Relations)
	if err != nil {
		return err
	}
	record.Relations = relations
	if err := validateScope(record.Scope); err != nil {
		return err
	}
	return validateCommon(record)
}

func applySkillFrontmatter(record *Record, frontmatter string) error {
	var meta skillFrontmatter
	if err := decodeKnownFrontmatter(frontmatter, &meta); err != nil {
		return err
	}
	record.Origin = strings.TrimSpace(meta.Origin)
	record.TaskPattern = strings.TrimSpace(meta.TaskPattern)
	record.Status = Status(strings.TrimSpace(meta.Status))
	record.Created = strings.TrimSpace(meta.Created)
	record.Updated = strings.TrimSpace(meta.Updated)
	record.ValidFrom = strings.TrimSpace(meta.ValidFrom)
	record.ValidUntil = strings.TrimSpace(meta.ValidUntil)
	record.SourceRun = strings.TrimSpace(meta.SourceRun)
	record.SourceRefs = normalizeRefList(meta.SourceRefs)
	record.EvidenceRefs = normalizeRefList(meta.EvidenceRefs)
	relations, err := normalizeRelations(meta.Relations)
	if err != nil {
		return err
	}
	record.Relations = relations
	record.Tags = taskPatternTags(record.TaskPattern)
	if err := validateCommon(record); err != nil {
		return err
	}
	return validateProcedureRecord(*record)
}

func decodeKnownFrontmatter(frontmatter string, out any) error {
	decoder := yaml.NewDecoder(bytes.NewBufferString(frontmatter))
	decoder.KnownFields(true)
	return decoder.Decode(out)
}

func validateCommon(record *Record) error {
	if record.Status != StatusUnverified && record.Status != StatusVerified && record.Status != StatusRetired {
		return fmt.Errorf("status must be unverified, verified, or retired")
	}
	if len(record.Tags) == 0 {
		return fmt.Errorf("tags are required")
	}
	if strings.TrimSpace(record.Created) == "" {
		return fmt.Errorf("created is required")
	}
	if strings.TrimSpace(record.Updated) == "" {
		return fmt.Errorf("updated is required")
	}
	if err := validateDateField("created", record.Created); err != nil {
		return err
	}
	if err := validateDateField("updated", record.Updated); err != nil {
		return err
	}
	if err := validateDateField("valid_from", record.ValidFrom); err != nil {
		return err
	}
	if err := validateDateField("valid_until", record.ValidUntil); err != nil {
		return err
	}
	return nil
}

func validateScope(scope string) error {
	scope = strings.TrimSpace(scope)
	if scope == "user" {
		return nil
	}
	if strings.HasPrefix(scope, "workspace:") && strings.TrimSpace(strings.TrimPrefix(scope, "workspace:")) != "" {
		return nil
	}
	return fmt.Errorf("scope must be user or workspace:{slug}")
}

func normalizeList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.ToLower(strings.TrimSpace(item))
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func normalizeRefList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func normalizeRelations(items []relationFrontmatter) ([]RecordRelation, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]RecordRelation, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		relationType := RelationType(strings.TrimSpace(item.Type))
		switch relationType {
		case RelationSupports, RelationDerivedFrom, RelationSupersedes, RelationContradicts:
		default:
			return nil, fmt.Errorf("relation type must be supports, derived_from, supersedes, or contradicts")
		}
		target := strings.TrimSpace(item.Target)
		if target == "" {
			return nil, fmt.Errorf("relation target is required")
		}
		key := string(relationType) + "\x00" + target
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, RecordRelation{
			Type:   relationType,
			Target: target,
			Reason: strings.TrimSpace(item.Reason),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Target < out[j].Target
	})
	return out, nil
}

func relationFrontmatterFromDomain(items []RecordRelation) []relationFrontmatter {
	if len(items) == 0 {
		return nil
	}
	out := make([]relationFrontmatter, 0, len(items))
	for _, item := range items {
		out = append(out, relationFrontmatter{
			Type:   string(item.Type),
			Target: item.Target,
			Reason: item.Reason,
		})
	}
	return out
}

func validateDateField(name string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", trimmed); err != nil {
		return fmt.Errorf("%s must use YYYY-MM-DD", name)
	}
	return nil
}

func taskPatternTags(pattern string) []string {
	return normalizeList(strings.Split(pattern, ","))
}

func titleFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}

func anchor(title string) string {
	lower := strings.ToLower(strings.TrimSpace(title))
	replaced := nonAnchorChars.ReplaceAllString(lower, "-")
	return strings.Trim(replaced, "-")
}
