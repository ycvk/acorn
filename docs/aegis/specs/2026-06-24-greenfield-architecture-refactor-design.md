# Acorn Greenfield Architecture Refactor — Design Spec

Date: `2026-06-24`
Status: `historical superseded by 2026-06-25-radical-simplification-design.md`
Complexity: `high — full codebase restructure`

## 1. Problem Statement

当前架构有三个根因问题,互为因果:

1. **依赖方向错误**:store 接口由消费者定义(`ExecutorStore`/`RunnerFactoryStore`/`containerAppStore`),消费者反过来引用 store 包的具体类型(`store.RunCreateParams`)。tools 包自引用(tools → tools)。contextplane 依赖 tools 实现层。
2. **God Package / God Interface**:`runtime` 包依赖 14 个内部包,同时做执行编排+运行装配+工具调度+能力快照+MCP 管理+fact 提取。`containerAppStore` 有 30+ 方法,每个 consumer 只用 3-5 个。
3. **中间层冗余**:4 个 Assembler struct(ModelBuilder/CapabilityAssembler/ContextAssembler/MCPAssembler)只搬运代码不简化核心流。`MemoryService` 是零逻辑 delegate。`executorHandle`/`runtimeExecutorHandle` 是无意义适配层。

## 2. Goal

用 Clean Slim Layers 架构重建 internal 包结构:
- 依赖单向流动,无环
- 每个 import 一个 internal 包,不超过 5 个
- Store 端口由 store 自己定义,窄接口
- 删除所有纯搬运中间层
- SQLite schema 重新设计,消除冗余表和列

## 3. Non-Goals

- 不改变产品行为:单用户自托管 agent、direct_response 唯一编排模式、mobile control surface
- 不引入新外部依赖(无 pgvector/LanceDB/Bleve/FAISS/CGO)
- 不改变 file-backed memory 模型(facts/history markdown)
- 不改变 embedding 惰性接线策略
- 不做 plan_execute/single_agent/child_agent 复活

## 4. Architecture: Clean Slim Layers

### 4.1 目标包结构

```
internal/
  domain/          # 纯数据类型 + 错误 + context plumbing。零依赖。
  port/            # 窄接口定义。只依赖 domain。
  store/           # SQLite 实现 port 接口 + schema/migrations。
  memory/          # file-backed memory + semantic search。实现 memory ports。
  mcp/             # MCP provider lifecycle + OAuth。
  tools/           # 工具实现(file/git/browser/web/command/artifact)。
  context/         # 上下文装配 + masking + auto-compact。
  agent/           # 执行引擎:direct_response loop。
  api/             # HTTP handlers + DTO + 业务 service。
  wire/            # 唯一组合根。
  cli/             # CLI 入口。
  config/          # 配置加载/校验。
  workspace/       # workspace 抽象(checkpoint/worktree/git)。
  webaccess/       # web_search/web_fetch/browser 共享。
  skills/          # skill loader(只读 markdown)。
  stream/          # StreamItem→event 投影 + assistant streaming。
  clientevents/    # live RunEvent → mobile subset 投影。
```

### 4.2 依赖规则(不可违反)

```
Layer 0 (零依赖):     domain
Layer 1 (只依赖 L0):   port, config
Layer 2 (实现 L1):     store, memory, mcp, tools, context, workspace, webaccess, skills, stream, clientevents
Layer 3 (编排):        agent (依赖 L0-L2)
Layer 4 (HTTP):        api (依赖 L0-L3)
Layer 5 (组合根):      wire (依赖所有)
Layer 6 (入口):        cli (依赖 wire, api, config)
```

**硬约束**:
- `domain` 不 import 任何 internal 包
- `port` 只 import `domain`
- `store`/`memory`/`mcp` 只 import `port`/`domain`/`config`
- `tools` import `port`/`domain`/`config`/`workspace`/`webaccess`
- `context` import `port`/`domain`/`memory`/`skills`/`tools`(只引用类型,不引用实现)
- `agent` import `port`/`domain`/`config`/`tools`/`context`/`stream`/`mcp`/`memory`
- `api` import `port`/`domain`/`agent`/`clientevents`
- `wire` import 所有(唯一知道具体实现的地方)
- `cli` import `wire`/`api`/`config`

### 4.3 依赖图(无环)

