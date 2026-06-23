# Plan: Split `internal/runtime` God-Package

Date: 2026-06-23
Status: Approved
Parent Spec: `docs/aegis/specs/2026-06-23-radical-refactor-design.md` (Phase 1 scope)

## Goal

Split `internal/runtime` (~4,800 LOC, 22 non-test files) into 3 sub-packages + 1 new kernel package, breaking the god-package while preserving all behavior and wire contracts.

**Problem**: `runtime` mixes 7 responsibilities — execution orchestration, runner building, tool dispatch, event projection, fact extraction, memory tools, and skill selection. Files are individually well-named but the package is an unstructured grab-bag.

**Non-goal**: No logic changes. No interface redesign. No wire contract changes. Pure file movement + import path adjustment.

## Architecture

### Current (flat)

```
internal/runtime/          22 files, ~4,800 LOC — god package
  executor.go              Run lifecycle + ExecuteMessages + consume + finalize
  runner.go                RunnerFactory + ActiveRunner + buildRun
  runner_toolset.go        toolset assembly
  runner_selection.go      skill selection
  runner_mcp.go            MCP bootstrap
  runner_emit.go           run event emission
  direct_response.go       direct_response agent
  agent_loop.go            ExecuteRound
  safe_parallel_tools_node.go  tool dispatch + scheduler + streaming executor
  side_effects.go          side effect extraction
  audit.go                 tool audit wrapper
  validator.go             tool argument validation
  catalog.go               catalog spec helpers + load_tools
  fact_extractor.go        fact extraction from tool results
  memory_tools.go          memory file tools
  memory_tools_search.go   memory search + remember tools
  eventstream_projection.go    StreamItem → event projection
  eventstream_accessors.go     StreamItem typed accessors
  eventstream_payloads.go      Stream* value types
  eventstream_agent.go         AgentEvent → StreamItem conversion
  assistant_stream.go          assistant streaming + delta persistence
  streaming_assistant_stream.go  interleaved streaming + directAssistantStreamer
  interrupt.go             interrupt signal helpers
  types.go                 all types + ExecutorStore + RunnerFactoryStore + RuntimeDeps
  run.go                   RunnerBuildRequest + ActiveRunner + buildRun entry
```

### Target (split)

```
internal/stream/                  NEW — stream types + projection (kernel-level)
  types.go                        Stream* value types (from eventstream_payloads.go)
  projection.go                   StreamItem → event projection (from eventstream_projection.go)
  accessors.go                    StreamItem typed accessors (from eventstream_accessors.go)
  agent.go                        AgentEvent → StreamItem (from eventstream_agent.go)
  assistant_stream.go             assistant streaming + delta (from assistant_stream.go)
  streaming_assistant.go          interleaved streaming (from streaming_assistant_stream.go)

internal/runtime/tooldispatch/    NEW — tool dispatch + side effects
  node.go                         SafeParallelToolsNode (from safe_parallel_tools_node.go)
  streaming_executor.go           StreamingToolExecutor (split from safe_parallel_tools_node.go)
  scheduler.go                    toolExecutionScheduler (split from safe_parallel_tools_node.go)
  side_effects.go                 SideEffectRef + extractors (from runtime/side_effects.go)
  types.go                        StreamingExecutor + ToolInvoker interfaces (from runtime/types.go)

internal/runtime/factextract/     NEW — fact extraction + memory tools
  extractor.go                    ExtractSemanticFact (from fact_extractor.go)
  memory_tools.go                 BuildMemoryFileTools (from memory_tools.go)
  memory_tools_search.go          memory search + remember tools (from memory_tools_search.go)

internal/runtime/                 ROOT — orchestration only
  executor.go                     Run lifecycle
  runner.go                       RunnerFactory + buildRun
  runner_toolset.go               toolset assembly
  runner_selection.go             skill selection
  runner_mcp.go                   MCP bootstrap
  runner_emit.go                  run event emission (uses stream.AppendStreamItem)
  direct_response.go              direct_response agent
  agent_loop.go                   ExecuteRound (uses tooldispatch.ToolInvoker)
  audit.go                        tool audit wrapper (uses tooldispatch.SideEffectRef)
  validator.go                    tool argument validation
  catalog.go                      catalog spec helpers
  interrupt.go                    interrupt signal helpers
  types.go                        RuntimeDeps + ExecutorStore + RunnerFactoryStore + remaining types
  run.go                          RunnerBuildRequest + ActiveRunner
```

