---
doc_type: implementation-plan
status: proposed
last_reviewed: 2026-05-28
slug: architecture-refactor-2026-05-28
---

# Acorn Architecture Refactor Plan 2026-05-28

This plan is intentionally destructive: when a new owner is chosen, the old path should be removed in the same change. It is not a compatibility plan. It is also not permission to weaken persisted truth, fail-loud validation, or remote/mobile contracts.

## Research Baseline

Primary Go guidance used for this plan:

- Go modules should use `internal/` to create compile-time import boundaries and leave room for implementation refactors.
- Packages should be named from the client point of view and should group cohesive behavior, not arbitrary technical buckets or file-count targets.
- Interfaces are best defined by the consuming package when they describe the consumer's required behavior. Implementing packages should usually return concrete types.
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

- Merge `contextplane/compaction` into `contextplane` if import cycles stay clean and tests prove identical behavior.
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

### Phase 1: Package and Import Graph Inventory

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

### Phase 2: Runtime Assembly Split

1. Move runner assembly only after the owner name and dependency direction are proven.
2. Update imports in one hard cut.
3. Delete old files and alias paths in the same change.
4. Keep root modes and child-run contracts unchanged unless OpenAPI/mobile/docs are updated in the same phase.

Validation:

- `go test ./internal/runtime ./internal/orchestration`
- focused tests for root mode routing, child agent execution, tool scheduler, and resume behavior
- `go test ./...`
- architecture grep for deleted owner paths

### Phase 3: ContextPlane File Layout Simplification

1. Decide whether `compaction` and `toollifecycle` remain subpackages based on import direction and readability.
2. If merged, move tests first or in the same patch.
3. Preserve context boundary schema, BudgetGovernor pressure semantics, and rehydration packet behavior.
4. Add regression tests before deleting any context packet kind.

Validation:

- `go test ./internal/contextplane ./internal/contextplane/compaction ./internal/contextplane/toollifecycle`
- context boundary SQLite tests
- reactive compact and proactive compact tests

### Phase 4: MemoryModule Simplification

1. Merge mechanically split files only when ownership is identical.
2. Keep strict Record V2 validation.
3. Add or refresh replay fixtures before changing ranking or insight behavior.
4. Treat semantic rebuild and search errors as fail-loud acceptance gates.

Validation:

- `go test ./internal/memorymodule`
- semantic rebuild/search tests
- replay fixture tests for insights/source refs/relations

### Phase 5: Test Boundary Cleanup

1. Tag each SQLite-backed test as integration, projection, or pure logic.
2. Keep integration tests on real SQLite temp DBs.
3. Extract pure logic where it reduces setup cost without losing persisted coverage.
4. Update `sqliteImportAllowlist` only after the replacement test proves the same behavior.

Validation:

- `go test ./tests/architecture`
- `go test ./...`

### Phase 6: Docs and Generated Contracts

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