```
                    domain  ← 所有人依赖
                      ↑
                    port  ← 接口定义,只依赖 domain
                      ↑
          ┌──────────┼──────────┐
          ↑          ↑          ↑
        store      memory      mcp        (基础设施,实现 port)
          ↑          ↑          ↑
          └────┬─────┴──────┬───┘
               ↑            ↑
             tools        context     (工具实现 + 上下文管理)
               ↑            ↑
               └─────┬──────┘
                     ↑
                   agent          (执行引擎)
                     ↑
                   api            (HTTP + 业务 service)
                     ↑
                   wire           (组合根)
                     ↑
                   cli            (入口)
```

## 5. Key Design Decisions

### 5.1 Store 端口翻转

**现状**:消费者(runtime/app)定义 `ExecutorStore`/`RunnerFactoryStore`/`containerAppStore`,引用 `store.RunCreateParams`。

**目标**:store 自己在 `port` 包定义窄 Repo 接口,消费者只 import 它需要的 1-2 个。

```go
// internal/port/repo.go
package port

// SessionRepo — 会话 CRUD
type SessionRepo interface {
    CreateSession(ctx context.Context, sessionID, title string) (*domain.SessionRecord, error)
    LoadSession(ctx context.Context, sessionID string) (*domain.SessionRecord, error)
    ListSessions(ctx context.Context, limit int) ([]domain.SessionRecord, error)
    LoadLatestRunForSession(ctx context.Context, sessionID string) (*domain.RunRecord, error)
    LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*domain.RunRecord, error)
    UpdateSessionTitle(ctx context.Context, sessionID, title string) error
    UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
    DeleteSession(ctx context.Context, sessionID string) error
}

// MessageRepo — 会话消息
type MessageRepo interface {
    ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]domain.SessionMessageRecord, error)
    NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
    AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*domain.SessionMessageRecord, error)
    CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
    LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*domain.SessionMessageRecord, error)
    SyncAssistantMessageForRun(ctx context.Context, runID string) error
    SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status domain.RunStatus) error
}

// RunRepo — 运行记录
type RunRepo interface {
    CreateRun(ctx context.Context, params domain.RunCreateParams) error
    LoadRun(ctx context.Context, runID string) (*domain.RunRecord, error)
    FinishRun(ctx context.Context, runID string, status domain.RunStatus, output, errText string) error
    MarkInterrupted(ctx context.Context, runID, output string) error
    UpdateRunOutput(ctx context.Context, runID, output string) error
    ListActiveRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
    ListRecentTerminalRuns(ctx context.Context, limit int) ([]domain.RunRecord, error)
}

// EventRepo — 事件追加/查询
type EventRepo interface {
    AppendEvent(ctx context.Context, runID, kind string, payload any) (domain.EventRecord, error)
    LoadEvents(ctx context.Context, runID string) ([]domain.EventRecord, error)
    LoadEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]domain.EventRecord, error)
}

// PendingActionRepo — 待审批操作
type PendingActionRepo interface {
    CreatePendingAction(ctx context.Context, input domain.PendingActionInput) (*domain.PendingActionRecord, error)
    ListPendingActions(ctx context.Context, limit int) ([]domain.PendingActionRecord, error)
    LoadPendingAction(ctx context.Context, actionID string) (*domain.PendingActionRecord, error)
    DecidePendingAction(ctx context.Context, actionID string, status domain.PendingActionStatus, decisionJSON string) (*domain.PendingActionRecord, error)
}

// DeviceRepo — 设备认证
type DeviceRepo interface {
    SavePairingCode(ctx context.Context, code *domain.PairingCode) error
    ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*domain.PairingCode, error)
    SaveDevice(ctx context.Context, device *domain.Device) error
    LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*domain.Device, error)
    ListDevices(ctx context.Context) ([]domain.Device, error)
    TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
    RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ArtifactRepo — 产物
type ArtifactRepo interface {
    WriteArtifact(ctx context.Context, req domain.ArtifactWriteRequest) (domain.ArtifactRecord, error)
    ReadArtifactRange(ctx context.Context, req domain.ArtifactReadRangeRequest) (domain.ArtifactReadRangeResult, error)
    ListArtifactsByRun(ctx context.Context, runID string) ([]domain.ArtifactRecord, error)
    ListArtifactsBySession(ctx context.Context, sessionID string) ([]domain.ArtifactRecord, error)
}

// SummaryRepo — 会话摘要
type SummaryRepo interface {
    SaveSummary(ctx context.Context, sessionID, sourceRunID, runStatus, summary string) error
    LoadSummary(ctx context.Context, sessionID string) (*domain.SessionSummary, error)
}

// OAuthRepo — MCP OAuth token
type OAuthRepo interface {
    SaveOAuthToken(ctx context.Context, token domain.OAuthToken) error
    LoadOAuthToken(ctx context.Context, providerName string) (*domain.OAuthToken, error)
}
```

