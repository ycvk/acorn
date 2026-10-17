package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ycvk/acorn/internal/core"
)

// ManifestFileName is the optional file at a package's root that declares
// what the package provides besides its instructions.
const ManifestFileName = "acorn-package.yaml"

// Manifest is a package's acorn-package.yaml.
type Manifest struct {
	MCP *MCPServer `yaml:"mcp"`
}

// MCPServer is the stdio MCP server a package runs on the workstation, from
// the package's directory. Secrets names the environment variables it gets
// from the values the owner sets.
type MCPServer struct {
	Command string   `yaml:"command" json:"command"`
	Args    []string `yaml:"args" json:"args,omitempty"`
	Secrets []string `yaml:"secrets" json:"secrets,omitempty"`
}

const maxManifestBytes = 64 << 10

var secretNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidSecretName reports whether name can be a secret's environment
// variable.
func ValidSecretName(name string) bool {
	return len(name) <= 128 && secretNamePattern.MatchString(name)
}

// ParseManifest reads acorn-package.yaml strictly: one document, no unknown
// fields, and an MCP server with a command and well-formed secret names.
func ParseManifest(content []byte) (Manifest, error) {
	if len(content) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("%s is %d bytes; the limit is %d", ManifestFileName, len(content), maxManifestBytes)
	}
	var m Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		if errors.Is(err, io.EOF) {
			return Manifest{}, fmt.Errorf("%s declares nothing", ManifestFileName)
		}
		return Manifest{}, fmt.Errorf("invalid %s: %w", ManifestFileName, err)
	}
	var next yaml.Node
	if err := decoder.Decode(&next); !errors.Is(err, io.EOF) {
		return Manifest{}, fmt.Errorf("%s must hold one YAML document", ManifestFileName)
	}
	if m.MCP == nil {
		return Manifest{}, fmt.Errorf("%s declares no mcp server", ManifestFileName)
	}
	server := MCPServer{Command: strings.TrimSpace(m.MCP.Command), Args: m.MCP.Args, Secrets: m.MCP.Secrets}
	switch {
	case server.Command == "":
		return Manifest{}, fmt.Errorf("%s: mcp.command is required", ManifestFileName)
	case strings.ContainsRune(server.Command, 0) || slices.ContainsFunc(server.Args, func(a string) bool { return strings.ContainsRune(a, 0) }):
		return Manifest{}, fmt.Errorf("%s: mcp.command and mcp.args cannot hold NUL", ManifestFileName)
	}
	seen := map[string]bool{}
	for _, name := range server.Secrets {
		if !ValidSecretName(name) {
			return Manifest{}, fmt.Errorf("%s: secret %q must be an environment variable name of uppercase letters, digits and underscores", ManifestFileName, name)
		}
		if seen[name] {
			return Manifest{}, fmt.Errorf("%s: secret %s appears twice", ManifestFileName, name)
		}
		seen[name] = true
	}
	return Manifest{MCP: &server}, nil
}

// ManifestOf parses the manifest among a package's files; ok is false when
// the package has none.
func ManifestOf(files []core.SkillFile) (Manifest, bool, error) {
	i := slices.IndexFunc(files, func(f core.SkillFile) bool { return f.Path == ManifestFileName })
	if i < 0 {
		return Manifest{}, false, nil
	}
	m, err := ParseManifest(files[i].Content)
	return m, true, err
}

// CommandLine shows the server's command and arguments as one line, quoting
// the arguments a shell would split or expand.
func (s MCPServer) CommandLine() string {
	parts := []string{quoteArg(s.Command)}
	for _, a := range s.Args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

var plainArg = regexp.MustCompile(`^[A-Za-z0-9_./:=@%+,-]+$`)

func quoteArg(a string) string {
	if plainArg.MatchString(a) {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}
