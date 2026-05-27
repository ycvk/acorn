# internal/runtime/ Module-Split Feasibility Analysis

## Executive Summary

`internal/runtime/` is a 44-file monolith (excluding tests) with two remaining physically-separated runtime subpackages (`api/`, `graph/`) plus the canonical top-level `internal/stream` package. The old `internal/runtime/stream` duplicate and `alias_stream.go` compatibility shim have been hard-cut. The remaining flat runtime files can be split into **6 additional subpackages** with a clear priority order based on **external reference count × internal coupling density**. The lowest-risk extractions are pure type/interface definitions; the highest-risk is the core execution engine.

---

## 1. Alias Inventory

### 1.1 alias.go → runtime/api/

| Alias in runtime | Actual Source | Category |
|------------------|---------------|----------|
| `ErrRunNotActive` | `api.ErrRunNotActive` | Error |
| `ErrRunNotInterrupted` | `api.ErrRunNotInterrupted` | Error |
| `ErrExecutionNotReady` | `api.ErrExecutionNotReady` | Error |
| `ExecuteRequest` | `api.ExecuteRequest` | Request Type |
| `Plan` | `model.Plan` | Core Type |
| `PlanStep` | `model.PlanStep` | Core Type |
| `PlanStepStatus` | `model.PlanStepStatus` | Enum |
| `PlanStepRisk` | `model.PlanStepRisk` | Enum |
| `PlanRepoTarget` | `model.PlanRepoTarget` | Type |
| `VerificationIntent` | `model.VerificationIntent` | Type |
| `PlanStore` | `api.PlanStore` | Interface |
| `PlanPersistenceStore` | `api.PlanPersistenceStore` | Interface |
| `PlanEvidence` | `model.PlanEvidence` | Core Type |
| `EvidenceKind` | `model.EvidenceKind` | Enum |
| `EvidenceStatus` | `model.EvidenceStatus` | Enum |
| `WithRunID`, `GetRunID` | `api.WithRunID`, `api.GetRunID` | Context Helper |
| `WithSessionID`, `SessionIDFromContext` | `api.WithSessionID`, `api.SessionIDFromContext` | Context Helper |
| `WithStore` | `api.WithStore` | Context Helper |
| `EventAppender` | `api.EventAppender` | Interface |

**Verdict:** `alias.go` is a backward-compatibility shim. All 21 aliases point to `runtime/api/`. **Action:** Deprecate and migrate external callers to import `runtime/api` directly.

### 1.2 Stream package hard-cut

| Item | Current Source |
|------------------|---------------|
| Stream item/kind/payload types | `internal/stream` |
| Stream context helpers | `internal/stream` |
| Stream event projection helpers | `internal/stream` |
| Runtime stream compatibility layer | Deleted |

**Verdict:** `internal/runtime/stream` was a duplicate of `internal/stream`, and `alias_stream.go` re-exported that duplicate from `runtime`. Both have been removed. Runtime, app, and web callers now import `github.com/ycvk/acorn/internal/stream` directly.

---

## 2. External Import Count by Package

| External Package | Files Importing runtime | Total Refs |
|------------------|------------------------|------------|
| `internal/app/` | 20 files | ~120+ |
| `internal/web/` | 8 files | ~40+ |
| `internal/clientevents/` | 1 file | ~5 |

### 2.1 Top Externally-Referenced Types (from runtime package)

| Type | External Refs | Defined In | Notes |
|------|--------------|------------|-------|
| `Result` | 31 | `run.go` | Run result struct |
| `StreamSink` | 24 | `internal/stream` | Streaming output sink |
| `Plan` | 11 | `alias.go` → `api/` | Execution plan |
| `ExecuteRequest` | 11 | `alias.go` → `api/` | Request DTO |
| `ErrRunNotInterrupted` | 9 | `alias.go` → `api/` | Error var |
| `PlanEvidence` | 8 | `alias.go` → `api/` | Evidence record |
| `TraceSummary` | 6 | `trace.go` | Trace summary |
| `RunController` | 5 | `run.go` | Controller interface |
| `ErrExecutionNotReady` | 5 | `alias.go` → `api/` | Error var |
| `SessionState` | 4 | `run.go` | Session state enum |
| `RunnerFactory` | 4 | `runner.go` | Factory interface |
| `PlanStep` | 4 | `alias.go` → `api/` | Plan step |

