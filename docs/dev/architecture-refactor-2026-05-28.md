---
doc_type: implementation-plan
status: proposed
last_reviewed: 2026-05-29
slug: architecture-refactor-2026-05-28
---

# Acorn Architecture Refactor Plan 2026-05-28

This plan is intentionally destructive: when a new owner is chosen, the old path should be removed in the same change. It is not a compatibility plan. It is also not permission to weaken persisted truth, fail-loud validation, or remote/mobile contracts.

## Research Baseline

Primary Go guidance used for this plan:

- Go modules should use `internal/` to create compile-time import boundaries and leave room for implementation refactors. Source: https://go.dev/doc/modules/layout
- Packages should be named from the client point of view and should group cohesive behavior, not arbitrary technical buckets or file-count targets. Source: https://go.dev/blog/package-names
- Interfaces are best defined by the consuming package when they describe the consumer's required behavior. Implementing packages should usually return concrete types. Source: https://go.dev/wiki/CodeReviewComments#interfaces
- Tests should be written at the level that proves the behavior under risk. Pure logic should move into small functions with cheap tests; persisted runtime behavior should keep real SQLite integration tests.

Practical Acorn interpretation:

- `internal/store/sqlite` is an adapter. It may be opened and wired by the app composition root; app/runtime/provider business logic should consume narrow ports.
- Store ports are not automatically "zombie interfaces." A single implementation is acceptable when the interface is owned by the consumer and protects a business boundary.
- Deleting a package or interface is only a simplification if the new owner is clearer and tests still prove the original contract.
- Reducing file count is a metric, not the objective. The objective is lower coupling, clearer ownership, and faster local reasoning.

## Non-Negotiable Contracts

These contracts must survive every phase unless a later accepted design explicitly replaces them with equivalent tests and documentation:

- ContextSession remains the root-run model-input owner.
- ContextBoundary remains durable compact/resume truth.
- Tool output remains model-visible truth until it ages into durable `tool_result_ref` replacement.
- Post-compact rehydration must restore the required packets through explicit sources and token-counted limits; it must not scan the workspace or silently drop active context.
- Memory Record V2 remains strict: unknown frontmatter keys, unknown relation types, unresolved refs, and malformed provenance refs are errors.
- `.index/insights` is an L1 retrieval-routing layer until replay fixtures prove a replacement has equal or better behavior.
- Semantic retrieval failures remain fail-loud. There is no fake-vector, keyword fallback, or partial-success path.
- Mobile and `/v1` consume backend truth; they must not infer status from prose, local state, or raw markdown.

## Target Shape

### App Read Models

Target:

- Keep `internal/app` as the `/v1`, mobile inbox, and workbench read-model owner.
- Keep app services thin: services should load the records for one surface, then delegate shared latest-run projection to package-owned projectors.
- Centralize shared latest-run projection in `internal/app` only when the output is consumed by at least two app surfaces and still names real domain concepts: session state, resume status, raw run events, trace summary, selected skill, latest decision, and session summary.
- Keep surface-specific DTO shaping in the service that owns the surface. `ClientService`, `SessionStateService`, `InboxService`, and `RuntimeWorkbenchService` may share projectors, but they should not share a generic DTO that forces unrelated fields into every response.

Allowed cleanup:

- Extract duplicated latest-run state/resume/trace/decision projection from `SessionStateService` and `RuntimeWorkbenchService` into a package-local projector.
- Keep run-summary and pending-action summary builders next to inbox/notification code while only extracting helpers that have multiple current callers.
- Add tests around projection failure behavior when the extracted projector changes error context.

Rejected cleanup:

- Creating `common`, `shared`, `helpers`, or `utils` packages.
- Moving `/v1` DTOs, mobile inbox DTOs, or workbench-only DTOs into a generic app model package.
- Swallowing projection errors or treating malformed stored events, decisions, or pending actions as empty projections.

### Store and App Boundary

Target:

- `internal/app/container.go` remains the production composition root that opens SQLite.
- App services define consumer-owned ports for the store capabilities they actually consume.
- Shared persistence records and sentinel errors stay in `internal/store`.
- Runtime and provider packages keep their own consumer-owned ports.

Allowed cleanup:

- Merge duplicated port definitions when two app services truly consume the same contract.
- Split oversized port files by service family if it improves readability.
- Add architecture tests for new adapter boundaries.

Rejected cleanup:

- Passing `*sqlite.Store` directly into app business services.
- Moving app service logic into `internal/store/sqlite`.
- Replacing narrow ports with a single global `Store` interface.

### Runtime Split

Target:

- `internal/runtime` should become the execution entry and lifecycle package.
- Runner assembly should move behind a clearer owner only if that owner can be named from its clients' view.
- Existing `internal/runtime/plan`, `graph`, and `tool` subpackages should be evaluated separately before moving; a hard move is acceptable only if all imports and docs move in the same phase.

