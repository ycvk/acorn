# Acorn 架构模块化重构实现计划

Date: 2026-06-23
Plan Basis: `docs/aegis/specs/2026-06-23-modular-refactor-design.md`(已 approved)
Architecture: Go 1.26 + Eino ADK 单用户自托管 AI agent 后端
Tech Stack: Go 1.26, modernc.org/sqlite, cloudwego/eino/adk, cobra CLI

BaselineUsageDraft:
- Required baseline refs: AGENTS.md, INVARIANTS.md, ARCHITECTURE.md
- Acknowledged before plan refs: structural_limits_test.go, store_interface_count_test.go, client_projection_boundary_test.go
- Cited in plan refs: spec §4 Step 1-5, §5 不变量, §7 架构测试更新
- Missing refs: none
- Decision: continue

Requirement Ready Check:
- Requirement source refs: spec §2(5 个问题诊断), spec §4(5 个 Step)
- Acceptance / verification criteria refs: spec §5(不变量), spec §6(迁移策略), spec §7(架构测试更新)
- Open blocker questions: none
- Decision: ready

Compatibility Boundary:
- `docs/openapi.yaml` / `mobile-kotlin/` / SQLite schema / CLI 接口 / 配置格式 全部不变
- `NewRunnerFactory` 签名不变
- `direct_response` 单一编排模式不变
- `ActiveRunner` / `RunnerBuildRequest` / `RunnerFactoryOptions` / `RuntimeDeps` 类型不变

Verification: 每 Step 完成后 `go build ./...` + `go test ./...` + `make format-check && make lint`;Step 2/3/4 后 `make test-architecture`;全部完成后 `go test -race ./...`

Risks:
- [RISK-001] Step 2 全量 import rename 可能遗漏 → go build + goimports 兜底
- [RISK-002] Step 5 executor_e2e_test.go 直接构造 `&RunnerFactory{}` 需更新 fixture
- [RISK-003] browser 文件合并后可能超 800 行守卫 → 按需拆分
- [RISK-004] Step 4 拆 client_service 后 `client_projection_boundary_test.go` 引用 `client_helpers.go` → 同步更新守卫文件列表
- [RISK-005] Step 1 后 `TestAppServicesDoNotImportStreamOutsideRuntimeAdapter` 仍有效(app 不 import stream),无需改

Retirement: 无旧路径需要 compat alias。每步是 hard cutover,删除旧文件同时迁移。

---

## Phase 1: Step 1 — stream/domain 类型收敛

### Task 1.1: 移动 stream payload 类型到 domain

**Files:**
- Create: `internal/domain/stream_types.go`
- Create: `internal/domain/stream_accessors.go`
- Delete: `internal/stream/types.go`
- Delete: `internal/stream/accessors.go`
- Modify: `internal/stream/agent.go`(`StreamMessageFromSchema` 返回类型改为 `*domain.StreamMessage`)
- Modify: `internal/stream/streaming_assistant.go`(引用改 `domain.InterleavedStream` 等)
- Modify: `internal/stream/projection.go`(引用改 `domain.*` payload types 用于 reencode)
- Modify: `internal/runtime/*.go`(6 文件 import stream → 改为 import domain + stream)
- Modify: `internal/runtime/direct_response_test.go`, `executor_test.go`(同上)

**Why:** StreamItem 和它的 payload struct 分布在两个包,共同管一个概念。收敛到 domain 消除分散。

**Impact/Compatibility:** `stream` 包仍保留(投影 + streaming 逻辑),只是类型定义移到 domain。所有外部引用 `stream.StreamMessage` 等改为 `domain.StreamMessage`。`client_projection_boundary_test.go` 的 `TestAppServicesDoNotImportStreamOutsideRuntimeAdapter` 检查 app 包不能 import `stream`——Step 1 后 app 包仍不 import stream,payload 类型在 domain,守卫不受影响。

**Verification:**
```bash
go build ./...
go test ./internal/domain ./internal/stream ./internal/runtime ./internal/app ./tests/architecture
make format-check
```

**Steps:**

- [ ] 1. 创建 `internal/domain/stream_types.go`,从 `internal/stream/types.go` 复制全部 16 个 type + const,改 package 为 `domain`。需要的 import:`context`, `einomodel "github.com/cloudwego/eino/components/model"`, `"github.com/cloudwego/eino/schema"`。

- [ ] 2. 创建 `internal/domain/stream_accessors.go`,从 `internal/stream/accessors.go` 复制全部内容,改 package 为 `domain`。删除 `import "github.com/ycvk/acorn/internal/domain"`(已在同包)。helper 函数 `getPayloadMap`/`getNestedMap`/`getString`/`getInt`/`getInt64`/`getBool`/`getStringSlice`/`compactInterruptInfo` 保留为 unexported。`ItemGet*` 函数参数类型从 `domain.StreamItem` 改为 `StreamItem`(同包)。

- [ ] 3. 删除 `internal/stream/types.go` 和 `internal/stream/accessors.go`。

- [ ] 4. 更新 `internal/stream/agent.go`:
  - `StreamMessageFromSchema` 返回类型从 `*StreamMessage` 改为 `*domain.StreamMessage`
  - 函数体中 `&StreamMessage{...}` 改为 `&domain.StreamMessage{...}`
  - `streamInterruptFromInfo` 返回类型从 `*StreamInterrupt` 改为 `*domain.StreamInterrupt`

