# Acorn 彻底简化重构 — Design Spec

Date: `2026-06-25`
Status: `implemented (partial — Wave 1 + Phase 9 executed; Wave 2 runtime subpackages and Phase 8 API interface purge evaluated and skipped; see plan Self-Review for rationale)`
Complexity: `high — 核心层纯化 + god-package 拆分 + 接口精简 + 子包隔离`
Predecessor: `2026-06-24-convergent-core-runtime-refactor-design.md` (已 merge,解决了类型切片问题但留下了新的结构性债务)

## 1. Problem Statement

上一轮 convergent refactor 把 20 包合并到 13 包、21 个 store 接口收敛到 3 个、统一了 plugin registry。方向正确,但「合并」做完了,「精简」没做。当前状态是**合并后的臃肿**:core 不纯、runtime 是 god-package、API 层接口膨胀、tools 没有子包边界。

### 1.1 证据基线(merge `c84fb56` 后)

| 维度 | 当前值 | 目标值 | 证据 |
|---|---|---|---|
| core 包含 service struct | 1 个(`SessionSummaryService`) | 0 | `internal/core/store_types.go:128` — Layer 0 混入业务逻辑 |
| 重复 ArtifactStore 接口 | 2 套(`core.ArtifactStore` 8 方法 vs `store.ArtifactStore` 4 方法,签名不兼容) | 1 套 | `internal/core/store.go:69` vs `internal/store/artifacts.go:33` |
| runtime 导出符号 | 191 个(5858 LOC, 24 文件) | ~100(拆 3 子包后) | `internal/runtime/` — 最大 god-package |
| runner.go 函数数 | 44 个函数(578 行) | ~15 | `internal/runtime/runner.go` — RunnerFactory 承担 7 职责 |
| API 层 ServiceAPI 接口 | 11 个(全单实现) | 0(直接用具体类型) | `internal/api/server.go:14-71` — 每个接口只有 wire 注入的 1 个实现 |
| tools dispatch 文件 | 5 个 dispatch_*.go(894 LOC) | 独立子包 | `internal/tools/dispatch_*.go` — 调度逻辑和工具实现混在一起 |
| 死代码 | 6 处 | 0 | deadcode: `DefaultConfig`/`GetCallSite`/`Toolset.All`/`OnToolResult`/`WithToolLifecycleContext`/`MustInferTool` |
| 测试覆盖率 | core 27.3%, runtime 38.9% | ≥40% | `go test -cover` — 接口膨胀未转化为测试隔离价值 |

### 1.2 上一轮为什么没解决这些问题

| 问题 | convergent refactor 做了什么 | 遗留 |
|---|---|---|
| core 混入 service | 把 domain 类型移入 core | `SessionSummaryService` 跟着 `SessionSummary` 一起被搬进 core,没人发现它是 service 不是 type |
| 重复 store 接口 | core 定义 3 个 Store 接口 | store 包的 `ArtifactStore` 是 `ArtifactService` 的依赖接口,没被统一 |
| runtime god-package | 合并 agent+context+stream → runtime | 合并方向对,但没做二次拆分。5858 LOC 的新 god-package 替代了 3 个小 god-package |
| API 接口膨胀 | 合并 clientevents → api | 把 11 个 ServiceAPI 接口原样搬过来,没精简 |
| tools dispatch 混杂 | tools 包保留 | dispatch 调度逻辑从没被独立过 |

### 1.3 本次方案的根本区别

**做精简,不做合并。** 上一轮是「合并碎片包」,这一轮是「拆臃肿包 + 删冗余抽象」。不是回到拆碎片,而是按职责拆 god-package 为有边界的子包;不是删接口,而是删只有单实现的假解耦接口。

## 2. Goal