### 2.2 Top Externally-Referenced Types (from runtimehistory)

| Type | External Refs | Notes |
|------|--------------|-------|
| `SessionSummary` | 15 | Already separate package |
| `SessionSummaryService` | 2 | Already separate package |

---

## 3. Internal File Dependency Graph

### 3.1 Already-Separated Subpackages

```
runtime/api/          ← 12 runtime files import it
  ↑
runtime/graph/        ← imports only runtime/api/
  ↑
internal/stream/      ← runtime/app/web stream item, payload, sink and projection types
```

### 3.2 Cross-File Imports Within runtime/ (excluding api/, stream/, graph/)

| Imported Package | Importing Files | Count |
|------------------|-----------------|-------|
| `runtime/graph` | `act_node.go`, `agent_graph.go`, `plan_evidence.go`, `plan_execute.go`, `plan.go`, `registry.go`, `runner_orchestration.go` | 7 |
| `runtimehistory` | `executor.go`, `run.go`, `runner.go`, `runtime_deps.go`, `store_ports.go` | 5 |

### 3.3 Declaration Density per File (types + funcs + vars)

| File | Total | Types | Funcs | Vars | Coupling |
|------|-------|-------|-------|------|----------|
| `tool.go` | 49 | 8 | 41 | 0 | High (core tool system) |
| `executor.go` | 42 | 3 | 39 | 0 | High (imports runtimehistory) |
| `trace.go` | 38 | 6 | 32 | 0 | Medium (imports events, orchestration) |
| `plan_evidence.go` | 32 | 4 | 27 | 1 | Medium (imports graph, orchestration) |
| `run.go` | 30 | 9 | 21 | 0 | High (imports runtimehistory) |
| `helpers.go` | 28 | 3 | 24 | 1 | Low (pure utilities) |
| `plan_execute.go` | 28 | 2 | 26 | 0 | High (core execution) |
| `runner_toolset_build.go` | 26 | 1 | 25 | 0 | Medium |
| `act_node.go` | 21 | 2 | 19 | 0 | Medium (imports graph) |
| `alias.go` | 21 | 14 | 5 | 2 | **Zero** (pure aliases) |
| `runner.go` | 19 | 1 | 18 | 0 | High (imports runtimehistory) |
| `runner_build.go` | 16 | 2 | 14 | 0 | Medium |
| `plan_store.go` | 16 | 1 | 15 | 0 | Medium |
| `runner_orchestration.go` | 15 | 2 | 13 | 0 | Medium (imports graph) |
| `fact_extractor.go` | 14 | 0 | 14 | 0 | Low |
| `plan.go` | 13 | 2 | 8 | 3 | Medium (imports graph, stream) |
| `store_ports.go` | 9 | 9 | 0 | 0 | **Low** (pure interfaces) |
| `plan_steps.go` | 10 | 0 | 10 | 0 | Medium |
| `runner_catalog.go` | 10 | 4 | 6 | 0 | Low |
| `streaming_tool_executor.go` | 9 | 3 | 6 | 0 | Medium |
| `runner_toolset.go` | 9 | 2 | 7 | 0 | Medium |
| `assistant_stream.go` | 9 | 2 | 7 | 0 | Medium |
| `agent_graph.go` | 9 | 0 | 9 | 0 | Medium (imports graph) |
| `checkpoint_json.go` | 5 | 2 | 2 | 1 | Low |
| `runtime_deps.go` | 4 | 3 | 1 | 0 | High (DI container) |
| `assistant_streamer.go` | 4 | 1 | 3 | 0 | Medium |
| `skill_types.go` | 3 | 2 | 1 | 0 | **Low** (alias + 1 type) |
| `elicitation.go` | 2 | 2 | 0 | 0 | **Zero** (pure types) |
| `contextplane_bridge.go` | 2 | 0 | 2 | 0 | Medium |
| `registry.go` | 2 | 0 | 1 | 1 | Low (imports graph, orchestration) |
| `streaming_assistant_stream.go` | 1 | 0 | 1 | 0 | Medium |
| `context_session_bridge.go` | 1 | 0 | 1 | 0 | Medium |

