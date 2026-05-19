# Acorn Architecture Evolution Plan

**Status**: Implemented  
**Date**: 2026-05-09

This records the lean execution plan completed on 2026-05-09. It is not a policy document and not a compatibility plan.

## Goal

Make Acorn better at real repo work:

- use context with less waste
- split work into child runs
- let child runs modify code without stepping on the parent
- turn successful work into reusable procedures
- show the user what happened with concrete evidence

## Current Baseline

Acorn already has the backend pieces that should be treated as the foundation:

- `ToolContract`
- `ToolExecutionScheduler`
- `ToolResultLedger`
- `procedure.activation`
- verifier child-run contract
- mutation checkpoint and explicit rollback
- `/v1` RunEvent and RunDetail projection
- file-backed memorymodule facts, skills, history, and insights

Do not rebuild those under new names. The next work should compose them into stronger agent behavior.

## Not Doing

These are intentionally out of scope for this plan:

- compatibility layers
- dual-read or dual-write migrations
- fallback behavior that hides errors
- feature flags for half-enabled runtime paths
- automatic skill extraction after every run
- L0-L4 memory directory migration
- new legacy `/api` or debug-only web surfaces
- core runtime logic tied to one provider's pricing model

## Phase 1: Context Economy

**Implementation status**: Done

**Outcome**: Acorn spends fewer tokens on tool output, memory, and repeated context while keeping full evidence recoverable.

### Build

- Measure model input size per run before and after context assembly.
- Make tool result references easy to rehydrate from `ToolResultLedger`.
- Ensure large tool outputs enter model context as summary plus ref, not full text by default.
- Keep full content recoverable through backend-owned ledger lookup.
- Add RunDetail projection for context economy facts:
  - result refs used
  - large outputs elided
  - memory entries injected
  - approximate input token size when available

### Likely Files

- `internal/contextplane/`
- `internal/toolresult/`
- `internal/runtime/plan_evidence.go`
- `internal/app/runtime_workbench_service.go`
- `internal/web/runtime_workbench_dto.go`
- `mobile/lib/src/features/chat/chat_models.dart`
- `mobile/lib/src/features/chat/chat_screen.dart`

### Done When

- A run with large file or command output does not stuff the full result into model context.
- The full result can still be opened through a result ref.
- RunDetail shows enough evidence for the user to understand what was summarized.
- Tests cover result-ref rehydration and RunDetail projection.

### Verify

```bash
go test ./internal/contextplane ./internal/toolresult ./internal/runtime ./internal/app ./internal/web
flutter test
```

## Phase 2: Forked Child Run

**Implementation status**: Done

**Outcome**: A parent run can delegate a focused task to a child run and receive a structured result with evidence.

### Build

- Extend the existing child-agent contract instead of adding a new executor path.
- Add a fork mode to child runs:
  - parent run id
  - child run id
  - directive
  - inherited context snapshot
  - allowed tools
  - evidence refs
- Store parent-child lineage in SQLite.
- Emit RunEvents for child run start, completion, failure, and evidence summary.
- Surface child run results in RunDetail.

### Likely Files

- `internal/orchestration/child_agent.go`
- `internal/orchestration/single_agent_builder.go`
- `internal/runtime/runner_factory_orchestration.go`
- `internal/store/sqlite/`
- `internal/app/runtime_workbench_service.go`
- `docs/openapi.yaml`
- `mobile/lib/src/api/run_event_stream.dart`
- `mobile/lib/src/features/chat/chat_models.dart`

### Done When

- Parent run can launch a child run for a read-only analysis task.
- Child result includes status, summary, key files, and evidence refs.
- Parent RunDetail shows the child run and its result.
- Failure in the child run is visible as failed child result, not hidden.

### Verify

```bash
go test ./internal/orchestration ./internal/runtime ./internal/store/sqlite ./internal/app ./internal/web
flutter test
```

## Phase 3: Worktree Child Execution

**Implementation status**: Done