- [ ] 5. 更新 `internal/stream/streaming_assistant.go`:
  - 所有 `InterleavedStream` → `domain.InterleavedStream`
  - `AssistantStreamResult` → `domain.AssistantStreamResult`
  - `AssistantStreamRequest` → `domain.AssistantStreamRequest`
  - `AssistantStreamer` → `domain.AssistantStreamer`
  - `AssistantStopReason` → `domain.AssistantStopReason`
  - `directAssistantStreamer` struct 的字段类型和构造函数签名同步更新

- [ ] 6. 更新 `internal/stream/projection.go`:
  - `ProjectStreamItemToEvent` 中 payload reencode 逻辑不需要改(操作 `map[string]any`,不直接引用 payload struct)
  - 如果 `streamPayloadMap` 中有类型断言引用 payload struct,改为 `domain.*` 前缀

- [ ] 7. 更新 `internal/runtime/` 中 6 个文件:
  - `runner.go` / `types.go` / `direct_response.go` / `runner_emit.go` / `agent_loop.go` / `executor.go`
  - 已 import `"github.com/ycvk/acorn/internal/stream"` 的文件:保留 stream import(投影/streaming 函数仍在 stream),增加 `"github.com/ycvk/acorn/internal/domain"` import(如果还没有)
  - 将所有 `stream.StreamMessage` / `stream.StreamToolCall` / `stream.StreamInterrupt` / `stream.StreamSkill` / `stream.StreamAssistantDelta` / `stream.StreamMemoryPrepared` / `stream.StreamMemoryPreparedEntry` / `stream.StreamMemoryPreparedNudge` / `stream.StreamSkillCandidate` / `stream.StreamSkillRequirements` / `stream.StreamPlannedToolCall` / `stream.StreamInterruptContext` / `stream.AssistantStreamResult` / `stream.AssistantStreamRequest` / `stream.AssistantStopReason` / `stream.InterleavedStream` / `stream.AssistantStreamer` → `domain.*` 前缀
  - 将 `stream.ItemGetMessage` / `stream.ItemGetAssistantDelta` / `stream.ItemGetToolCall` / `stream.ItemGetInterrupt` / `stream.ItemGetSkill` / `stream.ItemGetMemoryPrepared` / `stream.ItemGetError` → `domain.ItemGet*` 前缀
  - 保留 `stream.AppendStreamItem` / `stream.ProjectStreamItemToEvent` / `stream.StreamItemsFromAgentEvent` / `stream.StreamMessageFromSchema` / `stream.NewDirectAssistantStreamer` 的引用(这些函数留在 stream 包)

- [ ] 8. 更新 `internal/runtime/direct_response_test.go` 和 `executor_test.go` 中对 stream 类型的引用,改为 domain 前缀。

- [ ] 9. 运行验证:
```bash
go build ./...
go test ./internal/domain ./internal/stream ./internal/runtime ./internal/app ./tests/architecture
make format-check
```

- [ ] 10. Commit: `refactor: move stream payload types to domain package`

---

## Phase 2: Step 2 — 合并 toolkit + toolset → tools

### Task 2.1: 创建 internal/tools 包并迁移 toolkit 文件

**Files:**
- Create: `internal/tools/contract.go` (合并 toolkit/contracts.go + catalog.go + specs.go)
- Create: `internal/tools/builtin_registry.go` (原 toolkit/builtin_registry.go)
- Create: `internal/tools/eligibility.go` (原 toolkit/skills.go)
- Create: `internal/tools/*_test.go` (原 toolkit 测试文件,package 改为 `tools_test`)
- Delete: `internal/toolkit/` 目录

**Why:** toolkit 和 toolset 命名混淆,toolset import toolkit 47 次。合并消除混淆。

**Impact/Compatibility:** 全量 import path rename。所有 `toolkit.ToolSpec` → `tools.ToolSpec` 等。架构测试需更新。

**Verification:**
```bash
go build ./...
go test ./internal/tools
make format-check
```

**Steps:**

- [ ] 1. 创建 `internal/tools/contract.go`:合并 `toolkit/contracts.go` + `catalog.go` + `specs.go` 的内容,package 改为 `tools`。检查合并后行数不超 800。三个文件合计 597 行,合并后应在 600 行左右,不超限。

- [ ] 2. 创建 `internal/tools/builtin_registry.go`:从 `toolkit/builtin_registry.go` 复制,package 改为 `tools`。

- [ ] 3. 创建 `internal/tools/eligibility.go`:从 `toolkit/skills.go` 复制,package 改为 `tools`。

- [ ] 4. 迁移 toolkit 测试文件:`toolkit/*_test.go` → `tools/*_test.go`,package 改为 `tools_test`。

- [ ] 5. 删除 `internal/toolkit/` 目录。

- [ ] 6. 全量替换 import:`"github.com/ycvk/acorn/internal/toolkit"` → `"github.com/ycvk/acorn/internal/tools"`,符号前缀 `toolkit.` → `tools.`。受影响文件(33 个,含测试):
  - `internal/app/capability_service.go`, `skill_service.go`
  - `internal/contextplane/tool_lifecycle.go`, `types.go`, `tool_lifecycle_test.go`
  - `internal/runtime/` 下 16 个文件
  - `internal/toolset/` 下 13 个文件(这些将在 Task 2.2 中合并到 tools)

- [ ] 7. 运行验证:
```bash
go build ./...
go test ./internal/tools
make format-check
```

- [ ] 8. Commit: `refactor: merge toolkit into tools package`

### Task 2.2: 迁移 toolset 文件到 internal/tools