---

## 4. Prioritized Split Plan with Interface Boundaries

### Ranking Formula
**Priority = External Refs × (1 / Internal Coupling)**

Higher score = extract first.

---

### Phase 1: Clean Existing Subpackage Boundaries (Risk: ZERO)
**Files:** `alias.go`, `skill_types.go`

**Action:**
1. Migrate external imports of `runtime.Plan` and runtime/api aliases to `runtime/api` directly.
2. Keep `internal/stream` as the canonical stream package; do not restore `runtime/stream` or `alias_stream.go`.
3. Delete `alias.go` after runtime/api callers are migrated.
4. Move `SelectedSkill` alias to `runtime/api/` or have callers import `contextplane` directly.

**Interface Boundary:** None needed — pure import path migration.

**Estimated Impact:** ~50 import lines changed across `internal/app/` and `internal/web/`.

---

### Phase 2: Extract Store Ports (Risk: LOW)
**Source File:** `store_ports.go` (9 type declarations, zero funcs, pure interfaces)
**New Package:** `runtime/ports/`

**Rationale:**
- `store_ports.go` defines the **storage contract boundary** for the entire runtime.
- It imports `contextplane`, `decision`, `events`, `mcpprovider`, `providerusage`, `runtimehistory`, `store` — but only to reference types in interface signatures.
- External packages like `internal/app/container.go` wire concrete stores to these interfaces.

**Interface Boundary:**
```go
package ports

type RunStore interface { ... }
type EventStore interface { ... }
type ArchiveStore interface { ... }
type EvidenceStore interface { ... }
type ProviderUsageStore interface { ... }
type SessionTurnStore interface { ... }

// Composite interfaces
type ExecutorStore interface {
    RunStore
    EventStore
    ArchiveStore
    EvidenceStore
    // ...
}

type RunnerFactoryStore interface {
    ExecutorStore
    ProviderUsageStore
    SessionTurnStore
    // ...
}
```

**Migration:**
- `internal/app/` files referencing `runtime.RunStore` → `runtimeports.RunStore`
- `internal/store/` can implement `runtimeports.RunStore` instead of `runtime.RunStore`
- Breaks the circular-ish dependency where `runtime` knows about `internal/store` concrete types.

**Dependencies to Cut:**
- `runtime` → `internal/store` (via `store.RunCreateParams`, `store.EvidenceRef`, `store.ToolResultLedger`)
- `runtime` → `internal/model` (via `ArchiveStore` using `RunArchive`)

**Note:** `store_ports.go` references `runtime/api.PlanPersistenceStore`, so `runtime/ports/` would import `runtime/api`. This is fine — api/ is already a leaf package.

---

### Phase 3: Extract Base Types (Risk: LOW)
**Source Files:** `elicitation.go`, `skill_types.go`, `checkpoint_json.go` (partial)
**New Package:** `runtime/base/` or merge into `runtime/api/`

**Contents:**
- `ElicitationInterruptInfo`, `ElicitationInterruptState`
- `SkillMatch`
- `CheckpointJSON` types (if any pure types exist there)

**Rationale:** Zero external dependencies, tiny surface area. Can be merged into `runtime/api/` since they are core DTOs.

