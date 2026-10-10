package knowledge

import (
	"fmt"
	"path"
	"strings"
)

// CleanNotePath validates a note path: slash-separated, relative, no "..",
// no hidden segments, outside attachments/, ending in ".md".
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
