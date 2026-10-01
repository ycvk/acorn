---
doc_type: architecture
status: current
last_reviewed: 2026-10-01
slug: runtime-context-memory
---

# Runtime Context, Memory

## Plane

`internal/runtime.ContextPlane` assembles the context messages that are prepended to a run:

- selected skill context
- skill catalog inventory
- memory context (Active Memory snapshot + prepared memory from `memory.Service.Prepare`)
- deferred tool lifecycle messages

Tool lifecycle state is derived from `core.ToolContract`. Plane splits eager/deferred tools only from `ToolContract.Loading.Mode`.

## Session

Session owns root-run model input. The assembled context plus the stable instruction form the session prefix, which is never compacted. Observation masking and non-blocking auto-compact operate on the conversation after the prefix; see [runtime-orchestration.md](runtime-orchestration.md).

Context pressure is a simple token threshold (`window_tokens - compact_margin_tokens`). Public YAML exposes only `context.window_tokens`, `context.compact_margin_tokens`, `context.mask_after_turns`, `context.preserve_recent_turns`.

## Memory

`internal/memory.Service` owns file-backed memory under `runtime.storage_dir`:

- `facts/` — structured facts (Record V2 frontmatter: status / tags / created / updated / source_run / source_refs)
- `history/` — append-only run history
- `skills/` — indexed for retrieval; generated skills are written by `internal/skills` under `skills/generated`
- `worldstate/state.json` — cross-run key-value WorldState, mutated only through `ApplyDelta`

The `remember` tool writes facts via structured `CreateFact`; `memory_create_file` requires complete frontmatter.

Memory follows the three-layer design of [ADR-0002](../adr/0002-three-layer-memory.md):

1. **Active Memory** — non-retired user-scoped facts, a frozen snapshot (bounded by `memory.active.char_limit`) injected into every run.
2. **Archive** — history and facts, retrieved by keyword search, or vector KNN + keyword RRF fusion when `memory.embedding.enabled` (sqlite-vec at `{storage_dir}/vectors.db`, reusing the primary provider's `/v1/embeddings`).
3. **Periodic Review** — every `memory.review.review_interval` runs, one asynchronous LLM call distills durable facts from recent runs.
