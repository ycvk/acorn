# Acorn Convergent Core/Runtime Refactor — Design Spec

Date: `2026-06-24`
Status: `approved`
Complexity: `high — full internal package restructure + plugin registry`
Predecessor: `2026-06-24-greenfield-architecture-refactor-design.md` (merged but did not solve the root cause)

## 1. Problem Statement

当前架构的根因问题不是"god package",而是**类型层过度切片**:4 个包(domain/port/contract/clientevents)定义同一组类型,21 个 store-like 接口散在 6 个包里,执行链跨 6 个包。用户已经做过 4 轮重构(4 个 spec),最后一轮 greenfield merge(`888f5cc`)后仍觉乱,因为前 4 轮全在做"拆 god package → 更小包",方向与痛点相反。

### 1.1 证据基线(当前状态,merge `337934e` 后)

| 维度 | 当前值 | 证据 |
|---|---|---|
| internal 包数 | 20 | `go list ./internal/...` |
| 类型定义包 | 4(domain/port/contract/clientevents) | `domain` 8 文件 670 行;`port` 3 文件 273 行;`contract` 1 文件 64 行;`clientevents` 3 文件 180 行 |
| store-like 接口 | 21 | `grep -rn "type.*Store.*interface\|type.*Repo.*interface\|type.*View.*interface" internal/` |
| port Repo 接口 | 9(SessionRepo/MessageRepo/RunRepo/EventRepo/PendingActionRepo/DeviceRepo/ArtifactRepo/SummaryRepo/OAuthRepo) | `internal/port/repo.go` |
| contract.StoreView 方法数 | 43 | `internal/contract/types.go:11-43` |
| agent runner 文件数 | 10(runner/run/runner_mcp/runner_selection/runner_emit/agent_loop/capability_assembler/context_assembler/model_builder/direct_response) | `internal/agent/` |
| per-run assembly struct | 7(ModelBuilder/CapabilityAssembler/ContextAssembler/MCPAssembler/SkillSelector/RunEmitter/ToolAssembler + RunnerFactory) | `internal/agent/` |
| MCP 子文件数 | 11(manager/catalog/connection/reconcile/runtime/oauth/elicitation/sampling/resource/prompt/transport) | `internal/providers/mcp/` |
| context 机制 | 3 套(masking + auto-compact + circuit breaker) | `internal/context/context_session.go` |
| 非测试代码总量 | ~32K LOC / 186 文件 | `find internal cmd -name "*.go" -not -name "*_test.go"` |
| 前序重构 spec | 4 个(全未解决类型切片问题) | `docs/aegis/specs/` |

### 1.2 前 4 轮为什么没解决根因

| Spec | 做法 | 包数变化 | 对类型切片的影响 |
|---|---|---|---|
| Structural Convergence (06-22) | 工具包 4→2 | -2 | 无 |
| Radical Refactor (06-23) | 17→14,删 orchestration/eventstream | -3 | 保留 domain/port 分离 |
| Modular Refactor (06-23) | RunnerFactory 拆分 | 0 | 拆得更碎 |
| Greenfield (06-24, 已 merge) | Clean Slim Layers | 16→16 | **新增 port 包的 9 个 Repo 接口,类型间接不减反增** |

greenfield 是最"成功"的一轮,但它把 runtime 拆成 agent+context+stream 三包,又新建 port 包放 9 个 Repo——**类型间接不减反增**。这是 merge 后仍觉乱的直接原因。

### 1.3 本次方案与之前的根本区别

**做减法,不做加法。** 不是"拆 god package",是"合并碎片包"。类型层 4→1,执行层 3→1,store 接口 21→3。新增的唯一东西是插件注册中心——因为用户要求插件式动态扩展,这是真实需求。

## 2. Goal

1. **类型层合一**:domain/port/contract/clientevents 合并为 `core`。所有类型定义、契约接口、注册中心接口在一个包。
2. **执行层合一**:agent/context/stream 合并为 `runtime`。一次 run 的执行链在一个包内闭合。
3. **Store 接口收敛**:21 个 store-like 接口 → 3 个能力接口(SessionStore/IdentityStore/ArtifactStore)+ 2 个窄 facet(EventAppender/SessionSummaryStore)用于 ISP。
4. **装配简化**:7 个 per-run assembly struct → 2 步(registry.Resolve + session.Bootstrap)。
5. **插件注册中心**:统一 ToolRegistry + ProviderRegistry 作为一等公民扩展点。
6. **包数缩减**:internal 包 20 → 13(8 个核心包:core/runtime/store/tools/memory/api/mcp/wire + 5 个不变包:config/workspace/webaccess/skills/cli)。类型定义包 4 → 1,执行层包 3 → 1。