**迁移类型**: `RunCreateParams` → `domain.RunCreateParams`,`CreatePendingActionInput` → `domain.PendingActionInput`,`PairingCode`/`Device`/`OAuthToken` → `domain.*`。

### 5.2 runtime 拆分:God Package → 3 个窄包

**现状**: `runtime` (7905 行, 14 依赖) 包含 Executor + RunnerFactory + 4 Assembler + tooldispatch + factextract + tooltest。

**目标**:

```
runtime 拆成:
  agent/      ≈800 行   执行循环
    - agent.go          Agent struct + Run/Resume方法
    - direct_response.go  direct_response loop + ExecuteRound
    - assembly.go       buildRun: 内联 model + capability + context + mcp 装配
    - types.go          AgentRunContext, ActiveRunner, RunnerBuildRequest
    - runner_factory.go 瘦工厂: buildRun 只调用 assembly.go 中的函数序列
  
  tools/      (现有 tools 包吸收 tooldispatch)
    - scheduler.go      (从 runtime/tooldispatch/ 合入)
    - streaming_executor.go
    - side_effects.go
    - node.go
  
  context/    (原 contextplane 改名)
    - context_session.go  (现有,保留)
    - assembly.go
    - masking.go
    - auto_compact.go
    - tool_lifecycle.go
    - types.go
    - memory_context.go
```

**删除**:
- `ModelBuilder` struct → 内联为 `agent` 包的 `newChatModel(ctx, cfg) (einomodel.BaseChatModel, error)` 函数
- `CapabilityAssembler` struct → 内联为 `agent` 包的 `buildCapabilities(ctx, req)` 函数
- `ContextAssembler` struct → 内联为 `agent` 包的 `assembleContext(ctx, req)` 函数
- `MCPAssembler` struct → 内联为 `agent` 包的 `buildMCPManager(ctx, req)` 函数
- `ToolAssembler` struct → 内联为 `agent` 包的 `assembleTooling(ctx, params)` 函数
- `runtime/factextract/` → 内联到 `agent/memtools.go`(memory tools 注册)
- `runtime/tooltest/` → 移到 `tests/` 目录

### 5.3 app 拆分:组合根 vs 业务逻辑

**现状**: `app` (6544 行) 同时拥有 Container + 11 个 service + executorHandle 适配层。

**目标**:

```
app 拆成:
  wire/       ≈300 行
    - container.go      Container struct + NewContainer + Close
    - 所有 build* 函数
    - 唯一 import store/memory/mcp 具体实现的地方
  
  api/        (现有 api 包吸收 app 的 service)
    - handlers_*.go     (现有 HTTP handlers,保留)
    - thread_service.go  (从 app 移入)
    - run_service.go
    - event_service.go
    - pending_action_service.go
    - capability_service.go
    - device_auth_service.go
    - inbox_service.go
    - skill_service.go
    - run_resume_service.go
    - notification_service.go
    - dto_*.go
    - routes.go
    - server.go
```

**删除**:
- `MemoryService` delegate wrapper → api 直接持有 `memory.Service`
- `executorHandle`/`runtimeExecutorHandle` → agent 直接暴露 `Agent` 接口
- `containerRuntimeStore`/`containerAppStore` 接口 → 替换为 `port.*Repo` 接口
- `containerRuntimeDeps` struct → wire 直接构造并注入

### 5.4 tools 分离:契约 vs 实现

**现状**: `tools` 包同时定义 `ToolContract`/`Catalog`/`ToolSpec` + 实现所有工具 + 自引用。

**目标**:

```go
// internal/port/tool.go — 工具契约(纯接口,纯类型)
package port

type ToolContract struct {
    Name      string
    Source    string
    Kind      ToolKind
    Category  ToolCategory
    Loading   ToolLoadingPolicy
    Execution ToolExecutionPolicy
}

type ToolSpec struct { ... }
type Catalog struct { ... }  // 接口
type ExecutionPolicyResolver interface { ... }
// 等所有类型定义移到 port

// internal/tools/ — 工具实现
// 只保留具体工具实现:file_read.go, file_mutate.go, browser_service.go, ...
// 不再有 contract.go/tools_types.go(移到 port)
```

