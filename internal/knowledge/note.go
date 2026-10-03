package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the note fields Acorn reads and writes. Extra keeps every
// other key (aliases and the like set in Obsidian) so a rewrite preserves it.
type Frontmatter struct {
	Title   string
	Tags    []string
	Source  string
	Created time.Time
	Updated time.Time
	Extra   []*yaml.Node // alternating key and value nodes
}

// Note is one markdown note. Commit is the sha of the write that produced it
// and is empty when the note was read.
type Note struct {
	Path        string
	Frontmatter Frontmatter
	Body        string
	Commit      string
}

var noteTimeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// CleanNotePath validates a note path relative to the knowledge directory:
// slash-separated, no "..", no hidden segments, outside attachments/, ending
// in ".md".
func CleanNotePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if err := checkRelativePath(p); err != nil {
		return "", err
	}
	if !strings.HasSuffix(p, ".md") {
		return "", fmt.Errorf("%w: %q must end in .md", ErrInvalidPath, p)
	}
	if strings.HasPrefix(p, attachmentsDir+"/") {
		return "", fmt.Errorf("%w: %q is under %s/, which holds attachments only", ErrInvalidPath, p, attachmentsDir)
	}
	return p, nil
}

func checkRelativePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: path is empty", ErrInvalidPath)
	}
	if strings.Contains(p, `\`) || path.IsAbs(p) || path.Clean(p) != p {
		return fmt.Errorf("%w: %q must be a clean relative path with forward slashes", ErrInvalidPath, p)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == ".." || strings.HasPrefix(segment, ".") {
			return fmt.Errorf("%w: %q has a hidden or parent segment", ErrInvalidPath, p)
		}
	}
	return nil
}

// ParseNote splits YAML frontmatter from the body. Without a title in the
// frontmatter the first "# " heading, else the file name, is the title;
// without an updated time the file's mtime is used. Times without a zone are
// read in loc.
func ParseNote(notePath string, raw []byte, mtime time.Time, loc *time.Location) (Frontmatter, string, error) {
	header, body, found := splitFrontmatter(raw)
	var fm Frontmatter
	if found {
		parsed, err := parseFrontmatter(header, loc)
		if err != nil {
			return Frontmatter{}, "", fmt.Errorf("parse frontmatter of %s: %w", notePath, err)
		}
		fm = parsed
	}
	body = strings.TrimLeft(body, "\r\n")
	if fm.Title == "" {
		fm.Title = titleFromBody(body)
	}
	if fm.Title == "" {
		fm.Title = strings.TrimSuffix(path.Base(notePath), ".md")
	}
	if fm.Updated.IsZero() {
		fm.Updated = mtime
	}
	return fm, body, nil
}

// RenderNote writes title, tags, source, created and updated in that order,
// then the extra keys, then the body. Times are written in loc.
func RenderNote(fm Frontmatter, body string, loc *time.Location) ([]byte, error) {
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, value *yaml.Node) {
		mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
	}
	add("title", &yaml.Node{Kind: yaml.ScalarNode, Value: fm.Title})
	if len(fm.Tags) > 0 {
		tags := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
		for _, tag := range fm.Tags {
			tags.Content = append(tags.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: tag})
		}
		add("tags", tags)
	}
	if fm.Source != "" {
		add("source", &yaml.Node{Kind: yaml.ScalarNode, Value: fm.Source})
	}
	add("created", &yaml.Node{Kind: yaml.ScalarNode, Value: fm.Created.In(loc).Format(time.RFC3339)})
	add("updated", &yaml.Node{Kind: yaml.ScalarNode, Value: fm.Updated.In(loc).Format(time.RFC3339)})
	mapping.Content = append(mapping.Content, fm.Extra...)

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(mapping); err != nil {
		return nil, fmt.Errorf("render frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("render frontmatter: %w", err)
	}
	buf.WriteString("---\n\n")
	buf.WriteString(strings.TrimSpace(body))
	buf.WriteString("\n")
	return buf.Bytes(), nil
}

// splitFrontmatter returns the YAML between a leading "---" line and the next
// "---" or "..." line. A file without a closed block has no frontmatter.
func splitFrontmatter(raw []byte) (string, string, bool) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", text, false
	}
	rest := text[len("---\n"):]
	offset := 0
	for {
		end := strings.IndexByte(rest[offset:], '\n')
		line := rest[offset:]
		if end >= 0 {
			line = rest[offset : offset+end]
		}
		if line == "---" || line == "..." {
			after := ""
			if end >= 0 {
				after = rest[offset+end+1:]
			}
			return rest[:offset], after, true
		}
		if end < 0 {
			return "", text, false
		}
		offset += end + 1
	}
}

func parseFrontmatter(header string, loc *time.Location) (Frontmatter, error) {
	var fm Frontmatter
	if strings.TrimSpace(header) == "" {
		return fm, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		return fm, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return fm, errors.New("frontmatter must be a mapping")
	}
	pairs := doc.Content[0].Content
	for i := 0; i+1 < len(pairs); i += 2 {
		key, value := pairs[i], pairs[i+1]
		var err error
		switch key.Value {
		case "title":
			fm.Title = strings.TrimSpace(value.Value)
		case "tags":
			fm.Tags, err = parseTags(value)
		case "source":
			fm.Source = strings.TrimSpace(value.Value)
		case "created":
			fm.Created, err = parseNoteTime(value.Value, loc)
		case "updated":
			fm.Updated, err = parseNoteTime(value.Value, loc)
		default:
			fm.Extra = append(fm.Extra, key, value)
		}
		if err != nil {
			return Frontmatter{}, fmt.Errorf("%s: %w", key.Value, err)
		}
	}
	return fm, nil
}

// parseTags accepts a YAML list or a string separated by commas or spaces,
// and drops a leading "#".
func parseTags(node *yaml.Node) ([]string, error) {
	var raw []string
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			raw = append(raw, item.Value)
		}
	case yaml.ScalarNode:
		raw = strings.FieldsFunc(node.Value, func(r rune) bool { return r == ',' || r == ' ' })
	default:
		return nil, errors.New("tags must be a list or a string")
	}
	return normalizeTags(raw), nil
}

func normalizeTags(raw []string) []string {
	var tags []string
	seen := map[string]bool{}
	for _, tag := range raw {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

func parseNoteTime(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range noteTimeLayouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", value)
}

func titleFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if title, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			return strings.TrimSpace(title)
		}
	}
	return ""
}