Candidate split:

- `internal/runtime`: executor, run lifecycle, root-mode routing, registry, public runtime contracts.
- `internal/runner` or `internal/runassembly`: per-run assembly and runner factory internals, if the import graph stays acyclic.
- `internal/runtime/plan` or `internal/plan`: plan execution domain. Move only when model/store contracts and tests move together.
- `internal/runtime/tool` or `internal/toolcontract`: tool execution scheduler and tool contract. Move only if it does not become a generic grab bag.

Rejected cleanup:

- Creating tiny packages whose only purpose is reducing file count.
- Keeping compatibility alias packages after a move.
- Moving types into a `common`, `shared`, or `utils` package to break cycles.

### ContextPlane

Target:

- Keep ContextSession, BudgetGovernor, CompactionEngine, tool lifecycle, and rehydration responsibilities explicit.
- Reduce package depth only where it improves ownership clarity.
- Keep durable `ContextBoundary` schema and rehydration packet tests as acceptance gates.

Allowed cleanup:

- Keep `contextplane/compaction` as a subpackage when the engine/pipeline/rehydration owner remains cohesive and avoids bloating the core context package.
- Merge `contextplane/toollifecycle` into `contextplane` if the lifecycle state remains explicit and ledger failures remain runtime failures.
- Remove duplicated helper types after the owner move is complete.

Rejected cleanup:

- Replacing compaction with string-level trimming of active context.
- Deleting preserved refs or transcript refs from `ContextBoundary` just to reduce fields.
- Dropping `prepared_memory`, `tool_state`, `session_summary`, or skill catalog rehydration without equivalent replay evidence.

### MemoryModule

Target:

- Keep file-backed Record V2 as L0 truth.
- Keep semantic index rebuildable and derived.
- Keep `Search`, `Prepare`, list APIs, and semantic projection on the same record selection semantics.

Allowed cleanup:

- Merge files that are only mechanically split and have the same owner.
- Remove dead eval/test-only seams after `rg` and package tests prove no consumer remains.
- Improve replay fixtures and explain output to make ranking behavior easier to validate.

Rejected cleanup:

- Relaxing frontmatter validation to warnings.
- Allowing unknown relation types or unresolved refs.
- Removing `.index/insights` before replay fixtures prove the replacement.

### Tests

Target:

- Keep SQLite-backed tests where persisted behavior, scan order, migrations, or `/v1` projections are under test.
- Move pure projection and selection logic into functions that can be tested without SQLite.
- Shrink `sqliteImportAllowlist` only when a real SQLite integration test is no longer proving unique behavior.

Rejected cleanup:

- Using fake stores solely to make an allowlist empty.
- Mocking runtime success paths instead of executing the real subsystem under test.

## Execution Plan

### Phase 0: Current Branch Unblock

1. Restore app consumer-owned store ports.
2. Keep direct `internal/store/sqlite` production imports confined to `internal/app/container.go`.
3. Move this plan out of `docs/architecture/` and keep it as a proposed dev implementation plan.
4. Validate:
   - `go test ./tests/architecture -run TestSQLiteStoreImportsStayBehindCompositionRoot -count=1`
   - `go test ./internal/app ./internal/memorymodule`
   - `go test ./...`
   - `make format-check`
   - `make lint`
   - `git diff --check HEAD`

### Phase 1: App Read-Model Projection Hard Cut

Scope:

- `internal/app/session_state_service.go`
- `internal/app/workbench_service.go`
- `internal/app/trace_service.go`
- `internal/app/workbench_projectors.go`
- `internal/app/pending_action_service.go`
- `internal/app/notification_service.go`
- `internal/app/store_ports.go`
- focused tests in `internal/app`

Live duplication to remove first:

- Latest session run state is assembled in both `SessionStateService.LoadSession` and `RuntimeWorkbenchService.Load`.
- Interrupted-run resume status is loaded in both services through `TraceService.ResumeStatus`.
- Raw run events are loaded in both services, then converted into `runtime.BuildTraceSummary` and `runtime.SelectedSkillFromEvents`.
- Latest run decision is loaded in both services after run-event projection.
- `defaultResumeReason` is already shared and should remain the single default reason source.

Implementation:

1. Add a package-local latest-run read-model projector in `internal/app`.
2. Make the projector consume only the narrow store methods it needs: `LoadEvents` and `LoadRunDecision`.
3. Keep `TraceService` as the resume inference owner; the projector may call `TraceService.ResumeStatus`, but it must not duplicate interrupt parsing.
4. Update `SessionStateService` to populate `SessionDetail` from the projector and keep only `MemoryContextBudget` and session-summary loading in the service.
5. Update `RuntimeWorkbenchService` to populate its common latest-run fields from the projector, then keep workbench-only projections in `workbench_service.go` and `workbench_projectors.go`: plan evidence, tool-result economy, artifacts, provider usage, subagents, workspace git status, and next-step hint.
6. Keep `InboxService` run summaries in place for now. They are a separate mobile aggregate shape and should only be touched if the new projector proves a concrete duplicate, not just a similar name.
7. Delete any now-dead private helper or test stub fields in the same change.