**Files:**
- Create: `internal/tools/ports.go` (原 toolset/ports.go)
- Create: `internal/tools/file_read.go` (合并 native_read_tools.go + native_search_tools.go)
- Create: `internal/tools/file_mutate.go` (CreateFile/ReplaceSpan/ApplyUnifiedPatch)
- Create: `internal/tools/file_edit.go` (MultiEdit/RollbackCheckpoint,从 workflow_tools.go 拆出)
- Create: `internal/tools/workflow.go` (workflow_tools.go 中 verification 部分)
- Create: `internal/tools/git.go` (toolset/tools.go 中 git 部分)
- Create: `internal/tools/command.go` (command_tool.go + processgroup_*.go)
- Create: `internal/tools/browser_service.go` (核心 Service struct + Config + Status + Scan + Snapshot)
- Create: `internal/tools/browser_service_actions.go` (Navigate + Console + Network,从 browser_service_navigate.go + browser_service_events.go 合并)
- Create: `internal/tools/browser_tool.go` (原 browser_tool.go)
- Create: `internal/tools/web.go` (合并 web_search_tool.go + web_fetch_tool.go)
- Create: `internal/tools/artifact.go` (原 artifact_tools.go)
- Create: `internal/tools/operator.go` (合并 operator_question_tool.go + progress_tool.go)
- Create: `internal/tools/catalog_builders.go` (合并 toolset/tools.go 中 catalog 部分 + catalog_builders.go)
- Create: `internal/tools/tools_types.go` (toolset/tools.go 中的 input/output struct 定义)
- Create: `internal/tools/*_test.go` (原 toolset 测试文件,package 改为 `tools_test`)
- Delete: `internal/toolset/` 目录

**Why:** 同 Task 2.1,完成合并。

**Impact/Compatibility:** 所有 `toolset.` 前缀 → `tools.`。受影响文件:`internal/providers/mcp/transport.go`, `internal/runtime/factextract/memory_tools.go`, `internal/runtime/runner_toolset.go`, `internal/runtime/runner.go`。

**Verification:**
```bash
go build ./...
go test ./internal/tools ./internal/runtime
make lint
```

**Steps:**

- [ ] 1. 逐文件迁移 toolset → tools。每个文件:复制内容,改 package 为 `tools`,将所有 `toolkit.` 前缀改为直接引用(同包,因 Task 2.1 已合并)。

- [ ] 2. **browser_service.go 合并检查**:browser_service.go(626行) + browser_service_navigate.go(280行) + browser_service_events.go(173行) = 1079 行。合并后超 800 行守卫。方案:创建 `internal/tools/browser_service.go`(核心 Service struct + Config + Status + Scan + Snapshot,约 700 行)+ `internal/tools/browser_service_actions.go`(Navigate + Console + Network,约 380 行)。

- [ ] 3. **file_mutate.go 合并检查**:native_mutation_tools.go(438行) + workflow_tools.go mutation 部分(CreateFile/ReplaceSpan/ApplyUnifiedPatch/MultiEdit/RollbackCheckpoint)。workflow_tools.go 总 717 行,mutation 部分约 500 行。合并后约 938 行,超限。方案:拆为 `file_mutate.go`(CreateFile/ReplaceSpan/ApplyUnifiedPatch,约 500 行)+ `file_edit.go`(MultiEdit/RollbackCheckpoint,约 440 行)。

- [ ] 4. 迁移 toolset 测试文件:`toolset/*_test.go` → `tools/*_test.go`,package 改为 `tools_test`。

- [ ] 5. 删除 `internal/toolset/` 目录。

- [ ] 6. 全量替换 import:`"github.com/ycvk/acorn/internal/toolset"` → `"github.com/ycvk/acorn/internal/tools"`,符号前缀 `toolset.` → `tools.`。受影响文件:
  - `internal/providers/mcp/transport.go`
  - `internal/runtime/factextract/memory_tools.go`
  - `internal/runtime/runner_toolset.go`(此时应已无此文件,Step 5 才拆)
  - `internal/runtime/runner.go`

- [ ] 7. 运行验证:
```bash
go build ./...
go test ./internal/tools ./internal/runtime
make lint
```

- [ ] 8. Commit: `refactor: merge toolset into tools package`

### Task 2.3: 更新架构测试

**Files:**
- Modify: `tests/architecture/structural_limits_test.go`

**Why:** `refactorOwnedDirs` 引用了已删除的 `internal/toolkit` 和 `internal/toolset`,需替换为 `internal/tools`。

**Verification:**
```bash
go test ./tests/architecture
```

**Steps:**

- [ ] 1. 在 `structural_limits_test.go` 中,`refactorOwnedDirs` 数组:删除 `"internal/toolkit"` 和 `"internal/toolset"`,加 `"internal/tools"`。

- [ ] 2. 运行:
```bash
go test ./tests/architecture
```

- [ ] 3. Commit: `test: update structural limits for tools package merge`

---

## Phase 3: Step 3 — 收敛 store 接口

### Task 3.1: 删除 PendingActionCreateStore,内联到 containerAppStore

**Files:**
- Modify: `internal/app/device_auth_service.go`
- Modify: `internal/app/container_runtime.go`
- Modify: `tests/architecture/store_interface_count_test.go`

**Why:** `PendingActionCreateStore` 的 6 个方法中有 5 个与 `containerAppStore` 完全相同——重复定义。

**Impact/Compatibility:** `DeviceAuthService.store` 字段类型从 `PendingActionCreateStore` 改为 `containerAppStore`。`containerRuntimeStore` 不再 embed `PendingActionCreateStore`。

