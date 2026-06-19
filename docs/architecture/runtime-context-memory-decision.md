---
doc_type: architecture
status: current
last_reviewed: 2026-05-18
slug: runtime-context-memory-decision
---

# Runtime Context, Memory, Decision

## ContextPlane

`internal/contextplane` assembles the context messages that are prepended to a run:

- selected skill context
- skill catalog inventory
- memory context
- deferred tool lifecycle messages

Memory context now consists of:

- file-backed prepared memory from `memorymodule.Service.Prepare`
- current working checkpoint
- session summary

ContextPlane no longer loads SQLite core-memory blocks, old knowledge facts, MemoryLens results, read plans, access logs, retrieval cards, or `hydrate_memory_refs` state. Provider errors are run-build errors; empty memory is represented by no memory message.

Tool lifecycle state is derived from `tooling.ToolContract`, not tool-name hard-code. Runtime builds each enabled tool with explicit identity, source, kind, category, resource scope, profiles, loading policy, execution policy, and plan policy; catalog construction fails if a contract is incomplete. ContextPlane splits eager/deferred tools only from `ToolContract.Loading.Mode`, and deferred records keep the contract reason.

Tool result truth is now durable and ledger-backed. `ContextPlane.OnToolResult` writes each tool result to SQLite `tool_results` through the `internal/store.ToolResultLedger` contract, using a deterministic `tool_result_ref` plus preview, full text, token estimate, status, arguments, side-effect refs, and evidence refs. Workspace mutation checkpoint and rollback side effects ride on the same refs. The lifecycle state keeps the same durable ref in `RecentResults`; missing ledger wiring is a runtime failure, not a fallback.

Procedure activation is a runtime trace, not a second durable procedure store. `memorymodule.Prepare` emits matched `ProcedureActivation` records, ContextPlane appends injected activations only for procedure entries actually attached to the memory context, and runtime emits selected/used activations for executable skills chosen by Decision. These activations are persisted as diagnostic `procedure.activation` events; they do not enter the mobile live RunEvent subset, block execution, or infer whether the model semantically followed a procedure.

Working checkpoints are owned by `internal/workingstate`. The `update_working_checkpoint` and `clear_working_checkpoint` tools are built there and registered by runtime as working-state tools, not as durable memory-module behavior.

Run archives, session summaries, run context snapshots, and context boundaries are runtime history model state owned by `internal/model` and persisted through store ports/SQLite; they are not durable file-backed memory records.

Context boundaries are the persisted fact layer for compacted context segments. A boundary records the session/run identity, sequence, turn index, root mode, compact trigger, covered message range, transcript reference, summary message reference, preserved segment references, token metrics, and created timestamp. There is no parallel compressed RunEvent truth.

Context pressure is computed by `BudgetGovernor`, not by percentage thresholds. Public YAML exposes only `context.window_tokens`, `context.compact_margin_tokens`, `context.preserve_recent_turns`, and `context.summary_max_tokens`; config derives the internal policy from those fields plus the enabled provider's `max_completion_tokens` and runtime defaults. The governor derives an effective input window from model context window minus reserved output/summary tokens and static overhead, then classifies pressure as `ok`, `warning`, `auto_compact`, or `blocking` using derived buffers. Both the ADK compression adapter and `ContextSession.BeforeModelCall` consume these states; pressure requiring compact cannot silently continue with the old full history.

ContextPlane initial assembly uses the same `TokenCounter` implementation as BudgetGovernor and CompactionEngine. Critical model-input context no longer uses `rune/4` estimates or the old `trimToBudget` string trimmer. Selected skill, skill catalog inventory, and memory context are assembled as complete context messages; if the assembled context exceeds the derived assembly budget, assembly fails instead of silently dropping active instructions. The skill catalog is a compact inventory of currently scanned skills plus runtime eligibility, meant to answer capability questions and route into `skill_list` / `skill_view` before the model falls back to a generic answer. Memory context fits prepared memory by complete nudge/entry lines under the memory token budget; working checkpoint is required when present, while session summary and prepared memory can be omitted as whole sections or entries.

Tool result messages are no longer passed through a character-count `toolOutputCompressor` before model calls. Current turn tool output remains the real tool result. When a tool result ages out of the live turn window, the lifecycle middleware replaces it with a durable `tool_result_ref` marker instead of a head/tail preview.