---

### Phase 4: Extract Utility Helpers (Risk: LOW-MEDIUM)
**Source File:** `helpers.go` (28 declarations: 3 types, 24 funcs, 1 var)
**New Package:** `runtime/util/` or `pkg/runtimeutil/`

**Contents:**
- `compactInterruptInfo()`, `compactText()`, `newRunID()`, `newSessionID()`
- `ExtractString()`, `ExtractBool()`, `ExtractMap()`
- JSON schema helpers, string utilities

**Rationale:**
- Pure functions with no runtime state.
- Used widely inside runtime and could be useful elsewhere.
- Only imports `decision` and `skills` for type references in function signatures.

**Interface Boundary:** No interfaces needed — pure function package.

**Risk:** Medium because `helpers.go` is likely imported by many runtime files; extracting it means those files gain an extra import. But this is mechanical.

---

### Phase 5: Extract Trace/Projection Subsystem (Risk: MEDIUM)
**Source File:** `trace.go` (38 declarations: 6 types, 32 funcs)
**New Package:** `runtime/trace/` or `runtime/projection/`

**Contents:**
- `Trace`, `TraceSummary` structs
- `BuildTrace()`, `BuildTraceSummary()`, `LatestRootInterruptContexts()`
- `summarizeStreamItems()` and related private funcs

**Rationale:**
- `TraceSummary` is externally referenced 6 times (by `internal/app/trace_service.go`).
- `trace.go` is a **read-only projection layer**: it consumes `events.EventRecord` and produces `StreamItem` / `TraceSummary`.
- It does not mutate runtime state.

**Interface Boundary:**
```go
package trace

import "github.com/ycvk/acorn/internal/events"
import "github.com/ycvk/acorn/internal/stream"

type Trace struct { ... }
type TraceSummary struct { ... }

func BuildTrace(run *events.RunRecord, raw []events.EventRecord) *Trace
func BuildTraceSummary(raw []events.EventRecord) *TraceSummary
func LatestRootInterruptContexts(raw []events.EventRecord) ([]stream.StreamInterruptContext, error)
```

**Dependencies:**
- Imports: `events`, `config`, `orchestration`, `skills`, `store`, `workspace`, `stream`
- All are read-only type references except `stream.StreamItem` (which is already a separate package).

**Migration:**
- `internal/app/trace_service.go` imports `runtime/trace` instead of `runtime`.
- `internal/web/dto_run_detail.go` references `runtime.TraceSummary` → `trace.TraceSummary`.

---

### Phase 6: Extract Plan Subsystem (Risk: MEDIUM-HIGH)
**Source Files:** `plan.go`, `plan_evidence.go`, `plan_store.go`, `plan_steps.go`, `plan_execute.go`
**New Package:** `runtime/plan/`

**Contents:**
- `PlanNode`, `PlanningPromptProvider`
- Evidence validation (`validatePlanEvidence`, `validEvidenceKind`, etc.)
- Plan store implementation
- Step execution logic
- Plan execution orchestration

**Rationale:**
- 5 files collectively implement the plan lifecycle: creation → validation → execution → evidence collection.
- `PlanEvidence` is externally referenced 8 times.
- `plan.go` and `plan_evidence.go` both import `runtime/graph` (for `graph.AgentGraphState`).

**Interface Boundary Challenge:**
- `plan_evidence.go` references `graph.AgentGraphState` indirectly through plan steps.
- **Solution:** Define a narrow interface in `runtime/plan/`:
  ```go
  type GraphStateProvider interface {
      CurrentState() graph.AgentGraphState  // or return a plan-specific state view
  }
  ```
  Or better: move `AgentGraphState`'s plan-relevant fields to a struct in `runtime/api/` and have `graph/` embed it.

**Dependencies to Manage:**
- `orchestration` (for `orchestration.RunAssembly`)
- `graph` (for `AgentGraphState`, `PhaseAct`, etc.)
- `store` (for `ToolResultLedger`, `EvidenceRef`)
- `tooling` (for tool execution types)

