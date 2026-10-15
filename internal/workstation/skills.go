package workstation

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/skills"
)

const (
	// skillsDir holds the skill packages, under the workstation's home.
	skillsDir = "skills"
	// packageMarker records the hash of the package a directory holds; a
	// directory without it is not one Acorn put there.
	packageMarker = ".acorn-package"
	// maxPackageBytes and maxPackageEntries bound what a package put may hold.
	maxPackageBytes   = 8 << 20
	maxPackageEntries = 512
)

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// PackageFile is one file of a package on the wire, its content in base64.
type PackageFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type PackageReadRequest struct {
	Path string `json:"path"`
}

type PackageReadResult struct {
	Files []PackageFile `json:"files"`
}

// SkillPackages lists the skill directory and the package each skill there
// holds.
type SkillPackages struct {
	Dir      string            `json:"dir"`
	Packages map[string]string `json:"packages"`
}

type SkillPackagePut struct {
	Hash     string        `json:"hash"`
	Markdown string        `json:"markdown"`
	Files    []PackageFile `json:"files"`
}

func (s *Server) readPackageHandler(w http.ResponseWriter, r *http.Request) {
	var req PackageReadRequest
	if !decode(w, r, &req) {
		return
	}
	path, err := s.requirePath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	files, err := skills.ReadPath(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	default:
		writeJSON(w, http.StatusOK, PackageReadResult{Files: toWire(files)})
	}
}

func (s *Server) skillPackages(w http.ResponseWriter, _ *http.Request) {
	dir := filepath.Join(s.home, skillsDir)
	out := SkillPackages{Dir: dir, Packages: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() || !skillNamePattern.MatchString(e.Name()) {
			continue
		}
		hash, err := os.ReadFile(filepath.Join(dir, e.Name(), packageMarker))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		out.Packages[e.Name()] = strings.TrimSpace(string(hash))
	}
	writeJSON(w, http.StatusOK, out)
}

// putSkillPackage replaces a skill's directory with the package: it is
// written beside the old one and swapped in, so the agent never reads half
// a package.
func (s *Server) putSkillPackage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2*maxPackageBytes)
	name := r.PathValue("name")
	if !skillNamePattern.MatchString(name) || len(name) > 64 {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid skill name %q", name))
		return
	}
	var req SkillPackagePut
	if !decode(w, r, &req) {
		return
	}
	files, err := fromWire(req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Hash) == "" || strings.TrimSpace(req.Markdown) == "" {
		writeError(w, http.StatusBadRequest, errors.New("hash and markdown are required"))
		return
	}
	if err := s.writeSkillPackage(name, req.Hash, req.Markdown, files); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) writeSkillPackage(name, hash, markdown string, files []core.SkillFile) error {
	dir := filepath.Join(s.home, skillsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	incoming := filepath.Join(dir, ".incoming-"+name+"-"+stamp)
	write := func(rel string, content []byte, mode os.FileMode) error {
		p := filepath.Join(incoming, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, content, mode)
	}
	defer os.RemoveAll(incoming)
	if err := write("SKILL.md", []byte(markdown), 0o644); err != nil {
		return err
	}
	for _, f := range files {
		mode := os.FileMode(0o644)
		if strings.HasPrefix(f.Path, "scripts/") {
			mode = 0o755
		}
		if err := write(f.Path, f.Content, mode); err != nil {
			return err
		}
	}
	if err := write(packageMarker, []byte(hash+"\n"), 0o644); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	old := filepath.Join(dir, ".old-"+name+"-"+stamp)
	if err := os.Rename(target, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(incoming, target); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

func (s *Server) deleteSkillPackage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	target := filepath.Join(s.home, skillsDir, name)
	if !skillNamePattern.MatchString(name) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid skill name %q", name))
		return
	}
	if _, err := os.Stat(filepath.Join(target, packageMarker)); err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("no skill package %s", name))
		return
	}
	if err := os.RemoveAll(target); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toWire(files []core.SkillFile) []PackageFile {
	out := make([]PackageFile, 0, len(files))
	for _, f := range files {
		out = append(out, PackageFile{Path: f.Path, Content: base64.StdEncoding.EncodeToString(f.Content)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// fromWire decodes files and keeps their paths inside the package.
func fromWire(files []PackageFile) ([]core.SkillFile, error) {
	if len(files) > maxPackageEntries {
		return nil, fmt.Errorf("the package has more than %d files", maxPackageEntries)
	}
	out := make([]core.SkillFile, 0, len(files))
	for _, f := range files {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(f.Path)))
		if f.Path == "" || clean != f.Path || strings.HasPrefix(clean, "../") || clean == ".." || filepath.IsAbs(f.Path) || strings.Contains(f.Path, "\\") || f.Path == packageMarker || f.Path == "SKILL.md" {
			return nil, fmt.Errorf("invalid package file path %q", f.Path)
		}
		content, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", f.Path, err)
		}
		out = append(out, core.SkillFile{Path: f.Path, Content: content})
	}
	return out, nil
}

// ReadPackage reads every file of a directory or zip on the workstation.
func (c *Client) ReadPackage(ctx context.Context, path string) ([]core.SkillFile, error) {
	var out PackageReadResult
	if err := c.call(ctx, http.MethodPost, "/v1/files/package", PackageReadRequest{Path: path}, &out); err != nil {
		return nil, err
	}
	files := make([]core.SkillFile, 0, len(out.Files))
	for _, f := range out.Files {
		content, err := base64.StdEncoding.DecodeString(f.Content)
		if err != nil {
			return nil, fmt.Errorf("workstation package file %s: %w", f.Path, err)
		}
		files = append(files, core.SkillFile{Path: f.Path, Content: content})
	}
	return files, nil
}

// SkillPackages reports the skill directory and the package each skill there
// holds.
func (c *Client) SkillPackages(ctx context.Context) (string, map[string]string, error) {
	var out SkillPackages
	if err := c.call(ctx, http.MethodGet, "/v1/skills", nil, &out); err != nil {
		return "", nil, err
	}
	return out.Dir, out.Packages, nil
}

func (c *Client) PutSkillPackage(ctx context.Context, name, hash, markdown string, files []core.SkillFile) error {
	var out struct{}
	return c.call(ctx, http.MethodPut, "/v1/skills/"+name, SkillPackagePut{Hash: hash, Markdown: markdown, Files: toWire(files)}, &out)
}

func (c *Client) DeleteSkillPackage(ctx context.Context, name string) error {
	var out struct{}
	return c.call(ctx, http.MethodDelete, "/v1/skills/"+name, nil, &out)
}