**Verification:**
```bash
go test ./internal/app ./tests/architecture
make format-check
```

**Steps:**

- [ ] 1. 在 `internal/app/device_auth_service.go` 中:
  - 删除 `type PendingActionCreateStore interface { ... }`(line 20-26)
  - `DeviceAuthService` struct 的 `store` 字段类型从 `PendingActionCreateStore` 改为 `containerAppStore`

- [ ] 2. 在 `internal/app/container_runtime.go` 中:
  - 从 `containerRuntimeStore` interface 定义中删除 `PendingActionCreateStore` embed(line 25)
  - `containerRuntimeDeps` struct 的 `mcpPendingActionStore` 字段类型从 `PendingActionCreateStore` 改为 `containerAppStore`(line 76)
  - `mcpPendingActionStore := PendingActionCreateStore(store)` 改为 `mcpPendingActionStore := containerAppStore(store)`(line 98)

- [ ] 3. 在 `tests/architecture/store_interface_count_test.go` 中:
  - `maxConsumerStoreInterfaces` 从 6 改为 5(暂时,Task 3.2 后再改为 4)

- [ ] 4. 运行:
```bash
go build ./...
go test ./internal/app ./tests/architecture
make format-check
```

- [ ] 5. Commit: `refactor: inline PendingActionCreateStore into containerAppStore`

### Task 3.2: 删除 skillSnapshotStore,内联依赖

**Files:**
- Modify: `internal/app/capability_service.go`
- Modify: `tests/architecture/store_interface_count_test.go`

**Why:** `skillSnapshotStore` 只有 1 个方法 `Snapshot(ctx) (*skills.Snapshot, error)`,单实现接口,不需要抽象。

**Impact/Compatibility:** `CapabilitiesService` 直接持有 `*skills.Loader`。

**Verification:**
```bash
go test ./internal/app ./tests/architecture
```

**Steps:**

- [ ] 1. 在 `internal/app/capability_service.go` 中:
  - 删除 `type skillSnapshotStore interface { ... }`(line 115-117)
  - `CapabilitiesService` struct 的 `skills` 字段类型从 `skillSnapshotStore` 改为 `*skills.Loader`
  - `NewCapabilitiesService` 参数 `skills skillSnapshotStore` 改为 `skills *skills.Loader`
  - 方法体中调用 `s.skills.Snapshot(ctx)` 不变(`*skills.Loader` 已有此方法)

- [ ] 2. 在 `tests/architecture/store_interface_count_test.go` 中:
  - `maxConsumerStoreInterfaces` 从 5 改为 4

- [ ] 3. 检查 `internal/app/container_runtime.go` 或 `container.go` 中 `NewCapabilitiesService` 的调用点,确认传入参数类型匹配(`*skills.Loader`)。

- [ ] 4. 运行:
```bash
go build ./...
go test ./internal/app ./tests/architecture
make format-check
```

- [ ] 5. Commit: `refactor: inline skillSnapshotStore into direct Loader dependency`

---

## Phase 4: Step 4 — 拆 app client_service → 按领域分 service

### Task 4.1: 创建 ThreadService 并迁移 thread/message 方法

**Files:**
- Create: `internal/app/thread_service.go`
- Modify: `internal/app/client_service.go`(删除 thread/message 方法)
- Modify: `internal/app/client_queries.go`(删除 thread/message 方法,保留 projection helper 如需要)
- Modify: `internal/app/client_helpers.go`(删除 thread/message projection)
- Modify: `internal/api/server.go`
- Modify: `internal/app/container.go`
- Modify: `internal/app/container_runtime.go`
- Modify: `internal/api/handlers_thread.go`, `handlers_message.go`

**Why:** `client_service.go` 同时管 thread CRUD + run 创建 + event 加载 + artifact 列表 + run 中断,5 个不相关职责。

**Impact/Compatibility:** `api/server.go` 的 `ClientService` interface 拆为 `ThreadService`。`Container` 增加持有 `*ThreadService`。

**Verification:**
```bash
go test ./internal/app ./internal/api ./tests/architecture
make format-check
```

**Steps:**

- [ ] 1. 创建 `internal/app/thread_service.go`:
  - 定义 `ThreadService struct { store containerAppStore }`
  - 定义 `NewThreadService(store containerAppStore) *ThreadService`
  - 从 `client_queries.go` 迁移:`ListThreads` / `CreateThread` / `GetThread` / `UpdateThread` / `DeleteThread` / `ListMessages` / `CreateMessage` / `threadTitleFromRecentUserMessage` / `generatedThreadTitle` / `projectThread` / `projectThreadState` / `createUserMessage` / `projectMessage` / `projectMessageParts`
  - 从 `client_helpers.go` 迁移:`validateMessagePart` / `validateDisclosureItem` / `validateMessageAction`

- [ ] 2. 从 `client_service.go` 删除上述方法。从 `client_queries.go` 删除上述方法。从 `client_helpers.go` 删除上述 projection 方法。

- [ ] 3. 在 `internal/api/server.go` 中:
  - 定义 `ThreadService interface { ListThreads / CreateThread / GetThread / UpdateThread / DeleteThread / ListMessages / CreateMessage }`(从 `ClientService` interface 中拆出 thread/message 方法)
  - `Dependencies` struct 加 `Threads ThreadService`
  - `Server` struct 加 `threads ThreadService` 字段
  - `NewServer` 构造函数加 `threads ThreadService` 参数

