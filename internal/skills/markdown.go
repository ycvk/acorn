package skills

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	ID              string            `yaml:"id"`
	Name            string            `yaml:"name"`
	Version         string            `yaml:"version,omitempty"`
	Category        string            `yaml:"category,omitempty"`
	Summary         string            `yaml:"summary,omitempty"`
	PromotedFrom    string            `yaml:"promoted_from,omitempty"`
	Origin          Origin            `yaml:"origin,omitempty"`
	LifecycleStatus LifecycleStatus   `yaml:"lifecycle_status,omitempty"`
	TaskPattern     string            `yaml:"task_pattern,omitempty"`
	Tags            []string          `yaml:"tags,omitempty"`
	Platforms       []string          `yaml:"platforms,omitempty"`
	Requires        frontRequirements `yaml:"requires,omitempty"`
	TriggerHints    []string          `yaml:"trigger_hints,omitempty"`
	CreatedByRunID  string            `yaml:"created_by_run_id,omitempty"`
	UpdatedByRunID  string            `yaml:"updated_by_run_id,omitempty"`
	EvidenceRefs    []string          `yaml:"evidence_refs,omitempty"`
	Replaces        []string          `yaml:"replaces,omitempty"`
	ReplacedBy      []string          `yaml:"replaced_by,omitempty"`
}

type frontRequirements struct {
	Tools    []string `yaml:"tools,omitempty"`
	Toolsets []string `yaml:"toolsets,omitempty"`
	Bins     []string `yaml:"bins,omitempty"`
	Env      []string `yaml:"env,omitempty"`
}

func parseSkillMarkdown(raw string) (frontmatter, string, string, string, error) {
	text := strings.TrimPrefix(strings.ReplaceAll(raw, "\r\n", "\n"), "\uFEFF")
	meta, body, err := splitFrontmatter(text)
	if err != nil {
		return frontmatter{}, "", "", "", err
	}
	name, instruction := parseSkillBody(body)
	return meta, body, name, instruction, nil
}

func splitFrontmatter(text string) (frontmatter, string, error) {
	if !strings.HasPrefix(text, "---\n") {
		return frontmatter{}, text, nil
	}
	lines := strings.Split(text, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return frontmatter{}, "", fmt.Errorf("missing closing frontmatter delimiter")
	}
	var meta frontmatter
	frontmatterText := strings.Join(lines[1:end], "\n")
	if strings.TrimSpace(frontmatterText) != "" {
		if err := yaml.Unmarshal([]byte(frontmatterText), &meta); err != nil {
			return frontmatter{}, "", fmt.Errorf("invalid frontmatter: %w", err)
		}
	}
	body := strings.Join(lines[end+1:], "\n")
	return meta, body, nil
}

func parseSkillBody(raw string) (name string, instruction string) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	start := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			name = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			start = i + 1
		}
		break
	}
	instruction = strings.TrimSpace(strings.Join(lines[start:], "\n"))
	if name == "" && instruction == "" {
		instruction = strings.TrimSpace(text)
	}
	return name, instruction
}

func renderSkillMarkdown(meta frontmatter, instruction string) (string, error) {
	markdownBody := "# " + meta.Name + "\n\n" + strings.TrimSpace(instruction) + "\n"
	return renderSkillMarkdownBody(meta, markdownBody)
}

func renderSkillMarkdownBody(meta frontmatter, body string) (string, error) {
	frontmatterBody, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal skill frontmatter: %w", err)
	}
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.Write(frontmatterBody)
	builder.WriteString("---\n\n")
	builder.WriteString(strings.TrimRight(strings.ReplaceAll(body, "\r\n", "\n"), "\n"))
	builder.WriteString("\n")
	return builder.String(), nil
}
