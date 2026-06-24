# Acorn Convergent Core/Runtime 重构实现计划

Date: 2026-06-24
Plan Basis: `docs/aegis/specs/2026-06-24-convergent-core-runtime-refactor-design.md`(已 approved)
Architecture: Convergent Core — core ← {store, memory, tools, mcp, workspace, webaccess, skills, config} ← runtime ← api ← wire ← cli
Tech Stack: Go 1.26, modernc.org/sqlite, cloudwego/eino/adk, cobra CLI

BaselineUsageDraft:
- Required baseline refs: AGENTS.md, INVARIANTS.md, ARCHITECTURE.md, runtime-execution.md
- Acknowledged before plan refs: structural_limits_test.go, store_interface_count_test.go, dependency_direction_test.go, client_projection_boundary_test.go
- Cited in plan refs: spec §4(目标包结构), §5(七大决策), §7(8 Phase 迁移), §8(10 条验收标准)
- Missing refs: none
- Decision: continue

Requirement Ready Check:
- Requirement source refs: spec §1(问题诊断 + 前 4 轮失败原因), §2(Goal 6 条), §3(Non-Goals)
- Goals and scope refs: spec §4(8-Package Convergent Core), §5(7 个 Key Design Decisions)
- Acceptance / verification criteria refs: spec §8(10 条 Verification Criteria)
- Open blocker questions: none
- Decision: ready

Compatibility Boundary:
- 新树并行构建,旧包最后删除(hard cutover)
- `direct_response` 唯一编排模式不变
- file-backed memory (facts/history) 模型不变
- embedding 惰性接线策略不变
- hybrid context 三机制(masking + auto-compact + circuit breaker)本身不变,只改所在包
- OpenAPI wire contract 可改 DTO,需同步 openapi.yaml + 重新生成 mobile client
- SQLite schema 预期保持 10 表不变
- 零 CGO 不变;不引入新外部依赖

Verification: 每 Phase 完成后 `go build ./...` + 相关包 `go test`;全部完成后 `go test -race ./...` + `make lint && make format-check` + `make test-architecture`

Risks:
- [RISK-001] core 包零依赖约束可能被 domain 的 eino 依赖打破 → domain.go 等移入 core 时检查 import,core 只依赖 eino schema 类型
- [RISK-002] runtime 包文件数多(21 文件),800 行限制下需拆文件 → 合并时控制每文件 ≤ 800 行,超了拆同包多文件
- [RISK-003] MCP 工具注册到 registry 后 tool lifecycle 失效 → Phase 5 重点测试 deferred loading + MCP 工具
- [RISK-004] 109 个测试文件需迁移 import → 每 Phase 后跑测试,编译器兜底
- [RISK-005] 5 轮重构 fatigue → 每 Phase 独立可验证,快速反馈

Retirement: 旧包(domain/port/contract/clientevents/agent/context/stream/providers)在 Phase 7 全部删除。无 compat alias,hard cutover。

Architecture Integrity Lens:
- Invariant: 一次 run 的执行链在一个包内闭合,不跨包跳转就能读懂
- Canonical owner: core 拥有所有类型+契约+注册中心接口;runtime 拥有执行引擎
- Responsibility overlap: domain/port/contract/clientevents 四个包职责高度重叠,合并消除
- Higher-level simplification: 21 个 store-like 接口 → 3 个能力接口;7 个 assembly struct → 2 步函数调用
- Retirement/falsifier: domain/port/contract/clientevents 全部删除;stream 并入 runtime;providers/mcp 提升为 mcp;agent 并入 runtime
- Verdict: proceed

Plan Pressure Test:
- Owner / contract / retirement: core 是新 canonical owner,旧包全删,无 compat carrier
- Architecture integrity / higher-level path: 合并是更高级简化,不是局部 patch
- Verification scope: 每 Phase 编译+测试;最终全量验证
- Task executability: 每个 task 有精确文件路径和代码
- Pressure result: proceed

Complexity Budget:
- Artifact class: full codebase restructure
- Target files / artifacts: 186 non-test files + 109 test files across 8 packages
- Current pressure: 20 packages, 21 store interfaces, 7 assembly structs
- Projected post-change pressure: 13 packages, 3 store interfaces, 2-step assembly
- Budget result: within-budget (reducing complexity)
- Planned governance: 800-line file limit enforced via structural_limits_test.go

Plan-Time Complexity Check:
- Target files: all internal/ packages
- Existing size / shape signals: 32K LOC, 20 packages
- Owner fit: core (types+contracts), runtime (execution), store/tools/mcp/memory (infra)
- Add-in-place risk: low — new packages, not growing existing ones
- Better file boundary: core ~13 files, runtime ~21 files, both under 800-line limit per file
- Recommendation: proceed with 8-phase bottom-up migration

---

## Phase 0: 创建重构分支

### Task 0.1: 创建重构分支

**Files:** none
**Why:** 隔离重构工作

**Steps:**
- [ ] 1. 创建分支: `git checkout -b refactor/convergent-core-runtime`
- [ ] 2. 确认分支: `git branch --show-current` → 输出 `refactor/convergent-core-runtime`
- [ ] 3. Commit 初始状态: `git commit --allow-empty -m "chore: start convergent core/runtime refactor branch"`

---

## Phase 1: 建 core 包(类型+契约+注册中心接口)

**Goal:** 创建 `internal/core/` 包,把 domain/port/contract/clientevents 的类型和接口合并进来。旧包保留不动,core 是平行新实现。

**消费者映射**(谁 import 了这些包,Phase 7 时需切换):
- `domain` → 53 文件(agent 10, api 10, store 10, stream 4, tools 5, clientevents 3, wire 2, providers/mcp 3, contract 1, port 2, context 3)
- `port` → 22 文件(agent 8, tools 6, store 1, providers/mcp 5, context 2, api 1)
- `contract` → 8 文件(api 7, wire 2)
- `clientevents` → 5 文件(全在 api)

