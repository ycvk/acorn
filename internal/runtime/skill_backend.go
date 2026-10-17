package runtime

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"

	"github.com/ycvk/acorn/internal/skills"
)

// skillSystemPrompt replaces Eino's default skill prompt, which assumes the
// agent can run scripts from skill directories.
const skillSystemPrompt = `Skills are written procedures for recurring kinds of work. The %s tool lists the skills you have and loads one by name. When a task matches a skill's description, load the skill first and follow it.`

// skillContentFormat is a loaded skill: its whole document and the revision
// skill_write and skill_retire take as base_revision.
const skillContentFormat = "Skill %s, revision %d.\n\n%s"

// skillFilesFormat tells where a loaded skill's files are.
const skillFilesFormat = "Skill %s, revision %d. Its files are in %s on your workstation: %s. Paths in the skill are relative to that directory; read them with files and run scripts with shell.\n\n%s"

// skillToolsFormat is skillFilesFormat for a skill whose package runs an MCP
// server.
const skillToolsFormat = "Skill %s, revision %d. Its files are in %s on your workstation: %s. Paths in the skill are relative to that directory; read them with files and run scripts with shell. Its package's MCP server gives you the tools named %s*.\n\n%s"

// skillBackend serves the run's eligible skills to the Eino skill middleware.
type skillBackend struct {
	matters []skill.FrontMatter
	byName  map[string]skills.View
	dir     string
}

func newSkillBackend(snapshot *skills.Snapshot, dir string) *skillBackend {
	b := &skillBackend{byName: map[string]skills.View{}, dir: dir}
	for _, view := range snapshot.Skills {
		if !view.Eligible {
			continue
		}
		b.matters = append(b.matters, skill.FrontMatter{Name: view.Name, Description: view.Description})
		b.byName[view.Name] = view
	}
	sort.Slice(b.matters, func(i, j int) bool { return b.matters[i].Name < b.matters[j].Name })
	return b
}

func (b *skillBackend) List(context.Context) ([]skill.FrontMatter, error) {
	return append([]skill.FrontMatter(nil), b.matters...), nil
}

func (b *skillBackend) Get(_ context.Context, name string) (skill.Skill, error) {
	view, ok := b.byName[strings.TrimSpace(name)]
	if !ok {
		return skill.Skill{}, fmt.Errorf("skill %q is not available; use a name from the skill tool's list", name)
	}
	return skill.Skill{FrontMatter: skill.FrontMatter{Name: view.Name, Description: view.Description}, Content: view.Markdown}, nil
}

func (b *skillBackend) content(_ context.Context, loaded skill.Skill, _ string) (string, error) {
	view := b.byName[loaded.Name]
	if len(view.Files) == 0 {
		return fmt.Sprintf(skillContentFormat, view.Name, view.Revision, view.Markdown), nil
	}
	paths := make([]string, 0, len(view.Files))
	for _, f := range view.Files {
		paths = append(paths, f.Path)
	}
	if view.MCP != nil {
		return fmt.Sprintf(skillToolsFormat, view.Name, view.Revision, path.Join(b.dir, view.Name), strings.Join(paths, ", "), skills.MCPToolPrefix(view.Name), view.Markdown), nil
	}
	return fmt.Sprintf(skillFilesFormat, view.Name, view.Revision, path.Join(b.dir, view.Name), strings.Join(paths, ", "), view.Markdown), nil
}

func newSkillMiddleware(ctx context.Context, snapshot *skills.Snapshot, dir string) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	backend := newSkillBackend(snapshot, dir)
	middleware, err := skill.NewTyped[*schema.AgenticMessage](ctx, &skill.TypedConfig[*schema.AgenticMessage]{
		Backend:      backend,
		BuildContent: backend.content,
		CustomSystemPrompt: func(_ context.Context, toolName string) string {
			return fmt.Sprintf(skillSystemPrompt, toolName)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("skill middleware: %w", err)
	}
	return middleware, nil
}