**变化**: `tools` 不再自引用。`context`/`agent` 依赖 `port.ToolSpec` 而非 `tools.ToolSpec`。

### 5.5 domain 瘦身

**现状**: `domain.go` (379 行) 混合 domain records + context plumbing + stream types + ports。

**目标**:

```
domain/
  domain.go          # 纯类型: RunRecord/EventRecord/SessionRecord/PendingActionRecord
                    # + RunCreateParams/PendingActionInput/Device/PairingCode/OAuthToken
                    # + ArtifactWriteRequest/ArtifactRecord 等 store 相关值类型
                    # + sentinel errors
  context.go         # WithRunID/WithSessionID/WithTurnIndex/WithCallSite/WithStreamSink
  stream_types.go    # StreamItem/StreamItemKind/StreamSink (从 stream_types.go 移入)
  stream_accessors.go # typed accessors (保留)
  session_summary.go # SessionSummary + SessionSummaryService (保留)
  ports.go           # EventAppender/ToolCallContextBridge (保留在 domain,因为是核心域端口)
```

`port` 包的 `*Repo` 接口是基础设施端口,`domain` 的 `EventAppender`/`ToolCallContextBridge` 是核心域端口——两者分开。

### 5.6 SQLite Schema 重设计

**现状**: 12 张表,有冗余。

**目标 schema** (10 张表):

```sql
-- 1. sessions (保留,简化)
CREATE TABLE sessions (
    session_id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 2. session_messages (保留,新增 content_parts 列保留)
CREATE TABLE session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    turn_index INTEGER NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    content_parts TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE INDEX idx_session_messages_run_id ON session_messages(run_id);
CREATE INDEX idx_session_messages_session_turn ON session_messages(session_id, turn_index);

-- 3. runs (保留,简化:删除 updated_at,用 created_at 排序)
CREATE TABLE runs (
    run_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL DEFAULT '',
    turn_index INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    input_text TEXT NOT NULL,
    output_text TEXT NOT NULL DEFAULT '',
    error_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);

-- 4. events (保留)
CREATE TABLE events (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_events_run_sequence ON events(run_id, sequence ASC);

-- 5. pending_actions (保留,简化:合并 decided_at/resolved_at 为 resolved_at)
CREATE TABLE pending_actions (
    action_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    interrupt_id TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    decision_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_pending_actions_run_id_status ON pending_actions(run_id, status, created_at DESC);
CREATE UNIQUE INDEX idx_pending_actions_interrupt_id ON pending_actions(interrupt_id) WHERE interrupt_id <> '';

-- 6. devices (保留)
CREATE TABLE devices (
    device_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    platform TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT '',
    revoked_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_devices_token_hash ON devices(token_hash);

-- 7. pairing_codes (保留)
CREATE TABLE pairing_codes (
    code_hash TEXT PRIMARY KEY,
    expires_at TEXT NOT NULL,
    used_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

-- 8. artifacts (保留)
CREATE TABLE artifacts (
    artifact_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL,
    session_id TEXT NOT NULL DEFAULT '',
    source_tool_result_ref TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    relative_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX idx_artifacts_run ON artifacts(run_id, created_at ASC, artifact_id ASC);
CREATE INDEX idx_artifacts_session ON artifacts(session_id, created_at ASC, artifact_id ASC);

-- 9. mcp_oauth_tokens (保留)
CREATE TABLE mcp_oauth_tokens (
    provider_name TEXT PRIMARY KEY,
    access_token TEXT,
    refresh_token TEXT,
    expiry TEXT,
    updated_at TEXT
);

-- 10. schema_migrations (保留)
CREATE TABLE schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL
);
```

**删除的表**:
- `owner_profile` — 单用户系统,owner 固定。如果需要 owner 标识,用配置文件。
- `session_summaries` — summary 是 context 层的内存状态,不需要持久化(compact 边界不持久化原则)。如果需要跨 session summary,存 file-backed memory。
- `memory_vectors` — 保持惰性创建策略。首次语义检索时 CREATE TABLE IF NOT EXISTS。

**runs 表变化**:
- `updated_at` → `finished_at`(语义更清晰:run 创建时 finished_at 为空,完成时填入)
- 新增 `idx_runs_session_created` 索引优化 session 查询

