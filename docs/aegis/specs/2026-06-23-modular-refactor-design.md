# Acorn 架构模块化重构设计

Date: 2026-06-23
Status: Proposed
Scope: Go 后端 `internal/` 包结构与职责拆分;产品形态、wire contract、SQLite schema、CLI 接口不变
Predecessor: `2026-06-23-radical-refactor-design.md`(其中 Phase 1-4/6/8/9 已由近期提交完成)

## 1. 上下文

前一轮 radical refactor(旧 spec)已通过 14 次提交完成:合并 orchestration/eventstream 进 runtime、rename memorymodule→memory / web→api / contextplane、合并 store/sqlite、内联 DefaultPlane strategy、删除 harness 自演化系统。当前工作区干净,`main` 分支。

本轮聚焦旧 spec 未解决的 **5 个结构性问题**——全部基于代码证据,不涉及产品功能变更。

## 2. 问题诊断(证据基线)

### 2.1 RunnerFactory 是 god-object(5 个文件,47 方法)

**证据**:`RunnerFactory` struct(`runner.go:33-44`)持有 `RuntimeDeps`(16 字段)。47 个方法分散在 5 个文件:

| 文件 | 行数 | 职责 |
|---|---|---|
| `runner.go` | 621 | struct 定义 + model 构建 + capability 装配 + context 装配 + tool lifecycle + checkpoint store + chat model 工厂 |
| `runner_selection.go` | 267 | skill 选择 + eligibility + capability discovery instruction |
| `runner_toolset.go` | 293 | toolset 构建 + local catalog + aux tools + web services + browser service + memory tools |
| `runner_emit.go` | 199 | run event emission + memory events |
| `runner_mcp.go` | 230 | MCP tool specs + MCP registration specs + MCP bootstrap + MCP manager lifecycle |

`buildRun` 调用链(`run.go:21-46`)串了 7 个步骤:
```
registerRunForBuild → buildRunPrerequisites(buildRunChatModel + buildRunCapabilityAssembly)
→ newDirectResponseRunner(prepareRunMemory + assembleContext + buildAssembly → buildDirectResponse → assembleTooling)
```

这 7 个步骤是独立的 pipeline stage,但全部挂在同一个 struct 上,没有职责边界。

**被引用位置**(非 runtime 包内):
- `internal/app/container_runtime.go` — `*runtime.RunnerFactory` 作为字段 + 构造
- `internal/app/capability_service.go:125` — `catalogBuilder *runtime.RunnerFactory`
- `internal/app/client_service_test.go:606` — 测试构造
- `internal/runtime/executor.go` — `runRuntime *RunnerFactory` 字段
- `internal/runtime/executor_e2e_test.go:124` — 测试构造 `&RunnerFactory{}`

### 2.2 app 包职责过载(3424 行 / 12 文件)

Container 是组合根(正确),但 `client_service.go`(346 行)同时管 5 个不相关职责:

| 方法组 | 行范围 | 职责 |
|---|---|---|
| `GetRun` / `RunIsTerminal` / `InterruptRun` | 53-92 | run 状态查询 + 中断 |
| `CreateRun` / `executeRun` / `reportBackgroundRunFailure` / `recordStartedRunFailure` | 94-266 | run 创建 + 执行 + 失败处理 |
| `LoadRunEventsAfter` / `LoadRunEventsForDetail` | 267-325 | event 加载 + 投影 |
| `EventPollInterval` | 326 | 配置 |
| (client_queries.go) `ListThreads` / `CreateThread` / `GetThread` / `UpdateThread` / `DeleteThread` / `ListMessages` / `CreateMessage` | — | thread + message CRUD |

`Container` 直接持有 `*runtime.RunnerFactory`、`*store.Store`、`*runtime.RunController` 具体类型——组合根与运行时强耦合。

### 2.3 toolkit vs toolset 命名混淆 + 双向耦合

**证据**:
- `toolkit`(597 行)= 契约层:ToolContract / Catalog / ToolSpec / ToolLoadingPolicy / ToolExecutionPolicy / ProgressTool / EligibilityContext
- `toolset`(4536 行)= 实现层:file/git/browser/web/command/artifact 工具 + ports(WorkspaceView / ArtifactService / WebFetchService / BrowserService / OperatorQuestionStore)
- `toolset` import `toolkit` **47 次**(`ToolProgressEmitter`、`ProgressTool`、`ToolSpec` 等)
- `toolkit` 不直接 import `toolset`,但注释中提到 "toolset" 3 次
- 两个包名几乎一样,边界靠脑记

