package skills

import (
	"maps"
	"slices"

	"github.com/ycvk/acorn/internal/core"
)

// RequiresToolsKey is the metadata key that lists, separated by spaces, the
// tools a skill cannot run without.
const RequiresToolsKey = "acorn-requires-tools"

// Spec is a parsed skill in the Agent Skills format. Revision, Markdown and
// Files come from the stored revision it was read from; MCP, Triggers and
// Secrets from its package's manifest.
type Spec struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  string
	RequiresTools []string
	Instruction   string
	Revision      int64
	Markdown      string
	Files         []core.SkillFileInfo
	MCP           *MCPServer
	Triggers      []Trigger
	Secrets       []string
}

type Problem struct {
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
}

type ScanResult struct {
	Skills   []Spec    `json:"skills,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

type View struct {
	Spec
	Eligible        bool     `json:"eligible"`
	DisabledReasons []string `json:"disabled_reasons,omitempty"`
}

type Snapshot struct {
	Skills   []View    `json:"skills,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
}

func CopySpec(item Spec) Spec {
	out := item
	out.Metadata = maps.Clone(item.Metadata)
	out.RequiresTools = slices.Clone(item.RequiresTools)
	out.Files = slices.Clone(item.Files)
	if item.MCP != nil {
		out.MCP = &MCPServer{Command: item.MCP.Command, Args: slices.Clone(item.MCP.Args)}
	}
	out.Triggers = slices.Clone(item.Triggers)
	out.Secrets = slices.Clone(item.Secrets)
	return out
}

// Hash names the package the spec was read from, as hosts hold it.
func (s Spec) Hash() string {
	return packageHash(s.Markdown, infoSums(s.Files))
}

// withManifest is the spec with what its package's manifest declares.
func (s Spec) withManifest(m Manifest) Spec {
	s.MCP, s.Triggers, s.Secrets = m.MCP, m.Triggers, m.Secrets
	return s
}

func CopyView(item View) View {
	out := item
	out.Spec = CopySpec(item.Spec)
	out.DisabledReasons = slices.Clone(item.DisabledReasons)
	return out
}

func CopySnapshot(item Snapshot) Snapshot {
	out := Snapshot{Skills: make([]View, 0, len(item.Skills)), Problems: slices.Clone(item.Problems)}
	for _, view := range item.Skills {
		out.Skills = append(out.Skills, CopyView(view))
	}
	return out
}
