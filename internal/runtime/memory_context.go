package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/memory"
)

const defaultMemoryContextTokenBudget = 8000

type memoryContextSectionID string

const (
	memoryContextSectionSession  memoryContextSectionID = "session_summary"
	memoryContextSectionPrepared memoryContextSectionID = "prepared_memory"
)

type memoryContextSection struct {
	ID      memoryContextSectionID
	Content string
}

type memoryContextPacket struct {
	Sections          []memoryContextSection
	AttachedEntryRefs []string
}

func buildMemoryContextPacket(ctx context.Context, counter TokenCounter, budget int, sessionSummarySection string, prepared *memory.PrepareResult) (*memoryContextPacket, error) {
	if counter == nil {
		return nil, errors.New("memory context token counter is required")
	}

	tokenBudget := budget
	if tokenBudget <= 0 {
		tokenBudget = defaultMemoryContextTokenBudget
	}

	packet := &memoryContextPacket{}
	remaining := tokenBudget
	used := 0

	appendOptionalSection := func(id memoryContextSectionID, content string) error {
		trimmed := strings.TrimSpace(content)
		if trimmed == "" {
			return nil
		}
		tokens, err := counter.CountText(ctx, trimmed)
		if err != nil {
			return fmt.Errorf("count memory context %s tokens: %w", id, err)
		}
		if tokens > remaining {
			return nil
		}
		packet.Sections = append(packet.Sections, memoryContextSection{ID: id, Content: trimmed})
		remaining -= tokens
		used += tokens
		return nil
	}

	if err := appendOptionalSection(memoryContextSectionSession, sessionSummarySection); err != nil {
		return nil, err
	}

	if hasPreparedMemory(prepared) {
		preparedSection, attachedRefs, err := fitPreparedMemoryToBudget(ctx, counter, prepared, remaining)
		if err != nil {
			return nil, err
		}
		packet.AttachedEntryRefs = append(packet.AttachedEntryRefs, attachedRefs...)
		if strings.TrimSpace(preparedSection) != "" {
			packet.Sections = append(packet.Sections, memoryContextSection{ID: memoryContextSectionPrepared, Content: preparedSection})
			tokens, err := counter.CountText(ctx, preparedSection)
			if err != nil {
				return nil, fmt.Errorf("count memory context prepared memory tokens: %w", err)
			}
			remaining -= tokens
			used += tokens
		}
	}

	if used > tokenBudget {
		return nil, fmt.Errorf("memory context uses %d tokens over budget %d", used, tokenBudget)
	}
	if len(packet.Sections) == 0 {
		return nil, nil
	}
	return packet, nil
}

func buildMemoryMessageFromPacket(packet *memoryContextPacket) *schema.Message {
	if packet == nil {
		return nil
	}
	parts := make([]string, 0, len(packet.Sections))
	for _, section := range packet.Sections {
		parts = append(parts, section.Content)
	}
	return buildContextEnvelopeMessage("memory-context", parts...)
}