### 2.4 consumer-owned store 接口分散(6 个,有重叠)

| 接口 | 定义位置 | 消费者 | 方法数 | 重叠 |
|---|---|---|---|---|
| `ExecutorStore` | runtime/types.go | Executor | 12 | — |
| `RunnerFactoryStore` | runtime/types.go | RunnerFactory | = ExecutorStore + TokenStore + PendingActionStore | extends ExecutorStore |
| `containerRuntimeStore` | app/container_runtime.go | Container runtime wiring | = RunnerFactoryStore + SessionSummaryStore + PendingActionCreateStore | extends RunnerFactoryStore |
| `containerAppStore` | app/container_runtime.go | 8 个 app service | **30** | mega-interface |
| `PendingActionCreateStore` | app/device_auth_service.go | DeviceAuthService | 6 | 与 containerAppStore 重叠 5 个方法 |
| `skillSnapshotStore` | app/capability_service.go | CapabilitiesService | 1 | — |

`containerAppStore` 是 30 方法的 mega-interface,被 `ClientService`、`PendingActionService`、`DeviceAuthService`、`RunResumeService`、`InboxService` 共享——每个只用其中一部分,违反 ISP。

`PendingActionCreateStore` 的 6 个方法中有 5 个与 `containerAppStore` 完全相同——重复定义。

### 2.5 stream / domain 类型分散

**证据**:
- `domain/domain.go:244-269` 定义 `StreamItemKind`(23 个常量)+ `StreamItem` struct + `StreamSink` + `EventAppender`
- `stream/types.go` 定义 15 个 payload struct(`StreamMessage` / `StreamToolCall` / `StreamInterrupt` / `StreamSkill` / `StreamAssistantDelta` ...)
- `stream/accessors.go` 提供 `ItemGet*` 访问器,操作 `domain.StreamItem` 的 payload
- `stream` 包只被 `runtime` 包引用(8 个文件)
- 两个包共同管"流式数据"这一概念,但类型定义分散在两个包

## 3. 设计原则

1. **职责单一**:每个 struct / package 有一个清晰职责,包名即职责
2. **pipeline 而非 god-object**:per-run assembly 是 pipeline,每个 stage 是独立 struct
3. **依赖单向**:依赖指向更底层/更稳定的包,无环
4. **Hard cutover**:不保留 compat alias / shim / 旧路径
5. **wire contract 不变**:`/v1` endpoint + OpenAPI schema 不动,mobile 不受影响
6. **架构测试同步更新**:重构后守卫反映新边界

## 4. 重构动作

### Step 1: stream/domain 类型收敛(最低风险,先清场)

**问题**:`StreamItem` + `StreamItemKind` 在 `domain`,`stream` 包有 15 个 payload struct + 投影逻辑。两个包共同管流式数据。

**做法**:`stream` 包的 15 个 payload struct(`StreamMessage` / `StreamToolCall` / `StreamInterrupt` / `StreamSkill` / `StreamAssistantDelta` 等)移入 `domain`,与 `StreamItem` / `StreamItemKind` 同包。`stream` 包保留投影逻辑(`AppendStreamItem` / `ProjectStreamItemToEvent` / `StreamItemsFromAgentEvent`)和 assistant streaming 逻辑。

**改动文件**:
- `internal/stream/types.go` → payload struct 移到 `internal/domain/stream_types.go`
- `internal/stream/accessors.go` → 移到 `internal/domain/stream_accessors.go`(操作 domain 类型,归属 domain)
- `internal/stream/agent.go` / `projection.go` / `streaming_assistant.go` → 保留在 `stream`(投影 + streaming 逻辑)
- 更新所有 import(`stream.StreamMessage` → `domain.StreamMessage`、`stream.ItemGetMessage` → `domain.ItemGetMessage`)

**验证**:`go test ./internal/domain ./internal/stream ./internal/runtime ./internal/app`

### Step 2: 合并 toolkit + toolset → tools(机械操作,消除命名混淆)

**问题**:两个几乎同名的包,`toolset` import `toolkit` 47 次。