Context pressure is the `BudgetPressure` value returned by contextplane to runtime model-call decisions. It is not persisted as a diagnostic RunEvent; mobile code does not estimate pressure from message length or token counters.

`ContextSession` is the root-run model input owner. Runtime bootstraps each run from ContextPlane assembly plus initial user messages, returns copied `ModelInput` values, binds the session into the root runner execution context, and records direct_response assistant/tool messages through `RecordAssistant` and `RecordToolResults`. For direct_response, the stable instruction is a leading system message in ContextSession bootstrap, not a local prepend in the agent loop. Resume does not read old sliding-window markers or reconstruct boundaries from event payloads.

`ContextSession.BeforeModelCall` owns direct_response proactive compact. It evaluates BudgetGovernor pressure with the currently loaded tool infos and calls `CompactionEngine` for `auto_compact` or `blocking` states when compaction is allowed. Missing compaction engine, compact-disabled pressure, invalid compact output, or compression event emission failure are runtime errors; there is no fallback to the pre-compact message chain.

Reactive compact is only triggered by explicit provider/model context overflow errors such as `context_length_exceeded`, `model_context_window_exceeded`, or prompt-too-long messages. `ContextSession.ReactiveCompact` uses `CompactTriggerReactive`, the same CompactionEngine and post-compact rehydration protocol, and returns a compacted model input for one same-provider retry. Rate limit, auth, network, tool, parser, and ordinary runtime errors are not compacted or retried.

`CompactionEngine` owns proactive compact execution. The ADK compression middleware evaluates pressure through `BudgetGovernor` and, only for `auto_compact` or `blocking`, delegates to the engine. The engine builds a no-tool summary input, calls the configured summary model without tool infos, rejects tool-call responses, validates the required structured continuation sections, preserves the recent tail without splitting assistant tool-call/tool-result pairs, and returns the final messages plus `CompressionOutcome`. Empty or invalid summaries fail the model call path loudly; the middleware no longer owns summary prompt/finalize/outcome callbacks.

ContextPlane's concrete post-compact rehydration helper owns context packets. During compaction, the engine asks the helper to extract active context from compact-before messages and tool lifecycle state, then injects packet messages after the continuation summary and before the preserved tail. Current packet kinds are working checkpoint, selected skill, skill catalog, tool state, session summary, prepared memory, plan, and recent files. Packet content has a source and token limit counted by the shared token counter; an oversized packet fails compaction instead of being string-truncated. Recent files only come from explicit request paths and are not discovered by scanning the workspace.

## Memory Module

`internal/memorymodule` is the active memory boundary. It owns:

- `facts/`
- `skills/`
- `history/`
- `Prepare`
- `Search`
- `AppendHistory`
- `BuildMemoryInstruction`

Runtime code does not parse memory markdown, scan memory files, or format memory nudges itself. It asks the module for a `PrepareResult`, emits `memory.prepared`, and passes that result into ContextPlane. Runtime also asks the module for a direct memory instruction string before execution when the run path needs a reminder to update durable memory.

Memory records use the canonical Record V2 projection. Facts, procedure records, history projections, search results, `/v1/memory/*` DTOs, and semantic rebuild inputs all carry the same metadata shape: `status`, `tags`, `created`, `updated`, validity window, `source_run`, `source_refs`, `evidence_refs`, and typed relations. Supported relation types are `supports`, `derived_from`, `supersedes`, and `contradicts`. Unknown frontmatter keys, unknown relation types, unresolved relation targets, and malformed provenance refs are errors, not best-effort warnings. Facts are written through the structured `remember` tool (`LocalService.CreateFact`), which generates the frontmatter and auto-stamps `status`/`created`/`updated`/`scope`; `tags` and the validity/provenance/relation fields are optional. The raw `memory_create_file` path still requires complete frontmatter and is retained for precise edits and CLI use.

`Search`, `Prepare`, memory list APIs, and semantic projection share `RecordSelection`. The default selection is active records only: retired records are excluded, records outside their validity window are inactive, and records superseded by an active `supersedes` relation are not returned. `IncludeRetired=true` implies `IncludeInactive=true`; explicit inactive/retired reads are available to operators and remote clients, but prompt injection remains active/admissible only.

