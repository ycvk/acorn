package runtime

import (
	"context"
	"fmt"
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

// skillBackend serves the run's eligible skills to the Eino skill middleware.
type skillBackend struct {
	matters   []skill.FrontMatter
	byName    map[string]skill.Skill
	revisions map[string]int64
}

func newSkillBackend(snapshot *skills.Snapshot) *skillBackend {
	b := &skillBackend{byName: map[string]skill.Skill{}, revisions: map[string]int64{}}
	for _, view := range snapshot.Skills {
		if !view.Eligible {
			continue
		}
		matter := skill.FrontMatter{Name: view.ID, Description: skillDescription(view.Spec)}
		b.matters = append(b.matters, matter)
		b.byName[view.ID] = skill.Skill{FrontMatter: matter, Content: view.Markdown}
		b.revisions[view.ID] = view.Revision
	}
	sort.Slice(b.matters, func(i, j int) bool { return b.matters[i].Name < b.matters[j].Name })
	return b
}

func (b *skillBackend) List(context.Context) ([]skill.FrontMatter, error) {
	return append([]skill.FrontMatter(nil), b.matters...), nil
}

func (b *skillBackend) Get(_ context.Context, name string) (skill.Skill, error) {
	found, ok := b.byName[strings.TrimSpace(name)]
	if !ok {
		return skill.Skill{}, fmt.Errorf("skill %q is not available; use a name from the skill tool's list", name)
	}
	return found, nil
}

// skillDescription is the summary plus a few trigger hints, which tell the
// model when the skill applies.
func skillDescription(spec skills.Spec) string {
	description := strings.TrimSpace(spec.Summary)
	if description == "" {
		description = spec.Name
	}
	if len(spec.TriggerHints) > 0 {
		description += " Use for: " + strings.Join(spec.TriggerHints[:min(4, len(spec.TriggerHints))], "; ") + "."
	}
	return description
}

func (b *skillBackend) content(_ context.Context, loaded skill.Skill, _ string) (string, error) {
	return fmt.Sprintf(skillContentFormat, loaded.Name, b.revisions[loaded.Name], loaded.Content), nil
}

func newSkillMiddleware(ctx context.Context, snapshot *skills.Snapshot) (adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage], error) {
	backend := newSkillBackend(snapshot)
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
