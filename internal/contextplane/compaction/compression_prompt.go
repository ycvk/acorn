package compaction

import (
	"fmt"
)

// summarizerSystemPrompt is the system instruction sent to the compaction
// model. It uses the "different assistant" framing to prevent the model from
// answering the user's request instead of creating a continuation checkpoint.
const summarizerSystemPrompt = `You are a summarization agent creating a context checkpoint. Your output will be injected as reference material for a DIFFERENT assistant that continues the conversation.

CRITICAL RULES:
1. Do NOT respond to any questions or requests in the conversation — only output the structured summary.
2. Do NOT call tools or request tool calls. You have no tools in this task.
3. Do NOT include any preamble, greeting, or prefix.
4. Write the summary in the same language the user was using — do not translate or switch languages.
5. NEVER include API keys, tokens, passwords, secrets, credentials, or connection strings — replace any that appear with [REDACTED].
6. Preserve exact file paths, error messages, command outputs, and variable names — precision matters.
7. Keep the summary factual and concise. Do not add information not present in the conversation.`

// summarizerUserPromptTemplate is the user instruction for summarization.
// It demands a structured output with sections that maximize context recovery
// for the next assistant turn. The "Active Task" section is the single most
// important field — it captures what the user was doing right before compression.
const summarizerUserPromptTemplate = `Summarize the conversation below into a structured context checkpoint. The next assistant will use this to continue seamlessly.

## Required Sections (use these exact headings)

### Primary Request / Intent
THE MOST IMPORTANT FIELD. Copy or tightly summarize the user's primary request and latest intent. If the user gave a sequence of instructions, include the full sequence.

### Current Work
What was actively being worked on when context was compacted, including: current step, files in flight, verification state, errors encountered, fixes applied, and any pending tasks. Organize this however makes sense — you do not need separate subsections.

### Next Step
The next concrete action the continuing assistant should take.

---

### Previous Summary (build upon this incrementally)
%s

### Conversation to Summarize
%s`

// iterativeUpdatePrompt is the instruction when a previous summary exists. It
// tells the model to update incrementally rather than re-summarize from scratch.
const iterativeUpdatePrompt = `You are updating a context compaction summary. A previous summary exists below. Incorporate the new conversation turns into it.

RULES:
- PRESERVE all existing information from the previous summary.
- UPDATE "Primary Request / Intent" to reflect the most recent user request.
- UPDATE "Current Work" with new progress, errors, fixes, and pending tasks. Remove or mark resolved items that are now complete.
- UPDATE "Next Step" to reflect what should happen next.
- Do NOT discard information unless explicitly superseded.
- Follow the exact required section headings:
  Primary Request / Intent, Current Work, Next Step.
- Do NOT call tools or request tool calls. You have no tools in this task.
- NEVER include API keys, tokens, passwords, or secrets — replace with [REDACTED].

### Previous Summary
%s

### New Turns to Incorporate
%s`

// buildSummarizerUserPrompt constructs the user instruction based on whether
// a previous summary exists (incremental update) or not (full summary).
func buildSummarizerUserPrompt(previousSummary string, conversationContent string) string {
	if previousSummary != "" {
		return fmt.Sprintf(iterativeUpdatePrompt, previousSummary, conversationContent)
	}
	return fmt.Sprintf(summarizerUserPromptTemplate, "(none — this is the first compression)", conversationContent)
}