`skills/` records are procedure records, not executable `internal/skills` specs. Their durable schema is parsed by `memorymodule` as `ProcedureRecord`: `origin` must be `human`, `agent_draft`, or `action_verified`; `task_pattern`, `status`, `created`, and `updated` are required; validity, source refs, evidence refs, and typed relations preserve lifecycle, provenance, and verification evidence. `agent_draft` procedures must be `status: unverified` and carry `source_run`; `action_verified` procedures must be `status: verified` and carry both `source_run` and `evidence_refs`. Old `origin: built-in` / `origin: learned` procedure frontmatter is not accepted.

`Prepare` can search and nudge unverified procedure records, but it only injects admissible verified procedures into model context. Human verified procedures are admissible; action-verified procedures are admissible only with evidence refs. Agent drafts remain search/nudge-only until a later action-verified promotion path writes verified evidence. Runtime does not create a second SQLite procedure truth.

`Search` and `Prepare` can return optional `SearchExplain` metadata when callers set `Explain=true`. Explain records the ranking stages that ran (semantic vector/hybrid candidates, source-ref backlink boost, typed relation boosts) and each returned item's final score; it does not carry a per-item score-contribution ledger. Relation stages are deterministic ranking signals; they never replace canonical refs with prose, and unresolved relation targets fail search/rebuild instead of being skipped. This metadata is for tests, replay, and debug; ContextPlane does not parse it or inject it into model context. Search paths share fail-loud semantics for retrieval boost failures.

Retrieval ranking includes deterministic source-backed boosts. `source_ref_backlink` resolves `source_refs` back to canonical L0 records, respects active selection, and adds or boosts those targets, recorded as a `source_ref_backlink` explain stage. Relation boosts resolve typed relation targets and are recorded as `relation_<type>` stages. Missing source refs or relation targets fail search instead of being skipped.

Semantic retrieval contracts and explicit rebuild now exist in `internal/memorymodule` for the active Bleve+FAISS retrieval index: `Embedder`, `SemanticIndex`, embed request/result types, `SemanticRecord`, `IndexedSemanticRecord`, rebuild request/result types, semantic search request/result types, and explain stages for `semantic_vector`, `semantic_fts`, and `semantic_hybrid`. `LocalService.RebuildSemanticIndex` projects canonical Record V2 file-backed records, computes deterministic content hashes over body plus v2 metadata, batches non-empty record text through a real OpenAI-compatible embedding endpoint, validates ordered vectors/model/dimensions, and commits the indexed records to `SemanticIndex.Rebuild`. `SemanticRecord` text/hash/Bleve document projection includes provenance, validity, lifecycle, origin/task pattern, tags, and relations. The Bleve adapter prevalidates vector dimensions before indexing because mismatched document vectors are not acceptable runtime truth. It does not create fake vectors, partial-success receipts, or a keyword fallback when embedding/indexing fails.

The active semantic rebuild command is `acorn memory semantic rebuild [-c path] [--json]`. There is no `memory.semantic.enabled` switch. Semantic dependencies are wired lazily: container startup (and thus `acorn pair` / `doctor` / `serve`) never opens Bleve/FAISS or constructs the embedder, so it is not blocked by FAISS availability or embedding configuration. The embedder and Bleve+FAISS index are built on the first real `Search`/`Prepare`/rebuild and fail loudly there — in a non-FAISS build that first semantic call returns `ErrBleveFAISSSupportNotBuilt` (no silent degradation, no keyword fallback). Semantic retrieval is an optional enhancement, not a gate on running a task. When embedding is not configured at all, no semantic runtime is wired (`Config.MemorySemanticConfigured` keys off embedding model/base_url, and `ValidateExecutionReady` does not require the embedding section): explicit retrieval (`Search`/`SearchSemantic`, the memory search tool, `/v1/memory/search`, `acorn memory`) still fails loud with "semantic search runtime is required", while the run hot-path `Prepare` degrades to an empty memory result so the run still proceeds (zero recalled memory is a legal baseline, not a keyword fallback). Once a semantic runtime is wired, a failing embed/index call still fails loud. Bleve imports are confined by `tests/architecture/bleve_faiss_release_guard_test.go` to the `bleve_faiss,vectors,cgo` build-tagged adapter file. The semantic runtime makes `LocalService.Search` embed the query, ask `SemanticIndex.Search` for Bleve hybrid text/vector candidates, resolve every hit back to canonical Record V2 memory records, preserve Go-owned active selection, scope/kind filtering, source-ref boost, and typed relation boost, and fail loudly on provider/index errors, missing canonical refs, unresolved source/relation targets, stale model/dimensions, or schema mismatch. `Prepare` inherits semantic candidate generation through `Search` and keeps its existing procedure admission policy.