## 3. Non-Goals

- 不改产品行为:单用户自托管 agent、direct_response 唯一编排模式、mobile control surface
- 不引入新外部依赖(无 pgvector/LanceDB/Bleve/FAISS/CGO)
- 不改 file-backed memory 模型(facts/history markdown)
- 不改 embedding 惰性接线策略
- 不复活 plan_execute/single_agent/child_agent
- 不改 skill 的 file-backed loader 模型
- 不改 hybrid context 三机制(masking + auto-compact + circuit breaker)本身,只改它所在的包

## 4. Architecture: 8-Package Convergent Core

### 4.1 目标包结构

```
cmd/acorn/
internal/
  core/      ← domain + port + contract + clientevents 合并
              类型 + 契约 + 注册中心接口 + 投影类型
  runtime/   ← agent + context + stream 合并
              Executor + Runner + Session + masking + compact + 投影
  store/     ← SQLite 适配器(实现 core 的 3 个 Store 接口)
  tools/     ← 原生工具实现 + ToolRegistry 实现
  memory/    ← file-backed memory + semantic search(不变)
  api/       ← HTTP handlers + DTO + device auth + RunEvent 投影
  mcp/       ← 从 providers/mcp 提升为顶级包
  wire/      ← 组合根(不变)
  config/    ← 配置加载/校验(不变)
  workspace/ ← workspace + checkpoint + worktree(不变)
  webaccess/ ← web_search/web_fetch/browser 共享(不变)
  skills/    ← skill file loader(不变)
  cli/       ← CLI 入口(不变)
```

**包数变化**:20 → 13(其中 5 个不变包:config/workspace/webaccess/skills/cli)。
核心变化:8 个包合并为 3 个(core/runtime/store 不变) + mcp 提升 + api 吸收 clientevents。
实际"动"的包:domain/port/contract/clientevents→core(4→1),agent/context/stream→runtime(3→1),providers/mcp→mcp(提升),api+clientevents→api(2→1)。

### 4.2 依赖规则(不可违反)

```
Layer 0 (零依赖):     core
Layer 1 (实现 L0):     store, memory, tools, mcp, workspace, webaccess, skills, config
Layer 2 (编排):        runtime (依赖 core + L1 实现类型)
Layer 3 (HTTP):        api (依赖 core + runtime)
Layer 4 (组合根):      wire (依赖所有)
Layer 5 (入口):        cli (依赖 wire, api, config)
```

**硬约束**:
- `core` 不 import 任何 internal 包(零依赖,纯类型+接口)
- `store`/`memory`/`tools`/`mcp` 只 import `core`/`config`/`workspace`/`webaccess`(按需)
- `runtime` import `core` + L1 实现包
- `api` import `core`/`runtime`/`memory`/`skills`/`mcp`(按需)
- `wire` import 所有(唯一知道具体实现的地方)
- `cli` import `wire`/`api`/`config`

### 4.3 依赖图(无环)

```
                    core  ← 所有人依赖
                      ↑
    ┌──────┬──────┬───┴───┬──────┬──────┐
    ↑      ↑      ↑       ↑      ↑      ↑
  store  memory  tools   mcp  workspace  skills   (基础设施)
    ↑      ↑      ↑       ↑      ↑      ↑
    └──────┴──┬───┴───────┴──────┴──────┘
              ↑
           runtime                      (执行引擎)
              ↑
            api                          (HTTP + 投影)
              ↑
            wire                         (组合根)
              ↑
            cli                          (入口)
```

## 5. Key Design Decisions

### 5.1 统一插件注册中心(core 包)

**现状**:工具注册埋在 `tools.Catalog` + `tools.configuredLocalSpec` + MCP manager 的 `buildCapabilitySpecs` 里,没有统一入口。MCP reconcile 通过 `RunnerFactory.ReconcileMCPProviders` 间接调用,绕了多层。

**目标**:注册中心成为 `core` 包的一等公民接口,`tools` 和 `mcp` 都注册到同一个 registry。

```go
// core/registry.go
package core

import (
    "context"
    einotool "github.com/cloudwego/eino/components/tool"
)

// ToolFactory creates a tool instance for a given run context.
// Native tools use a static factory; MCP tools use a factory that
// wraps the MCP session's tool dispatch.
type ToolFactory func(ctx context.Context, runCtx RunContext) (einotool.BaseTool, error)

// ToolRegistry is the unified registry for all tools (native + MCP).
type ToolRegistry interface {
    Register(spec ToolSpec, factory ToolFactory) error
    Unregister(name string) error
    List() []ToolSpec
    // Resolve returns tool instances for the given names. Tools not
    // found are skipped; the caller checks len(result) vs len(names).
    Resolve(ctx context.Context, runCtx RunContext, names []string) ([]einotool.BaseTool, error)
}

// ProviderRegistry is the unified registry for MCP providers.
type ProviderRegistry interface {
    RegisterProvider(config ProviderConfig) error
    UnregisterProvider(name string) error
    Providers() []ProviderInfo
    // Reconcile applies a new provider config set to the live registry.
    Reconcile(ctx context.Context, configs []ProviderConfig) error
}
```

