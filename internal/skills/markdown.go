package skills

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	ID           string            `yaml:"id"`
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Category     string            `yaml:"category"`
	Summary      string            `yaml:"summary"`
	Tags         []string          `yaml:"tags"`
	Platforms    []string          `yaml:"platforms"`
	Requires     frontRequirements `yaml:"requires"`
	TriggerHints []string          `yaml:"trigger_hints"`
}

type frontRequirements struct {
	Tools    []string `yaml:"tools"`
	Toolsets []string `yaml:"toolsets"`
	Bins     []string `yaml:"bins"`
	Env      []string `yaml:"env"`
}

func parseSkillMarkdown(raw string) (frontmatter, string, string, error) {
	text := strings.TrimPrefix(strings.ReplaceAll(raw, "\r\n", "\n"), "\uFEFF")
	meta, body, err := splitFrontmatter(text)
	if err != nil {
		return frontmatter{}, "", "", err
	}
	name, instruction := parseSkillBody(body)
	return meta, name, instruction, nil
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
		decoder := yaml.NewDecoder(strings.NewReader(frontmatterText))
		decoder.KnownFields(true)
		if err := decoder.Decode(&meta); err != nil {
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