SQLite `conversation_segments` remains a runtime persisted fact table written at run finalization and readable by segment/run identity. The old SQLite `conversation_segments_idx` FTS table, `conv_seg_*` triggers, and `SearchConversationHistory` reader have been removed; store open drops those objects from existing DBs. Conversation/history retrieval must go through `memorymodule.Search` and the Bleve+FAISS semantic index rather than a SQLite FTS mirror.

Release packaging always includes Bleve+FAISS. `scripts/build-release.sh` always builds with `CGO_ENABLED=1` and `-tags "bleve_faiss vectors"`. `scripts/build-faiss-artifacts.sh` builds the Bleve-compatible FAISS fork at the pinned checkpoint and validates `include/faiss/c_api/Index_c.h`, `lib/${GOOS}_${GOARCH}/libfaiss_c.so`, and `lib/${GOOS}_${GOARCH}/libfaiss.so` for each requested Linux target. `linux/amd64` and `linux/arm64` GitHub Release assets therefore include the native `libfaiss*.so*` runtime libraries; missing native artifacts or toolchain failures are release failures, not reasons to publish a non-FAISS fallback package.

Terminal finalization appends a compact Record V2-compatible history event through `memorymodule.AppendHistory`. Durable fact/learned-skill updates are done by the agent through `memory_search`, `memory_read_file`, `memory_create_file`, and `memory_replace_span`; there is no separate memory-root grep tool and no backend LLM distillation worker in the ordinary run path. Memory write helpers run service-owned mutation application: they plan first, reject invalid Record V2 writes before touching disk, write only inside `facts/` or `skills/`, refresh the in-memory canonical index after successful writes, and rebuild the semantic retrieval index when semantic runtime is configured. The planner is a validation boundary for file writes, not a candidate inbox or automatic memory writer.

## Removed Old Memory Path

The following old paths are no longer active runtime truth:

- `internal/reflection`
- reflection proposal staging / approve / reject / rollback
- background review daemon
- decision `ActionEvolve`
- memory candidate review / backend admission queue / background distillation
- old FactService-backed execution-path crystallization
- the removed optional auto-crystallization pipeline
- MemoryLens, read plans, access logs, usage envelope
- `search_knowledge` and `RetrievalService`
- SQLite core-memory injection into prompt context
- one-shot `acorn memory migrate`
- runtime sliding-window marker compression (`[Earlier conversation compressed]`)
- run-wide cumulative `TokenBudget` hard stop and `token_budget.exceeded` events
- the never-wired opt-in retrieval/skill-routing eval sample schema + JSONL sink (`memorymodule` capture, `skills` candidate capture); no runtime path ever created a sink, so the speculative infrastructure was removed rather than parked

SQLite legacy memory tables/readers are removed, not parked behind a migration command. The schema migration drops leftover old memory/search/patch-history tables on open.

Procedure records now enter through the active `memorymodule` and skill lifecycle paths. There is no separate runtime auto-crystallization service, insight-index adapter, or `crystallization.*` RunEvent contract.

## Skills

`internal/skills` remains the skill loader and selector for executable native skills. Current source boundaries are explicit:

- `./skills` is the release seed pack source and the local-development builtin source.
- release installs bundled seed skills under the installer-owned `~/.acorn/skills`.
- `{runtime.storage_dir}/skills/generated` stores generated skills created by the runtime.
- `./.acorn/skills/workspace` stores workspace-local writable executable skills.
- `~/.acorn/skills` also stores user-local executable skills.

Seed skills include `skill.creator` and `skill.procedure.curator` plus inspection/debug/patch defaults. `skill.creator` writes Acorn-native `SKILL.md` and supporting files through the `skill_create` runtime tool. `skill_assess` applies evidence-backed lifecycle updates to mutable skill sources. Non-builtin `lifecycle_status: verified` requires `evidence_refs`.

Executable skill health is a deterministic `internal/skills` contract. `BuildHealthReport` consumes the current `ScanResult`, eligibility context, and optional routing fixtures; it reports loader problems, eligibility failures, unreachable skills, exact duplicate trigger/task-pattern failures, and expected-skill routing fixture misses. Health checks do not mutate skill files, do not promote lifecycle status, and do not restore `skill_eval` / `skill_curate`; `skill_assess` remains the active lifecycle action.