**ToolSpec 统一**:原生工具和 MCP 工具共用 `ToolSpec`,区别只在 `ToolFactory`:
- 原生工具:`factory` 直接返回预构建的 `einotool.BaseTool`
- MCP 工具:`factory` 内部调用 MCP session 的 tool dispatch,包装成 `einotool.BaseTool`

```go
// core/tool.go — 统一 ToolSpec(合并 port.ToolContract + port.ToolSpec)
package core

type ToolSpec struct {
    Name       string
    Source     string         // "native" | "mcp:<provider>"
    Kind       ToolKind
    Category   ToolCategory
    Loading    ToolLoadingPolicy
    Execution  ToolExecutionPolicy
    Factory    ToolFactory    // 统一:原生工具和 MCP 工具都用这个
    Health     ToolHealth
}

// ToolContract(嵌入 ToolSpec)保留为 static descriptor,
// Factory 在注册时填充。
```

**注册流程**:
1. `wire.Container` 启动时构造 `ToolRegistry` 实例(在 `tools` 包)
2. `tools` 包注册所有原生工具:`registry.Register(spec, factory)` per tool
3. `mcp` 包连接 MCP server 后,把发现的工具注册到同一个 `ToolRegistry`
4. `runtime.Executor.buildRun` 调用 `registry.Resolve(ctx, runCtx, toolNames)` 得到工具实例
5. MCP provider 变更时 `mcp` 包调用 `registry.Unregister` + `registry.Register` 热更新

### 5.2 Store 接口:21 → 3

**现状**:21 个 store-like 接口散在 6 个包:
- `port`:9 个 Repo(SessionRepo/MessageRepo/RunRepo/EventRepo/PendingActionRepo/DeviceRepo/ArtifactRepo/SummaryRepo/OAuthRepo)
- `contract`:StoreView(43 方法)
- `agent`:ExecutorStore + RunnerFactoryStore
- `wire`:containerRuntimeStore
- `domain`:SessionSummaryStore
- `tools`:WorkspaceView + OperatorQuestionStore + ArtifactService + WebFetchService + WebSearchService + BrowserService
- `memory`:VectorStore

**目标**:3 个能力接口,按数据域切分:

```go
// core/store.go
package core

// SessionStore — 会话/消息/run/event/pending-action CRUD
type SessionStore interface {
    // sessions
    CreateSession(ctx context.Context, sessionID, title string) (*SessionRecord, error)
    LoadSession(ctx context.Context, sessionID string) (*SessionRecord, error)
    ListSessions(ctx context.Context, limit int) ([]SessionRecord, error)
    DeleteSession(ctx context.Context, sessionID string) error
    UpdateSessionTitle(ctx context.Context, sessionID, title string) error
    UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
    // messages
    ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]SessionMessageRecord, error)
    NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
    AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*SessionMessageRecord, error)
    CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
    LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*SessionMessageRecord, error)
    SyncAssistantMessageForRun(ctx context.Context, runID string) error
    SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status RunStatus) error
    // runs
    CreateRun(ctx context.Context, params RunCreateParams) error
    LoadRun(ctx context.Context, runID string) (*RunRecord, error)
    FinishRun(ctx context.Context, runID string, status RunStatus, output, errText string) error
    MarkInterrupted(ctx context.Context, runID, output string) error
    UpdateRunOutput(ctx context.Context, runID string, output string) error
    ListActiveRuns(ctx context.Context, limit int) ([]RunRecord, error)
    ListRecentTerminalRuns(ctx context.Context, limit int) ([]RunRecord, error)
    LoadLatestRunForSession(ctx context.Context, sessionID string) (*RunRecord, error)
    LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*RunRecord, error)
    // events
    AppendEvent(ctx context.Context, runID, kind string, payload any) (EventRecord, error)
    LoadEvents(ctx context.Context, runID string) ([]EventRecord, error)
    LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]EventRecord, error)
    // pending actions
    CreatePendingAction(ctx context.Context, input PendingActionInput) (*PendingActionRecord, error)
    ListPendingActions(ctx context.Context, limit int) ([]PendingActionRecord, error)
    LoadPendingAction(ctx context.Context, actionID string) (*PendingActionRecord, error)
    DecidePendingAction(ctx context.Context, actionID string, status PendingActionStatus, decisionJSON string) (*PendingActionRecord, error)
}

// IdentityStore — 设备/配对/auth
type IdentityStore interface {
    SavePairingCode(ctx context.Context, code *PairingCode) error
    ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*PairingCode, error)
    SaveDevice(ctx context.Context, device *Device) error
    LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*Device, error)
    ListDevices(ctx context.Context) ([]Device, error)
    TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
    RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ArtifactStore — artifacts + summaries + OAuth tokens
type ArtifactStore interface {
    WriteArtifact(ctx context.Context, req ArtifactWriteRequest) (ArtifactRecord, error)
    ReadArtifactRange(ctx context.Context, req ArtifactReadRangeRequest) (ArtifactReadRangeResult, error)
    ListArtifactsByRun(ctx context.Context, runID string) ([]ArtifactRecord, error)
    ListArtifactsBySession(ctx context.Context, sessionID string) ([]ArtifactRecord, error)
    UpsertSessionSummary(ctx context.Context, summary SessionSummary) error
    GetSessionSummary(ctx context.Context, sessionID string) (*SessionSummary, error)
    SaveOAuthToken(ctx context.Context, token OAuthToken) error
    GetOAuthToken(ctx context.Context, providerName string) (*OAuthToken, error)
}
```