1. **core 纯化为 Layer 0**:移出所有 service struct,只保留类型 + 契约接口 + context plumbing。`SessionSummaryService` 下沉到 `runtime`。
2. **统一 store 接口**:删除 `store.ArtifactStore` 重复接口,`ArtifactService` 直接依赖 `core.ArtifactStore`。消除签名不兼容的两套接口。
3. **拆 runtime god-package**:runtime(5858 LOC)按职责拆为核心 + 2 个子包(`runner` 装配 + `context` 上下文管理)。每个包 <2000 LOC、<60 exported symbols。
4. **精简 API 接口**:删除 11 个单实现 `XxxServiceAPI` 接口,`Dependencies` 和 `Server` 直接持有具体 service 指针。
5. **隔离 tools dispatch**:dispatch 调度逻辑提取为 `tools/dispatch` 子包,tools 根包只保留工具实现。
6. **删除死代码**:6 处 unreachable 函数全部删除。

## 3. Non-Goals

- 不改产品行为:direct_response 唯一编排模式、mobile control surface、CLI 命令接口
- 不改 SQLite schema(10 表不变)
- 不改 OpenAPI wire contract(`docs/openapi.yaml` 不动,mobile-kotlin 不动)
- 不改 hybrid context 三机制本身(masking + auto-compact + circuit breaker 逻辑不变,只改所属包)
- 不改 file-backed memory 模型
- 不改 embedding 惰性接线策略
- 不引入新外部依赖
- 不拆 runtime 为 >3 个子包(避免回到碎片化)
- 不删 tools 包内部的 service 接口(`WorkspaceView`/`WebFetchService` 等,它们是 tools 的依赖注入接口,属于实现细节)

## 4. Architecture: 纯化 + 子包化

### 4.1 目标包结构

```
internal/
  core/          ← 纯 Layer 0:类型 + 契约接口 + context plumbing(零 service)
  runtime/       ← 执行引擎核心:Executor + direct_response + Session + Plane + projection
    runner/      ← 装配子包:RunnerFactory + ActiveRunner + capability/skill/memory assembly
    context/     ← 上下文子包:masking + auto_compact + memory_context + context_assembler
  store/         ← SQLite adapter + ArtifactService(依赖 core.ArtifactStore,不再有重复接口)
  tools/         ← 工具实现:file/browser/web/command/artifact
    dispatch/    ← 调度子包:scheduler + node + streaming + side_effects
  memory/        ← file-backed memory + semantic search(不变)
  api/           ← HTTP handlers + DTO + device auth + RunEvent 投影(无假接口)
  mcp/           ← MCP provider manager(不变)
  wire/          ← 组合根(不变)
  config/        ← 配置(不变)
  workspace/     ← workspace + checkpoint + worktree(不变)
  webaccess/     ← web_search/web_fetch/browser 共享(不变)
  skills/        ← skill file loader(不变)
  cli/           ← CLI 入口(不变)
```

**包数变化**:13 → 15(+`runtime/runner` +`runtime/context` +`tools/dispatch`,但都是子包,不是新顶级包)。核心变化在 runtime 和 tools 内部分包。

### 4.2 依赖规则(不变,更严格)

```
Layer 0:     core(零 internal 依赖,纯类型+接口)
Layer 2:     store, memory, tools, tools/dispatch, mcp, workspace, webaccess, skills, config
Layer 2.5:   runtime/context, runtime/runner(依赖 core + L2)
Layer 3:     runtime(依赖 core + L2 + runtime 子包)
Layer 4:     api(依赖 core + runtime)
Layer 5:     wire(依赖所有)
Layer 6:     cli(依赖 wire, api, config)
```

**新增约束**:
- `runtime/runner` 和 `runtime/context` 只 import `core` + Layer 2 包,不 import `runtime` 根包(避免环)
- `runtime` 根包 import `runtime/runner` 和 `runtime/context`,不反向
- `tools/dispatch` 只 import `core` + `tools` 类型,不 import 工具实现文件

### 4.3 依赖图(无环)

```
                    core  ← 所有人依赖
                      ↑
    ┌──────┬──────┬───┴───┬──────┬──────┐
    ↑      ↑      ↑       ↑      ↑      ↑
  store  memory  tools   mcp  workspace  skills
           │      │  ↑
           │      │  └─dispatch/      (tools 子包)
           │      │
    ┌──────┴──────┘
    ↑      ↑
 runner/  context/     (runtime 子包,依赖 core + L2)
    ↑      ↑
    └──┬───┘
       ↑
    runtime               (执行引擎,组合 runner + context)
       ↑
     api                   (HTTP + 投影)
       ↑
     wire                  (组合根)
       ↑
     cli                   (入口)
```