Learned memory skills are still file-backed memory records under `memorymodule/skills/`; they are procedures, not executable `internal/skills` specs. Runtime exposes memory file tools so the agent can create or edit procedure records as files. The skill lifecycle path does not move builtin executable skills into `memorymodule`.

## Decision

`internal/decision` is a small run selection policy for tool-enabled modes. It selects direct execution, skill execution, ask-user, block, or resume behavior from explicit skill input, eligible skill candidates, workspace decision profile, and working context state. Runtime persists the decision record, resolves the selected skill and context priority, then ContextPlane assembles messages from the current decision, current skill catalog inventory, working checkpoint, session summary, and prepared file-backed memory.

## Config

The active memory config keeps file-backed context budgeting and requires semantic retrieval configuration:

```yaml
memory:
  search:
    memory_context_token_budget: 2000
  semantic:
    bleve:
      path: ""
      index_name: memory_records
    embedding:
      provider: openai_compatible
      model: text-embedding-3-small
      base_url: https://api.openai.com/v1
      api_key: ${OPENAI_API_KEY}
      dimensions: 1536
      timeout_seconds: 30
      batch_size: 64
```

`memory.semantic` is required configuration for the rebuildable semantic retrieval index, not current L0 memory truth. There is no enable switch, and config load does not open Bleve or call an embedding provider. Runtime composition injects lazy Bleve+FAISS index and embedder wrappers; they are actually opened (including `ValidateMemorySemanticReady`) on the first real `Search`/`Prepare`/rebuild, failing loudly there if the binary lacks Bleve+FAISS support or embedding is misconfigured. Config validation requires an independent embedding API key after environment expansion; chat provider credentials are not reused as semantic embedding credentials. Unknown provider and invalid embedding numeric values are config errors.

Old `memory.blocks`, `memory.facts`, `memory.end_of_run`, `memory.background_review`, segmenter, max fact, and max archive settings have been removed.

## Invariants

- Runtime memory reads go through `memorymodule.Service.Prepare`.
- Runtime memory writes go through `memorymodule.AppendHistory` or explicit file-backed memory tools.
- Canonical memory reads use Record V2 metadata and active selection; clients must not infer active status, relation resolution, or provenance from raw markdown.
- Procedure durable truth is file-backed `memorymodule/skills/` with `ProcedureRecord` schema; there is no SQLite procedure table or compatibility reader for old procedure origins.
- Agent-written procedure drafts must be `origin: agent_draft`, `status: unverified`, and include `source_run`; action-verified procedures must include `source_run` plus `evidence_refs`.
- Procedure activation truth is observable through persisted diagnostic `procedure.activation` events; matched comes from memorymodule, injected comes from ContextPlane attachment, and selected/used executable-skill activations come from Decision/skill selection.
- Native skill lifecycle truth is observable through persisted diagnostic `skill.lifecycle` events and file-backed skill frontmatter. Generated/workspace/user skills can be curated by `skill_assess`; release seed updates are delivered by the installer.
- ContextPlane does not know old memory store internals.
- Context compact/resume work must use persisted context boundaries as runtime-history facts, not durable memory records and not `events.payload_json` reconstruction.
- Context pressure must use BudgetGovernor effective-window thresholds from derived context policy; `threshold_pct`, client-side estimates, and character-count fallback are not active context truth.
- Client pressure/boundary visibility must come from backend-owned trace projections when explicitly exposed, not client-local estimation, old compact marker parsing, or a broad RunDetail workbench diagnostic aggregate.
- Sliding-window marker compression, public `compression.*` config, `compression.max_history_turns`, `compression.hard_token_cap_pct`, and run-wide `TokenBudget` are not active runtime paths.
- Root run initial model input must be produced through ContextSession Bootstrap; direct prepend helpers are not the execution truth.
- Root runner execution context must carry ContextSession. direct_response must fail loudly if the binding is missing and must not fall back to ADK runner input messages.
- Reactive compact is a narrow provider-overflow recovery path with one retry on the same model/options. Non-overflow provider/runtime errors remain explicit failures.
- Proactive compact rules must live in CompactionEngine; middleware adapters cannot own summary shape, tail cutting, summary validation, or compression metrics.
- Post-compact active context must be restored as contextplane rehydration packets with explicit kind/source/token limits; oversized packets fail instead of being truncated, and compact summary alone is not enough continuation context.
- Tool failures remain tool results; Acorn runtime wiring/storage/model failures fail the run.
- No silent fallback path recreates old memory behavior.