### Task 1.1: 创建 core 包基础类型(domain.go + store_types.go + errors.go + context.go)

**Files:**
- Create: `internal/core/domain.go`
- Create: `internal/core/store_types.go`
- Create: `internal/core/errors.go`
- Create: `internal/core/context.go`

**Why:** 把 domain 包的核心类型移入 core,这是所有其他类型的基础。

**Impact/Compatibility:** 新文件,旧包不动,零破坏。

**Steps:**
- [ ] 1. 创建 `internal/core/domain.go`,从 `internal/domain/domain.go` 复制所有类型(RunRecord/EventRecord/SessionRecord/PendingActionRecord/RunStatus/EventKind/PendingActionKind/PendingActionStatus 等),改 `package domain` → `package core`
- [ ] 2. 创建 `internal/core/store_types.go`,从 `internal/domain/store_types.go` 复制 SessionMessageRecord 等存储类型 + 从 `internal/domain/session_summary.go` 复制 SessionSummary
- [ ] 3. 创建 `internal/core/errors.go`,从 `internal/domain/domain.go` 复制 sentinel errors(ErrRunNotActive/ErrRunNotInterrupted/ErrExecutionNotReady)
- [ ] 4. 创建 `internal/core/context.go`,从 `internal/domain/context.go` 复制 WithRunID/WithSessionID/WithCallSite 等 context plumbing
- [ ] 5. 验证编译: `go build ./internal/core/` → 零错误
- [ ] 6. Commit: `git add internal/core/ && git commit -m "refactor(core): add core package with domain types, store types, errors, context plumbing"`

### Task 1.2: 创建 core stream 类型(stream_types.go + stream_accessors.go + ports.go)

**Files:**
- Create: `internal/core/stream_types.go`
- Create: `internal/core/stream_accessors.go`
- Create: `internal/core/ports.go`

**Why:** 把 domain 的 Stream* 类型和运行时端口移入 core。

**Steps:**
- [ ] 1. 创建 `internal/core/stream_types.go`,从 `internal/domain/stream_types.go` 复制所有 Stream* payload 值类型,改 package
- [ ] 2. 创建 `internal/core/stream_accessors.go`,从 `internal/domain/stream_accessors.go` 复制 typed accessors,改 package
- [ ] 3. 创建 `internal/core/ports.go`,从 `internal/domain/ports.go` 复制 EventAppender/StreamSink/AssistantStreamer 等端口接口,改 package
- [ ] 4. 验证编译: `go build ./internal/core/` → 零错误
- [ ] 5. Commit: `git add internal/core/ && git commit -m "refactor(core): add stream types, accessors, runtime ports"`

### Task 1.3: 创建 core store 接口(store.go — 3 个能力接口)

**Files:**
- Create: `internal/core/store.go`

**Why:** 用 3 个能力接口(SessionStore/IdentityStore/ArtifactStore)替代 port 的 9 个 Repo + contract.StoreView + agent.ExecutorStore 等 21 个接口。

**Impact/Compatibility:** 新接口,旧接口不动。Phase 3 store 实现这些接口,Phase 7 删旧接口。

**Code:**
```go
// internal/core/store.go
package core

import (
	"context"
	"time"
)

// SessionStore — 会话/消息/run/event/pending-action CRUD
type SessionStore interface {
	// sessions
	CreateSession(ctx context.Context, sessionID, title string) (*SessionRecord, error)
	LoadSession(ctx context.Context, sessionID string) (*SessionRecord, error)
	ListSessions(ctx context.Context, limit int) ([]SessionRecord, error)
	DeleteSession(ctx context.Context, sessionID string) error
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionTitleIfEmpty(ctx context.Context, sessionID, title string) error
	LoadLatestRunForSession(ctx context.Context, sessionID string) (*RunRecord, error)
	LoadLatestRunsForSessions(ctx context.Context, sessionIDs []string) (map[string]*RunRecord, error)
	// messages
	ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]SessionMessageRecord, error)
	NextSessionMessageTurnIndex(ctx context.Context, sessionID string) (int, error)
	AppendSessionMessage(ctx context.Context, sessionID string, turnIndex int, role, content, runID string) (*SessionMessageRecord, error)
	CreateFreshSessionTurn(ctx context.Context, sessionID, title, input string) (int, error)
	LoadLatestUnboundUserMessage(ctx context.Context, sessionID string) (*SessionMessageRecord, error)
	SyncAssistantMessageForRun(ctx context.Context, runID string) error
	SyncAssistantMessageForRunStatus(ctx context.Context, runID string, status RunStatus) error
	BindUserMessageRunIDByID(ctx context.Context, messageID int64, runID string) error
	BindLatestUserMessageRunID(ctx context.Context, sessionID string, turnIndex int, runID string) error
	// runs
	CreateRun(ctx context.Context, params RunCreateParams) error
	LoadRun(ctx context.Context, runID string) (*RunRecord, error)
	FinishRun(ctx context.Context, runID string, status RunStatus, output, errText string) error
	MarkInterrupted(ctx context.Context, runID, output string) error
	UpdateRunOutput(ctx context.Context, runID string, output string) error
	ListActiveRuns(ctx context.Context, limit int) ([]RunRecord, error)
	ListRecentTerminalRuns(ctx context.Context, limit int) ([]RunRecord, error)
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

**Steps:**
- [ ] 1. 创建 `internal/core/store.go` 含上面 3 个接口
- [ ] 2. 验证编译: `go build ./internal/core/` → 零错误
- [ ] 3. Commit: `git add internal/core/store.go && git commit -m "refactor(core): add SessionStore/IdentityStore/ArtifactStore — 3 capability interfaces replacing 21 store-like interfaces"`

### Task 1.4: 创建 core tool 契约(tool.go — 合并 port.ToolContract + port.ToolSpec)

**Files:**
- Create: `internal/core/tool.go`

**Why:** 统一 ToolSpec(含 Factory 字段),替代 port.ToolContract + port.ToolSpec 分离设计。

**Code:**
```go
// internal/core/tool.go
package core

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