**做法**:合并为 `internal/tools/`。内部分层:
```
internal/tools/
  contract.go        — ToolContract / Catalog / ToolSpec / policies / ProgressTool (原 toolkit/contracts.go + catalog.go + specs.go)
  builtin_registry.go — BuiltinToolSpec / BuiltinToolNames (原 toolkit/builtin_registry.go)
  eligibility.go     — EligibilityContext (原 toolkit/skills.go)
  ports.go            — WorkspaceView / ArtifactService / WebFetchService / BrowserService / OperatorQuestionStore (原 toolset/ports.go)
  file_read.go        — ReadFile / ListFiles / SearchText (原 native_read_tools.go + native_search_tools.go)
  file_mutate.go      — CreateFile / ReplaceSpan / ApplyUnifiedPatch / MultiEdit / RollbackCheckpoint (原 native_mutation_tools.go + workflow_tools.go)
  git.go              — InspectGitStatus / InspectGitDiff / GitSummary (原 toolset/tools.go git 部分)
  command.go          — RunCommand (原 command_tool.go + processgroup_*.go)
  browser_service.go  — Browser Service (原 browser_service.go + browser_service_navigate.go + browser_service_events.go)
  browser_tool.go     — Browser tool (原 browser_tool.go)
  web.go              — WebSearch / WebFetch (原 web_search_tool.go + web_fetch_tool.go)
  artifact.go         — Artifact tools (原 artifact_tools.go)
  operator.go         — AskOperator / progress (原 operator_question_tool.go + progress_tool.go)
  catalog_builders.go — BuildCatalog / CatalogConfig (原 toolset/tools.go + catalog_builders.go)
```

**改动**:全量 rename `internal/toolkit` → `internal/tools`,rename `internal/toolset` → `internal/tools`,合并同名文件。更新所有 import path。

**风险**:browser 相关文件(browser_service.go 626行 + browser_service_navigate.go 280行 + browser_service_events.go 173行 = 1079 行)合并后超 800 行守卫。方案:合并后如超限,按 navigation/events 拆为 `browser_service.go`(核心+状态) + `browser_service_actions.go`(navigate+events)。

**验证**:`go build ./...` + `go test ./internal/tools ./internal/runtime` + `make lint`

### Step 3: 收敛 store 接口(消除重叠)

**问题**:6 个 consumer-owned store 接口,`PendingActionCreateStore` 与 `containerAppStore` 有 5 个方法重叠,`containerAppStore` 是 30 方法的 mega-interface。

**做法**:
1. 删除 `PendingActionCreateStore`(app/device_auth_service.go):`DeviceAuthService` 改为依赖 `containerAppStore`(它已包含全部所需方法)。消除重复定义。
2. `skillSnapshotStore`(1 方法)内联为直接 struct 依赖:`CapabilitiesService` 直接持有 `*skills.Loader` 或一个 `func(ctx) (*skills.Snapshot, error)` 闭包。
3. `containerAppStore` 保持 mega-interface——它在组合根中,是 Store 的统一视图。拆分它会产生更多小接口,反而增加复杂度。接受宽接口 + 明确注释"这是组合根 store 视图,非 ISP 违规"。
4. `ExecutorStore` + `RunnerFactoryStore` 保持现状——`RunnerFactoryStore` extends `ExecutorStore` 是合理的接口组合,不同消费者定义不同宽度的接口。

**改动**:
- 删除 `internal/app/device_auth_service.go:20-26` 的 `PendingActionCreateStore` interface
- `DeviceAuthService` 的 `store` 字段类型从 `PendingActionCreateStore` 改为 `containerAppStore`
- 删除 `internal/app/container_runtime.go` 中 `PendingActionCreateStore` 从 `containerRuntimeStore` 的 embed(改用 `containerAppStore` 的方法)
- 删除 `internal/app/capability_service.go:115-117` 的 `skillSnapshotStore` interface,内联依赖
- 更新 `store_interface_count_test.go`:`maxConsumerStoreInterfaces` 从 6 降到 4

**验证**:`go test ./internal/app ./tests/architecture`

### Step 4: 拆 app client_service → 按领域分 service(中等风险)

**问题**:`client_service.go`(346 行)同时管 thread CRUD + run 创建 + event 加载 + artifact 列表 + run 中断。

**做法**:拆为 3 个 service,共享 `containerAppStore`:

```
internal/app/
  thread_service.go    — ListThreads / CreateThread / GetThread / UpdateThread / DeleteThread
                       + ListMessages / CreateMessage + thread/message projection
                       (原 client_queries.go + client_helpers.go 的 thread/message 部分)
  run_service.go      — CreateRun / GetRun / RunIsTerminal / InterruptRun
                       + executeRun / reportBackgroundRunFailure / recordStartedRunFailure
                       (原 client_service.go 的 run 管理部分)
  event_service.go    — LoadRunEventsAfter / LoadRunEventsForDetail / ListRunArtifacts / EventPollInterval
                       (原 client_service.go 的 event 部分 + client_helpers.go 的 artifact 部分)
```

`api/server.go` 的 `ClientService` interface 拆为 3 个对应接口:`ThreadService` / `RunService` / `EventService`。`Server` struct 持有 3 个依赖。`Dependencies` struct 对应增加。

**改动文件**:
- 新建 `internal/app/thread_service.go` / `run_service.go` / `event_service.go`
- 删除 `internal/app/client_service.go` / `client_helpers.go` / `client_queries.go`
- 更新 `internal/api/server.go` — 拆 `ClientService` interface
- 更新 `internal/app/container.go` — `Container` 持有 3 个 service
- 更新 `internal/app/container_runtime.go` — 构造 3 个 service
- 更新 `internal/api/handlers_*.go` — 使用新 service 名
- 更新所有 `*_test.go`

**验证**:`go test ./internal/app ./internal/api ./tests/architecture`

### Step 5: 拆 RunnerFactory god-object(核心,最高风险)

**问题**:`RunnerFactory` 有 47 个方法,混合 model 构建 + capability 装配 + context 装配 + tool 装配 + MCP 集成 + event emission + skill selection。

**做法**:拆为 7 个独立 struct + thin coordinator。不动包边界(都在 `internal/runtime/`),只拆 struct:

```
internal/runtime/
  runner.go              — RunnerFactory(thin coordinator:buildRun pipeline + Registry + RunController)
  model_builder.go       — ModelBuilder:buildRunChatModel / newChatModel / buildRuntimeChatModel / newOpenAIChatModel
  capability_assembler.go — CapabilityAssembler:buildRunCapabilities / assembleRunCapabilitiesCatalog
                           + buildRunToolset / buildToolset / buildLocalToolset / buildAuxTools
                           + buildToolsetWebServices / buildBrowserService / resolveOperatorStore
                           + buildLocalCatalog / buildMemoryTools
                           (合并 runner_toolset.go + runner.go 的 capability 部分)
  context_assembler.go   — ContextAssembler:prepareRunMemory / assembleContext / buildAssembly
                           + buildAssembleRequest / emitRunMemoryEvents
                           (合并 runner.go 的 context 部分)
  runner_mcp.go           — MCPAssembler:buildMCPToolSpecs / buildMCPRegistrationsSpecs
                           + bootstrapRunMCP / MCP manager lifecycle
                           (保留现有文件,改为独立 struct)
  runner_selection.go     — SkillSelector:resolveRunSelection / resolveRunSelectionByDecision
                           + blockRun / resolveExplicitSkill / skillEligibilityContext
                           (保留现有文件,改为独立 struct)
  runner_emit.go          — RunEmitter:emitRunMemoryEvents + run event emission
                           (保留现有文件,改为独立 struct)
  direct_response.go      — ToolAssembler:assembleTooling / buildDirectResponse
                           (保留现有位置,assembleTooling 改为 ToolAssembler struct 方法)
```

`RunnerFactory` 变为 thin coordinator:
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
    // ...
}