- [ ] 4. 在 `internal/app/container.go` 中:
  - `Container` struct 加 `threads *ThreadService` 字段
  - 加 `func (c *Container) Threads() *ThreadService { return c.threads }`
  - 在 `buildContainerAppServices` 中构造 `container.threads = NewThreadService(store)`

- [ ] 5. 在 `internal/api/handlers_thread.go` 和 `handlers_message.go` 中:
  - `s.client.ListThreads` → `s.threads.ListThreads` 等

- [ ] 6. 在 `internal/app/container.go` 的 `Server` 构造中:
  - `Dependencies{ ..., Threads: container.Threads() }`

- [ ] 7. 运行:
```bash
go build ./...
go test ./internal/app ./internal/api
make format-check
```

- [ ] 8. Commit: `refactor: extract ThreadService from ClientService`

### Task 4.2: 创建 EventService + RunService,删除旧文件,更新守卫

**Files:**
- Create: `internal/app/event_service.go`
- Create: `internal/app/run_service.go`
- Delete: `internal/app/client_service.go`(全部方法已迁出)
- Delete: `internal/app/client_helpers.go`(全部方法已迁出)
- Delete: `internal/app/client_queries.go`(全部方法已迁出)
- Modify: `internal/api/server.go`
- Modify: `internal/app/container.go`
- Modify: `internal/app/container_runtime.go`
- Modify: `internal/api/handlers_run.go`
- Modify: `tests/architecture/client_projection_boundary_test.go`

**Why:** 同 Task 4.1,完成拆分。

**Impact/Compatibility:** `ClientService` interface 最终拆为 `ThreadService` + `RunService` + `EventService`。`client_projection_boundary_test.go` 的 `clientProjectionBoundaryFiles` 列表引用了 `internal/app/client_helpers.go`(将被删除),需同步更新为新的文件路径。

**Verification:**
```bash
go test ./internal/app ./internal/api ./tests/architecture
make format-check
```

**Steps:**

- [ ] 1. 创建 `internal/app/event_service.go`:
  - 定义 `EventService struct { store containerAppStore }`
  - 定义 `NewEventService(store containerAppStore) *EventService`
  - 从 `client_service.go` 迁移:`LoadRunEventsAfter` / `LoadRunEventsForDetail` / `EventPollInterval`
  - 从 `client_helpers.go` 迁移:`ListRunArtifacts` / `buildArtifactSummaries`

- [ ] 2. 创建 `internal/app/run_service.go`:
  - 定义 `RunService struct { store containerAppStore; newExecutor func(context.Context) (executorHandle, error); controller *runtime.RunController }`
  - 定义 `NewRunService(store containerAppStore, newExecutor func(context.Context) (executorHandle, error), controller *runtime.RunController) *RunService`
  - 从 `client_service.go` 迁移:`GetRun` / `RunIsTerminal` / `InterruptRun` / `CreateRun` / `executeRun` / `reportBackgroundRunFailure` / `recordStartedRunFailure`
  - 从 `client_helpers.go` 迁移:`projectRunStatus` / `projectionError`

- [ ] 3. 从 `client_service.go` 删除所有剩余方法。删除 `client_service.go` / `client_helpers.go` / `client_queries.go`(已全部迁出)。删除 `BuildClientService` 函数。

- [ ] 4. 在 `internal/api/server.go` 中:
  - 删除 `ClientService` interface(已全部拆出)
  - 加 `RunService interface { CreateRun / GetRun / RunIsTerminal / InterruptRun }`
  - 加 `EventService interface { LoadRunEventsAfter / LoadRunEventsForDetail / ListRunArtifacts / EventPollInterval }`
  - `Dependencies` struct 加 `Runs RunService` 和 `Events EventService`
  - `Server` struct 加 `runs RunService` 和 `events EventService` 字段

- [ ] 5. 在 `internal/app/container.go` 中:
  - `Container` struct 字段:`client *ClientService` → `threads *ThreadService` + `runs *RunService` + `events *EventService`
  - 删除 `func (c *Container) Client() *ClientService`
  - 加 `func (c *Container) Runs() *RunService` / `func (c *Container) Events() *EventService`
  - `buildContainerAppServices` 中:
    ```go
    container.threads = NewThreadService(store)
    container.runs = NewRunService(store, deps.executors, deps.runController)
    container.events = NewEventService(store)
    ```

- [ ] 6. 更新 `internal/api/handlers_run.go`:
  - `s.client.GetRun` → `s.runs.GetRun`
  - `s.client.CreateRun` → `s.runs.CreateRun`
  - `s.client.InterruptRun` → `s.runs.InterruptRun`
  - `s.client.RunIsTerminal` → `s.runs.RunIsTerminal`
  - `s.client.LoadRunEventsAfter` → `s.events.LoadRunEventsAfter`
  - `s.client.LoadRunEventsForDetail` → `s.events.LoadRunEventsForDetail`
  - `s.client.ListRunArtifacts` → `s.events.ListRunArtifacts`
  - `s.client.EventPollInterval` → `s.events.EventPollInterval`

- [ ] 7. 更新 `tests/architecture/client_projection_boundary_test.go`:
  - `clientProjectionBoundaryFiles` 数组:`"internal/app/client_helpers.go"` → 替换为 `"internal/app/thread_service.go"`(thread projection 现在此文件)和 `"internal/app/event_service.go"`(artifact projection 现在此文件)
  - 守卫语义不变:projection 文件不能 import runtime