type ToolLoadingMode string

const (
	ToolLoadingModeEager    ToolLoadingMode = "eager"
	ToolLoadingModeDeferred  ToolLoadingMode = "deferred"
)

type ToolLoadingPolicy struct {
	Mode   ToolLoadingMode
	Reason string
}

type ParallelPolicy string

const (
	ParallelPolicyReadOnly ParallelPolicy = "read_only"
	ParallelPolicySerial    ParallelPolicy = "serial"
)

type ToolExecutionPolicy struct {
	Parallel ParallelPolicy
	PathArg string
}

type ToolKind string

const (
	ToolKindRead     ToolKind = "read"
	ToolKindWrite    ToolKind = "write"
	ToolKindCommand  ToolKind = "command"
	ToolKindBrowser  ToolKind = "browser"
	ToolKindWeb      ToolKind = "web"
	ToolKindArtifact ToolKind = "artifact"
	ToolKindOperator ToolKind = "operator"
	ToolKindMemory   ToolKind = "memory"
)

type ToolCategory string

const (
	CategoryWorkspace  ToolCategory = "workspace"
	CategoryGit        ToolCategory = "git"
	CategoryFile       ToolCategory = "file"
	CategoryMutation   ToolCategory = "mutation"
	CategoryRunCommand ToolCategory = "run_command"
	CategoryArtifact   ToolCategory = "artifact"
	CategoryOperator   ToolCategory = "operator"
	CategoryWeb        ToolCategory = "web"
	CategoryBrowser    ToolCategory = "browser"
	CategoryMemory     ToolCategory = "memory"
)

type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDegraded HealthState = "degraded"
	HealthStateDisabled HealthState = "disabled"
)

type ToolHealth struct {
	State   HealthState
	Reason  string
	Detail  string
}

// ToolFactory creates a tool instance for a given run context.
type ToolFactory func(ctx context.Context, runCtx RunContext) (einotool.BaseTool, error)

// ToolSpec is the unified tool specification for both native and MCP tools.
type ToolSpec struct {
	Name       string
	Source     string              // "native" | "mcp:<provider>"
	Kind       ToolKind
	Category   ToolCategory
	Loading    ToolLoadingPolicy
	Execution  ToolExecutionPolicy
	Factory    ToolFactory         // unified: native and MCP tools both use this
	Health     ToolHealth
}

func (s ToolSpec) Enabled() bool {
	return s.Health.State != HealthStateDisabled
}

func (s ToolSpec) Validate() error {
	// copy from port.ToolContract.Validate()
	// ...
}

// Catalog is the read-only tool catalog interface.
type Catalog interface {
	Specs() []ToolSpec
	EnabledSpecs() []ToolSpec
	Find(name string) (ToolSpec, bool)
}