---

### Phase 7: Extract Core Execution Engine (Risk: HIGH)
**Source Files:** `executor.go`, `runner.go`, `run.go`, `runner_*.go`, `act_node.go`, `agent_graph.go`, `safe_parallel_tools_node.go`, `streaming_*.go`, `assistant_stream*.go`, `context_*_bridge.go`, `tool.go`, `fact_extractor.go`
**New Package:** `runtime/engine/` (or keep as `runtime/` core)

**Rationale:**
- These 20+ files form the actual execution engine: runner factory, executor, node logic, streaming, tool invocation.
- They have the highest internal coupling (import each other, `runtimehistory`, `orchestration`, `graph`, `contextplane`).
- `executor.go` (42 funcs) and `run.go` (30 declarations) are the heart of the system.

**Why This is High Risk:**
- `RuntimeDeps` (from `runtime_deps.go`) is the dependency injection container for the engine. It references almost every subsystem.
- `runner.go` imports `runtimehistory` for session archives.
- `executor.go` imports `runtimehistory` for checkpoint storage.
- `tool.go` (49 declarations) is tightly coupled to `eino` tool framework.

**Recommended Approach:**
- **Do NOT extract the entire engine at once.** Instead:
  1. First extract `runtime/ports/` (Phase 2) so the engine depends on interfaces, not concrete stores.
  2. Then extract `runtime/plan/` (Phase 6) to remove plan logic from engine.
  3. Then extract `runtime/trace/` (Phase 5) to remove read-only projection.
  4. What's left in `runtime/` is the engine core — which may be acceptable as a single package if its responsibilities are clearly "execute runs".

---

## 5. Lowest-Dependency Submodules (Extract First)

| Rank | Submodule | Files | External Refs | Internal Coupling | Risk |
|------|-----------|-------|---------------|-------------------|------|
| 1 | `runtime/api/` | `api/api.go`, `api/plan.go` | Very High (Plan, ExecuteRequest, errors) | **Zero** (leaf package) | Already done |
| 2 | `internal/stream/` | `stream/*.go` (14 files) | High (StreamSink, StreamItem) | **Zero** (leaf package) | Already done |
| 3 | `runtime/graph/` | `graph/*.go` (5 files) | Medium (AgentGraphState ×75 internally) | Low (only imports api/) | Already done |
| 4 | **`runtime/ports/`** | `store_ports.go` | Medium (store interfaces) | **Zero** (pure interfaces) | **LOW** |
| 5 | **`runtime/base/`** | `elicitation.go`, `skill_types.go` | Low | **Zero** (pure types) | **LOW** |
| 6 | **`runtime/util/`** | `helpers.go` | Low | Low (pure funcs) | **LOW-MEDIUM** |
| 7 | **`runtime/trace/`** | `trace.go` | Medium (TraceSummary ×6) | Medium (reads events/stream) | **MEDIUM** |
| 8 | **`runtime/plan/`** | `plan*.go` (5 files) | High (PlanEvidence ×8, Plan ×11) | High (imports graph, orchestration) | **MEDIUM-HIGH** |
| 9 | `runtime/engine/` | `executor.go`, `runner.go`, `run.go`, etc. | Very High (Result ×31) | Very High (core orchestration) | **HIGH** |

---

## 6. Recommended Implementation Order

### Immediate (Zero Risk)
1. **Delete `alias.go`** after migrating all external references to `runtime/api`; `alias_stream.go` is already deleted and must not be restored.
2. **Keep stream types in `internal/stream`** and continue importing that package directly from runtime/app/web.
3. **Move `skill_types.go` contents** to `runtime/api/` or `contextplane`.