func (f *RunnerFactory) buildRun(ctx, req) (*ActiveRunner, error) {
    cleanup := f.registerRunForBuild(req)
    chatModel, caps := f.modelBuilder.build(ctx, req)
    memPrepared := f.contextAsm.prepareMemory(ctx, req)
    contextResult := f.contextAsm.assemble(ctx, req, caps, nil, memPrepared)
    assembly := f.toolAssembler.build(ctx, req, caps.catalog, chatModel, contextResult)
    return &ActiveRunner{...}, nil
}
```

**关键约束**:
- `ActiveRunner` struct 不变(`executor.go` 依赖它)
- `RunnerBuildRequest` / `RunnerFactoryOptions` / `RuntimeDeps` 类型不变
- `NewRunnerFactory` 签名不变(外部调用者 `app/container_runtime.go` 不需要改)
- `BuildCapabilitySpecs` 留在 `RunnerFactory`(委托给 `capabilityAsm`)
- `Registry()` / `Config()` / `MemoryModule()` / `SessionSummarySvc()` / `NewChatModel()` 留在 `RunnerFactory`(thin accessor)
- 测试中直接构造 `&RunnerFactory{}` 的地方(`executor_e2e_test.go:125`)需要更新为构造新 struct

**改动文件**:
- `internal/runtime/runner.go` — 缩减为 thin coordinator + struct 定义
- 新建 `internal/runtime/model_builder.go` / `capability_assembler.go` / `context_assembler.go`
- `internal/runtime/runner_mcp.go` / `runner_selection.go` / `runner_emit.go` — 改为独立 struct
- `internal/runtime/direct_response.go` — `assembleTooling` 改为 `ToolAssembler` struct 方法
- `internal/runtime/executor.go` — 不变(通过 `*RunnerFactory` 调用)
- `internal/runtime/run.go` — `buildRun` 简化为委托调用
- `internal/app/container_runtime.go` — 不变(`NewRunnerFactory` 签名不变)
- `internal/app/capability_service.go` — 不变(通过 `*RunnerFactory` 调用 `BuildCapabilitySpecs`)
- 更新 `internal/runtime/*_test.go` / `internal/app/*_test.go`

**验证**:
- `go test ./internal/runtime ./internal/app`
- `go test -race ./internal/runtime ./internal/app`
- `make lint && make format-check`

## 5. 不变量(wire contract 不变)

以下在重构中**必须保持不变**:
- `docs/openapi.yaml` 不改一个字
- `/v1` endpoint 路径、request/response shape 不变
- `mobile-kotlin/` 不改一行
- SQLite schema(12 张表)不变
- CLI 命令(`serve`/`run`/`smoke`/`init`/`pair`/`devices`/`token`/`skills`/`memory`/`doctor`)不变
- 配置文件格式(`configs/*.yaml`)不变
- `make build` / `make serve` / `make test` / `make lint` / `make format-check` 命令不变
- `direct_response` 单一编排模式不变
- hybrid context(masking + auto-compact)方案不变
- file-backed memory + 语义检索方案不变
- device auth(pairing code → bearer token)不变

## 6. 迁移策略:增量重构

每步是一个独立 commit,每步完成后:
```bash
go build ./...
go test ./...
make format-check && make lint
make test-architecture  # Step 2/3 后需要更新守卫
```

执行顺序(按风险从低到高):
1. **Step 1**(stream/domain 类型收敛)— 最小改动,先验证 pipeline
2. **Step 2**(合并 toolkit+toolset)— 机械 rename,工作量大但无逻辑变化
3. **Step 3**(收敛 store 接口)— 删重复接口 + 更新守卫
4. **Step 4**(拆 app client_service)— service 拆分
5. **Step 5**(拆 RunnerFactory)— 核心重构,最高风险

可随时在任一步后停止,代码保持可编译可测试。

## 7. 架构测试更新

Step 2 后:
- `structural_limits_test.go`:`refactorOwnedDirs` 删除 `internal/toolkit` / `internal/toolset`,加 `internal/tools`

Step 3 后:
- `store_interface_count_test.go`:`maxConsumerStoreInterfaces` 从 6 降到 4

Step 4 后:
- `client_projection_boundary_test.go`:更新 `clientProjectionBoundaryFiles` 路径(如果有文件 rename)

Step 5 后:
- 无守卫变化(struct 在包内拆分,包边界不变)

## 8. 风险

- **[RISK-001]** Step 2(合并 toolkit+toolset)涉及全量 import rename,可能遗漏 → `go build ./...` + `goimports` 兜底
- **[RISK-002]** Step 5(拆 RunnerFactory)中 `executor_e2e_test.go` 直接构造 `&RunnerFactory{}`,需要更新测试 fixture → 测试会失败暴露,不会漏
- **[RISK-003]** browser 相关文件合并后可能超 800 行守卫 → 按需拆为 service + actions 两个文件
- **[RISK-004]** Step 4 拆 client_service 后 `api/server.go` 的 `Dependencies` struct 变化,需要同步更新 `container.go` 的 `Server` 构造 → 编译器强制暴露

## 9. 非 Goals

- 不改 OpenAPI wire contract
- 不改 mobile-kotlin
- 不改 SQLite schema
- 不改 CLI 命令接口
- 不改配置文件格式
- 不重写工具实现逻辑(只移动文件边界)
- 不改 runtime 执行流程(direct_response 逻辑不变)
- 不改 MCP provider 集成逻辑
- 不新增包层级(所有拆分在现有包内)