**消除的接口**(全部删除,被上面 3 个替代):
- `port.SessionRepo` + `port.MessageRepo` → `core.SessionStore`(sessions+messages)
- `port.RunRepo` + `port.EventRepo` + `port.PendingActionRepo` → `core.SessionStore`(runs+events+pending)
- `port.DeviceRepo` → `core.IdentityStore`
- `port.ArtifactRepo` + `port.SummaryRepo` + `port.OAuthRepo` → `core.ArtifactStore`
- `contract.StoreView` → 删除(消费者直接用 `core.SessionStore` / `core.IdentityStore` / `core.ArtifactStore`)
- `agent.ExecutorStore` + `agent.RunnerFactoryStore` → 删除(用 `core.SessionStore` + `core.ArtifactStore`)
- `wire.containerRuntimeStore` → 删除
- `domain.SessionSummaryStore` → 删除(并入 `core.ArtifactStore`)
- `port.MCPTokenStore` + `port.MCPPendingActionStore` → 删除(MCP 包直接用 `core.ArtifactStore` + `core.SessionStore`)
- `tools.WorkspaceView` + `tools.ArtifactService` + `tools.WebFetchService` + `tools.WebSearchService` + `tools.BrowserService` + `tools.OperatorQuestionStore` → 保留为 tools 包内部接口(实现细节,不是 core 契约)

**注意**:`tools.WorkspaceView` 等 tools 包内部的 service 接口保留在 `tools` 包,因为它们是 tools 包的依赖注入接口,不是跨包契约。这些接口描述 tools 包需要什么依赖,属于实现细节。`tools.OperatorQuestionStore` 如果只被 tools 包内部使用,也保留在 tools 包。

### 5.3 执行链合并(agent + context + stream → runtime)

**现状**:一次 run 跨 6 包:
```
wire → agent.Executor → RunnerFactory.buildRun →
  [ModelBuilder, CapabilityAssembler, ContextAssembler, MCPAssembler,
   SkillSelector, RunEmitter, ToolAssembler] →
  context.Plane + Session → stream.projection → store
```

**目标**:2 包,2 步:
```
wire → runtime.Executor.buildRun:
  1. registry.Resolve(tools) + memory.Prepare + skills.Select → context assembly
  2. Session.Bootstrap(assembly) → model loop → store
```

**runtime 包文件结构**:

```
runtime/
  executor.go          — Executor struct + Run/Resume/ExecuteMessages
  runner.go            — RunnerFactory + ActiveRunner + buildRun(合并 runner/run/runner_mcp/runner_selection/runner_emit)
  direct_response.go   — direct_response loop + ExecuteRound
  session.go           — Session interface + defaultSession(从 context/context_session.go 移入)
  plane.go             — Plane interface + defaultPlane(从 context/types.go 移入)
  masking.go           — observation masking(从 context/ 移入)
  auto_compact.go      — LLM auto-compact + circuit breaker(从 context/ 移入)
  tool_lifecycle.go    — tool lifecycle(合并 context/tool_lifecycle.go + tool_lifecycle_runtime.go)
  memory_context.go    — memory context assembly(从 context/memory_context.go 移入)
  context_helpers.go   — context plumbing helpers(从 context/context_helpers.go 移入)
  projection.go        — StreamItem→event 投影(从 stream/projection.go 移入)
  assistant_stream.go  — assistant streaming(从 stream/assistant_stream.go 移入)
  streaming_assistant.go — streaming assistant(从 stream/streaming_assistant.go 移入)
  types.go             — runtime-internal types(ActiveRunner/RunnerBuildRequest/assembly 等)
  agent_loop.go        — agent loop(保留)
  audit.go             — tool audit(保留)
  validator.go         — tool validator(保留)
  catalog.go           — tool catalog helpers(保留)
  model_builder.go     — chat model construction(保留为函数,不是 struct)
  run_context.go       — RunContext + Registry + RunController(从 types.go 拆出)
  memtools.go          — memory tools(从 agent/factextract/ 内联)
```