Acceptance:

- No `/v1` DTO shape, OpenAPI schema, generated mobile client, or persisted store schema changes.
- Projection failures remain fail-loud and include the affected run id.
- Interrupted runs still require a trace service for resumability; there is no checkpoint fallback.
- `SessionStateService` and `RuntimeWorkbenchService` use the same projector for latest-run resume/trace/decision facts.
- Workbench-only projections stay in the workbench owner and do not move into a generic helper bucket.

Validation:

- `go test ./internal/app`
- `go test ./tests/architecture -run TestSQLiteStoreImportsStayBehindCompositionRoot -count=1`
- `go test ./internal/runtime ./internal/contextplane ./internal/memorymodule`
- `make format-check`
- `make lint`
- `git diff --check`

### Phase 2: Package and Import Graph Inventory

1. Generate current package dependency graph with `go list -deps` and targeted `rg`.
2. Identify packages with mixed ownership by call chain, not file count.
3. Classify each candidate move:
   - domain owner
   - adapter owner
   - consumer port owner
   - wire contract impact
   - generated client impact
   - required architecture tests
4. Produce a move map before editing.

Acceptance:

- No proposed package introduces import cycles.
- No package is named `common`, `shared`, `helpers`, or `utils`.
- Every proposed move has a delete plan for old paths and tests.

2026-05-29 live inventory result:

Current import direction:

- `internal/app` and `internal/web` are the main external runtime consumers. Production app wiring uses `runtime.RunnerFactory`, `runtime.Executor`, `runtime.RunController`, `runtime.Toolset`, trace/workbench DTOs, and `runtime/api`.
- `internal/runtime` imports `internal/orchestration`, `internal/runtime/plan`, `internal/runtime/graph`, `internal/runtime/tool`, `internal/contextplane`, `internal/memorymodule`, `internal/skills`, providers, tools, workspace, and store ports.
- `internal/orchestration` does not import `internal/runtime`; it receives runtime-specific graph/tool/context builders through injected functions and interfaces.
- `internal/runtime/plan`, `internal/runtime/graph`, and `internal/runtime/tool` already form meaningful subpackages. Moving them before simplifying the runner assembly would mostly shuffle imports.

Candidate classification:

| Candidate | Owner | Current files | Wire/OpenAPI impact | Move now? | Delete plan |
| --- | --- | --- | --- | --- | --- |
| App latest-run read model | `internal/app` | `latest_run_projector.go`, `session_state_service.go`, `workbench_service.go` | None | Done in Phase 1 | Duplicated resume/events/decision projection removed from both services |
| Runtime run assembly wrapper cleanup | `internal/runtime` | `run.go`, `runner_catalog.go` | None | Done in current branch | Deleted `runCoordinator` and object-style assembler wrappers; kept direct `RunnerFactory` methods |
| New `internal/runner` package | Not proven yet | `runner*.go`, `run.go`, `runtime_deps.go`, `subagent_executor.go` | None expected, but import churn is large | No | Child-agent construction no longer exposes `*RunnerFactory`; still blocked until the runner assembly owner is proven enough to justify package churn |
| Orchestration plane package move | `internal/orchestration` already owns it | `runner_orchestration.go`, `internal/orchestration/*` | None | No | Keep runtime-owned wiring seam; do not recreate cross-package `Plane` abstraction |
| Toolset builder move | Runtime run assembly, not generic tooling | `runner_toolset*.go`, `toolset.go` | None | No | Revisit after runner assembly wrapper cleanup shows a stable owner |

Completed hard-cut action:

1. Collapse `runCoordinator` into `RunnerFactory.New` / direct package-local build methods.
2. Replace `modelProviderAssembler`, `capabilityAssembler`, and `contextSelectionAssembler` with concrete `RunnerFactory` methods.
3. Delete lazy `ensure*Assembler` helpers in the same change.
4. Keep all behavior and root mode contracts unchanged.
5. Validate with `go test ./internal/runtime ./internal/orchestration`, focused root mode tests, `go test ./...`, `make format-check`, `make lint`, and `git diff --check`.

2026-05-29 Phase 3 pre-cut:

1. Require `RunnerFactoryOptions.ChildAgentExecutorFactory`; `NewRunnerFactory` fails if it is missing.
2. Move concrete `SubagentExecutor` construction to app/test composition through `NewSubagentExecutorFactory`.
3. Keep `RunnerFactory` responsible for invoking the injected factory, not constructing `SubagentExecutor` directly.
4. Preserve existing child-run behavior and MCP sampling behavior.
5. Remaining blocker before package split: `SubagentExecutor` still receives parent registry depth, child worktree creation/opening, and workspace-cloned runtime through the factory contract.

2026-05-29 Phase 3 dependency narrowing:

1. Replace `ChildAgentExecutorFactory func(*RunnerFactory)` with `ChildAgentExecutorFactory func(ChildAgentRuntimeDeps) (ChildAgentExecutor, error)`.
2. Pass only `RunRuntime`, parent-depth resolver, child-workspace creator, and workspace-runtime factory into the child-agent factory.
3. Make `NewSubagentExecutor` validate required config/store/runtime/dependency functions at construction.
4. Keep default construction in app/test composition through `NewSubagentExecutorFactory`; no new package or alias layer yet.
5. Remaining blocker before package split: prove that runner assembly itself has a stable external owner. Do not move files just because the child-agent dependency is now narrower.

### Phase 3: Runtime Assembly Split

1. Move runner assembly only after the owner name and dependency direction are proven.
2. Update imports in one hard cut.
3. Delete old files and alias paths in the same change.
4. Keep root modes and child-run contracts unchanged unless OpenAPI/mobile/docs are updated in the same phase.

Validation:

- `go test ./internal/runtime ./internal/orchestration`
- focused tests for root mode routing, child agent execution, tool scheduler, and resume behavior
- `go test ./...`
- architecture grep for deleted owner paths

### Phase 4: ContextPlane File Layout Simplification

1. Merge `contextplane/toollifecycle` into `contextplane`; the parent package already owns lifecycle state and context binding.
2. Keep `contextplane/compaction` as a subpackage; it remains the cohesive owner of compaction engine, compression pipeline, middleware builder, prompts, and rehydration planner.
3. Update runtime tool call sites to use `contextplane.OnToolCall`, `contextplane.OnToolResult`, `contextplane.DeferredLoad`, and `contextplane.ToolCallRejectedError` directly.
4. Preserve context boundary schema, BudgetGovernor pressure semantics, rehydration packet behavior, and fail-loud ledger writes.
5. Do not add alias packages or compatibility imports.

Validation:

- `go test ./internal/contextplane ./internal/contextplane/compaction`
- context boundary SQLite tests
- reactive compact and proactive compact tests

2026-05-29 Phase 4 result:

1. Merged `contextplane/toollifecycle` into `contextplane`.
2. Deleted the child package rather than leaving compatibility aliases.
3. Updated runtime tool call sites to consume the parent package API directly.
4. Kept `contextplane/compaction` as a subpackage because its engine/pipeline/rehydration owner is still cohesive.
5. Verified package removal with `go list ./internal/contextplane/...`.

### Phase 5: MemoryModule Simplification

1. Merge mechanically split files only when ownership is identical.
2. Keep strict Record V2 validation.
3. Add or refresh replay fixtures before changing ranking or insight behavior.
4. Treat semantic rebuild and search errors as fail-loud acceptance gates.

Validation:

- `go test ./internal/memorymodule`
- semantic rebuild/search tests
- replay fixture tests for insights/source refs/relations

2026-05-29 Phase 5 result:

1. Kept semantic, Bleve, frontmatter, mutation, history, and procedure-learning files separate because they each own real behavior.
2. Merged the mechanically split search projection helpers from `search_item.go` and search explain helpers from `explain.go` into `search.go`.
3. Did not change Record V2 validation, semantic ranking, semantic rebuild, insight source boosts, relation boosts, or replay fixture behavior.

### Phase 6: Test Boundary Cleanup

1. Tag each SQLite-backed test as integration, projection, or pure logic.
2. Keep integration tests on real SQLite temp DBs.
3. Extract pure logic where it reduces setup cost without losing persisted coverage.
4. Update `sqliteImportAllowlist` only after the replacement test proves the same behavior.

Validation:

- `go test ./tests/architecture`
- `go test ./...`

### Phase 7: Docs and Generated Contracts

1. Update `docs/architecture/` only for current truth after code lands.
2. Keep future plans in `docs/dev/`.
3. If `/v1`, RunEvent, memory DTOs, or mobile types change, update `docs/openapi.yaml` and generated Dart client in the same change.

Validation:

- `python3 mobile/tool/generate_openapi_client.py --check` when OpenAPI is touched
- architecture docs grep for deleted paths and old package names

## Commit Strategy

Each phase should land as one coherent hard-cut commit:

- code move and import updates
- old path deletion
- tests updated
- docs updated
- no compatibility aliases

Do not commit a phase if any required validation gate fails.
