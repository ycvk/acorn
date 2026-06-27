---
doc_type: architecture
status: current
last_reviewed: 2026-06-27
slug: runtime-context-memory
---

# Runtime Context, Memory

## Plane

`internal/runtime` assembles the context messages that are prepended to a run:

- selected skill context
- skill catalog inventory
- memory context
- deferred tool lifecycle messages

Memory context consists of file-backed prepared memory from `memory.Service.Prepare`. Working checkpoint and session summary sections have been removed.

Tool lifecycle state is derived from `tooling.ToolContract`. Runtime builds each enabled tool with explicit identity, source, kind, category, loading policy, and execution policy. Plane splits eager/deferred tools only from `ToolContract.Loading.Mode`.

Tool result messages are not durable ledger-backed. Results stay in the message stream and are subject to observation masking by Session. `OnToolResult` only validates the event payload; no SQLite ledger write.

## Hybrid Context (masking + non-blocking auto-compact + re-inject)

Session owns root-run model input. `BeforeModelCall` executes:

1. **Observation masking**: tool results older than `mask_after_turns` (default 2) turns are replaced with a compact placeholder `[tool result elided: call_id=...]`. Pure in-memory, no SQLite write.
2. **Apply pending compact**: if a background summary from a previous turn has settled, splice `[summary + current messages]`. Non-blocking — if not ready, proceed with current messages and retry next turn.
3. **LLM auto-compact (non-blocking)**: when token count exceeds `window_tokens - compact_margin` (default 13000), `maybeStartCompact` launches a background goroutine to generate a conversation summary. It returns immediately with the original messages; the summary is spliced in between turns by step 2. Circuit breaker stops after 3 consecutive failures.
4. **Re-inject**: after compact splice, system prompt + memory context + skill context are re-injected from assembly.

Non-blocking compaction keeps the controller running while the summariser works: the summary goroutine only reads its snapshot and writes into `pendingCompact`; `applyPendingCompact` runs from the session's single goroutine, so `s.messages` stays single-writer. Conversation is only spliced between turns, never mid-LLM-call.

No CompactionEngine, BudgetGovernor, reactive compact, context boundary persistence, or rehydration packet system.

Context pressure is a simple token threshold (`window_tokens - compact_margin`), not a multi-state BudgetGovernor. Public YAML exposes only `context.window_tokens`, `context.compact_margin_tokens`, `context.mask_after_turns`, `context.preserve_recent_turns`.

## MemoryModule

`internal/memory` owns file-backed memory:

- `facts/` — structured facts (Record V2 frontmatter: status / tags / created / updated / source_run / source_refs)
- `history/` — run history records
- `skills/` — learned/generated skill files (markdown + frontmatter)

Canonical Memory Record V2 frontmatter (simplified): no evidence_refs, relations, validity window, procedure origin. `remember` tool writes facts via structured `CreateFact`; `memory_create_file` still requires complete frontmatter.