**删除的 assembly struct**(全部内联为函数):
- `ModelBuilder` struct → `newChatModel(ctx, cfg)` 函数
- `CapabilityAssembler` struct → `buildCapabilities(ctx, req)` 函数
- `ContextAssembler` struct → `assembleContext(ctx, req)` 函数
- `MCPAssembler` struct → `buildMCPManager(ctx, req)` 函数(MCP 通过 registry,不再需要单独 assembler)
- `SkillSelector` struct → `selectSkills(ctx, req)` 函数
- `RunEmitter` struct → `emitRunStarted(ctx, runID, input, sink)` 函数
- `ToolAssembler` struct → `assembleTooling(ctx, params)` 函数

**保留的 struct**:
- `Executor` — 顶层执行器
- `RunnerFactory` — 瘦工厂,只做 buildRun 委托
- `ActiveRunner` — per-run 状态
- `RunController` — run 中断控制
- `Registry` — run context 注册(已有,保留)

### 5.4 core 包文件结构

```
core/
  domain.go          — RunRecord/EventRecord/SessionRecord/PendingActionRecord 等核心类型(从 domain/domain.go)
  store_types.go     — SessionMessageRecord/PairingCode/Device/OAuthToken 等存储类型(从 domain/store_types.go + domain/session_summary.go)
  stream_types.go    — Stream* payload 值类型(从 domain/stream_types.go)
  stream_accessors.go — typed accessors(从 domain/stream_accessors.go)
  context.go         — context plumbing: WithRunID/WithSessionID/WithCallSite(从 domain/context.go)
  ports.go           — EventAppender/StreamSink/AssistantStreamer 等运行时端口(从 domain/ports.go)
  store.go           — SessionStore/IdentityStore/ArtifactStore 三个接口(新建)
  tool.go            — ToolSpec/ToolContract/ToolKind/ToolCategory/Catalog 接口(从 port/tool.go)
  registry.go        — ToolRegistry/ProviderRegistry 接口(新建)
  mcp.go             — ProviderConfig/ProviderInfo/MCP 相关类型(从 providers/mcp + port/mcp.go)
  projection.go      — RunEvent → mobile subset 投影类型(从 clientevents/types.go + clientevents/projector.go)
  errors.go          — sentinel errors(从 domain/domain.go 的 ErrRunNotActive 等)
  helpers.go         — compactText/NewRunID 等小工具函数(从 agent/types.go)
```

### 5.5 api 包合并 clientevents

**现状**:`clientevents` 包(3 文件)做 RunEvent → mobile live subset 投影,只被 `api` 包引用。

**目标**:投影逻辑移入 `api` 包,作为 `api/projection.go` + `api/live_types.go`。投影类型移入 `core`(如果被 api 和 runtime 共用)或留在 api(如果只被 api 用)。

**判断依据**:如果 `runtime` 也需要投影类型(用于 emit),则类型放 `core`;如果只有 `api` 在投影,则留在 `api`。从当前代码看,`clientevents` 只被 `api` 引用,所以投影逻辑和类型都留在 `api`。

### 5.6 mcp 提升为顶级包

**现状**:`internal/providers/mcp/` 有 11 个文件,实现 MCP provider lifecycle + OAuth + elicitation + sampling + resource + prompt。

**目标**:提升为 `internal/mcp/`,实现 `core.ProviderRegistry` 接口。MCP 工具注册到 `core.ToolRegistry`。

**不变**:MCP 的内部文件结构基本不变(manager/catalog/connection/reconcile 等),只是改包路径。Manager 额外实现 `ProviderRegistry` 接口。

**MCP 工具注册流程**:
1. `mcp.Manager` 连接 provider 后,获取 tool list
2. 对每个 MCP tool,构造 `ToolSpec`(Source=`"mcp:<provider>"`)+ `ToolFactory`(包装 MCP session dispatch)
3. 调用 `registry.Register(spec, factory)` 注册到统一 `ToolRegistry`
4. provider 断开时 `registry.Unregister(name)` 清理