func fitPreparedMemoryToBudget(ctx context.Context, counter TokenCounter, prepared *memory.PrepareResult, maxTokens int) (string, []string, error) {
	if counter == nil {
		return "", nil, errors.New("prepared memory token counter is required")
	}
	if prepared == nil || (len(prepared.Nudges) == 0 && len(prepared.Entries) == 0 && prepared.SkillTree == nil && len(prepared.ActiveFacts) == 0) || maxTokens <= 0 {
		return "", nil, nil
	}

	lines := make([]string, 0, len(prepared.Nudges)+len(prepared.Entries)+8)
	appendIfFits := func(line string) (bool, error) {
		candidate := append([]string(nil), lines...)
		candidate = append(candidate, line)
		tokens, err := counter.CountText(ctx, strings.Join(candidate, "\n"))
		if err != nil {
			return false, fmt.Errorf("count prepared memory tokens: %w", err)
		}
		if tokens > maxTokens {
			return false, nil
		}
		lines = candidate
		return true, nil
	}

	// Active Memory: frozen snapshot of non-retired user facts, always visible.
	// Injected first so the agent sees its most important persistent facts
	// before search-matched entries.
	if len(prepared.ActiveFacts) > 0 {
		if ok, err := appendIfFits(memory.RenderActiveFacts(prepared.ActiveFacts)); err != nil {
			return "", nil, err
		} else if ok {
			if _, err := appendIfFits(""); err != nil {
				return "", nil, err
			}
		}
	}

	if prepared.SkillTree != nil {
		skillTreeText := formatSkillTree(prepared.SkillTree)
		if ok, err := appendIfFits(skillTreeText); err != nil {
			return "", nil, err
		} else if ok {
			if _, err := appendIfFits(""); err != nil {
				return "", nil, err
			}
		}
	}

	if len(prepared.Nudges) > 0 {
		if ok, err := appendIfFits("## Memory Nudges"); err != nil {
			return "", nil, err
		} else if ok {
			for _, nudge := range prepared.Nudges {
				if _, err := appendIfFits(formatMemoryNudge(nudge)); err != nil {
					return "", nil, err
				}
			}
		}
	}

	omitted := 0
	attachedRefs := make([]string, 0, len(prepared.Entries))
	if len(prepared.Entries) > 0 {
		if len(lines) > 0 {
			if _, err := appendIfFits(""); err != nil {
				return "", nil, err
			}
		}
		headingFits, err := appendIfFits("## Memory Entries")
		if err != nil {
			return "", nil, err
		}
		if !headingFits {
			return strings.Join(lines, "\n"), nil, nil
		}
		for _, entry := range prepared.Entries {
			ok, err := appendIfFits(formatMemoryEntry(entry))
			if err != nil {
				return "", nil, err
			}
			if !ok {
				omitted++
				continue
			}
			attachedRefs = append(attachedRefs, strings.TrimSpace(entry.Ref))
		}
	}

	if omitted > 0 {
		_, err := appendIfFits(fmt.Sprintf("... [%d memory entries omitted to fit total memory context budget]", omitted))
		if err != nil {
			return "", nil, err
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n")), attachedRefs, nil
}

func formatSkillTree(tree *memory.SkillTreeIndex) string {
	if tree == nil || len(tree.Categories) == 0 {
		return ""
	}
	parts := []string{"## Available Skills"}
	for name, cat := range tree.Categories {
		if len(cat.Skills) == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("- %s (%d skills):", name, len(cat.Skills)))
		for _, skill := range cat.Skills {
			parts = append(parts, fmt.Sprintf("  - %s [ref: %s, path: %s]", skill.Title, skill.Ref, skill.RelPath))
		}
	}
	parts = append(parts, "Use memory_search for semantic retrieval and memory_read_file to load a skill's full content when needed.")
	return strings.Join(parts, "\n")
}

func hasPreparedMemory(prepared *memory.PrepareResult) bool {
	return prepared != nil && (len(prepared.Nudges) > 0 || len(prepared.Entries) > 0 || prepared.SkillTree != nil || len(prepared.ActiveFacts) > 0)
}

func formatMemoryNudge(nudge memory.Nudge) string {
	parts := []string{strings.TrimSpace(nudge.Ref)}
	if kind := strings.TrimSpace(nudge.Kind); kind != "" {
		parts = append(parts, "kind="+kind)
	}
	if status := strings.TrimSpace(nudge.Status); status != "" {
		parts = append(parts, "status="+status)
	}
	if title := strings.TrimSpace(nudge.Title); title != "" {
		parts = append(parts, "title="+title)
	}
	if reason := strings.TrimSpace(nudge.Reason); reason != "" {
		parts = append(parts, "reason="+reason)
	}
	return "- " + strings.Join(NonEmptyMemoryParts(parts), " ")
}

func formatMemoryEntry(entry memory.Entry) string {
	parts := []string{strings.TrimSpace(entry.Ref)}
	if kind := strings.TrimSpace(entry.Kind); kind != "" {
		parts = append(parts, "kind="+kind)
	}
	if title := strings.TrimSpace(entry.Title); title != "" {
		parts = append(parts, "title="+title)
	}
	prefix := ""
	if joined := strings.TrimSpace(strings.Join(parts, " ")); joined != "" {
		prefix = joined + " "
	}
	return "- " + prefix + strings.TrimSpace(entry.Content)
}

func NonEmptyMemoryParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return out
}
