package contextplane

import (
	"fmt"
	"regexp"
)

var (
	privateKeyBlockRe = regexp.MustCompile(`(?is)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	apiKeyTokenRe     = regexp.MustCompile(`(?i)\b(?:sk-proj|sk-ant|sk-live|sk-test|sk|ak)-[A-Za-z0-9_\-]{3,}\b`)
	bearerTokenRe     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	secretKeyValueRe  = regexp.MustCompile(`(?i)\b(api_key|apikey|api_secret|secret_key|access_key|access_token|auth_token|password|passwd|credential|private_key)\b(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s"',;]+)`)
	apiKeyHeaderRe    = regexp.MustCompile(`(?im)\b(Authorization|X-Api-Key|X-Auth-Token)(\s*:\s*)([^\r\n]+)`)
	uriPasswordRe     = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^/\s:@]+:)([^@\s/]+)(@)`)
	querySecretRe     = regexp.MustCompile(`(?i)([?&](?:api_key|apikey|access_token|auth_token|token|password|secret)=)([^&#\s]+)`)
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

## Required Sections

### Primary Request / Intent
THE MOST IMPORTANT FIELD. Copy or tightly summarize the user's primary request and latest intent. If the user gave a sequence of instructions, include the full sequence.

### Technical Concepts
Important domain concepts, architecture terms, runtime components, protocols, APIs, and invariants.

### Files and Code Sections
File paths, functions, types, config keys, docs, tests, commands, and code sections that were read, modified, or discussed.

### Errors and Fixes
Exact errors, failed commands, blocked checks, fixes applied, and unresolved failures.

### Problem Solving
Numbered list of significant actions taken. Each entry should include:
- Tool name and key arguments (especially file paths)
- Outcome (success/failure/partial)
Keep entries concise — focus on what changed, not how.

### All User Messages
List every user message or explicit user instruction that matters for continuation. Preserve exact wording for hard constraints.

### Pending Tasks
Explicit user asks or implementation steps that have not been completed yet.

### Current Work
What was actively being worked on when context was compacted, including current step, files in flight, verification state, and known dirty-worktree facts.

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
- UPDATE sections where new information supersedes old information.
- ADD new entries to "Problem Solving", "Files and Code Sections", "Pending Tasks", "Current Work", etc.
- MOVE completed items from "Current Work" or "Pending Tasks" into "Problem Solving" when appropriate.
- UPDATE "Primary Request / Intent" to reflect the most recent user request.
- REMOVE items from "Errors and Fixes" if they were resolved in the new turns, or mark them resolved with the fix.
- Do NOT discard information unless explicitly superseded.
- Follow the exact required section headings:
  Primary Request / Intent, Technical Concepts, Files and Code Sections, Errors and Fixes, Problem Solving, All User Messages, Pending Tasks, Current Work, Next Step.
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

// redactSecrets removes common secret patterns before summaries are reused as
// model-facing context or persisted as in-run compression state.
func redactSecrets(s string) string {
	result := s
	result = privateKeyBlockRe.ReplaceAllString(result, `[REDACTED:private-key]`)
	result = apiKeyHeaderRe.ReplaceAllString(result, `${1}${2}[REDACTED]`)
	result = bearerTokenRe.ReplaceAllString(result, `Bearer [REDACTED]`)
	result = apiKeyTokenRe.ReplaceAllString(result, `[REDACTED:api-key]`)
	result = secretKeyValueRe.ReplaceAllString(result, `${1}${2}[REDACTED]`)
	result = uriPasswordRe.ReplaceAllString(result, `${1}[REDACTED]${3}`)
	result = querySecretRe.ReplaceAllString(result, `${1}[REDACTED]`)
	return result
}