### 5.7 SQLite Schema 简化

**现状**:10 张表(runs/events/sessions/session_messages/pending_actions/mcp_oauth_tokens/devices/pairing_codes/artifacts/schema_migrations)。

**目标**:用户允许改 schema。但当前 schema 已经很精简(从 23 表降到 10 表是前序 ADR-0005 的成果)。本次重构**不强制改 schema**,但允许在迁移过程中做小幅优化:
- 如果 `core.SessionStore` 接口暴露的方法比当前 store 实现少,删除未使用的方法对应的 SQL
- 如果 `core.ArtifactStore` 合并了 summary + OAuth,确认表结构仍合理(当前 artifacts + mcp_oauth_tokens 是分开的表,保持不变)

**结论**:schema 保持 10 表不变,除非迁移过程中发现明确的冗余。

## 6. Impact Statement

### 6.1 Affected Layers

| 层 | 影响 | 动作 |
|---|---|---|
| domain/port/contract/clientevents | **删除** | 类型合并到 core |
| agent/context/stream | **删除** | 合并到 runtime |
| providers/mcp | **rename + 提升** | → mcp |
| api | **吸收 clientevents** | 投影逻辑移入 |
| store | **适配新接口** | 实现 3 个 core Store 接口 |
| tools | **适配新 ToolSpec** | 注册到 ToolRegistry |
| memory | **不变** | 保持 |
| wire | **重新接线** | 构造 registry,注入新依赖 |
| cli | **import 路径更新** | 跟随 |
| tests/architecture | **守卫更新** | 反映新包结构 |

### 6.2 Invariants Preserved

- direct_response 唯一编排模式
- SQLite 是 runtime 真相
- file-backed memory 是长期记忆
- OpenAPI 是 wire contract(可改 DTO,需同步 mobile client)
- single-owner device auth
- hybrid context 三机制(masking + auto-compact + circuit breaker)
- embedding 惰性接线
- 架构守卫(结构上限 / 依赖方向 / store 接口数)

### 6.3 Compatibility Boundary

- **OpenAPI wire contract**:允许改 DTO shape,需同步 `docs/openapi.yaml` + 重新生成 mobile client
- **SQLite schema**:允许改,但预期保持 10 表不变
- **mobile-kotlin client**:重新生成(跟随 OpenAPI 变更)
- **配置文件**:如果 config struct 不变则兼容;如果变了则需迁移

### 6.4 ADR Signals

本 spec 触及以下 ADR-worthy 决策:
- 统一插件注册中心替代分散的 catalog + reconcile
- 类型层从 4 包合并为 1 包(core)
- Store 接口从 21 个收敛为 3 个
- 执行层从 3 包合并为 1 包(runtime)
- MCP 从 providers/mcp 提升为顶级 mcp 包

建议后续创建 ADR 记录这些决策。不创建"未执行的架构记忆"。

## 7. Migration Plan (Bottom-Up)

迁移分 8 个 Phase,每个 Phase 是一个独立 commit,可编译可测试。新树在旧包旁并行构建,旧包最后删除。

### Phase 1: 建 core 包(类型+契约+注册中心接口)

- 创建 `internal/core/` 包
- 把 `domain/` 的所有类型移入 `core/domain.go` + `core/store_types.go` + `core/stream_types.go` + `core/stream_accessors.go` + `core/context.go` + `core/ports.go` + `core/errors.go`
- 把 `port/repo.go` 的 9 个 Repo 接口合并为 `core/store.go` 的 3 个接口(SessionStore/IdentityStore/ArtifactStore)
- 把 `port/tool.go` 的 ToolContract/ToolSpec/ToolKind 等移入 `core/tool.go`
- 把 `port/mcp.go` 的 MCPTokenStore/MCPPendingActionStore 删除(被 core.SessionStore/ArtifactStore 替代)
- 把 `contract/types.go` 的 StoreView/ExecutorHandle 删除(被 core 接口替代)
- 新建 `core/registry.go` 定义 ToolRegistry/ProviderRegistry 接口
- 把 `clientevents/types.go` 的投影类型移入 `core/projection.go`(如果被跨包使用)或留 api(如果只 api 用)
- 此时旧包仍存在,core 包是新的平行实现;旧包的测试仍通过

### Phase 2: 建 runtime 包(agent + context + stream 合并)