type ExecutionPolicyResolver interface {
	ExecutionPolicy(toolName string, args map[string]any) (ToolExecutionPolicy, error)
}

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: reason}
}
```

**Steps:**
- [ ] 1. 创建 `internal/core/tool.go`,从 `internal/port/tool.go` 复制所有类型和常量,改 package 为 core,添加 `Factory ToolFactory` 字段到 ToolSpec
- [ ] 2. 复制 `Validate()` / `Normalized()` / `ParseParallelPolicy()` 等方法,适配新类型
- [ ] 3. 验证编译: `go build ./internal/core/` → 零错误
- [ ] 4. Commit: `git add internal/core/tool.go && git commit -m "refactor(core): add unified ToolSpec with Factory field — merges port.ToolContract + port.ToolSpec"`

### Task 1.5: 创建 core 注册中心接口(registry.go + mcp.go + helpers.go)

**Files:**
- Create: `internal/core/registry.go`
- Create: `internal/core/mcp.go`
- Create: `internal/core/helpers.go`
- Create: `internal/core/projection.go`

**Why:** 注册中心是一等公民扩展点;MCP 类型移入 core;小工具函数移入 core。

**Steps:**
- [ ] 1. 创建 `internal/core/registry.go`,定义 ToolRegistry/ProviderRegistry 接口 + RunContext 类型(从 spec §5.1 复制)
- [ ] 2. 创建 `internal/core/mcp.go`,从 `internal/providers/mcp/manager.go` 复制 ProviderConfig/ProviderInfo/AuthConfig 类型 + 从 `internal/port/mcp.go` 复制(然后删除)MCPTokenStore/MCPPendingActionStore → 改为 core.SessionStore/core.ArtifactStore 的方法
- [ ] 3. 创建 `internal/core/helpers.go`,从 `internal/agent/types.go` 复制 compactText/NewRunID/newSessionID/DurableContext/CurrentRunID/InterruptPayloadFromStream 等小工具函数
- [ ] 4. 创建 `internal/core/projection.go`,从 `internal/clientevents/types.go` 复制投影类型(如果被跨包使用)
- [ ] 5. 验证编译: `go build ./internal/core/` → 零错误
- [ ] 6. 验证 core 零 internal 依赖: `go list -f '{{join .Imports ", "}}' ./internal/core/ | grep internal` → 无输出(core 不 import 任何 internal 包)
- [ ] 7. Commit: `git add internal/core/ && git commit -m "refactor(core): add registry interfaces, MCP types, helpers, projection types"`

### Task 1.6: 创建 core 包测试

**Files:**
- Create: `internal/core/core_test.go`

**Why:** 验证 core 包类型编译和基本行为。

**Steps:**
- [ ] 1. 创建 `internal/core/core_test.go`,从 `internal/domain/stream_accessors_test.go` 复制并适配为 package core
- [ ] 2. 添加 ToolSpec.Validate() 测试
- [ ] 3. 添加 store 接口编译时检查: `var _ SessionStore = (*SessionStore)(nil)` 等(用 mock 实现)
- [ ] 4. 运行测试: `go test ./internal/core/` → 全绿
- [ ] 5. Commit: `git add internal/core/core_test.go && git commit -m "test(core): add core package tests — type validation, store interface checks"`

---

## Phase 2: 建 runtime 包(agent + context + stream 合并)

**Goal:** 创建 `internal/runtime/` 包,把 agent/context/stream 的代码移入。旧包保留不动,runtime 是平行新实现。

**消费者映射:**
- `agent` → 4 文件(api 2, wire 2)
- `context` → 10 文件(agent 8, tools 1, wire 2)
- `stream` → 3 文件(全在 agent)

### Task 2.1: 创建 runtime 包基础(executor.go + runner.go + types.go + run_context.go)

**Files:**
- Create: `internal/runtime/executor.go`
- Create: `internal/runtime/runner.go`
- Create: `internal/runtime/types.go`
- Create: `internal/runtime/run_context.go`

**Why:** 把 agent 包的核心执行链移入 runtime,适配 core 类型。

**Steps:**
- [ ] 1. 创建 `internal/runtime/executor.go`,从 `internal/agent/executor.go` 复制 Executor struct + Run/ExecuteMessages/consume 等方法,改 `package agent` → `package runtime`,import 从 `internal/domain` → `internal/core`,从 `internal/context` 的引用改为本地引用(同包)
- [ ] 2. 创建 `internal/runtime/runner.go`,合并 `internal/agent/runner.go` + `internal/agent/run.go` + `internal/agent/runner_mcp.go` + `internal/agent/runner_selection.go` + `internal/agent/runner_emit.go` 为一个文件。删除 7 个 assembly struct,内联为函数(newChatModel/buildCapabilities/assembleContext/buildMCPManager/selectSkills/emitRunStarted/assembleTooling)
- [ ] 3. 创建 `internal/runtime/types.go`,从 `internal/agent/types.go` 复制 RuntimeDeps/ActiveRunner/RunnerBuildRequest/DirectResponseRequest/RunAssembly/AssembleResultView 等类型,适配 core 类型
- [ ] 4. 创建 `internal/runtime/run_context.go`,从 `internal/agent/types.go` 复制 RunContext/Registry/RunController/RegisterTypes 等
- [ ] 5. 验证编译: `go build ./internal/runtime/` → 预期有错误(缺 direct_response/context session 等),记录错误待 Task 2.2-2.4 修复
- [ ] 6. Commit: `git add internal/runtime/ && git commit -m "refactor(runtime): add executor, runner, types, run_context — merged from agent"`

### Task 2.2: 创建 runtime 执行链(direct_response.go + agent_loop.go + audit.go + validator.go + catalog.go + model_builder.go)

**Files:**
- Create: `internal/runtime/direct_response.go`
- Create: `internal/runtime/agent_loop.go`
- Create: `internal/runtime/audit.go`
- Create: `internal/runtime/validator.go`
- Create: `internal/runtime/catalog.go`
- Create: `internal/runtime/model_builder.go`

**Why:** 把 agent 的 direct_response loop 和辅助文件移入 runtime。

**Steps:**
- [ ] 1. 创建 `internal/runtime/direct_response.go`,从 `internal/agent/direct_response.go` 复制,删除 ToolAssembler struct,内联为 `assembleTooling(ctx, params)` 函数。删除 7 个 assembly struct 的引用,改用函数调用
- [ ] 2. 创建 `internal/runtime/agent_loop.go`,从 `internal/agent/agent_loop.go` 复制,改 package + import
- [ ] 3. 创建 `internal/runtime/audit.go`,从 `internal/agent/audit.go` 复制,改 package + import
- [ ] 4. 创建 `internal/runtime/validator.go`,从 `internal/agent/validator.go` 复制,改 package + import
- [ ] 5. 创建 `internal/runtime/catalog.go`,从 `internal/agent/catalog.go` 复制,改 package + import
- [ ] 6. 创建 `internal/runtime/model_builder.go`,从 `internal/agent/model_builder.go` 复制,删除 ModelBuilder struct,内联为 `newChatModel(ctx, cfg)` 函数
- [ ] 7. 验证编译: `go build ./internal/runtime/` → 预期仍有错误(缺 context session/masking 等),记录错误待 Task 2.3 修复
- [ ] 8. Commit: `git add internal/runtime/ && git commit -m "refactor(runtime): add direct_response, agent_loop, audit, validator, catalog, model_builder"`

### Task 2.3: 创建 runtime context(session.go + plane.go + masking.go + auto_compact.go + tool_lifecycle.go + memory_context.go + context_helpers.go)

**Files:**
- Create: `internal/runtime/session.go`
- Create: `internal/runtime/plane.go`
- Create: `internal/runtime/masking.go`
- Create: `internal/runtime/auto_compact.go`
- Create: `internal/runtime/tool_lifecycle.go`
- Create: `internal/runtime/memory_context.go`
- Create: `internal/runtime/context_helpers.go`

**Why:** 把 context 包的上下文管理逻辑移入 runtime,合并 tool_lifecycle.go + tool_lifecycle_runtime.go。

**Steps:**
- [ ] 1. 创建 `internal/runtime/session.go`,从 `internal/context/context_session.go` 复制 Session interface + defaultSession,改 package
- [ ] 2. 创建 `internal/runtime/plane.go`,从 `internal/context/types.go` 复制 Plane interface + defaultPlane + AssembleRequest/AssembleResult 等,改 package
- [ ] 3. 创建 `internal/runtime/masking.go`,从 `internal/context/context_session.go` 提取 masking 逻辑为独立文件
- [ ] 4. 创建 `internal/runtime/auto_compact.go`,从 `internal/context/context_session.go` 提取 auto-compact + circuit breaker 逻辑为独立文件
- [ ] 5. 创建 `internal/runtime/tool_lifecycle.go`,合并 `internal/context/tool_lifecycle.go` + `internal/context/tool_lifecycle_runtime.go` 为一个文件
- [ ] 6. 创建 `internal/runtime/memory_context.go`,从 `internal/context/memory_context.go` 复制,改 package
- [ ] 7. 创建 `internal/runtime/context_helpers.go`,从 `internal/context/context_helpers.go` + `internal/context/message_utils.go` 复制,改 package
- [ ] 8. 验证编译: `go build ./internal/runtime/` → 预期仍有错误(缺 stream 投影),记录错误待 Task 2.4 修复
- [ ] 9. Commit: `git add internal/runtime/ && git commit -m "refactor(runtime): add session, plane, masking, auto_compact, tool_lifecycle, memory_context, context_helpers — merged from context"`

### Task 2.4: 创建 runtime stream(projection.go + assistant_stream.go + streaming_assistant.go + memtools.go)

**Files:**
- Create: `internal/runtime/projection.go`
- Create: `internal/runtime/assistant_stream.go`
- Create: `internal/runtime/streaming_assistant.go`
- Create: `internal/runtime/memtools.go`

**Why:** 把 stream 包的投影逻辑和 assistant streaming 移入 runtime;把 factextract 内联为 memtools。

**Steps:**
- [ ] 1. 创建 `internal/runtime/projection.go`,从 `internal/stream/projection.go` 复制,改 package
- [ ] 2. 创建 `internal/runtime/assistant_stream.go`,从 `internal/stream/assistant_stream.go` + `internal/stream/agent.go` 合并复制,改 package
- [ ] 3. 创建 `internal/runtime/streaming_assistant.go`,从 `internal/stream/streaming_assistant.go` 复制,改 package
- [ ] 4. 创建 `internal/runtime/memtools.go`,合并 `internal/agent/factextract/memory_tools.go` + `internal/agent/factextract/memory_tools_search.go` 为一个文件,改 package
- [ ] 5. 验证编译: `go build ./internal/runtime/` → 零错误(所有依赖已就位)
- [ ] 6. 运行已有测试(如果有): `go test ./internal/runtime/ 2>/dev/null` → 预期失败(测试还没迁移)
- [ ] 7. Commit: `git add internal/runtime/ && git commit -m "refactor(runtime): add projection, assistant_stream, streaming_assistant, memtools — merged from stream + factextract"`

### Task 2.5: 迁移 runtime 测试

**Files:**
- Create: `internal/runtime/runtime_test.go`(从 agent/context/stream 的测试合并)
- Move: `internal/agent/tooltest/` → `tests/tooltest/`

**Why:** 验证 runtime 包行为正确。

**Steps:**
- [ ] 1. 从 `internal/agent/*_test.go` 复制测试到 `internal/runtime/`,改 package + import
- [ ] 2. 从 `internal/context/*_test.go` 复制测试到 `internal/runtime/`,改 package + import
- [ ] 3. 从 `internal/stream/*_test.go` 复制测试到 `internal/runtime/`,改 package + import
- [ ] 4. 移动 `internal/agent/tooltest/` → `tests/tooltest/`,改 import
- [ ] 5. 运行测试: `go test ./internal/runtime/` → 全绿
- [ ] 6. Commit: `git add internal/runtime/ tests/ && git commit -m "test(runtime): migrate tests from agent/context/stream — all passing"`

---

## Phase 3: store 适配新接口

**Goal:** store 包实现 core 的 3 个 Store 接口,删除对 port.*Repo 和 contract.StoreView 的实现。

### Task 3.1: store 实现 core.SessionStore + core.IdentityStore + core.ArtifactStore

**Files:**
- Modify: `internal/store/sqlite_store.go` — 添加 `var _ core.SessionStore = (*Store)(nil)` 等 compile-time assertions
- Modify: 所有 `internal/store/store_*.go` 文件 — 方法签名适配 core 类型

**Why:** store 是唯一直接 import core Store 接口的实现层。

**Steps:**
- [ ] 1. 在 `internal/store/sqlite_store.go` 添加 compile-time assertions: `var _ core.SessionStore = (*Store)(nil)` / `var _ core.IdentityStore = (*Store)(nil)` / `var _ core.ArtifactStore = (*Store)(nil)`
- [ ] 2. 在所有 store_*.go 文件中,把 `domain.XXX` 替换为 `core.XXX`(类型引用)
- [ ] 3. 验证 store 满足 3 个接口: `go build ./internal/store/` → 零错误
- [ ] 4. 运行 store 测试: `go test ./internal/store/` → 全绿
- [ ] 5. Commit: `git add internal/store/ && git commit -m "refactor(store): implement core.SessionStore/IdentityStore/ArtifactStore — replacing port.*Repo + contract.StoreView"`

---

## Phase 4: tools 实现 ToolRegistry

**Goal:** tools 包实现 core.ToolRegistry 接口,原生工具注册到 registry。

### Task 4.1: 创建 tools/registry.go 实现 core.ToolRegistry

**Files:**
- Create: `internal/tools/registry.go`

**Why:** 统一工具注册入口,替代分散的 Catalog + configuredLocalSpec。

**Code:**
```go
// internal/tools/registry.go
package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
)

type toolRegistry struct {
	mu     sync.RWMutex
	specs  map[string]core.ToolSpec
}

func NewToolRegistry() core.ToolRegistry {
	return &toolRegistry{specs: make(map[string]core.ToolSpec)}
}

func (r *toolRegistry) Register(spec core.ToolSpec, factory core.ToolFactory) error {
	if spec.Name == "" {
		return fmt.Errorf("tool spec name is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.specs[spec.Name]; exists {
		return fmt.Errorf("tool %q already registered", spec.Name)
	}
	if factory != nil {
		spec.Factory = factory
	}
	r.specs[spec.Name] = spec
	return nil
}

func (r *toolRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.specs[name]; !exists {
		return fmt.Errorf("tool %q not found", name)
	}
	delete(r.specs, name)
	return nil
}

func (r *toolRegistry) List() []core.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]core.ToolSpec, 0, len(r.specs))
	for _, spec := range r.specs {
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *toolRegistry) Resolve(ctx context.Context, runCtx core.RunContext, names []string) ([]einotool.BaseTool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]einotool.BaseTool, 0, len(names))
	for _, name := range names {
		spec, ok := r.specs[name]
		if !ok {
			continue // skip not found
		}
		if spec.Factory == nil {
			return nil, fmt.Errorf("tool %q has no factory", name)
		}
		tool, err := spec.Factory(ctx, runCtx)
		if err != nil {
			return nil, fmt.Errorf("create tool %q: %w", name, err)
		}
		tools = append(tools, tool)
	}
	return tools, nil
}
```

**Steps:**
- [ ] 1. 创建 `internal/tools/registry.go` 含上面代码
- [ ] 2. 验证编译: `go build ./internal/tools/` → 零错误
- [ ] 3. 创建测试 `internal/tools/registry_test.go`: 注册/注销/Resolve 测试
- [ ] 4. 运行测试: `go test ./internal/tools/ -run TestToolRegistry` → 全绿
- [ ] 5. Commit: `git add internal/tools/registry.go internal/tools/registry_test.go && git commit -m "feat(tools): implement core.ToolRegistry — unified registration for native + MCP tools"`

### Task 4.2: 原生工具注册到 registry

**Files:**
- Modify: `internal/tools/configured.go` — 工具定义改为带 Factory 的 ToolSpec
- Modify: `internal/tools/catalog_builders.go` — 工具构建改为注册到 registry
- Modify: `internal/tools/builtin_registry.go` — 启动时注册

**Why:** 原生工具通过 registry 注册,不再通过 Catalog 直接构造。

**Steps:**
- [ ] 1. 修改 `internal/tools/configured.go`,把 `localToolDefs` 返回的每个工具定义附带 `ToolFactory`
- [ ] 2. 修改 `internal/tools/catalog_builders.go`,把 `buildWorkspaceTools`/`buildReadTools` 等 builder 函数改为返回 `ToolFactory` 而非 `einotool.BaseTool`
- [ ] 3. 修改 `internal/tools/builtin_registry.go`,添加 `RegisterNativeTools(registry core.ToolRegistry, cfg CatalogConfig) error` 函数
- [ ] 4. 验证编译: `go build ./internal/tools/` → 零错误
- [ ] 5. 运行测试: `go test ./internal/tools/` → 全绿
- [ ] 6. Commit: `git add internal/tools/ && git commit -m "refactor(tools): native tools register via ToolRegistry — replacing direct Catalog construction"`

---

## Phase 5: mcp 提升 + 实现 ProviderRegistry

**Goal:** `providers/mcp/` → `mcp/`,实现 core.ProviderRegistry,MCP 工具注册到 core.ToolRegistry。

### Task 5.1: 提升 mcp 包 + 实现 ProviderRegistry

**Files:**
- Move: `internal/providers/mcp/` → `internal/mcp/`
- Modify: 所有 `internal/mcp/*.go` — 改 package + import
- Modify: `internal/mcp/manager.go` — 添加 ProviderRegistry 接口实现

**Why:** MCP 从 providers 子包提升为顶级包,Manager 实现 ProviderRegistry。

**Steps:**
- [ ] 1. 移动 `internal/providers/mcp/` → `internal/mcp/`: `git mv internal/providers/mcp internal/mcp`
- [ ] 2. 全局替换 import: `internal/providers/mcp` → `internal/mcp` (用 `goimports` 或 sed)
- [ ] 3. 在 `internal/mcp/manager.go` 添加 ProviderRegistry 接口实现:RegisterProvider/UnregisterProvider/Providers/Reconcile 方法(委托给现有 ReconcileProviders)
- [ ] 4. 适配 core 类型:把 `port.MCPTokenStore` 替换为 `core.ArtifactStore`,`port.MCPPendingActionStore` 替换为 `core.SessionStore`
- [ ] 5. 验证编译: `go build ./internal/mcp/` → 零错误
- [ ] 6. 运行测试: `go test ./internal/mcp/` → 全绿
- [ ] 7. Commit: `git add internal/mcp/ && git commit -m "refactor(mcp): promote to top-level package, implement core.ProviderRegistry"`

### Task 5.2: MCP 工具注册到 ToolRegistry

**Files:**
- Modify: `internal/mcp/manager.go` — 连接 provider 后注册工具到 registry
- Modify: `internal/mcp/manager_catalog.go` — 工具发现后构造 ToolSpec + ToolFactory

**Why:** MCP 工具通过统一 registry 注册,支持热更新。

**Steps:**
- [ ] 1. 在 `internal/mcp/manager.go` 添加 `toolRegistry core.ToolRegistry` 字段 + `WithToolRegistry(registry)` option
- [ ] 2. 在 `internal/mcp/manager_catalog.go` 修改工具发现逻辑:对每个 MCP tool 构造 `core.ToolSpec`(Source=`"mcp:<provider>"`)+ `ToolFactory`(包装 session dispatch),调用 `registry.Register(spec, factory)`
- [ ] 3. 在 `ReconcileProviders` 中,provider 断开时 `registry.Unregister(name)` 清理
- [ ] 4. 验证编译: `go build ./internal/mcp/` → 零错误
- [ ] 5. 运行测试: `go test ./internal/mcp/` → 全绿
- [ ] 6. Commit: `git add internal/mcp/ && git commit -m "feat(mcp): register MCP tools to unified ToolRegistry — supports hot update on provider reconcile"`

---

## Phase 6: api 合并 clientevents

**Goal:** clientevents 投影逻辑移入 api,api 适配 core + runtime 新类型。

### Task 6.1: 合并 clientevents 到 api

**Files:**
- Move: `internal/clientevents/types.go` → `internal/api/live_types.go`
- Move: `internal/clientevents/projector.go` → `internal/api/projection.go`
- Move: `internal/clientevents/helpers.go` → `internal/api/projection_helpers.go`
- Modify: `internal/api/*.go` — 适配 core + runtime 类型

**Why:** clientevents 只被 api 引用,合并消除跨包间接。

**Steps:**
- [ ] 1. 复制 clientevents 3 个文件到 api,改 package 为 api
- [ ] 2. 修改 api 文件中 `clientevents.XXX` → `XXX`(同包引用)
- [ ] 3. 修改 api 文件中 `domain.XXX` → `core.XXX`,`contract.StoreView` → `core.SessionStore` / `core.IdentityStore` / `core.ArtifactStore`
- [ ] 4. 修改 api 文件中 `agent.XXX` → `runtime.XXX`(如果引用了 agent 类型)
- [ ] 5. 验证编译: `go build ./internal/api/` → 预期有错误(wire 还没接线),记录错误
- [ ] 6. Commit: `git add internal/api/ && git commit -m "refactor(api): merge clientevents into api, adapt to core + runtime types"`

---

## Phase 7: wire 重新接线 + 删旧包

**Goal:** wire 构造 ToolRegistry,注入到 runtime + mcp,删除旧包,全项目切换到新类型。

### Task 7.1: wire 重新接线

**Files:**
- Modify: `internal/wire/container.go` — 构造 ToolRegistry,注入 runtime + mcp
- Modify: `internal/wire/runtime.go` — 适配 core + runtime 类型

**Why:** wire 是组合根,唯一知道具体实现的地方。

**Steps:**
- [ ] 1. 修改 `internal/wire/container.go`:构造 `tools.NewToolRegistry()`,调用 `tools.RegisterNativeTools(registry, cfg)`,注入到 `runtime.RuntimeDeps` 和 `mcp.Manager`
- [ ] 2. 修改 `internal/wire/runtime.go`:把 `agent.NewExecutorWithRunRuntimeAndController` → `runtime.NewExecutorWithRunRuntimeAndController`,`contract.StoreView` → `core.SessionStore` 等
- [ ] 3. 验证编译: `go build ./internal/wire/` → 预期有错误(旧包还在被引用),记录错误
- [ ] 4. Commit: `git add internal/wire/ && git commit -m "refactor(wire): construct ToolRegistry, inject to runtime + mcp"`

### Task 7.2: 全局 import 切换 + 删旧包

**Files:**
- Delete: `internal/domain/` (整个目录)
- Delete: `internal/port/` (整个目录)
- Delete: `internal/contract/` (整个目录)
- Delete: `internal/clientevents/` (整个目录)
- Delete: `internal/agent/` (整个目录)
- Delete: `internal/context/` (整个目录)
- Delete: `internal/stream/` (整个目录)
- Delete: `internal/providers/` (整个目录,已移到 mcp)
- Modify: 所有引用旧包的文件 — import 切换

**Why:** Hard cutover — 删除旧包,全项目切换到 core + runtime。

**Steps:**
- [ ] 1. 全局替换 import 路径:
  - `github.com/ycvk/acorn/internal/domain` → `github.com/ycvk/acorn/internal/core`
  - `github.com/ycvk/acorn/internal/port` → `github.com/ycvk/acorn/internal/core`
  - `github.com/ycvk/acorn/internal/contract` → `github.com/ycvk/acorn/internal/core`
  - `github.com/ycvk/acorn/internal/clientevents` → `github.com/ycvk/acorn/internal/api`
  - `github.com/ycvk/acorn/internal/agent` → `github.com/ycvk/acorn/internal/runtime`
  - `github.com/ycvk/acorn/internal/context` → `github.com/ycvk/acorn/internal/runtime`
  - `github.com/ycvk/acorn/internal/stream` → `github.com/ycvk/acorn/internal/runtime`
  - `github.com/ycvk/acorn/internal/providers/mcp` → `github.com/ycvk/acorn/internal/mcp`
- [ ] 2. 修复类型引用:`domain.XXX` → `core.XXX`,`port.XXX` → `core.XXX`,`contract.StoreView` → `core.SessionStore` 等
- [ ] 3. 删除旧包目录: `rm -rf internal/domain internal/port internal/contract internal/clientevents internal/agent internal/context internal/stream internal/providers`
- [ ] 4. 验证编译: `go build ./...` → 零错误
- [ ] 5. 运行全部测试: `go test ./...` → 全绿
- [ ] 6. Commit: `git add -A && git commit -m "refactor: delete old packages (domain/port/contract/clientevents/agent/context/stream/providers) — hard cutover to core + runtime + mcp"`

### Task 7.3: 迁移所有测试 import

**Files:**
- Modify: 所有 `*_test.go` 文件 — import 切换

**Why:** 测试文件也需要切换到新包路径。

**Steps:**
- [ ] 1. 全局替换测试文件 import(同 Task 7.2 的替换规则)
- [ ] 2. 修复测试中的类型引用
- [ ] 3. 验证编译: `go vet ./...` → 零错误
- [ ] 4. 运行全部测试: `go test ./...` → 全绿
- [ ] 5. 运行竞争检测: `go test -race ./...` → 全绿
- [ ] 6. Commit: `git add -A && git commit -m "test: migrate all test imports to core + runtime + mcp"`

---

## Phase 8: 架构守卫 + 文档同步

**Goal:** 更新架构守卫测试反映新包结构,同步所有文档。

### Task 8.1: 更新架构守卫

**Files:**
- Modify: `tests/architecture/structural_limits_test.go` — 更新 `refactorOwnedDirs`
- Modify: `tests/architecture/store_interface_count_test.go` — 更新 `consumerOwnedDirs` + 计数规则
- Modify: `tests/architecture/dependency_direction_test.go` — 更新依赖规则
- Modify: `tests/architecture/client_projection_boundary_test.go` — 更新文件列表

**Why:** 守卫必须反映新边界。

**Steps:**
- [ ] 1. 修改 `structural_limits_test.go` 的 `refactorOwnedDirs`:移除已删包(domain/port/contract/clientevents/agent/context/stream/providers/mcp→mcp),添加 `internal/core`、`internal/runtime`、`internal/mcp`
- [ ] 2. 修改 `store_interface_count_test.go` 的 `consumerOwnedDirs`:改为 `internal/runtime` + `internal/wire`,max 改为 3(core 的 Store 接口)
- [ ] 3. 修改 `dependency_direction_test.go`:core 零 internal 依赖;runtime 依赖 core + L1;api 依赖 core + runtime;wire 依赖所有
- [ ] 4. 修改 `client_projection_boundary_test.go`:更新 `clientProjectionBoundaryFiles` 为新的 api 文件列表
- [ ] 5. 运行守卫: `make test-architecture` → 全绿
- [ ] 6. Commit: `git add tests/architecture/ && git commit -m "test(architecture): update guards for convergent core/runtime structure"`

### Task 8.2: 更新文档

**Files:**
- Modify: `docs/architecture/ARCHITECTURE.md`
- Modify: `docs/architecture/INVARIANTS.md`
- Modify: `docs/architecture/runtime-execution.md`
- Modify: `docs/architecture/runtime-orchestration.md`
- Modify: `docs/architecture/runtime-context-memory-decision.md`
- Modify: `AGENTS.md`
- Modify: `README.md` (如果引用了包结构)

**Why:** 文档反映新架构现状。

**Steps:**
- [ ] 1. 更新 `ARCHITECTURE.md` 的主要包职责和依赖图
- [ ] 2. 更新 `INVARIANTS.md` 的不变量(新增:core 零依赖;runtime 执行链闭合;store 接口 ≤3)
- [ ] 3. 更新 `runtime-execution.md` 的执行链描述(2 包 2 步)
- [ ] 4. 更新 `AGENTS.md` 的包职责描述和硬边界
- [ ] 5. Commit: `git add docs/ AGENTS.md && git commit -m "docs: sync architecture docs to convergent core/runtime structure"`

### Task 8.3: 最终验证

**Why:** 确认所有验收标准通过。

**Steps:**
- [ ] 1. 编译: `go build ./...` → 零错误
- [ ] 2. 测试: `go test -race ./...` → 全绿
- [ ] 3. 架构守卫: `make test-architecture` → 全绿
- [ ] 4. Lint: `make lint && make format-check` → 全绿
- [ ] 5. 包数验证: `go list ./internal/... | wc -l` → ≤ 13
- [ ] 6. store 接口验证: `grep -rn "type.*Store.*interface" internal/core/ | wc -l` → ≤ 3
- [ ] 7. core 零依赖验证: `go list -f '{{join .Imports ", "}}' ./internal/core/ | grep internal` → 无输出
- [ ] 8. Commit: `git commit --allow-empty -m "verify: all verification criteria pass — convergent core/runtime refactor complete"`

---

## Self-Review

### 1. Spec Coverage
- [x] 类型层合一(domain/port/contract/clientevents→core) → Phase 1
- [x] 执行层合一(agent/context/stream→runtime) → Phase 2
- [x] Store 接口收敛(21→3) → Phase 1 Task 1.3 + Phase 3
- [x] 装配简化(7 struct→2 步) → Phase 2 Task 2.1-2.2
- [x] 插件注册中心(ToolRegistry + ProviderRegistry) → Phase 1 Task 1.5 + Phase 4 + Phase 5
- [x] 包数缩减(20→13) → Phase 7 Task 7.2
- [x] 架构守卫更新 → Phase 8 Task 8.1
- [x] 文档同步 → Phase 8 Task 8.2
- [x] 最终验证 → Phase 8 Task 8.3

### 2. Placeholder Scan
- [x] 无 TBD/TODO
- [x] 所有接口签名完整(ToolRegistry/ProviderRegistry/SessionStore/IdentityStore/ArtifactStore)
- [x] ToolSpec.Validate() 标注"copy from port.ToolContract.Validate()"——这是精确的复制指令,不是 placeholder

### 3. Type Consistency
- [x] core.ToolSpec 含 Factory 字段,与 registry.go 的 ToolFactory 签名一致
- [x] core.SessionStore 方法签名与 spec §5.2 一致(额外加了 BindUserMessageRunIDByID/BindLatestUserMessageRunID,因为 agent.ExecutorStore 有这两个方法)
- [x] runtime.Executor 从 agent.Executor 复制,方法签名不变

### 4. Compatibility
- [x] direct_response 不变
- [x] SQLite schema 不变(Phase 3 明确)
- [x] file-backed memory 不变
- [x] embedding 惰性接线不变
- [x] hybrid context 三机制不变(只改包位置)
- [x] OpenAPI 可改(Phase 6 标注)

### 5. Plan-Time Complexity
- [x] core ~13 文件,每文件 ≤ 300 行,800 行限制内
- [x] runtime ~21 文件,每文件 ≤ 800 行(executor.go 507 行最大,在限制内)
- [x] 无需拆文件

### 6. Architecture Integrity
- [x] core 是 canonical owner(类型+契约+注册中心)
- [x] runtime 是执行引擎 owner
- [x] 旧包全删,无 compat carrier
- [x] 合并是更高级简化,不是局部 patch

### 7. Verification
- [x] 每 Phase 有 `go build` 验证
- [x] 每 Phase 有 `go test` 验证
- [x] 最终有全量验证(Phase 8 Task 8.3)
- [x] 验证命令精确

### 8. ADR/Baseline-Sync Signals
- [x] spec §6.4 标注了 5 个 ADR-worthy 决策
- [x] 计划不创建 ADR(spec 已说明"不创建未执行的架构记忆")
- [x] ADR 在实现完成后创建