- [ ] 8. 更新所有 `*_test.go` 文件:
  - `client_service_test.go` 拆为 `thread_service_test.go` / `run_service_test.go` / `event_service_test.go`
  - 测试中 `BuildClientService(...)` → 分别构造 `NewThreadService(...)` / `NewRunService(...)` / `NewEventService(...)`

- [ ] 9. 运行:
```bash
go build ./...
go test ./internal/app ./internal/api ./tests/architecture
make format-check
```

- [ ] 10. Commit: `refactor: split ClientService into ThreadService, RunService, EventService`

---

## Phase 5: Step 5 — 拆 RunnerFactory god-object

### Task 5.1: 提取 ModelBuilder

**Files:**
- Create: `internal/runtime/model_builder.go`
- Modify: `internal/runtime/runner.go`

**Why:** RunnerFactory 47 个方法混合 7 个职责。model 构建是独立职责。

**Impact/Compatibility:** `NewRunnerFactory` 签名不变。`RunnerFactory` 内部组合 `*ModelBuilder`。`runChatModelBuilder` 字段从 `RunnerFactory` 移到 `ModelBuilder`。

**Verification:**
```bash
go test ./internal/runtime ./internal/app
```

**Steps:**

- [ ] 1. 创建 `internal/runtime/model_builder.go`:
  ```go
  package runtime

  import (
      "context"
      "github.com/cloudwego/eino/adk"
      einomodel "github.com/cloudwego/eino/components/model"
      "github.com/ycvk/acorn/internal/config"
  )

  type ModelBuilder struct {
      cfg *config.Config
      runChatModelBuilder func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error)
  }

  func NewModelBuilder(cfg *config.Config) *ModelBuilder {
      return &ModelBuilder{
          cfg: cfg,
          runChatModelBuilder: func(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
              return newOpenAIChatModel(ctx, cfg.Providers.Primary())
          },
      }
  }

  func (b *ModelBuilder) buildRunChatModel(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, error) {
      return b.runChatModelBuilder(ctx, req)
  }
  ```
  - 从 `runner.go` 迁移:`newChatModel` / `buildRunChatModel` / `buildRuntimeChatModel` / `buildRuntimeChatModelWithProvider` / `newRuntimeChatModel` / `newOpenAIChatModel` / `chatModelBuilder` type / `buildRunnerAgentHandlers`
  - 这些函数改为 `ModelBuilder` 的方法或保持包级函数(如果 `ModelBuilder` 不需要持有它们)。`buildRunnerAgentHandlers` 引用 `contextplane.ContextPlane` 和 `adk.ChatModelAgentMiddleware`,保持包级函数。

- [ ] 2. 在 `runner.go` 中:
  - `RunnerFactory` struct 加 `modelBuilder *ModelBuilder` 字段
  - 删除 `runChatModelBuilder` 字段(移到 ModelBuilder)
  - `NewChatModel` 方法改为委托:`return f.modelBuilder.buildRunChatModel(ctx, RunnerBuildRequest{})`
  - `assembleRunnerFactory` 函数中初始化 `modelBuilder: NewModelBuilder(deps.Config)`
  - 删除被迁移到 model_builder.go 的函数定义

- [ ] 3. 更新 `executor_e2e_test.go` 的 `newTestRunnerFactory`:
  ```go
  return &RunnerFactory{
      deps: RuntimeDeps{
          Config:       &config.Config{},
          MemoryModule: memSvc,
      },
      registry: NewRegistry(),
      modelBuilder: &ModelBuilder{
          cfg: &config.Config{},
          runChatModelBuilder: func(context.Context, RunnerBuildRequest) (einomodel.BaseChatModel, error) {
              if chatFunc != nil { return chatFunc(context.Background()) }
              return &fakeChatModel{}, nil
          },
      },
  }
  ```

- [ ] 4. 运行:
```bash
go build ./...
go test ./internal/runtime ./internal/app
```

- [ ] 5. Commit: `refactor: extract ModelBuilder from RunnerFactory`

### Task 5.2: 提取 CapabilityAssembler + ToolAssembler

**Files:**
- Rename: `internal/runtime/runner_toolset.go` → `internal/runtime/capability_assembler.go`
- Modify: `internal/runtime/direct_response.go`
- Modify: `internal/runtime/runner.go`

**Why:** capability 装配 + toolset 构建是独立职责。

**Impact/Compatibility:** `RunnerFactory` 组合 `*CapabilityAssembler`。`buildRunToolset` / `buildToolset` 等方法变为 `CapabilityAssembler` 方法。`assembleTooling` 变为 `ToolAssembler` 方法。

**Verification:**
```bash
go test ./internal/runtime ./internal/app
make format-check
```

**Steps:**

- [ ] 1. 重命名 `runner_toolset.go` 为 `capability_assembler.go`,将 `RunnerFactory` 方法改为 `CapabilityAssembler` 方法:
  ```go
  type CapabilityAssembler struct {
      deps RuntimeDeps
  }

  func NewCapabilityAssembler(deps RuntimeDeps) *CapabilityAssembler {
      return &CapabilityAssembler{deps: deps}
  }
  ```
  - 迁移方法:`buildRunToolset` / `buildToolset` / `validateToolsetDeps` / `buildLocalToolset` / `buildToolsetWebServices` / `buildBrowserService` / `resolveOperatorStore` / `buildLocalCatalog` / `buildAuxTools` / `buildMemoryTools`
  - `f.deps.X` → `a.deps.X`
  - 保留包级函数:`assembleToolsetCatalog` / `buildCoreToolSpecs` / `buildExtraToolSpecs` / `closeToolsetOnErr`

