package skills

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// SkillFileName is the file that makes a directory a skill.
const SkillFileName = "SKILL.md"

const (
	maxPackageBytes = 8 << 20
	maxPackageFiles = 256
	maxPathLength   = 255
)

// cleanPath checks a file path inside a skill package: relative,
// slash-separated, without empty, "." or ".." segments, and not the package's
// own SKILL.md.
func cleanPath(p string) (string, error) {
	switch {
	case p == "" || len(p) > maxPathLength:
		return "", fmt.Errorf("skill file path %q must be 1-%d bytes", p, maxPathLength)
	case strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00"):
		return "", fmt.Errorf("skill file path %q must be relative and slash-separated", p)
	case p == SkillFileName:
		return "", fmt.Errorf("skill file path %q is the skill itself; write it as the markdown", p)
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("skill file path %q has an empty, . or .. segment", p)
		}
	}
	return p, nil
}

// normalizeFiles checks a package's files and their limits and sorts them by
// path.
func normalizeFiles(markdown string, files []core.SkillFile) ([]core.SkillFile, error) {
	if len(files) > maxPackageFiles {
		return nil, fmt.Errorf("skill package has %d files; the limit is %d", len(files), maxPackageFiles)
	}
	total := len(markdown)
	seen := map[string]bool{}
	out := make([]core.SkillFile, 0, len(files))
	for _, f := range files {
		p, err := cleanPath(f.Path)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			return nil, fmt.Errorf("skill file %q appears twice", p)
		}
		seen[p] = true
		total += len(f.Content)
		out = append(out, core.SkillFile{Path: p, Content: slices.Clone(f.Content)})
	}
	if total > maxPackageBytes {
		return nil, fmt.Errorf("skill package is %d bytes; the limit is %d", total, maxPackageBytes)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// fileSum is a file's path with the hash of its content.
type fileSum struct {
	path, sha256 string
}

// packageHash names a package's content: SKILL.md and every file's path and
// content hash, in path order.
func packageHash(markdown string, files []fileSum) string {
	sorted := slices.Clone(files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].path < sorted[j].path })
	h := sha256.New()
	h.Write([]byte(SkillFileName + "\x00" + markdown + "\x00"))
	for _, f := range sorted {
		h.Write([]byte(f.path + "\x00" + f.sha256 + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func contentSums(files []core.SkillFile) []fileSum {
	sums := make([]fileSum, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.Content)
		sums = append(sums, fileSum{path: f.Path, sha256: hex.EncodeToString(sum[:])})
	}
	return sums
}

func infoSums(files []core.SkillFileInfo) []fileSum {
	sums := make([]fileSum, 0, len(files))
	for _, f := range files {
		sums = append(sums, fileSum{path: f.Path, sha256: f.SHA256})
	}
	return sums
}

// PackageHash names the content of a package given in full.
func PackageHash(markdown string, files []core.SkillFile) string {
	return packageHash(markdown, contentSums(files))
}

// osMetadata reports files a desktop adds to directories and archives.
func osMetadata(p string) bool {
	base := path.Base(p)
	return strings.HasPrefix(p, "__MACOSX/") || strings.Contains(p, "/__MACOSX/") || base == ".DS_Store" || strings.HasPrefix(base, "._")
}

// FromEntries makes a package of the files read from a directory or zip: the
// SKILL.md nearest the top marks the skill's directory, whose other files the
// package carries. Desktop metadata is left out; any other file outside that
// directory is an error.
func FromEntries(entries []core.SkillFile) (string, []core.SkillFile, error) {
	var kept []core.SkillFile
	for _, e := range entries {
		if !osMetadata(e.Path) {
			kept = append(kept, e)
		}
	}
	root, depth, found := "", -1, 0
	for _, e := range kept {
		if path.Base(e.Path) != SkillFileName {
			continue
		}
		d := strings.Count(e.Path, "/")
		switch {
		case depth == -1 || d < depth:
			root, depth, found = path.Dir(e.Path), d, 1
		case d == depth:
			found++
		}
	}
	switch {
	case found == 0:
		return "", nil, fmt.Errorf("no %s found", SkillFileName)
	case found > 1:
		return "", nil, fmt.Errorf("more than one %s at the same depth; import one skill at a time", SkillFileName)
	}
	prefix := ""
	if root != "." {
		prefix = root + "/"
	}
	var markdown string
	var files []core.SkillFile
	for _, e := range kept {
		rel, ok := strings.CutPrefix(e.Path, prefix)
		if !ok {
			return "", nil, fmt.Errorf("%s is outside the skill's directory %s", e.Path, root)
		}
		if rel == SkillFileName {
			markdown = string(e.Content)
			continue
		}
		files = append(files, core.SkillFile{Path: rel, Content: e.Content})
	}
	return markdown, files, nil
}

// maxReadEntries bounds the files read from a directory or archive, which may
// hold desktop metadata the package leaves out.
const maxReadEntries = 2 * maxPackageFiles

// ReadFS reads every regular file of a package directory or archive, with
// slash-separated paths relative to its root.
func ReadFS(fsys fs.FS) ([]core.SkillFile, error) {
	var files []core.SkillFile
	total := 0
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file; a package holds regular files only", p)
		}
		if len(files) == maxReadEntries {
			return fmt.Errorf("the package has more than %d files", maxReadEntries)
		}
		f, err := fsys.Open(p)
		if err != nil {
			return err
		}
		content, err := io.ReadAll(io.LimitReader(f, int64(maxPackageBytes-total+1)))
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		total += len(content)
		if total > maxPackageBytes {
			return fmt.Errorf("the package is larger than %d bytes", maxPackageBytes)
		}
		files = append(files, core.SkillFile{Path: p, Content: content})
		return nil
	})
	return files, err
}

// ReadPath reads a package from a directory or a zip file.
func ReadPath(root string) ([]core.SkillFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return ReadFS(os.DirFS(root))
	}
	archive, err := zip.OpenReader(root)
	if err != nil {
		return nil, fmt.Errorf("%s is neither a directory nor a zip: %w", root, err)
	}
	defer archive.Close()
	return ReadFS(&archive.Reader)
}