### Short Term (Low Risk)
3. **Create `runtime/ports/`** from `store_ports.go`.
   - Move `SessionTurnStore`, `RunStore`, `EventStore`, `ArchiveStore`, `EvidenceStore`, `ProviderUsageStore`, `ExecutorStore`, `RunnerFactoryStore`.
   - Update `internal/app/container.go` and store implementations to use `runtime/ports`.
4. **Create `runtime/base/`** from `elicitation.go` + `skill_types.go`.
5. **Create `runtime/util/`** from `helpers.go`.

### Medium Term (Medium Risk)
6. **Create `runtime/trace/`** from `trace.go`.
   - Requires `stream/` and `events/` as dependencies (both already separate).
7. **Create `runtime/plan/`** from `plan*.go` files.
   - Requires abstracting `graph.AgentGraphState` dependency.

### Long Term (High Risk — Defer)
8. **Evaluate whether to split `runtime/engine/`** after Phases 1-7.
   - If `runtime/` (post-extraction) is <15 files, it may be acceptable as the engine core.
   - If still too large, extract `runtime/runner/` (runner factory + build logic) and `runtime/nodes/` (act_node, agent_graph, safe_parallel_tools_node).

---

## 7. Interface Boundary Specifications

### 7.1 `runtime/ports/` — Storage Contract

```go
package ports

import (
    "context"
    "github.com/ycvk/acorn/internal/events"
    "github.com/ycvk/acorn/internal/providers"
    "github.com/ycvk/acorn/internal/runtime/api"
    "github.com/ycvk/acorn/internal/model"
    "github.com/ycvk/acorn/internal/store"
)

type SessionTurnStore interface {
    CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
}

type RunStore interface {
    CreateBoundRunWithParams(ctx context.Context, params store.RunCreateParams) error
    LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
    FinishRunContext(ctx context.Context, runID string, status events.RunStatus, output, errText string) error
    MarkInterruptedContext(ctx context.Context, runID, output string) error
    UpdateRunOutputContext(ctx context.Context, runID, output string) error
    FindLatestInterruptedRun(ctx context.Context) (*events.RunRecord, error)
}

type EventStore interface {
    LoadEvents(ctx context.Context, runID string) ([]events.EventRecord, error)
    LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]events.EventRecord, error)
    SyncAssistantMessageForRun(ctx context.Context, runID string) error
    SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status events.RunStatus) error
    CreateSegmentFromRun(ctx context.Context, runID string, runStatus events.RunStatus) (int64, error)
}

type ArchiveStore interface {
    GetRunArchive(ctx context.Context, runID string) (*runtimehistory.RunArchive, error)
    UpsertRunArchive(ctx context.Context, archive runtimehistory.RunArchive) error
}

type EvidenceStore interface {
    AppendEvidenceRef(ctx context.Context, resultRef string, ref store.EvidenceRef) (store.ToolResultRecord, error)
}

type ProviderUsageStore interface {
    ListProviderUsagesByRun(ctx context.Context, runID string) ([]providerusage.Record, error)
}

type runDecisionStore interface { // unexported, kept internal
    // ...
}

type ExecutorStore interface {
    SessionTurnStore
    RunStore
    EventStore
    ArchiveStore
    EvidenceStore
    providerusage.Recorder
    store.ToolResultLedger
    runDecisionStore
    api.EventAppender
    // adk.CheckPointStore  // consider if this belongs here
    // contextplane.RunContextSnapshotStore  // consider if this belongs here
}

type RunnerFactoryStore interface {
    ExecutorStore
    ProviderUsageStore
}
```

### 7.2 `runtime/trace/` — Read-Only Projection