## 5. Key Design Decisions

### 5.1 core 纯化:移出 SessionSummaryService

**现状**:`core/store_types.go:128-178` 定义了 `SessionSummaryService` struct + `NewSessionSummaryService` 构造函数 + `Get`/`Upsert` 方法。这是一个有状态、有依赖的业务 service,不是 domain type。

**目标**:把 `SessionSummaryService` 移到 `runtime` 包(它的唯一消费者是 `Executor`)。core 只保留 `SessionSummary` 类型 + `SessionSummaryStore` 接口。

**移动内容**:
```go
// 从 internal/core/store_types.go 删除:
// - type SessionSummaryService struct
// - func NewSessionSummaryService(...)
// - func (s *SessionSummaryService) Get(...)
// - func (s *SessionSummaryService) Upsert(...)

// 移到 internal/runtime/session_summary.go:
package runtime

import "github.com/ycvk/acorn/internal/core"

type SessionSummaryService struct {
    store    core.SessionSummaryStore
    maxChars int
}

func NewSessionSummaryService(store core.SessionSummaryStore, maxChars int) *SessionSummaryService {
    // ... 原逻辑不变
}
```

**core 保留**:`SessionSummary` struct + `SessionSummaryStore` interface(纯类型+契约)。

**影响**:`wire/runtime.go:39` 的 `core.NewSessionSummaryService` → `runtime.NewSessionSummaryService`。`runtime/types.go:66` 的 `SessionSummarySvc *core.SessionSummaryService` → `*runtime.SessionSummaryService`(同包引用)。

### 5.2 统一 store 接口:删除 store.ArtifactStore

**现状**:两套 `ArtifactStore` 接口并存,签名不兼容:
- `core.ArtifactStore`(8 方法):`WriteArtifact`/`ReadArtifactRange`/`ListArtifactsByRun`/`ListArtifactsBySession`/`GetSessionSummary`/`UpsertSessionSummary`/`SaveOAuthToken`/`GetOAuthToken`
- `store.ArtifactStore`(4 方法):`SaveArtifact`/`LoadArtifact`/`ListArtifactsByRun`/`ListArtifactsBySession`

`store.ArtifactService` 依赖 `store.ArtifactStore`(4 方法版),而 `Store` struct 实现了 `core.ArtifactStore`(8 方法版)。`ArtifactService` 通过自己的窄接口访问 `Store`,绕过了 core 契约。

**目标**:删除 `store.ArtifactStore` 接口。`ArtifactService` 直接依赖 `core.ArtifactStore` 的 artifact 子集方法。因为 `core.ArtifactStore` 包含 artifact + summary + OAuth 三个域,`ArtifactService` 只需要 artifact 域,所以从 `core.ArtifactStore` 提取 artifact-only facet:

```go
// core/artifact_service.go — 已有的 ArtifactService 接口
type ArtifactService interface {
    WriteArtifact(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error)
    ReadArtifactRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error)
    ListArtifactsByRun(ctx context.Context, runID string) ([]ArtifactRecord, error)
    ListArtifactsBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error)
}
```

**变更**:`store.ArtifactService` 的 `store` 字段类型从 `store.ArtifactStore` 改为 `core.ArtifactService`。`Store` struct 已经满足 `core.ArtifactService`(它实现了 `core.ArtifactStore` 的超集)。删除 `store/artifacts.go:33-38` 的 `ArtifactStore` interface 定义。

**compile-time 验证**:在 `store/artifacts.go` 加 `var _ core.ArtifactService = (*Store)(nil)` 确保签名兼容。

### 5.3 拆 runtime god-package

**现状**:runtime 5858 LOC / 24 文件 / 191 exported symbols / 284 函数。`runner.go` 单文件 578 行 44 函数,`executor.go` 505 行 35 函数。

**目标**:按职责拆为 runtime 根 + 2 子包:

```
internal/runtime/
  executor.go          Run lifecycle + ExecuteMessages + consume + finalize (505→~400 LOC)
  direct_response.go   direct_response agent + ExecuteRound (382 LOC, 不变)
  session.go            Session interface + defaultContextSession (361 LOC, 不变)
  plane.go              Plane interface + DefaultPlane (200 LOC, 不变)
  projection.go         StreamItem projection (143 LOC, 不变)
  assistant_stream.go   assistant streaming (276 LOC, 不变)
  streaming_assistant.go streaming assistant (187 LOC, 不变)
  audit.go              tool audit (143 LOC, 不变)
  validator.go          tool validation (168 LOC, 不变)
  catalog.go            catalog helpers (142 LOC, 不变)
  types.go              RuntimeStore + RuntimeDeps + core types (179 LOC, 精简)
  run_context.go        RunContext + RunController (138 LOC, 不变)
  agent_loop.go         ExecuteRound loop (176 LOC, 不变)
  
  runner/               ← 装配子包(~1800 LOC)
    runner.go           RunnerFactory + ActiveRunner + buildRun (578 LOC)
    runner_mcp.go       MCP reconcile (243 LOC)
    capability_assembler.go capability assembly (380 LOC)
    model_builder.go    chat model construction (101 LOC)
    context_assembler.go context plane assembly (136 LOC)
    tool_lifecycle.go   tool lifecycle binding (417 LOC)
    types.go            RunnerFactoryOptions + RunnerBuildRequest + assembly types
  
  context/              ← 上下文子包(~700 LOC)
    masking.go          observation masking (60 LOC)
    auto_compact.go     LLM auto-compact + circuit breaker (100 LOC)
    memory_context.go   memory context injection (250 LOC)
    context_helpers.go  context assembly helpers (193 LOC)
```

**子包边界**:
- `runtime/runner`:「如何装配一次 run」——RunnerFactory、capability/skill/memory/tool assembly。依赖 core + tools + mcp + memory + skills + workspace。
- `runtime/context`:「如何管理上下文窗口」——masking、auto-compact、memory context injection。依赖 core + memory。
- `runtime` 根:「如何执行 run」——Executor、direct_response、Session、Plane、projection。依赖 core + runner + context。

**环依赖处理**:runtime 根包 import runner + context 子包,子包不 import runtime 根包。`Executor` 持有 `*runner.RunnerFactory` 而非 `*RunnerFactory`。`Session` 接口留在 runtime 根包(因为 executor 和 direct_response 直接依赖它),`defaultContextSession` 实现也留在根包。

**runner.go 拆分**(44 函数 → ~15 公开):
- 保留在 `runner/runner.go`:`RunnerFactory` struct + `New`/`buildRun`/`Close`/`ReconcileMCPProviders`/`BuildCapabilitySpecs`/`Registry`/`Config`/`MemoryModule`/`SessionSummarySvc`
- 移到 `runner/runner_mcp.go`:`ReconcileMCPProviders` + MCP 相关辅助(已有)
- 移到 `runner/capability_assembler.go`:`buildRunCapabilityAssembly` + `capabilityAssembly`(已有)
- 删除:`setCurrentRunID`/`ClearCurrentRunID`/`currentRunIDValue`(用 `atomic.Value` 直接在 `New`/`buildRun` 中操作,不需要单独方法)、`inMemoryCheckpointStore`(移到 runner 子包独立文件)、`AssembleResultToView`(0 外部引用,删除)、`streamMemoryNudges`/`streamMemoryEntries`/`streamSkillRequirementsFromDomain`(投影辅助,移到 `runtime/projection.go`)

### 5.4 精简 API 接口:删除 11 个单实现 ServiceAPI

**现状**:`api/server.go` 定义 11 个 `XxxServiceAPI` 接口,`Dependencies` struct 持有 11 个接口字段,`Server` struct 持有 11 个接口字段。每个接口只有 `wire.Container` 注入的一个具体 service 实现。

**判断依据**:这些接口不是跨包契约(只有 wire → api 单向依赖),不是测试替身(测试覆盖率 27% 说明接口没在帮测试),不是多实现(每个只有 1 个实现)。它们是「为了解耦而解耦」的冗余抽象。

**目标**:删除 11 个接口,`Dependencies` 和 `Server` 直接持有具体 service 指针:

```go
// internal/api/server.go — 精简后
type Dependencies struct {
    Threads       *ThreadService
    Runs          *RunService
    Events        *EventService
    PendingAction *PendingActionService
    RunResume     *RunResumeService
    Memory        memory.Service
    Skills        *SkillService
    Capabilities  *CapabilitiesService
    DeviceAuth    *DeviceAuthService
    Inbox         *InboxService
    Logger        *slog.Logger
    Config        *config.Config
}

type Server struct {
    threads       *ThreadService
    runs          *RunService
    // ... 同上
    logger        *slog.Logger
    cfg           *config.Config
}
```

**保留的接口**:
- `memory.Service`:memory 包自己定义的接口,多消费者(api + runtime),保留
- `api.StoreView`:wire 定义的 store 窄视图,保留(但有 .go:11 行,评估是否需要——见 5.6)
- `api.ExecutorHandle` + `RunStartObserver`:wire 定义的执行回调接口,保留(wire 需要注入 executor)

**删除的接口**(全删):
`RunServiceAPI`/`EventServiceAPI`/`ThreadServiceAPI`/`PendingActionServiceAPI`/`RunResumeServiceAPI`/`CapabilityServiceAPI`/`InboxServiceAPI`/`MemoryServiceAPI`/`SkillServiceAPI`/`DeviceAuthServiceAPI` + `inboxCapabilityService`(pending_action_service_decision.go:150 的内部接口,也是单实现)

### 5.5 隔离 tools/dispatch 子包

**现状**:`tools/` 28 文件 6418 LOC,其中 5 个 dispatch 文件(894 LOC)是工具调度逻辑,和具体工具实现(file/browser/web/command)混在一个包。

**目标**:dispatch 提取为 `tools/dispatch` 子包:

```
internal/tools/
  dispatch/
    scheduler.go       (从 dispatch_scheduler.go 移入, 135 LOC)
    node.go             (从 dispatch_node.go 移入, 269 LOC)
    streaming.go        (从 dispatch_streaming.go 移入, 256 LOC)
    side_effects.go     (从 dispatch_side_effects.go 移入, 195 LOC)
    types.go            (从 dispatch_types.go 移入, 39 LOC)
  // tools 根包保留:
  registry.go           ToolRegistry 实现 (237 LOC)
  builtin_registry.go   RegisterNativeTools (311 LOC)
  catalog.go             Catalog (137 LOC)
  catalog_builders.go   tool builders (214 LOC)
  file_read.go           file 工具 (467 LOC)
  file_mutate.go         mutation 工具 (446 LOC)
  file_edit.go           edit 工具 (324 LOC)
  browser_service.go     browser (552 LOC)
  browser_service_actions.go (517 LOC)
  browser_tool.go        browser tool (260 LOC)
  web.go                 web 工具 (226 LOC)
  command.go             command 工具 (225 LOC)
  artifact.go            artifact 工具 (201 LOC)
  operator.go            operator 工具 (270 LOC)
  workflow.go             workflow 工具 (405 LOC)
  // ... 其余工具文件
```

**子包边界**:`tools/dispatch` 拥有 tool 执行调度逻辑(scheduler、node、streaming、side_effects)。`tools` 根包拥有工具注册和具体工具实现。`runtime` 通过 `tools/dispatch` 包调用调度器。

**依赖方向**:`tools/dispatch` → `core` + `tools`(类型引用)。`tools` 根包不 import `tools/dispatch`。

### 5.6 评估 api.StoreView

**现状**:`api/store_view.go`(15 行)定义 `StoreView` 接口,wire 用它把 `*store.Store` 注入 api 的 pending action service。`wire/runtime.go:49` 有 `mcpPendingActionStore := api.StoreView(db)`。

**判断**:`StoreView` 是 `core.SessionStore` 的窄 facet(只暴露 pending action 相关方法)。但它定义在 api 包(Layer 4),wire(Layer 5)引用它,方向虽然对,但语义不对——store 的消费视图不该由 api 定义。

**目标**:评估能否直接用 `core.SessionStore` 替代。如果 api 的 `PendingActionService` 只需要 pending action 方法子集,保留 `StoreView` 但移到 `core` 包作为 `PendingActionStore` facet;如果 `PendingActionService` 已经持有 `core.SessionStore`,直接删 `StoreView`。在 Phase 2 中确认。