- [ ] 2. 在 `direct_response.go` 中创建 `ToolAssembler`:
  ```go
  type ToolAssembler struct {
      deps RuntimeDeps
  }

  func NewToolAssembler(deps RuntimeDeps) *ToolAssembler {
      return &ToolAssembler{deps: deps}
  }
  ```
  - `assembleTooling` 函数改为 `(a *ToolAssembler) assembleTooling(...)` 方法
  - `buildDirectResponse` 保持包级函数或改为 ToolAssembler 方法

- [ ] 3. 在 `runner.go` 中:
  - `RunnerFactory` struct 加 `capabilityAsm *CapabilityAssembler` 和 `toolAssembler *ToolAssembler` 字段
  - `assembleRunnerFactory` 中初始化
  - `buildRunCapabilities` 委托:`return f.capabilityAsm.buildRunCapabilities(ctx, ...)`
  - `BuildCapabilitySpecs` 委托:`return f.capabilityAsm.buildRunToolset(ctx, "").EnabledSpecs()` 或等价调用
  - 删除被迁移的方法定义

- [ ] 4. 更新 `executor_e2e_test.go`:初始化 `capabilityAsm` 和 `toolAssembler` 字段(可用 nil 或最小构造,因为 consume/finishCollectedRun 不触发 capability 装配)。

- [ ] 5. 运行:
```bash
go build ./...
go test ./internal/runtime ./internal/app
make format-check
```

- [ ] 6. Commit: `refactor: extract CapabilityAssembler and ToolAssembler from RunnerFactory`

### Task 5.3: 提取 ContextAssembler