```go
package trace

import (
    "github.com/ycvk/acorn/internal/events"
    "github.com/ycvk/acorn/internal/stream"
)

type Trace struct {
    Run     *events.RunRecord   `json:"run,omitempty"`
    Summary *TraceSummary       `json:"summary,omitempty"`
    Items   []stream.StreamItem `json:"items,omitempty"`
}

type TraceSummary struct {
    ItemCount                  int                    `json:"item_count"`
    LastKind                   stream.StreamItemKind  `json:"last_kind,omitempty"`
    AssistantMessageCount      int                    `json:"assistant_message_count,omitempty"`
    // ... (all existing fields)
}

func BuildTrace(run *events.RunRecord, raw []events.EventRecord) *Trace
func BuildTraceSummary(raw []events.EventRecord) *TraceSummary
func LatestRootInterruptContexts(raw []events.EventRecord) ([]stream.StreamInterruptContext, error)
```

### 7.3 `runtime/plan/` — Plan Lifecycle

```go
package plan

import (
    "context"
    "github.com/ycvk/acorn/internal/runtime/api"
    "github.com/ycvk/acorn/internal/runtime/graph"
    "github.com/ycvk/acorn/internal/store"
)

type Node struct {
    // ... (formerly PlanNode)
}

type PromptProvider interface {
    BuildPlanningPromptSection(enabledToolNames []string) (string, error)
}

// Evidence validation
func ValidateEvidence(stepID string, evidence model.PlanEvidence) error
func ValidEvidenceKind(kind model.EvidenceKind) bool
func ValidEvidenceStatus(status model.EvidenceStatus) bool

// Plan execution
func ExecuteStep(ctx context.Context, step model.PlanStep, state graph.AgentGraphState) error
// ... etc.
```

---

## 8. Files to Remain in `runtime/` (Engine Core)

After all extractions, `runtime/` should contain:

| File | Role |
|------|------|
| `executor.go` | Core execution orchestrator |
| `runner.go` | Runner lifecycle |
| `run.go` | Run state machine |
| `runner_build.go` | Runner construction |
| `runner_catalog.go` | Runner catalog |
| `runner_mcp.go` | MCP runner |
| `runner_orchestration.go` | Orchestration runner |
| `runner_toolset.go` | Toolset runner |
| `runner_toolset_build.go` | Toolset construction |
| `act_node.go` | Action execution node |
| `agent_graph.go` | Agent graph coordination |
| `safe_parallel_tools_node.go` | Parallel tool execution |
| `streaming_assistant_stream.go` | Assistant streaming |
| `streaming_tool_executor.go` | Tool streaming |
| `assistant_stream.go` | Assistant stream handling |
| `assistant_streamer.go` | Streamer logic |
| `context_session_bridge.go` | Session bridge |
| `contextplane_bridge.go` | ContextPlane bridge |
| `tool.go` | Tool system |
| `fact_extractor.go` | Fact extraction |
| `runtime_deps.go` | DI container |
| `registry.go` | Type registration |

**Count:** ~22 files (down from 44). This is a reasonable size for an execution engine package.

---

## 9. Validation Checklist

Before each phase:
- [ ] `go build ./...` passes
- [ ] `go test ./internal/runtime/...` passes
- [ ] No import cycles introduced
- [ ] External packages (`internal/app/`, `internal/web/`) updated
- [ ] OpenAPI/mobile types unaffected (they use DTOs, not runtime internals)

---

## 10. Summary

| Metric | Before | After (Post-Phase 7) |
|--------|--------|----------------------|
| Files in `runtime/` | 44 | ~22 |
| Subpackages | 3 (`api/`, `stream/`, `graph/`) | 8 (+ `ports/`, `base/`, `util/`, `trace/`, `plan/`) |
| External imports of `runtime` | 30 files | ~15 files (reduced by half) |
| Pure interface packages | 0 | 1 (`ports/`) |
| Read-only projection packages | 0 | 1 (`trace/`) |

**Bottom line:** The runtime is already partially modularized. The highest-value, lowest-risk next step is **Phase 2: `runtime/ports/`** — extracting `store_ports.go` into a dedicated storage-contract package. This immediately clarifies the boundary between execution and persistence, and enables future store implementations without touching the engine.
