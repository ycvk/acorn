package contextplane

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const defaultRehydratePlanTokenBudget = 50000
const defaultRehydrateMaxPacketTokens = 15000

type RehydratePacketKind string

const (
	RehydrateWorkingCheckpoint RehydratePacketKind = "working_checkpoint"
	RehydrateSelectedSkill     RehydratePacketKind = "selected_skill"
	RehydrateSkillCatalog      RehydratePacketKind = "skill_catalog"
	RehydrateToolState         RehydratePacketKind = "tool_state"
	RehydrateSessionSummary    RehydratePacketKind = "session_summary"
	RehydratePreparedMemory    RehydratePacketKind = "prepared_memory"
	RehydratePlanState         RehydratePacketKind = "plan"
	RehydrateRecentFiles       RehydratePacketKind = "recent_files"
)

type RehydrateRequest struct {
	SessionID          string
	RunID              string
	Mode               string
	Messages           []adk.Message
	ToolState          *ToolLifecycleState
	CurrentPlan        string
	RecentTouchedPaths []string
	TokenBudget        int
	TokenCounter       TokenCounter
}

type RehydratePlan struct {
	Packets      []RehydratePacket
	TokenBudget  int
	TokensBefore int
	TokensAfter  int
}

type RehydratePacket struct {
	Kind       RehydratePacketKind
	Source     string
	Content    string
	TokenLimit int
}

func BuildRehydrateMessages(plan *RehydratePlan) []adk.Message {
	if plan == nil || len(plan.Packets) == 0 {
		return nil
	}
	messages := make([]adk.Message, 0, len(plan.Packets))
	for _, packet := range plan.Packets {
		content := strings.Join([]string{
			"<rehydrate-packet>",
			referenceContextNote,
			"Kind: " + string(packet.Kind),
			"Source: " + packet.Source,
			fmt.Sprintf("Token limit: %d", packet.TokenLimit),
			"",
			packet.Content,
			"</rehydrate-packet>",
		}, "\n")
		messages = append(messages, &schema.Message{Role: schema.User, Content: content})
	}
	return messages
}

func cloneRehydratePlan(plan *RehydratePlan) *RehydratePlan {
	if plan == nil {
		return nil
	}
	clone := *plan
	clone.Packets = append([]RehydratePacket(nil), plan.Packets...)
	return &clone
}

type rehydratePlanBuilder struct {
	ctx          context.Context
	tokenCounter TokenCounter
	plan         RehydratePlan
}

func (b *rehydratePlanBuilder) append(kind RehydratePacketKind, source string, content string) error {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}
	tokens, err := b.tokenCounter.CountText(b.ctx, trimmed)
	if err != nil {
		return fmt.Errorf("count rehydrate packet %s tokens: %w", kind, err)
	}
	b.plan.TokensBefore += tokens
	if tokens > defaultRehydrateMaxPacketTokens {
		return fmt.Errorf("rehydrate packet %s requires %d tokens over limit %d", kind, tokens, defaultRehydrateMaxPacketTokens)
	}
	if b.plan.TokensAfter+tokens > b.plan.TokenBudget {
		return fmt.Errorf("rehydrate packet %s would exceed plan budget %d", kind, b.plan.TokenBudget)
	}
	b.plan.Packets = append(b.plan.Packets, RehydratePacket{
		Kind:       kind,
		Source:     strings.TrimSpace(source),
		Content:    trimmed,
		TokenLimit: defaultRehydrateMaxPacketTokens,
	})
	b.plan.TokensAfter += tokens
	return nil
}

func extractTaggedContent(messages []adk.Message, tag string) string {
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if content := extractTaggedBlock(messageText(msg), tag); content != "" {
			return content
		}
	}
	return ""
}

func extractTaggedBlock(content string, tag string) string {
	trimmedTag := strings.TrimSpace(tag)
	if trimmedTag == "" {
		return ""
	}
	startTag := "<" + trimmedTag + ">"
	endTag := "</" + trimmedTag + ">"
	start := strings.Index(content, startTag)
	if start < 0 {
		return ""
	}
	start += len(startTag)
	end := strings.Index(content[start:], endTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(content[start : start+end])
}

func removeTaggedBlock(content string, tag string) string {
	trimmedTag := strings.TrimSpace(tag)
	if trimmedTag == "" {
		return content
	}
	startTag := "<" + trimmedTag + ">"
	endTag := "</" + trimmedTag + ">"
	start := strings.Index(content, startTag)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start+len(startTag):], endTag)
	if end < 0 {
		return content
	}
	removeEnd := start + len(startTag) + end + len(endTag)
	return strings.TrimSpace(content[:start] + "\n" + content[removeEnd:])
}

func extractPreparedMemoryPacket(memoryContext string) string {
	trimmed := strings.TrimSpace(memoryContext)
	if trimmed == "" {
		return ""
	}
	trimmed = removeTaggedBlock(trimmed, "working-checkpoint")
	trimmed = removeTaggedBlock(trimmed, "session-summary")
	trimmed = strings.ReplaceAll(trimmed, referenceContextNote, "")
	trimmed = strings.TrimSpace(trimmed)
	if !strings.Contains(trimmed, "## Memory Nudges") && !strings.Contains(trimmed, "## Memory Entries") {
		return ""
	}
	return trimmed
}

func formatToolStatePacket(state *ToolLifecycleState) string {
	if state == nil {
		return ""
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	var sections []string
	if len(state.LoadedTools) > 0 {
		lines := []string{"Loaded tools:"}
		for _, name := range sortedLoadedToolNamesLocked(state) {
			record := state.LoadedTools[name]
			line := "- " + record.Name
			if record.LoadSource != "" {
				line += " source=" + record.LoadSource
			}
			lines = append(lines, line)
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(state.DeferredTools) > 0 {
		lines := []string{"Deferred tools:"}
		for _, name := range sortedDeferredToolNamesLocked(state) {
			record := state.DeferredTools[name]
			parts := []string{"- " + record.Name}
			if record.Reason != "" {
				parts = append(parts, "reason="+record.Reason)
			}
			if record.Description != "" {
				parts = append(parts, "description="+record.Description)
			}
			lines = append(lines, strings.Join(parts, " "))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(state.RecentResults) > 0 {
		lines := []string{"Recent tool result refs:"}
		for _, record := range state.RecentResults {
			parts := []string{
				"- call_id=" + record.CallID,
				"tool=" + record.ToolName,
				fmt.Sprintf("turn=%d", record.TurnIndex),
				"ref=" + record.ResultRef,
			}
			if record.IsError {
				parts = append(parts, "error=true")
			}
			lines = append(lines, strings.Join(nonEmptyMemoryParts(parts), " "))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func formatRecentTouchedPaths(paths []string) string {
	seen := make(map[string]struct{}, len(paths))
	var cleaned []string
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		return ""
	}
	sort.Strings(cleaned)
	lines := []string{"Recent touched paths:"}
	for _, path := range cleaned {
		lines = append(lines, "- "+path)
	}
	return strings.Join(lines, "\n")
}