### Dependency Direction (no cycles)

```
domain ← stream ← runtime/tooldispatch ← runtime
                    runtime/factextract  ← runtime
```

- `stream` depends on `domain` only (StreamItem, StreamSink, EventAppender are in domain)
- `runtime/tooldispatch` depends on `stream` (for AppendStreamItem in lifecycle emit) + `domain` + `toolkit`
- `runtime/factextract` depends on `memory` + `toolset` + `domain` + `toolkit`
- `runtime` (root) depends on `stream` + `tooldispatch` + `factextract` + everything else

### Type Migration Map

| Type | Current Location | Target Location | Reason |
|---|---|---|---|
| `StreamPlannedToolCall` | `eventstream_payloads.go` | `stream/types.go` | Pure value type, used by projection + assistant_stream |
| `StreamMessage` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamToolCall` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamInterruptContext` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamInterrupt` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamSkillCandidate` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamSkill` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamSkillRequirements` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamMemoryPreparedNudge` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamMemoryPreparedEntry` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamMemoryPrepared` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `StreamAssistantDelta` | `eventstream_payloads.go` | `stream/types.go` | Pure value type |
| `AppendStreamItem` | `eventstream_projection.go` | `stream/projection.go` | Projection function |
| `ProjectStreamItemToEvent` | `eventstream_projection.go` | `stream/projection.go` | Projection function |
| `streamKindToEventKind` | `eventstream_projection.go` | `stream/projection.go` | Projection helper |
| `streamPayloadMap` | `eventstream_projection.go` | `stream/projection.go` | Projection helper |
| `normalizeToolCallPayload` | `eventstream_projection.go` | `stream/projection.go` | Projection helper |
| `reencodeViaJSON` | `eventstream_projection.go` | `stream/projection.go` | Projection helper |
| `getPayloadMap` + `getNestedMap` + `getString` + `getInt` + `getInt64` + `getBool` + `getStringSlice` | `eventstream_accessors.go` | `stream/accessors.go` | Accessors |
| `streamItemGet*` | `eventstream_accessors.go` | `stream/accessors.go` | Accessors |
| `compactInterruptInfo` | `eventstream_accessors.go` | `stream/accessors.go` | Accessor helper |
| `StreamItemsFromAgentEvent` | `eventstream_agent.go` | `stream/agent.go` | Agent → stream conversion |
| `StreamMessageFromSchema` | `eventstream_agent.go` | `stream/agent.go` | Agent → stream conversion |
| `streamInterruptFromInfo` | `eventstream_agent.go` | `stream/agent.go` | Agent → stream conversion |
| `activeProviderName` | `eventstream_agent.go` | `stream/agent.go` | Agent helper |
| `assistantStreamAccumulator` | `assistant_stream.go` | `stream/assistant_stream.go` | Streaming accumulator |
| `streamAssistantMessage` | `assistant_stream.go` | `stream/assistant_stream.go` | Streaming function |
| `normalizeAssistantStopReason` | `assistant_stream.go` | `stream/assistant_stream.go` | Streaming helper |
| `assistantRawFinishReason` | `assistant_stream.go` | `stream/assistant_stream.go` | Streaming helper |
| `streamPlannedToolCalls` | `assistant_stream.go` | `stream/assistant_stream.go` | Streaming helper |
| `streamMessageMeta` | `assistant_stream.go` | `stream/assistant_stream.go` | Streaming helper |
| `streamAssistantInterleaved` | `streaming_assistant_stream.go` | `stream/streaming_assistant.go` | Interleaved streaming |
| `directAssistantStreamer` | `streaming_assistant_stream.go` | `stream/streaming_assistant.go` | Streamer impl |
| `NewDirectAssistantStreamer` | `streaming_assistant_stream.go` | `stream/streaming_assistant.go` | Constructor |
| `SafeParallelToolsNode` | `safe_parallel_tools_node.go` | `tooldispatch/node.go` | Tool dispatch node |
| `StreamingToolExecutor` | `safe_parallel_tools_node.go` | `tooldispatch/streaming_executor.go` | Streaming executor |
| `toolExecutionScheduler` | `safe_parallel_tools_node.go` | `tooldispatch/scheduler.go` | Scheduler |
| `classifiedCall` | `safe_parallel_tools_node.go` | `tooldispatch/scheduler.go` | Scheduler type |
| `pathsOverlap` + `executionPathsFromArgs` + `normalizeExecutionPaths` + `executionTrimmedPaths` | `safe_parallel_tools_node.go` | `tooldispatch/scheduler.go` | Scheduler helpers |
| `IsInterruptError` | `safe_parallel_tools_node.go` | `tooldispatch/node.go` | Dispatch helper |
| `invokeToolWithEinoCallbacks` + `toolCallbackType` | `safe_parallel_tools_node.go` | `tooldispatch/node.go` | Dispatch helpers |
| `emitToolCallLifecycle` + `emitToolResultLifecycle` | `safe_parallel_tools_node.go` | `tooldispatch/node.go` | Dispatch lifecycle emit |
| `attachToolMessageLedgerMeta` + `attachToolSideEffects` + `markToolMessageFailed` | `safe_parallel_tools_node.go` | `tooldispatch/node.go` | Dispatch helpers |
| `SideEffectRef` | `side_effects.go` | `tooldispatch/side_effects.go` | Side effect type |
| `SideEffectKind*` constants | `side_effects.go` | `tooldispatch/side_effects.go` | Side effect constants |
| `buildToolResultRef` | `side_effects.go` | `tooldispatch/side_effects.go` | Side effect helper |
| `toolSideEffectsFromResult` + all `*SideEffects` functions | `side_effects.go` | `tooldispatch/side_effects.go` | Side effect extractors |
| `normalizedSideEffectPaths` | `side_effects.go` | `tooldispatch/side_effects.go` | Side effect helper |
| `StreamingExecutor` interface | `types.go` | `tooldispatch/types.go` | Tool dispatch contract |
| `ToolInvoker` interface | `types.go` | `tooldispatch/types.go` | Tool dispatch contract |
| `AssistantStreamer` interface | `types.go` | `stream/types.go` | Stream contract |
| `AssistantStreamRequest` | `types.go` | `stream/types.go` | Stream type |
| `AssistantStreamResult` | `types.go` | `stream/types.go` | Stream type |
| `AssistantStopReason` + constants | `types.go` | `stream/types.go` | Stream type |
| `InterleavedStream` | `types.go` | `stream/types.go` | Stream type |
| `ExtractSemanticFact` + all `extract*Fact` helpers | `fact_extractor.go` | `factextract/extractor.go` | Fact extraction |
| `BuildMemoryFileTools` + `memoryNamespacedTool` + helpers | `memory_tools.go` | `factextract/memory_tools.go` | Memory tools |
| `memorySearchTool` + `memoryRememberTool` + helpers | `memory_tools_search.go` | `factextract/memory_tools_search.go` | Memory tools |
| `DirectResponseRequest` | `types.go` | stays in `runtime/types.go` | Orchestration type |
| `RunAssembly` | `types.go` | stays in `runtime/types.go` | Orchestration type |
| `ToolLifecycleStateView` | `types.go` | stays in `runtime/types.go` | Used by direct_response + runner |
| `AssembleResultView` | `types.go` | stays in `runtime/types.go` | Used by direct_response + runner |
| `ExecutorStore` | `types.go` | stays in `runtime/types.go` | Runtime contract |
| `RunnerFactoryStore` | `types.go` | stays in `runtime/types.go` | Runtime contract |
| `RuntimeDeps` | `types.go` | stays in `runtime/types.go` | Runtime config |
| `RunContext` + `Registry` + `RunController` | `types.go` | stays in `runtime/types.go` | Runtime infra |
| `ToolNodeFactory` field | `types.go` RuntimeDeps | stays, but signature uses `tooldispatch.ToolInvoker` | Orchestration config |

### Caller Update Map (non-test files)

| File | Current Reference | New Reference |
|---|---|---|
| `executor.go:271` | `StreamItemsFromAgentEvent(...)` | `stream.StreamItemsFromAgentEvent(...)` |
| `executor.go:280,305,369` | `AppendStreamItem(...)` | `stream.AppendStreamItem(...)` |
| `executor.go:372` | `&StreamSkill{...}` | `&stream.StreamSkill{...}` |
| `executor.go:378` | `StreamSkillRequirementsFromDomain(...)` | `stream.StreamSkillRequirementsFromDomain(...)` |
| `direct_response.go:110-111` | `NewSafeParallelToolsNode(...)` | `tooldispatch.NewSafeParallelToolsNode(...)` |
| `direct_response.go:151` | `toolNode ToolInvoker` | `toolNode tooldispatch.ToolInvoker` |
| `agent_loop.go:42` | `toolNode ToolInvoker` | `toolNode tooldispatch.ToolInvoker` |
| `agent_loop.go:52,115` | `toolNode.NewStreamingExecutor(ctx)` | returns `tooldispatch.StreamingExecutor` |
| `agent_loop.go:133` | `executor StreamingExecutor` | `executor tooldispatch.StreamingExecutor` |
| `audit.go:181` | `toolSideEffectsFromResult(...)` | `tooldispatch.ToolSideEffectsFromResult(...)` (exported) |
| `audit.go:191` | `SideEffectRef` | `tooldispatch.SideEffectRef` |
| `runner.go:273` | `NewDirectAssistantStreamer(...)` | `stream.NewDirectAssistantStreamer(...)` |
| `runner_emit.go:27,75,87` | `AppendStreamItem(...)` | `stream.AppendStreamItem(...)` |
| `runner_emit.go:17,36,47` | `StreamMemoryPrepared*` | `stream.StreamMemoryPrepared*` |
| `runner_emit.go:70,133,148,152` | `StreamSkill*`/`StreamSkillCandidate` | `stream.StreamSkill*`/`stream.StreamSkillCandidate` |
| `runner_emit.go:199` | `StreamSkillRequirementsFromDomain` | `stream.StreamSkillRequirementsFromDomain` |
| `runner_toolset.go:247` | `BuildMemoryFileTools(...)` | `factextract.BuildMemoryFileTools(...)` |
| `types.go:152-153` | `ToolNodeFactory` signature | `tooldispatch.ToolInvoker` in signature |
| `types.go:409-419` | `StreamingExecutor`/`ToolInvoker` defs | REMOVE (moved to tooldispatch/types.go) |
| `types.go:369-402` | `AssistantStreamer`/`AssistantStream*`/`InterleavedStream` defs | REMOVE (moved to stream/types.go) |

### Test File Migration Map

| Test File | Current Package | Target Package |
|---|---|---|
| `eventstream_projection_test.go` | `runtime` | `stream` |
| `eventstream_accessors_test.go` | `runtime` | `stream` |
| `eventstream_agent_test.go` | `runtime` | `stream` |
| `assistant_stream_test.go` | `runtime` | `stream` |
| `stream_skill_payload_test.go` | `runtime` | `stream` |
| `stream_tool_projection_test.go` | `runtime` | `stream` |
| `stream_projection_strict_test.go` | `runtime` | `stream` |
| `stream_projection_test_helpers_test.go` | `runtime` | `stream` |
| `stream_provider_payload_test.go` | `runtime` | `stream` |
| `fact_extractor_test.go` | `runtime` | `factextract` |
| `memory_tools_test.go` | `runtime` | `factextract` |
| `helpers_test.go` (TestMessageToMapPreservesToolContent) | `runtime` | `stream` (split: StreamMessageFromSchema test moves, compactText + buildExecutionContext tests stay) |
| `elicitation_test.go` (TestProjectStreamItemToEventElicitationKinds) | `runtime` | `stream` (split: ElicitationInterruptState test stays, projection test moves) |

### Architecture Test Updates

| Test File | Change |
|---|---|
| `structural_limits_test.go` | Add `"internal/stream"`, `"internal/runtime/tooldispatch"`, `"internal/runtime/factextract"` to `refactorOwnedDirs` |
| `store_interface_count_test.go` | No change (consumer-owned interfaces don't move) |
| `client_projection_boundary_test.go` | No change (projection boundary is api/clientevents, not runtime/stream) |

## Compatibility Boundary

**Must not change**:
- `docs/openapi.yaml` — not a byte
- `mobile-kotlin/` — not a line
- SQLite schema — not a column
- CLI command interface — not a flag
- Config file format — not a field
- `make build`/`make test`/`make lint`/`make format-check` — must pass

**Will change**:
- Go import paths for moved symbols (internal only, no external consumers)
- Go package declarations for moved files

## Verification

After each task:
- `go build ./...` — no compile errors, no import cycles
- `go test ./internal/runtime/... ./internal/stream/...` — moved tests pass

After all tasks:
- `go test ./...` — full suite green
- `make format-check && make lint` — CI gates pass
- `make test-architecture` — architecture guards pass
- `git diff --stat` — confirm no `docs/openapi.yaml` or `mobile-kotlin/` changes

## Risks

- **RISK-001**: `safe_parallel_tools_node.go` (662 lines) splits into 3 files. The split must be clean — `SafeParallelToolsNode` methods stay together, `StreamingToolExecutor` methods stay together, scheduler helpers stay together. If split incorrectly, compilation fails. Mitigation: split by struct receiver, not by function name.
- **RISK-002**: `helpers_test.go` and `elicitation_test.go` each test symbols from both staying and moving packages. Must split each test file. Mitigation: identify each test function's symbol dependencies, move only the tests that reference moved symbols.
- **RISK-003**: `ToolNodeFactory` field in `RuntimeDeps` references `ToolInvoker` — when `ToolInvoker` moves to `tooldispatch`, the `types.go` RuntimeDeps struct must import `tooldispatch`. This is a runtime→tooldispatch dependency, which is fine (root depends on child).
- **RISK-004**: `assistantStreamOptions` struct in `assistant_stream.go` is unexported but used by `streaming_assistant_stream.go`. Both move to `stream` package together, so no issue.
- **RISK-005**: `runner_emit.go` references `StreamSkillRequirementsFromDomain` which is currently in `runner_emit.go` itself (line 199). This function uses `skills.Requirements` (from `internal/skills`) and returns `StreamSkillRequirements`. It moves to `stream` package. But `stream` would then depend on `skills` — check if that's acceptable. **Resolution**: `StreamSkillRequirementsFromDomain` is a pure conversion function. It should move to `stream` and `stream` will import `skills`. This is fine — `skills` is a low-level package.

## Tasks

### Task 1: Create `internal/stream` package — move eventstream + assistant_stream files

**Files created**:
- `internal/stream/types.go` — Stream* value types (content from `runtime/eventstream_payloads.go`)
- `internal/stream/projection.go` — projection functions (content from `runtime/eventstream_projection.go`)
- `internal/stream/accessors.go` — accessor functions (content from `runtime/eventstream_accessors.go`)
- `internal/stream/agent.go` — agent conversion (content from `runtime/eventstream_agent.go`)
- `internal/stream/assistant_stream.go` — assistant streaming (content from `runtime/assistant_stream.go`)
- `internal/stream/streaming_assistant.go` — interleaved streaming (content from `runtime/streaming_assistant_stream.go`)
- `internal/stream/stream_types.go` — AssistantStreamer interface + AssistantStreamRequest/Result/StopReason + InterleavedStream (from `runtime/types.go` lines 369-402)

**Files modified**:
- `internal/runtime/types.go` — remove lines 369-402 (AssistantStreamer/AssistantStream*/InterleavedStream), remove unused imports
- `internal/runtime/executor.go` — update references: `stream.AppendStreamItem`, `stream.StreamItemsFromAgentEvent`, `stream.StreamSkill`, `stream.StreamSkillRequirementsFromDomain`
- `internal/runtime/direct_response.go` — no stream references to update (uses AssistantStreamer from types.go, which moves to stream)
- `internal/runtime/runner.go` — update `NewDirectAssistantStreamer` → `stream.NewDirectAssistantStreamer`
- `internal/runtime/runner_emit.go` — update all Stream* references to `stream.*`
- `internal/runtime/types.go` — `DirectResponseRequest.AssistantStreamer` field type → `stream.AssistantStreamer`

**Files deleted**:
- `internal/runtime/eventstream_payloads.go`
- `internal/runtime/eventstream_projection.go`
- `internal/runtime/eventstream_accessors.go`
- `internal/runtime/eventstream_agent.go`
- `internal/runtime/assistant_stream.go`
- `internal/runtime/streaming_assistant_stream.go`

**Test files moved**:
- `eventstream_projection_test.go` → `internal/stream/projection_test.go`
- `eventstream_accessors_test.go` → `internal/stream/accessors_test.go`
- `eventstream_agent_test.go` → `internal/stream/agent_test.go`
- `assistant_stream_test.go` → `internal/stream/assistant_stream_test.go`
- `stream_skill_payload_test.go` → `internal/stream/skill_payload_test.go`
- `stream_tool_projection_test.go` → `internal/stream/tool_projection_test.go`
- `stream_projection_strict_test.go` → `internal/stream/projection_strict_test.go`
- `stream_projection_test_helpers_test.go` → `internal/stream/projection_test_helpers_test.go`
- `stream_provider_payload_test.go` → `internal/stream/provider_payload_test.go`

**Test files split**:
- `helpers_test.go`: `TestMessageToMapPreservesToolContent` → moves to `internal/stream/helpers_test.go`. `TestBuildExecutionContextPropagatesTurnIndexToReader` + `TestCompactText` → stay in `runtime/helpers_test.go`.
- `elicitation_test.go`: `TestProjectStreamItemToEventElicitationKinds` → moves to `internal/stream/elicitation_test.go`. `TestElicitationInterruptStateGobRoundTrip` + `TestStreamKindElicitationConstants` → stay in `runtime/elicitation_test.go`.

**Steps**:
1. Create `internal/stream/` directory
2. Create `internal/stream/types.go` with package `stream`, copy all Stream* types from `eventstream_payloads.go`, add `StreamSkillRequirementsFromDomain` function from `runner_emit.go:199`
3. Create `internal/stream/stream_types.go` with AssistantStreamer interface + AssistantStreamRequest/Result/StopReason + InterleavedStream from `runtime/types.go:369-402`
4. Create `internal/stream/projection.go` — copy content from `eventstream_projection.go`, change package to `stream`, update internal references to use local types
5. Create `internal/stream/accessors.go` — copy from `eventstream_accessors.go`, change package to `stream`
6. Create `internal/stream/agent.go` — copy from `eventstream_agent.go`, change package to `stream`
7. Create `internal/stream/assistant_stream.go` — copy from `assistant_stream.go`, change package to `stream`, update `AssistantStreamResult`/`AssistantStopReason` references to local
8. Create `internal/stream/streaming_assistant.go` — copy from `streaming_assistant_stream.go`, change package to `stream`, update `AssistantStreamRequest`/`InterleavedStream` references to local
9. Move test files (change package to `stream`, update symbol references to use `stream.*` where needed — but since tests are in the same package, they use unqualified names)
10. Split `helpers_test.go` and `elicitation_test.go`
11. Delete old files from `runtime/`
12. Update `runtime/executor.go`, `runtime/runner.go`, `runtime/runner_emit.go`, `runtime/types.go` references
13. Run `go build ./...` — verify no cycles, no missing references
14. Run `go test ./internal/stream/... ./internal/runtime/...` — verify all moved tests pass
15. Commit

### Task 2: Create `internal/runtime/tooldispatch` package — move safe_parallel_tools_node + side_effects

**Files created**:
- `internal/runtime/tooldispatch/types.go` — `StreamingExecutor` interface + `ToolInvoker` interface (from `runtime/types.go:409-419`)
- `internal/runtime/tooldispatch/node.go` — `SafeParallelToolsNode` struct + `NewSafeParallelToolsNode` + `NewStreamingExecutor` + `invokeSingle` + `attachToolMessageLedgerMeta` + `attachToolSideEffects` + `markToolMessageFailed` + `invokeToolWithEinoCallbacks` + `toolCallbackType` + `emitToolCallLifecycle` + `emitToolResultLifecycle` + `IsInterruptError` (from `safe_parallel_tools_node.go` lines 1-286, 287-312)
- `internal/runtime/tooldispatch/streaming_executor.go` — `StreamingToolExecutor` struct + `NewStreamingToolExecutor` + `Submit` + `canExecuteImmediately` + `startExecution` + `GetRemainingResults` + `Discard` + `submittedTool` + `toolExecutionStatus` (from `safe_parallel_tools_node.go` lines 298-537)
- `internal/runtime/tooldispatch/scheduler.go` — `toolExecutionScheduler` + `newToolExecutionScheduler` + `classifiedCall` + `pathsOverlap` + `executionPathsFromArgs` + `normalizeExecutionPaths` + `executionTrimmedPaths` (from `safe_parallel_tools_node.go` lines 543-662)
- `internal/runtime/tooldispatch/side_effects.go` — `SideEffectRef` + `SideEffectKind*` constants + `buildToolResultRef` + `ToolSideEffectsFromResult` (renamed from `toolSideEffectsFromResult`, exported) + all `*SideEffects` functions + `normalizedSideEffectPaths` (from `runtime/side_effects.go`)

**Files modified**:
- `internal/runtime/types.go` — remove lines 409-419 (StreamingExecutor/ToolInvoker), remove unused imports. Update `RuntimeDeps.ToolNodeFactory` signature to use `tooldispatch.ToolInvoker`
- `internal/runtime/direct_response.go` — update `NewSafeParallelToolsNode` → `tooldispatch.NewSafeParallelToolsNode`, `ToolInvoker` → `tooldispatch.ToolInvoker`
- `internal/runtime/agent_loop.go` — update `ToolInvoker` → `tooldispatch.ToolInvoker`, `StreamingExecutor` → `tooldispatch.StreamingExecutor`
- `internal/runtime/audit.go` — update `toolSideEffectsFromResult` → `tooldispatch.ToolSideEffectsFromResult`, `SideEffectRef` → `tooldispatch.SideEffectRef`
- `internal/runtime/runner.go` — `directResponseRequest` uses `AssistantStreamer` (already moved to stream in Task 1). Check for ToolInvoker references.

**Files deleted**:
- `internal/runtime/safe_parallel_tools_node.go`
- `internal/runtime/side_effects.go`

**Test files**: Test stays in `runtime` package (direct_response_test.go uses `directResponseTestToolNode` which implements `ToolInvoker` — update interface ref to `tooldispatch.ToolInvoker`). No test file moves to tooldispatch because the tests are integration-level (testing direct_response behavior, not tooldispatch unit).

**Steps**:
1. Create `internal/runtime/tooldispatch/` directory
2. Create `tooldispatch/types.go` with `StreamingExecutor` + `ToolInvoker` interfaces
3. Create `tooldispatch/side_effects.go` — copy from `runtime/side_effects.go`, export `ToolSideEffectsFromResult` (was `toolSideEffectsFromResult`), change package to `tooldispatch`
4. Create `tooldispatch/node.go` — copy SafeParallelToolsNode + its methods + dispatch helpers from `safe_parallel_tools_node.go`, change package to `tooldispatch`, update `SideEffectRef`/`toolSideEffectsFromResult` to local, update `StreamingExecutor`/`ToolInvoker` to local, update `emitToolCallLifecycle`/`emitToolResultLifecycle` to use `stream.AppendStreamItem`
5. Create `tooldispatch/streaming_executor.go` — copy StreamingToolExecutor section, change package to `tooldispatch`
6. Create `tooldispatch/scheduler.go` — copy scheduler section, change package to `tooldispatch`
7. Delete `runtime/safe_parallel_tools_node.go` + `runtime/side_effects.go`
8. Update `runtime/types.go` — remove StreamingExecutor/ToolInvoker defs, update RuntimeDeps.ToolNodeFactory
9. Update `runtime/direct_response.go`, `runtime/agent_loop.go`, `runtime/audit.go` references
10. Update `direct_response_test.go` — `directResponseTestToolNode` implements `tooldispatch.ToolInvoker`, `directResponseTestStreamingExecutor` implements `tooldispatch.StreamingExecutor`
11. Run `go build ./...`
12. Run `go test ./internal/runtime/... ./internal/runtime/tooldispatch/...`
13. Commit

### Task 3: Create `internal/runtime/factextract` package — move fact_extractor + memory_tools

**Files created**:
- `internal/runtime/factextract/extractor.go` — `ExtractSemanticFact` + all `extract*Fact` helpers + `truncateFact` + `maxFactLen` + `firstNonEmptyLine` (from `runtime/fact_extractor.go`)
- `internal/runtime/factextract/memory_tools.go` — `BuildMemoryFileTools` + `buildMemoryToolCatalog` + `collectMemoryFileTools` + `memoryNamespacedTool` + helpers (from `runtime/memory_tools.go`)
- `internal/runtime/factextract/memory_tools_search.go` — `memorySearchTool` + `memoryRememberTool` + helpers (from `runtime/memory_tools_search.go`)

**Files modified**:
- `internal/runtime/runner_toolset.go` — update `BuildMemoryFileTools` → `factextract.BuildMemoryFileTools`
- `internal/runtime/safe_parallel_tools_node.go` (already deleted in Task 2) — `attachToolSideEffects` used `toolSideEffectsFromResult` for fact extraction? Check: no, `attachToolSideEffects` calls `tooldispatch.ToolSideEffectsFromResult`, not `ExtractSemanticFact`. But `invokeToolWithEinoCallbacks` in node.go might call `ExtractSemanticFact`. **Verify**: search for `ExtractSemanticFact` usage in safe_parallel_tools_node.go. If used, update to `factextract.ExtractSemanticFact`.

**Files deleted**:
- `internal/runtime/fact_extractor.go`
- `internal/runtime/memory_tools.go`
- `internal/runtime/memory_tools_search.go`

**Test files moved**:
- `fact_extractor_test.go` → `internal/runtime/factextract/extractor_test.go`
- `memory_tools_test.go` → `internal/runtime/factextract/memory_tools_test.go`

**Steps**:
1. Create `internal/runtime/factextract/` directory
2. Create `factextract/extractor.go` — copy from `fact_extractor.go`, change package to `factextract`
3. Create `factextract/memory_tools.go` — copy from `memory_tools.go`, change package to `factextract`
4. Create `factextract/memory_tools_search.go` — copy from `memory_tools_search.go`, change package to `factextract`
5. Move test files, change package to `factextract`
6. Delete old files from `runtime/`
7. Update `runtime/runner_toolset.go` reference to `factextract.BuildMemoryFileTools`
8. Search for `ExtractSemanticFact` usage in remaining runtime files, update to `factextract.ExtractSemanticFact`
9. Run `go build ./...`
10. Run `go test ./internal/runtime/... ./internal/runtime/factextract/...`
11. Commit

### Task 4: Update architecture tests + final verification

**Files modified**:
- `tests/architecture/structural_limits_test.go` — add `"internal/stream"`, `"internal/runtime/tooldispatch"`, `"internal/runtime/factextract"` to `refactorOwnedDirs`

**Steps**:
1. Update `structural_limits_test.go` `refactorOwnedDirs` slice
2. Run `go build ./...`
3. Run `go test ./...` — full suite
4. Run `make format-check && make lint`
5. Run `make test-architecture`
6. Verify `git diff --stat docs/openapi.yaml` is empty
7. Verify `git diff --stat mobile-kotlin/` is empty
8. Commit