**Files:**
- Create: `internal/runtime/context_assembler.go`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/runtime/run.go`

**Why:** context 装配 + memory prepare 是独立职责。

**Impact/Compatibility:** `RunnerFactory` 组合 `*ContextAssembler`。`prepareRunMemory` / `assembleContext` / `buildAssembly` 变为 `ContextAssembler` 方法。

**Verification:**
```bash
go test ./internal/runtime ./internal/app
make format-check
```

**Steps:**

- [ ] 1. 创建 `internal/runtime/context_assembler.go`:
  ```go
  type ContextAssembler struct {
      deps RuntimeDeps
  }

  func NewContextAssembler(deps RuntimeDeps) *ContextAssembler {
      return &ContextAssembler{deps: deps}
  }
  ```
  - 从 `runner.go` 迁移:`prepareRunMemory` / `assembleContext` / `buildAssembly` / `buildAssembleRequest` / `directResponseRequest` / `baseAssemblyFields` / `assembleRunCapabilitiesCatalog` / `buildRunCapabilities` / `workspaceSlug` / `emitRunMemoryEvents`
  - 从 `runner_emit.go` 迁移 memory event emission 相关函数到 `ContextAssembler` 方法(如果耦合紧密)或保留在 `RunEmitter`(Task 5.4)
  - `f.deps.X` → `a.deps.X`

- [ ] 2. 在 `runner.go` 中:
  - `RunnerFactory` struct 加 `contextAsm *ContextAssembler` 字段
  - `assembleRunnerFactory` 中初始化
  - `newDirectResponseRunner` 中调用改为 `f.contextAsm.prepareRunMemory(ctx, req)` / `f.contextAsm.assembleContext(...)` / `f.contextAsm.buildAssembly(...)`
  - 删除被迁移的方法定义

- [ ] 3. 更新 `executor_e2e_test.go`:初始化 `contextAsm` 字段。

- [ ] 4. 运行:
```bash
go build ./...
go test ./internal/runtime ./internal/app
make format-check
```

- [ ] 5. Commit: `refactor: extract ContextAssembler from RunnerFactory`

### Task 5.4: 提取 MCPAssembler, SkillSelector, RunEmitter

**Files:**
- Modify: `internal/runtime/runner_mcp.go` — 方法改为 `MCPAssembler` 方法
- Modify: `internal/runtime/runner_selection.go` — 方法改为 `SkillSelector` 方法
- Modify: `internal/runtime/runner_emit.go` — 方法改为 `RunEmitter` 方法
- Modify: `internal/runtime/runner.go`

**Why:** MCP 集成、skill 选择、event emission 是独立职责。

**Impact/Compatibility:** `RunnerFactory` 组合这三个 struct。`buildRun` pipeline 委托调用。

**Verification:**
```bash
go test ./internal/runtime ./internal/app
go test -race ./internal/runtime ./internal/app
make format-check && make lint
```

**Steps:**

- [ ] 1. 在 `runner_mcp.go` 中:
  ```go
  type MCPAssembler struct {
      deps RuntimeDeps
      mu             sync.Mutex
      cachedManager  *mcpprovider.Manager
      lastSessionOverlay string
  }

  func NewMCPAssembler(deps RuntimeDeps) *MCPAssembler {
      return &MCPAssembler{deps: deps}
  }
  ```
  - 所有 `func (f *RunnerFactory)` 方法改为 `func (m *MCPAssembler)` 方法
  - `f.deps.X` → `m.deps.X`
  - `f.mu` / `f.cachedManager` / `f.lastSessionOverlay` → `m.mu` / `m.cachedManager` / `m.lastSessionOverlay`

- [ ] 2. 在 `runner_selection.go` 中:
  ```go
  type SkillSelector struct {
      deps RuntimeDeps
  }

  func NewSkillSelector(deps RuntimeDeps) *SkillSelector {
      return &SkillSelector{deps: deps}
  }
  ```
  - 所有 `func (f *RunnerFactory)` 方法改为 `func (s *SkillSelector)` 方法
  - `f.deps.X` → `s.deps.X`

- [ ] 3. 在 `runner_emit.go` 中:
  ```go
  type RunEmitter struct {
      deps RuntimeDeps
  }

  func NewRunEmitter(deps RuntimeDeps) *RunEmitter {
      return &RunEmitter{deps: deps}
  }
  ```
  - 所有 `func (f *RunnerFactory)` 方法改为 `func (e *RunEmitter)` 方法
  - `f.deps.X` → `e.deps.X`

- [ ] 4. 在 `runner.go` 中:
  - `RunnerFactory` struct 加 `mcpAssembler *MCPAssembler` / `skillSelector *SkillSelector` / `emitter *RunEmitter` 字段
  - 删除 `mu` / `cachedManager` / `lastSessionOverlay` 字段(移到 MCPAssembler)
  - `assembleRunnerFactory` 中初始化全部 struct
  - `buildRunPrerequisites` 中调用 `f.mcpAssembler.bootstrapRunMCP(ctx, req)` 等
  - `newDirectResponseRunner` 中调用 `f.skillSelector.resolveRunSelection(ctx, req, caps)` 等
  - 删除被迁移的方法定义

- [ ] 5. 更新 `executor_e2e_test.go`:初始化所有新字段。因为 consume/finishCollectedRun 不触发这些路径,可用最小构造(nil 或空 struct)。

- [ ] 6. 更新 `direct_response_test.go`:如果有直接构造 `&RunnerFactory{}` 的测试,更新为构造新 struct。检查 `direct_response_test.go` 中 `ToolBuilder` 字段的引用——它在 `RuntimeDeps` 中,不需要改。

- [ ] 7. 运行全量验证:
```bash
go build ./...
go test ./internal/runtime ./internal/app
go test -race ./internal/runtime ./internal/app
make format-check && make lint
```

- [ ] 8. Commit: `refactor: extract MCPAssembler, SkillSelector, RunEmitter from RunnerFactory`

### Task 5.5: 简化 buildRun pipeline 为 thin coordinator

**Files:**
- Modify: `internal/runtime/run.go`
- Modify: `internal/runtime/runner.go`

**Why:** 5.1-5.4 提取了所有独立 struct,buildRun 现在可以简化为委托调用。

**Verification:**
```bash
go test ./internal/runtime ./internal/app
go test -race ./...
make format-check && make lint
```

**Steps:**

- [ ] 1. 在 `run.go` 中简化 `buildRun`:
  ```go
  func (f *RunnerFactory) buildRun(ctx context.Context, req RunnerBuildRequest) (active *ActiveRunner, err error) {
      if f == nil {
          return nil, errors.New("runner factory is not initialized")
      }
      cleanup, regErr := f.registerRunForBuild(req)
      if regErr != nil {
          return nil, regErr
      }
      var capabilities *runCapabilities
      defer func() {
          if err == nil { return }
          cleanup()
          if capabilities != nil { _ = capabilities.Close() }
      }()
      chatModel, capabilityAssembly, prereqErr := f.buildRunPrerequisites(ctx, req)
      if prereqErr != nil { return nil, prereqErr }
      capabilities = capabilityAssembly.capabilities
      active, err = f.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
      return active, err
  }
  ```
  确认 `buildRunPrerequisites` 和 `newDirectResponseRunner` 委托给正确的 struct:
  - `buildRunPrerequisites`: `f.modelBuilder.buildRunChatModel(ctx, req)` + `f.capabilityAsm.buildRunCapabilityAssembly(ctx, req)` + `f.mcpAssembler.bootstrapRunMCP(ctx, req)`
  - `newDirectResponseRunner`: `f.contextAsm.prepareRunMemory(ctx, req)` + `f.contextAsm.assembleContext(...)` + `f.toolAssembler.buildAssembly(...)` 或等价

- [ ] 2. 确认 `runner.go` 中 `RunnerFactory` struct 最终形态:
  ```go
  type RunnerFactory struct {
      modelBuilder      *ModelBuilder
      capabilityAsm     *CapabilityAssembler
      contextAsm        *ContextAssembler
      mcpAssembler      *MCPAssembler
      skillSelector     *SkillSelector
      emitter           *RunEmitter
      toolAssembler     *ToolAssembler

      deps       RuntimeDeps
      registry   *Registry
      currentRunID atomic.Value
  }
  ```

- [ ] 3. 运行全量验证:
```bash
go build ./...
go test ./...
go test -race ./...
make format-check && make lint
make test-architecture
```

- [ ] 4. Commit: `refactor: simplify RunnerFactory to thin coordinator`

### Task 5.6: 更新文档

**Files:**
- Modify: `docs/architecture/ARCHITECTURE.md`
- Modify: `docs/architecture/INVARIANTS.md`
- Modify: `AGENTS.md`

**Why:** 包结构变化后文档需同步。

**Steps:**

- [ ] 1. 更新 `ARCHITECTURE.md` 的包职责表:删除 toolkit / toolset,加 tools;更新 runtime 内部结构描述(7 个 struct)。

- [ ] 2. 更新 `INVARIANTS.md`:
  - 更新 store interface count 引用(6 → 4)
  - 更新结构守卫描述(toolkit/toolset → tools)
  - 更新 stream/domain 描述(payload 类型在 domain)

- [ ] 3. 更新 `AGENTS.md` 的关键包描述和验证要求。

- [ ] 4. Commit: `docs: sync architecture docs to modular refactor`