### 5.7 OpenAPI / Wire Contract 重设计

**原则**: 简化端点,合并冗余,但保持 mobile 可消费。

**端点变化**:

| 操作 | 现有 | 目标 | 原因 |
|---|---|---|---|
| 保留 | `GET /healthz` | 不变 | |
| 保留 | `POST /v1/devices:pair` | 不变 | |
| 保留 | `GET/DELETE /v1/devices` | 不变 | |
| 保留 | `GET/DELETE /v1/threads/{id}` | 不变 | |
| 保留 | `POST /v1/threads/{id}/messages` | 不变 | |
| 保留 | `POST /v1/threads/{id}/runs` | 不变 | |
| 保留 | `GET /v1/runs/{id}` | 不变 | |
| 保留 | `GET /v1/runs/{id}/events` | 不变 | |
| 保留 | `POST /v1/runs/{id}:interrupt` | 不变 | |
| 保留 | `POST /v1/runs/{id}:resume` | 不变 | |
| 保留 | `GET /v1/runs/{id}/detail` | 不变 | |
| 合并 | `GET /v1/pending-actions` + `:decide` | `GET/POST /v1/pending-actions` | decide 用 POST 子资源 |
| 保留 | `GET /v1/inbox` | 不变 | |
| 保留 | `GET /v1/system/status` | 不变 | |
| 保留 | `GET /v1/tools` | 不变 | |
| 删除 | `GET /v1/settings` | 已在后续 hard-cut 中删除 | current truth 由 `/v1/system/status` 承载 |
| 保留 | `GET /v1/memory/*` | 不变 | |
| 保留 | `GET /v1/skills` | 不变 | |

**结论**: wire contract 基本不变。`pending-actions/:decide` 改为 `POST /v1/pending-actions/{id}/decide` 更 RESTful,但 mobile 需同步重新生成 client。其余保持不变。

### 5.8 OpenAPI 精简

`docs/openapi.yaml` 当前 2578 行。重设计时:
- 删除 `owner_profile` 相关 schema
- 删除 `session_summaries` 相关 schema(如果 API 暴露了)
- 精简 DTO 嵌套(如果存在冗余中间 DTO)
- 保持所有现有端点行为不变

## 6. Migration Strategy

### 6.1 Greenfield 分支策略

在 `refactor/greenfield-architecture` 分支上工作。中间状态不可编译是可接受的。

### 6.2 迁移顺序

按依赖层从底到顶:

1. **domain 重构**: 移入 store 值类型,拆分文件,添加新类型
2. **port 创建**: 定义所有 Repo 接口 + 工具契约
3. **store 重写**: 实现 port 接口,重写 schema
4. **memory 适配**: 实现新 port 接口
5. **mcp 适配**: 实现新 port 接口
6. **tools 重写**: 分离契约到 port,实现保留
7. **context 适配**: 更新 import
8. **agent 创建**: 从 runtime 提取,内联 assembler
9. **api 重写**: 吸收 app service,更新 import
10. **wire 创建**: 新组合根
11. **cli 适配**: 更新 import
12. **测试迁移**: 更新所有测试 import 和 mock

### 6.3 保留与删除

**保留(直接迁移)**:
- `internal/store/` 的 SQL 逻辑(store_run.go, store_session.go 等)
- `internal/tools/` 的工具实现(file_read.go, browser_service.go 等)
- `internal/contextplane/` 的核心逻辑(context_session.go, masking, auto_compact)
- `internal/memory/` 的全部逻辑
- `internal/providers/mcp/` 的全部逻辑
- `internal/config/` 的全部逻辑
- `internal/workspace/` 的全部逻辑
- `internal/webaccess/` 的全部逻辑
- `internal/skills/` 的全部逻辑
- `internal/stream/` 的全部逻辑
- `internal/clientevents/` 的全部逻辑
- `internal/api/` 的 HTTP handler 逻辑
- `internal/cli/` 的 CLI 逻辑
- `internal/domain/` 的全部类型
- `tests/architecture/` 的守卫测试(需更新规则)

**删除(中间层)**:
- `internal/runtime/` 的 4 个 Assembler struct
- `internal/app/MemoryService` delegate
- `internal/app/executorHandle`/`runtimeExecutorHandle`
- `internal/app/containerRuntimeStore`/`containerAppStore` 接口
- `internal/runtime/factextract/` 子包(内联到 agent)
- `internal/runtime/tooldispatch/` 子包(合入 tools)
- `internal/runtime/tooltest/` 子包(移到 tests/)

