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
	"github.com/ycvk/acorn/internal/presence"
)

// ManifestFileName is the optional file at a package's root that declares
// what the package provides besides its instructions.
const ManifestFileName = "acorn-package.yaml"

// Manifest is a package's acorn-package.yaml: the MCP server and the
// triggers the package runs on the workstation, and the secrets both get as
// environment variables from the values the owner sets.
type Manifest struct {
	MCP      *MCPServer `yaml:"mcp"`
	Triggers []Trigger  `yaml:"triggers"`
	Secrets  []string   `yaml:"secrets"`
}

// MCPServer is the stdio MCP server a package runs on the workstation, from
// the package's directory.
type MCPServer struct {
	Command string   `yaml:"command" json:"command"`
	Args    []string `yaml:"args" json:"args,omitempty"`
}

// Deliver says where a trigger's events go.
type Deliver string

const (
	// DeliverImmediate events start a run in the package's thread.
	DeliverImmediate Deliver = "immediate"
	// DeliverDigest events wait for the morning briefing.
	DeliverDigest Deliver = "digest"
)

// Trigger runs a command from the package's directory on a cron schedule in
// the owner's time zone. Each line the command prints is an event. Timeout
// is how many seconds the command may run, up to MaxTriggerTimeoutSeconds;
// zero leaves the workstation's command timeout.
type Trigger struct {
	Name     string  `yaml:"name" json:"name"`
	Schedule string  `yaml:"schedule" json:"schedule"`
	Run      string  `yaml:"run" json:"run"`
	Deliver  Deliver `yaml:"deliver" json:"deliver"`
	Timeout  int     `yaml:"timeout" json:"timeout,omitempty"`
}

// MaxTriggerTimeoutSeconds is the longest a trigger's command may run, the
// workstation's limit for any command.
const MaxTriggerTimeoutSeconds = 3600

const maxManifestBytes = 64 << 10

var secretNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidSecretName reports whether name can be a secret's environment
// variable.
func ValidSecretName(name string) bool {
	return len(name) <= 128 && secretNamePattern.MatchString(name)
}

// ParseManifest reads acorn-package.yaml strictly: one document, no unknown
// fields, an MCP server with a command or at least one trigger, and
// well-formed secret names.
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
	if m.MCP == nil && len(m.Triggers) == 0 {
		return Manifest{}, fmt.Errorf("%s declares neither an mcp server nor triggers", ManifestFileName)
	}
	out := Manifest{Secrets: m.Secrets}
	if m.MCP != nil {
		server := MCPServer{Command: strings.TrimSpace(m.MCP.Command), Args: m.MCP.Args}
		switch {
		case server.Command == "":
			return Manifest{}, fmt.Errorf("%s: mcp.command is required", ManifestFileName)
		case strings.ContainsRune(server.Command, 0) || slices.ContainsFunc(server.Args, func(a string) bool { return strings.ContainsRune(a, 0) }):
			return Manifest{}, fmt.Errorf("%s: mcp.command and mcp.args cannot hold NUL", ManifestFileName)
		}
		out.MCP = &server
	}
	names := map[string]bool{}
	for i, t := range m.Triggers {
		trigger, err := checkTrigger(t)
		if err != nil {
			return Manifest{}, fmt.Errorf("%s: triggers[%d]: %w", ManifestFileName, i, err)
		}
		if names[trigger.Name] {
			return Manifest{}, fmt.Errorf("%s: trigger %s appears twice", ManifestFileName, trigger.Name)
		}
		names[trigger.Name] = true
		out.Triggers = append(out.Triggers, trigger)
	}
	seen := map[string]bool{}
	for _, name := range m.Secrets {
		if !ValidSecretName(name) {
			return Manifest{}, fmt.Errorf("%s: secret %q must be an environment variable name of uppercase letters, digits and underscores", ManifestFileName, name)
		}
		if seen[name] {
			return Manifest{}, fmt.Errorf("%s: secret %s appears twice", ManifestFileName, name)
		}
		seen[name] = true
	}
	return out, nil
}

func checkTrigger(t Trigger) (Trigger, error) {
	t.Name, t.Schedule, t.Run = strings.TrimSpace(t.Name), strings.TrimSpace(t.Schedule), strings.TrimSpace(t.Run)
	switch {
	case !ValidName(t.Name):
		return Trigger{}, fmt.Errorf("name %q must be 1-%d lowercase letters, digits and single hyphens", t.Name, maxNameLength)
	case t.Schedule == "":
		return Trigger{}, fmt.Errorf("trigger %s needs a schedule", t.Name)
	case t.Run == "":
		return Trigger{}, fmt.Errorf("trigger %s needs a command to run", t.Name)
	case strings.ContainsRune(t.Run, 0):
		return Trigger{}, fmt.Errorf("trigger %s: run cannot hold NUL", t.Name)
	case t.Deliver != DeliverImmediate && t.Deliver != DeliverDigest:
		return Trigger{}, fmt.Errorf("trigger %s: deliver must be immediate or digest, not %q", t.Name, t.Deliver)
	case t.Timeout < 0 || t.Timeout > MaxTriggerTimeoutSeconds:
		return Trigger{}, fmt.Errorf("trigger %s: timeout must be between 1 and %d seconds, not %d", t.Name, MaxTriggerTimeoutSeconds, t.Timeout)
	}
	if _, err := presence.ParseCron(t.Schedule); err != nil {
		return Trigger{}, fmt.Errorf("trigger %s schedule: %w", t.Name, err)
	}
	return t, nil
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