- 创建 `internal/runtime/` 包
- 把 `agent/` 的 executor/runner/direct_response/agent_loop/audit/validator/catalog/model_builder 移入
- 把 `context/` 的 context_session/types/masking/auto_compact/tool_lifecycle/memory_context/context_helpers 移入
- 把 `stream/` 的 projection/assistant_stream/streaming_assistant 移入
- 把 `agent/factextract/` 内联为 `runtime/memtools.go`
- 把 `agent/tooltest/` 移到 `tests/` 目录
- 删除 7 个 assembly struct,内联为函数
- 合并 `tool_lifecycle.go` + `tool_lifecycle_runtime.go`
- 适配 core 包的新类型和接口
- 此时旧包仍存在,runtime 包是新的平行实现

### Phase 3: store 适配新接口

- `store/` 包实现 `core.SessionStore` + `core.IdentityStore` + `core.ArtifactStore`
- 删除 store 包对 `port.*Repo` 接口的实现(改为直接实现 core 接口)
- 删除 `contract.StoreView` 的适配代码
- store schema 保持不变

### Phase 4: tools 实现 ToolRegistry

- `tools/` 包实现 `core.ToolRegistry` 接口(新建 `tools/registry.go`)
- 把 `tools/configured.go` + `tools/catalog_builders.go` 的工具注册改为调用 `registry.Register`
- 原生工具在启动时注册到 registry
- 适配 core 包的新 ToolSpec(含 Factory 字段)
- 保留 tools 包内部的 WorkspaceView/ArtifactService 等 service 接口

### Phase 5: mcp 提升 + 实现 ProviderRegistry

- `providers/mcp/` → `mcp/`
- `mcp.Manager` 实现 `core.ProviderRegistry` 接口
- MCP 工具连接后注册到 `core.ToolRegistry`
- 适配 core 包的新类型(ProviderConfig 等移入 core)
- 删除 `port.MCPTokenStore`/`MCPPendingActionStore` 的使用(用 core.ArtifactStore/SessionStore 替代)

### Phase 6: api 合并 clientevents

- `clientevents/` 投影逻辑移入 `api/projection.go`
- api 适配 core + runtime 的新类型
- 可改 DTO shape(需同步 openapi.yaml)

### Phase 7: wire 重新接线 + 删旧包

- `wire/container.go` 构造 `ToolRegistry` 实例
- wire 注入 registry 到 runtime + mcp
- 删除旧包:domain/port/contract/clientevents/agent/context/stream/providers
- 更新所有 import 路径
- 此时全项目编译通过,测试通过

### Phase 8: 架构守卫 + 文档同步

- 更新 `tests/architecture/structural_limits_test.go` 的 `refactorOwnedDirs`
- 更新 `tests/architecture/store_interface_count_test.go` 的 `consumerOwnedDirs` + 计数规则
- 更新 `tests/architecture/dependency_direction_test.go` 的依赖规则
- 更新 `tests/architecture/client_projection_boundary_test.go` 的文件列表
- 更新 `docs/architecture/ARCHITECTURE.md` + `INVARIANTS.md`
- 更新 AGENTS.md 的包职责描述
- 同步 `docs/openapi.yaml` + 重新生成 mobile client(如果 DTO 改了)

## 8. Verification Criteria

1. **编译通过**:`go build ./...` 零错误
2. **测试通过**:`go test ./...` 全绿(含 `-race`)
3. **架构守卫通过**:`make test-architecture` 全绿
4. **Lint 通过**:`make lint && make format-check` 全绿
5. **包数验证**:internal 包数 ≤ 13(从 20 降到 13)
6. **store 接口验证**:core 包的 Store 接口 ≤ 3
7. **执行链验证**:一次 run 的调用链不跨超过 2 个 internal 包(wire → runtime)
8. **注册中心验证**:新工具可通过 `registry.Register` 注册,不需改 runtime 代码
9. **MCP 热更新验证**:MCP provider 变更时 `registry.Unregister` + `Register` 生效
10. **mobile client 重新生成**:如果 DTO 改了,`./tool/generate_openapi_client.sh --check` 通过

## 9. Risk Assessment

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| core/runtime 包文件超 800 行 | 中 | 守卫失败 | 拆文件(同包多文件),不拆包 |
| 800 行限制下 runtime 包文件过多 | 中 | 可读性 | 用子目录但同包(`runtime/` + `runtime/` 子文件) |
| MCP 工具注册到 registry 后 tool lifecycle 失效 | 中 | 功能 regression | Phase 5 重点测试 MCP 工具 + deferred loading |
| store 接口合并后方法遗漏 | 低 | 编译失败 | Go 编译器会捕获 |
| OpenAPI DTO 改动导致 mobile 不兼容 | 中 | mobile 破损 | Phase 6 同步生成 client |
| 4 轮重构 fatigue——用户可能中途失去信心 | 中 | 项目停滞 | 每个 Phase 独立可验证,快速反馈 |