**Outcome**: A child run can modify code in an isolated worktree and report the actual diff and verification evidence.

### Build

- Add worktree creation for child runs that need file mutation.
- Record worktree path, branch, base commit, and child run id.
- Run mutation tools inside the child worktree.
- Attach mutation checkpoint, diff summary, test/lint evidence, and tool result refs to the child result.
- Do not auto-merge.
- Do not auto-commit.
- Parent receives a clear result:
  - changed files
  - verification commands
  - pass/fail status
  - worktree path

### Likely Files

- `internal/workspace/`
- `internal/tools/native_mutation_tools.go`
- `internal/runtime/tool_execution_scheduler.go`
- `internal/orchestration/child_agent.go`
- `internal/store/sqlite/`
- `internal/app/runtime_workbench_service.go`

### Done When

- A child run can edit files without touching the parent worktree.
- The parent can inspect the child diff.
- Verification evidence is attached to the child result.
- Dirty parent worktree does not get silently overwritten.

### Verify

```bash
go test ./internal/workspace ./internal/tools ./internal/runtime ./internal/orchestration ./internal/store/sqlite
```

## Phase 4: Procedure Learning

**Implementation status**: Done

**Outcome**: The user can turn a successful run into a reusable verified procedure.

### Build

- Add a manual extraction path from a completed run.
- Extract:
  - name
  - purpose
  - trigger
  - steps
  - required tools
  - source run id
  - tool result refs
  - verification evidence
- Save accepted procedures into existing memorymodule skills.
- Reuse existing `procedure.activation` to show when a procedure is matched, injected, used, or rejected.
- Keep extraction manual until quality is proven.

### Likely Files

- `internal/memorymodule/`
- `internal/runtime/plan_evidence.go`
- `internal/orchestration/verifier.go`
- `internal/cli/`
- `internal/app/runtime_workbench_service.go`

### Done When

- User can extract a procedure from a known successful run.
- The procedure is saved as a memorymodule skill with source evidence.
- Future matching shows `procedure.activation` in RunEvent/RunDetail.
- Low-evidence extraction fails clearly.

### Verify

```bash
go test ./internal/memorymodule ./internal/runtime ./internal/orchestration ./internal/cli ./internal/app
```

## Phase 5: Provider Usage And Cache Observability

**Implementation status**: Done

**Outcome**: Acorn records provider-reported token and cache usage when the provider supplies it.

### Build

- Define provider-neutral usage metadata.
- Record real provider usage on each model call:
  - input tokens
  - output tokens
  - cached input tokens when available
  - provider name
  - model name
- Surface usage in RunDetail.
- Do not estimate fake cache hits.
- Do not hard-code provider pricing in runtime core.

### Likely Files

- `internal/providers/`
- `internal/runtime/`
- `internal/store/sqlite/`
- `internal/app/runtime_workbench_service.go`
- `internal/web/runtime_workbench_dto.go`
- `mobile/lib/src/features/chat/chat_models.dart`
- `mobile/lib/src/features/chat/chat_screen.dart`

### Done When

- Runs show real provider usage when available.
- Providers without usage metadata show no usage data, not guessed data.
- Cache metrics are observable without coupling core runtime to a single provider.

### Verify

```bash
go test ./internal/providers ./internal/runtime ./internal/store/sqlite ./internal/app ./internal/web
flutter test
```

## Execution Order

1. Context economy
2. Forked child run
3. Worktree child execution
4. Procedure learning
5. Provider usage and cache observability

This order keeps the loop practical: first reduce context waste, then delegate work, then make delegated work safe to inspect, then learn from successful runs, then expose provider usage.

## Acceptance For The Whole Plan

- A user can ask Acorn to inspect a repo and see which evidence was used.
- A parent run can delegate a real child task.
- A child run can make isolated code changes and report a diff.
- A successful run can become a reusable procedure with source evidence.
- RunDetail shows token/result/procedure/child-run evidence without requiring the user to read raw logs.