## 7. Acceptance Criteria

1. `go build ./...` 通过
2. `go test ./...` 通过(所有迁移后的测试)
3. `make lint` 通过
4. `make format-check` 通过
5. 依赖图无环:任何 internal 包 import 的 internal 包不超过 5 个
6. 无 God Interface:每个 port 接口不超过 10 个方法
7. 无纯搬运中间层:没有只 delegate 不附加逻辑的 wrapper struct
8. `domain` 包零 internal 依赖
9. `port` 包只依赖 `domain`
10. `store` 实现 `port.*Repo` 接口(编译时保证)
11. SQLite 新 schema 10 张表(owner_profile/session_summaries 删除)
12. OpenAPI 端点行为不变(除 pending-actions:decide → POST 子资源)
13. 架构守卫测试更新并通过

## 8. Risks

| 风险 | 缓解 |
|---|---|
| 测试迁移量大(现有测试 40+ 文件) | 按依赖层逐步迁移,每层迁移完立即跑测试 |
| Mobile client 需重新生成 | OpenAPI 变化极小(仅 pending-actions:decide),重新生成成本低 |
| 丢失近期重构成果 | 保留所有业务逻辑代码,只重组包结构和删除中间层 |
| `port` 包可能膨胀 | 每个 Repo 严格限制方法数 ≤10,工具契约单独文件 |
| Schema 迁移丢数据 | greenfield 重建,新 DB 直接用新 schema。旧 DB 数据不迁移(单用户,owner 可 re-pair) |
| contextplane → context 改名影响 | import path 全局替换,一次完成 |

## 9. Non-Goals (显式)

- 不做产品功能变更
- 不做 performance 优化(除非重构自然带来)
- 不做 new feature
- 不做 UI 改动
- 不改变 external dependency 版本
- 不改变 build/release 流程
- 不改变 config YAML 结构(除非 schema 变化要求)

## 10. ADR Signals

- **Durable architecture change**: 包结构从 God Package 改为 Clean Slim Layers — 需要 ADR 记录
- **Source-of-truth boundary**: store 端口从 consumer-defined 改为 store-defined — 需要 ADR 记录
- **Schema change**: 删除 owner_profile/session_summaries 表 — 需要 ADR 记录
- **Contract change**: pending-actions:decide 端点路径变化 — 需要 ADR 记录

## 11. TaskIntentDraft

```text
TaskIntentDraft:
- Outcome: internal 包结构从 God Package 重构为 Clean Slim Layers
- Goal: 依赖单向流动、窄接口、无中间层
- Success evidence: go build + go test + lint 通过,依赖图无环,无 God Interface
- Stop condition: 所有 acceptance criteria 满足
- Non-goals: 不改变产品行为,不引入新依赖,不做功能变更
- Scope: internal/ 全部包 + docs/openapi.yaml + tests/architecture/
- Risks: 测试迁移量大,mobile client 需重新生成
```

## 12. ImpactStatementDraft

```text
ImpactStatementDraft:
- Affected layers: 全部 internal 包 + tests + docs
- Owners: domain(port 新增), store(schema 重写), runtime(拆分为 agent+context+tools 吸收), app(拆分为 wire+api 吸收), tools(契约分离到 port), api(吸收 app service)
- Invariants: direct_response 唯一编排模式不变;SQLite 零 CGO 不变;file-backed memory 不变;mobile control surface 不变
- Compat: 无兼容性要求(greenfield 重建);旧 DB 数据不迁移
- Non-goals: 不改变产品行为
```

## 13. Architecture Integrity Lens

```text
Architecture Integrity Lens:
- Invariant: 依赖单向流动(domain ← port ← infra ← agent ← api ← wire ← cli),无环
- Canonical owner / contract: store 自己在 port 定义 Repo 接口,消费者只 import 需要的接口
- Responsibility overlap: 删除 4 个 Assembler(纯搬运),删除 MemoryService(delegate),删除 executorHandle(适配层)
- Higher-level simplification: 不再需要 consumer-owned store 接口;port 包集中定义所有基础设施端口
- Retirement / falsifier: runtime 包完全消失(拆为 agent+context);app 包完全消失(拆为 wire+api 吸收);containerAppStore 30+ 方法接口消失
- Verdict: proceed — 方案满足 first-principles(最小充分路径),删除冗余,无新 fallback
```