## 10. Working Artifacts

### TaskIntentDraft

- **Outcome**:internal 包从 20 减到 13,类型层从 4 包合一为 core,执行层从 3 包合一为 runtime,store 接口从 21 减到 3,新增统一插件注册中心
- **Goal**:消除"追代码累"的根因——类型层过度切片
- **Success evidence**:`go build && go test -race && make lint && make test-architecture` 全绿;一次 run 的调用链不跨超过 2 个 internal 包
- **Stop condition**:8 个 Phase 全部完成,所有验证标准通过
- **Non-goals**:不改产品行为;不引入新外部依赖;不改 memory/skills/workspace/webaccess/config 包
- **Scope**:Go 后端 internal/ + cmd/ + tests/architecture/;mobile-kotlin 跟随 OpenAPI 变更
- **Risks**:4 轮重构 fatigue;800 行文件限制;MCP tool lifecycle regression

### BaselineUsageDraft

```text
BaselineUsageDraft:
- Required baseline refs:
  - AGENTS.md(硬约束入口)
  - docs/architecture/ARCHITECTURE.md(架构总入口)
  - docs/architecture/INVARIANTS.md(不变量)
  - docs/architecture/runtime-execution.md(执行层现状)
  - internal/domain/domain.go(核心类型定义)
  - internal/port/repo.go(当前 store 接口)
  - internal/port/tool.go(当前 tool 契约)
  - internal/contract/types.go(StoreView 定义)
  - internal/agent/types.go(ExecutorStore + RunnerFactoryStore)
  - internal/agent/runner.go(RunnerFactory 装配)
  - internal/agent/executor.go(Executor 执行链)
  - internal/context/types.go(Plane 接口)
  - internal/context/context_session.go(Session 接口)
  - internal/tools/catalog.go(Catalog 实现)
  - internal/tools/configured.go(工具配置)
  - internal/providers/mcp/manager.go(Manager 接口)
  - internal/providers/mcp/manager_reconcile.go(ReconcileProviders)
  - internal/wire/container.go(组合根)
  - internal/store/store_schema.go(schema 定义)
  - tests/architecture/(4 个守卫测试)
- Delivered context refs: 上述文件已在对话中读取
- Acknowledged before plan refs: 4 个前序 spec
- Cited in design refs: 见本文档第 1 节证据基线
- Missing refs: 无
- Decision: continue
```

### ImpactStatementDraft

```text
ImpactStatementDraft:
- Affected layers: domain/port/contract/clientevents(删除)→ core; agent/context/stream(删除)→ runtime; providers/mcp → mcp(提升); api + clientevents → api; store(适配); tools(适配); wire(重新接线)
- Owners: core 拥有类型+契约+注册中心接口; runtime 拥有执行引擎; store 实现 core 接口; tools 实现 ToolRegistry; mcp 实现 ProviderRegistry
- Invariants: direct_response 单模式; SQLite runtime 真相; file-backed memory; OpenAPI wire contract; single-owner device auth; hybrid context 三机制; embedding 惰性接线; 架构守卫
- Compat: OpenAPI 可改(需同步 mobile); schema 可改(预期不变); config 预期不变
- Non-goals: 不改产品行为;不引入新依赖;不改 memory/skills/workspace/webaccess/config
```

### Architecture Integrity Lens

```text
Architecture Integrity Lens:
- Invariant: 一次 run 的执行链在一个包内闭合,不跨包跳转就能读懂
- Canonical owner: core 拥有所有类型+契约+注册中心接口;runtime 拥有执行引擎
- Responsibility overlap: domain/port/contract/clientevents 四个包的职责高度重叠(都是类型定义),合并消除重叠
- Higher-level simplification: 21 个 store-like 接口 → 3 个能力接口,从接口碎片改为能力边界;7 个 assembly struct → 2 步函数调用
- Retirement: domain/port/contract/clientevents 全部删除;stream 并入 runtime;providers/mcp 提升为 mcp;agent 并入 runtime
- Verdict: proceed — 这是第一个真正减少包数和类型间接的方案,与前 4 轮"拆 god package"方向相反
```

### Product Risk Lens

```text
Product Risk Lens:
- Value: 消除"追代码累"的根因,让代码库可维护;新增插件注册中心支持动态扩展
- Non-goals: 不改产品行为;不引入新外部依赖
- Trade-offs: 短期迁移成本高(8 Phase);长期维护成本大幅降低;800 行文件限制可能需要拆文件
- Decision needed: 无(方向已确认)
```