### 5.7 删除死代码

6 处 unreachable 函数:
- `config.DefaultConfig`(`internal/config/config_defaults.go:4`)
- `core.GetCallSite`(`internal/core/context.go:77`)
- `runtime.Toolset.All`(`internal/runtime/capability_assembler.go:355`)
- `runtime.OnToolResult`(`internal/runtime/tool_lifecycle.go:254`)
- `tools.WithToolLifecycleContext`(`internal/tools/tool_lifecycle.go:71`)
- `tests/tooltest.MustInferTool`(`tests/tooltest/test_helpers.go:12`)

全部直接删除。无依赖。

## 6. Migration Strategy

### 6.1 三波顺序

```
Wave 1: core 纯化 + store 接口统一 + 死代码删除
  ├── 移 SessionSummaryService → runtime
  ├── 删 store.ArtifactStore,ArtifactService 依赖 core.ArtifactService
  └── 删 6 处死代码
  → 验证: go build + go test ./internal/core ./internal/runtime ./internal/store ./internal/wire

Wave 2: 拆 runtime god-package
  ├── 创建 runtime/runner 子包,移入装配逻辑
  ├── 创建 runtime/context 子包,移入上下文管理
  └── runtime 根包保留执行引擎
  → 验证: go build + go test ./internal/runtime/... ./internal/wire ./internal/api

Wave 3: 精简 API + 隔离 tools/dispatch
  ├── 删 11 个 ServiceAPI 接口,改具体类型
  ├── 提取 tools/dispatch 子包
  └── 评估 StoreView
  → 验证: go build + go test -race ./... + make lint + make test-architecture
```

### 6.2 Hard Cutover 原则

- 每波内 hard cutover:旧路径在波次结束时删除,不保留 compat alias
- 波次间可独立验证:Wave 1 完成后可 ship,Wave 2 完成后可 ship
- 每个波次内部按 Phase 推进,每个 Phase 是独立 commit

### 6.3 风险控制

| Risk | Mitigation |
|---|---|
| runtime 子包拆分引入环依赖 | 子包不 import runtime 根包;Executor 持有 `*runner.RunnerFactory` |
| 删 API 接口破坏 wire 注入 | wire 直接传 `*XxxService` 指针,类型安全由编译器保证 |
| store.ArtifactService 签名不兼容 core.ArtifactService | Phase 1 加 compile-time assertion 验证 |
| tools/dispatch 子包 import 路径变更 | 用 `goimports` 批量修复,编译器兜底 |
| 测试 import 路径变更 | 每 Phase 后跑测试,编译器兜底 |

## 7. Verification Criteria

1. `go build ./...` → 零错误
2. `go test -race ./...` → 全绿
3. `make lint && make format-check` → 全绿
4. `make test-architecture` → 全绿
5. core 零 internal 依赖: `go list -f '{{join .Imports ", "}}' ./internal/core/ | grep internal` → 无输出
6. core 无 service struct: `grep -rn 'type.*Service struct' internal/core/` → 无输出
7. store 无重复 ArtifactStore: `grep -rn 'type ArtifactStore interface' internal/` → 只有 `internal/core/store.go` 一处
8. runtime 根包 LOC: `wc -l internal/runtime/*.go` → <3500 LOC(从 5858 降低)
9. api 无 ServiceAPI 接口: `grep -rn 'type.*ServiceAPI interface' internal/api/` → 无输出
10. 死代码: `deadcode ./...` → 0 处
11. runtime 子包无环: `go build ./internal/runtime/runner/ && go build ./internal/runtime/context/` → 零错误
12. tools/dispatch 独立编译: `go build ./internal/tools/dispatch/` → 零错误

## 8. ADR-Worthy Decisions

实现完成后创建以下 ADR:
- ADR-0018: core 纯化 — 移出 SessionSummaryService 到 runtime
- ADR-0019: 统一 ArtifactStore 接口 — 删除 store 包重复定义
- ADR-0020: runtime 子包化 — runner + context 按职责拆分
- ADR-0021: 删除单实现 ServiceAPI 接口 — 具体类型替代假解耦
- ADR-0022: tools/dispatch 子包隔离
