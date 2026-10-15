package skills

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// frontmatter holds the fields the Agent Skills specification defines.
type frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license"`
	Compatibility string            `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	AllowedTools  string            `yaml:"allowed-tools"`
}

const (
	maxNameLength          = 64
	maxDescriptionRunes    = 1024
	maxCompatibilityRunes  = 500
	maxSkillMarkdownBytes  = 64 << 10
	frontmatterDelimiter   = "---"
	requiredFrontmatterMsg = "SKILL.md must start with YAML frontmatter between --- lines"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Parse reads a SKILL.md: strict frontmatter with the specification's fields,
// then the instruction.
func Parse(markdown string) (Spec, error) {
	if len(markdown) > maxSkillMarkdownBytes {
		return Spec{}, fmt.Errorf("SKILL.md is %d bytes; the limit is %d", len(markdown), maxSkillMarkdownBytes)
	}
	text := strings.TrimPrefix(strings.ReplaceAll(markdown, "\r\n", "\n"), "\uFEFF")
	meta, body, err := splitFrontmatter(text)
	if err != nil {
		return Spec{}, err
	}
	spec := Spec{
		Name:          strings.TrimSpace(meta.Name),
		Description:   strings.TrimSpace(meta.Description),
		License:       strings.TrimSpace(meta.License),
		Compatibility: strings.TrimSpace(meta.Compatibility),
		Metadata:      meta.Metadata,
		AllowedTools:  strings.TrimSpace(meta.AllowedTools),
		RequiresTools: strings.Fields(meta.Metadata[RequiresToolsKey]),
		Instruction:   strings.TrimSpace(body),
	}
	switch {
	case !ValidName(spec.Name):
		return Spec{}, fmt.Errorf("skill name %q must be 1-%d lowercase letters, digits and single hyphens, not starting or ending with a hyphen", spec.Name, maxNameLength)
	case spec.Description == "":
		return Spec{}, fmt.Errorf("skill %s needs a description of what it does and when to use it", spec.Name)
	case utf8.RuneCountInString(spec.Description) > maxDescriptionRunes:
		return Spec{}, fmt.Errorf("skill %s description is longer than %d characters", spec.Name, maxDescriptionRunes)
	case utf8.RuneCountInString(spec.Compatibility) > maxCompatibilityRunes:
		return Spec{}, fmt.Errorf("skill %s compatibility is longer than %d characters", spec.Name, maxCompatibilityRunes)
	}
	return spec, nil
}

// ValidName reports whether name is a skill name the specification allows.
func ValidName(name string) bool {
	return len(name) <= maxNameLength && namePattern.MatchString(name)
}

func splitFrontmatter(text string) (frontmatter, string, error) {
	if !strings.HasPrefix(text, frontmatterDelimiter+"\n") {
		return frontmatter{}, "", errors.New(requiredFrontmatterMsg)
	}
	lines := strings.Split(text, "\n")
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == frontmatterDelimiter {
			end = i
			break
		}
	}
	if end == -1 {
		return frontmatter{}, "", errors.New(requiredFrontmatterMsg)
	}
	var meta frontmatter
	decoder := yaml.NewDecoder(strings.NewReader(strings.Join(lines[1:end], "\n")))
	decoder.KnownFields(true)
	if err := decoder.Decode(&meta); err != nil {
		return frontmatter{}, "", fmt.Errorf("invalid frontmatter: %w", err)
	}
	return meta, strings.Join(lines[end+1:], "\n"), nil
}
