---
doc_type: architecture
status: current
last_reviewed: 2026-06-21
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

## Hybrid Context (masking + auto-compact + re-inject)

Session owns root-run model input. `BeforeModelCall` executes:

1. **Observation masking**: tool results older than `mask_after_turns` (default 2) turns are replaced with a compact placeholder `[tool result elided: call_id=...]`. Pure in-memory, no SQLite write.
2. **LLM auto-compact**: when token count exceeds `window_tokens - compact_margin` (default 13000), a model call generates a conversation summary. Old messages are replaced with `[summary + recent N turns]`. Circuit breaker stops after 3 consecutive failures.
3. **Re-inject**: after compact, system prompt + memory context + skill context are re-injected from assembly.

No CompactionEngine, BudgetGovernor, reactive compact, context boundary persistence, or rehydration packet system.

Context pressure is a simple token threshold (`window_tokens - compact_margin`), not a multi-state BudgetGovernor. Public YAML exposes only `context.window_tokens`, `context.compact_margin_tokens`, `context.mask_after_turns`, `context.preserve_recent_turns`.

## MemoryModule

`internal/memory` owns file-backed memory:

- `facts/` — structured facts (Record V2 frontmatter: status / tags / created / updated / source_run / source_refs)
- `history/` — run history records
- `skills/` — learned/generated skill files (markdown + frontmatter)

Canonical Memory Record V2 frontmatter (simplified): no evidence_refs, relations, validity window, procedure origin. `remember` tool writes facts via structured `CreateFact`; `memory_create_file` still requires complete frontmatter.

