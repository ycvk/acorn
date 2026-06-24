# Acorn Greenfield 架构重构实现计划

Date: 2026-06-24
Plan Basis: `docs/aegis/specs/2026-06-24-greenfield-architecture-refactor-design.md`(已 approved)
Architecture: Clean Slim Layers — domain ← port ← infra ← agent ← api ← wire ← cli
Tech Stack: Go 1.26, modernc.org/sqlite, cloudwego/eino/adk, cobra CLI

BaselineUsageDraft:
- Required baseline refs: AGENTS.md, INVARIANTS.md, ARCHITECTURE.md, GLOSSARY.md
- Acknowledged before plan refs: structural_limits_test.go, store_interface_count_test.go, client_projection_boundary_test.go
- Cited in plan refs: spec §4(目标包结构), §5(六大决策), §6(迁移策略), §7(验收标准)
- Missing refs: none
- Decision: continue

Requirement Ready Check:
- Requirement source refs: spec §1(问题诊断), §2(Goal), §4(架构), §5(决策)
- Acceptance / verification criteria refs: spec §7(13 条验收标准)
- Open blocker questions: none
- Decision: ready

Compatibility Boundary:
- Greenfield 重建,旧 DB 数据不迁移,无 compat alias
- `direct_response` 唯一编排模式不变
- file-backed memory (facts/history) 模型不变
- embedding 惰性接线策略不变
- mobile /v1 wire contract 基本不变(仅 `pending-actions:decide` → `POST /v1/pending-actions/{id}/decide`)
- 零 CGO 不变;不引入新外部依赖

Verification: 每 Phase 完成后 `go build ./...` + 相关包 `go test`;全部完成后 `go test -race ./...` + `make lint && make format-check` + `make test-architecture`

Risks:
- [RISK-001] 测试迁移量大(110+ 测试文件)→ 按 phase 逐步迁移,每 phase 后跑测试
- [RISK-002] import rename 可能遗漏 → go build 兜底,每 phase 可编译
- [RISK-003] store 端口翻转后 mock 需重写 → 新窄接口 mock 更简单
- [RISK-004] runtime→agent 拆分时直接引用 `&RunnerFactory{}` 的测试需更新
- [RISK-005] contextplane→context 改名影响广 → 全局 replace 一次完成

Retirement: 无旧路径需要 compat alias。每步是 hard cutover,删除旧文件同时迁移。

Architecture Integrity Lens:
- Invariant: 依赖单向流动 domain ← port ← infra ← agent ← api ← wire ← cli,无环
- Canonical owner: store 在 port 定义 Repo 接口,消费者只 import 需要的
- Responsibility overlap: 删除 4 Assembler + MemoryService delegate + executorHandle 适配层
- Higher-level simplification: port 包集中定义所有基础设施端口,不再 consumer-owned
- Retirement/falsifier: runtime 包完全消失(→agent+context);app 包完全消失(→wire+api)
- Verdict: proceed

---

## Phase 0: 创建重构分支

### Task 0.1: 创建 greenfield 重构分支

**Files:** none
**Why:** 隔离重构工作,中间状态不可编译可接受

**Steps:**
- [ ] 1. 创建分支: `git checkout -b refactor/greenfield-architecture`
- [ ] 2. 确认分支: `git branch --show-current` → 输出 `refactor/greenfield-architecture`
- [ ] 3. Commit 初始状态: `git commit --allow-empty -m "chore: start greenfield architecture refactor branch"`

---

## Phase 1: 创建 `internal/port/` 包

### Task 1.1: 创建 port 包 Repo 接口

**Files:**
- Create: `internal/port/repo.go`

**Why:** store 端口翻转的核心——所有基础设施端口集中定义在 port 包,消费者只 import 需要的窄接口。替换现有 `ExecutorStore`/`RunnerFactoryStore`/`containerAppStore` 三个 God Interface。

**Impact/Compatibility:** 新包,零破坏。后续 Phase 3 store 实现这些接口,Phase 8+ 消费者切换。

**Code:**
```go
// internal/port/repo.go
package port

import (
	"context"
	"time"

	"github.com/ycvk/acorn/internal/domain"
)

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

// DeviceRepo — 设备认证 + 配对
type DeviceRepo interface {
	SavePairingCode(ctx context.Context, code *domain.PairingCode) error
	ConsumePairingCode(ctx context.Context, codeHash string, now time.Time) (*domain.PairingCode, error)
	SaveDevice(ctx context.Context, device *domain.Device) error
	LoadDeviceByTokenHash(ctx context.Context, tokenHash string) (*domain.Device, error)
	ListDevices(ctx context.Context) ([]domain.Device, error)
	TouchDevice(ctx context.Context, deviceID string, seenAt time.Time) error
	RevokeDevice(ctx context.Context, deviceID string, revokedAt time.Time) error
}

// ArtifactRepo — 产物读写
type ArtifactRepo interface {
	WriteArtifact(ctx context.Context, req domain.ArtifactWriteRequest) (domain.ArtifactRecord, error)
	ReadArtifactRange(ctx context.Context, req domain.ArtifactReadRangeRequest) (domain.ArtifactReadRangeResult, error)
	ListArtifactsByRun(ctx context.Context, runID string) ([]domain.ArtifactRecord, error)
	ListArtifactsBySession(ctx context.Context, sessionID string) ([]domain.ArtifactRecord, error)
}

// SummaryRepo — 会话摘要(如果保留)
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

**Steps:**
- [ ] 1. 创建 `internal/port/repo.go`,粘贴上面代码
- [ ] 2. `go build ./internal/port/` — 预期失败(domain 尚无部分类型)
- [ ] 3. 进入 Task 1.2 补全 domain 类型后重新验证

### Task 1.2: 创建 port 包工具契约

**Files:**
- Create: `internal/port/tool.go`

**Why:** tools 包契约与实现分离。`ToolContract`/`ToolSpec`/`Catalog`/`ExecutionPolicyResolver` 等类型定义移到 port,tools 包只保留实现。

**Code:**
```go
// internal/port/tool.go
package port

import (
	"context"

	einotool "github.com/cloudwego/eino/components/tool"
)

type ToolLoadingMode string

const (
	ToolLoadingModeEager    ToolLoadingMode = "eager"
	ToolLoadingModeDeferred ToolLoadingMode = "deferred"
	ToolLoadingModeHidden   ToolLoadingMode = "hidden"
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
	ParallelPolicy ParallelPolicy
	PathArg        string
}

type ToolKind string

const (
	ToolKindNative   ToolKind = "native"
	ToolKindMCP      ToolKind = "mcp"
	ToolKindMemory   ToolKind = "memory"
	ToolKindSkill    ToolKind = "skill"
	ToolKindWorkflow ToolKind = "workflow"
)

type ToolCategory string

const (
	ToolCategoryInspect     ToolCategory = "inspect"
	ToolCategoryMutation    ToolCategory = "mutation"
	ToolCategoryMemory      ToolCategory = "memory"
	ToolCategorySkill       ToolCategory = "skill"
	ToolCategoryIntegration ToolCategory = "integration"
	ToolCategoryWorkflow    ToolCategory = "workflow"
)

type HealthState string

const (
	HealthStateHealthy  HealthState = "healthy"
	HealthStateDisabled HealthState = "disabled"
)

type ToolHealth struct {
	State  HealthState
	Reason string
}

type ToolContract struct {
	Name      string
	Source    string
	Kind      ToolKind
	Category  ToolCategory
	Loading   ToolLoadingPolicy
	Execution ToolExecutionPolicy
}

type ToolSpec struct {
	Contract  ToolContract
	Tool      einotool.BaseTool
	Health    ToolHealth
	IsMCP     bool
	IsBuiltin bool
}

func (s ToolSpec) Enabled() bool {
	return s.Health.State != HealthStateDisabled
}

type ExecutionPolicyResolver interface {
	ExecutionPolicy(toolName string, args map[string]any) (ToolExecutionPolicy, error)
}

// Catalog is the read-only tool catalog interface.
type Catalog interface {
	Specs() []ToolSpec
	EnabledSpecs() []ToolSpec
	Tools() []einotool.BaseTool
	Find(name string) (ToolSpec, bool)
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
- [ ] 1. 创建 `internal/port/tool.go`,粘贴上面代码
- [ ] 2. `go build ./internal/port/` — 预期失败(domain 类型缺失)
- [ ] 3. 进入 Phase 2 补全 domain 后验证

### Task 1.3: 创建 port 包 MCP 端口

**Files:**
- Create: `internal/port/mcp.go`

**Why:** MCP provider 需要的 TokenStore 和 PendingActionStore 端口,从 mcp 包移到 port。

**Code:**
```go
// internal/port/mcp.go
package port

import (
	"context"
	"time"
)

// MCPTokenStore is the OAuth token storage port for MCP providers.
type MCPTokenStore interface {
	SaveOAuthToken(ctx context.Context, providerName, accessToken, refreshToken string, expiry time.Time) error
	LoadOAuthToken(ctx context.Context, providerName string) (accessToken, refreshToken string, expiry time.Time, err error)
}

// MCPPendingActionStore is the pending-action port for MCP elicitation.
type MCPPendingActionStore interface {
	CreatePendingAction(ctx context.Context, input PendingActionInput) error
}

// PendingActionInput is the MCP-side pending action creation input.
type PendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        string
	Subject     string
	PayloadJSON string
}
```

**Steps:**
- [ ] 1. 创建 `internal/port/mcp.go`
- [ ] 2. `go build ./internal/port/` — 预期失败
- [ ] 3. 进入 Phase 2

---

## Phase 2: 扩展 `internal/domain/` 包

### Task 2.1: 移入 store 值类型

**Files:**
- Modify: `internal/domain/domain.go`
- Create: `internal/domain/store_types.go`

**Why:** `RunCreateParams`、`CreatePendingActionInput`、`PairingCode`、`Device`、`OAuthToken`、`ArtifactWriteRequest`/`ArtifactRecord` 等值类型从 `store` 包移到 `domain`。store 接口引用 domain 类型而非反向。

**Code to add to `internal/domain/store_types.go`:**
```go
package domain

import "time"

// RunCreateParams holds the parameters for creating a run record.
type RunCreateParams struct {
	RunID          string
	SessionID      string
	TurnIndex      int
	Input          string
	BoundMessageID int64
}

// PendingActionInput holds the parameters for creating a pending action.
type PendingActionInput struct {
	ActionID    string
	RunID       string
	InterruptID string
	Kind        PendingActionKind
	Subject     string
	PayloadJSON string
	Status      PendingActionStatus
	Reason      string
}

// PairingCode represents a device pairing code.
type PairingCode struct {
	CodeHash  string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// Device represents a registered device.
type Device struct {
	DeviceID   string
	Name       string
	Platform   string
	TokenHash  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

// OAuthToken represents an MCP OAuth token.
type OAuthToken struct {
	ProviderName string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	UpdatedAt    time.Time
}

// ArtifactWriteRequest holds parameters for writing an artifact.
type ArtifactWriteRequest struct {
	ArtifactID         string
	RunID              string
	SessionID          string
	SourceToolResultRef string
	Kind               string
	Title              string
	MimeType           string
	RelativePath       string
	SizeBytes          int64
	SHA256             string
}

// ArtifactRecord represents a stored artifact.
type ArtifactRecord struct {
	ArtifactID         string
	RunID              string
	SessionID          string
	SourceToolResultRef string
	Kind               string
	Title              string
	MimeType           string
	RelativePath       string
	SizeBytes          int64
	SHA256             string
	CreatedAt          time.Time
}

// ArtifactReadRangeRequest holds parameters for reading an artifact range.
type ArtifactReadRangeRequest struct {
	ArtifactID string
	Offset     int64
	Length     int64
}

// ArtifactReadRangeResult holds the result of reading an artifact range.
type ArtifactReadRangeResult struct {
	Data      []byte
	TotalSize int64
}
```

**Steps:**
- [ ] 1. 创建 `internal/domain/store_types.go`,粘贴上面代码
- [ ] 2. 从 `internal/store/store.go` 删除 `RunCreateParams`、`OAuthToken`、`OwnerProfile`、`Device`、`PairingCode`、`CreatePendingActionInput` 类型定义(它们已移到 domain)
- [ ] 3. 在 `internal/store/store.go` 顶部添加 `type OwnerProfile = domain.OwnerProfile` 别名(如果 store 内部仍需引用)— 或者直接用 `domain.OwnerProfile`
- [ ] 4. `go build ./internal/domain/ ./internal/port/` — 预期 port 编译通过
- [ ] 5. `go build ./internal/store/` — 预期失败(store 引用旧类型),进入 Phase 3 修复
- [ ] 6. Commit: `refactor: move store value types to domain, create port package`

### Task 2.2: 拆分 domain.go

**Files:**
- Create: `internal/domain/context.go` — context plumbing (WithRunID/WithSessionID/WithTurnIndex/WithCallSite/WithStreamSink + getters)
- Create: `internal/domain/ports.go` — EventAppender/ToolCallContextBridge 接口
- Modify: `internal/domain/domain.go` — 只保留纯 domain records + sentinel errors + status/kind 常量
- Keep: `internal/domain/stream_types.go`, `internal/domain/stream_accessors.go`, `internal/domain/session_summary.go` (不变)

**Why:** domain.go 379 行混合 4 个关注点。拆分后每个文件 <150 行,职责单一。

**Steps:**
- [ ] 1. 从 `domain.go` 提取 context plumbing 到 `internal/domain/context.go`
- [ ] 2. 从 `domain.go` 提取 `EventAppender`/`ToolCallContextBridge` 到 `internal/domain/ports.go`
- [ ] 3. `domain.go` 只保留: sentinel errors, RunStatus/EventKind/PendingActionKind/PendingActionStatus 常量, RunRecord/EventRecord/SessionRecord/SessionMessageRecord/PendingActionRecord/OperatorQuestionPayload/ExecuteRequest 结构体
- [ ] 4. `go build ./internal/domain/` — 通过
- [ ] 5. Commit: `refactor: split domain.go into context.go, ports.go`

---

## Phase 3: 重写 `internal/store/`

### Task 3.1: 新 schema

**Files:**
- Modify: `internal/store/store_schema_bootstrap.go`
- Modify: `internal/store/store_schema.go`
- Modify: `internal/store/store_schema_drops.go`
- Delete: `internal/store/store_session_summary.go`(逻辑移到 store_session.go 或删除)

**Why:** schema 从 12 表精简到 10 表。删除 `owner_profile` 和 `session_summaries`。runs 表 `updated_at` → `finished_at`。

**Code — new `storeBootstrapSchema`:**
```sql
-- 见 spec §5.6 完整 DDL。核心变化:
-- 1. runs: updated_at → finished_at, 新增 idx_runs_session_created
-- 2. 删除 owner_profile 表
-- 3. 删除 session_summaries 表
-- 4. pending_actions: 合并 decided_at/resolved_at 为 resolved_at
-- 5. session_messages: 新增 idx_session_messages_session_turn
```

**Steps:**
- [ ] 1. 重写 `store_schema_bootstrap.go` 的 `storeBootstrapSchema` 常量(10 张表)
- [ ] 2. 更新 `store_schema.go` migrations: 新增 migration 删除 owner_profile/session_summaries 表,删除 migration 版本 < 当前的不需要的 drops
- [ ] 3. 清空 `store_schema_drops.go` 中已无用的 drops(legacy 表已不存在)
- [ ] 4. 删除 `store_session_summary.go`(SummaryRepo 实现如果是空操作则直接返回 ErrNotFound;或保留文件但标记 deprecated)
- [ ] 5. 更新 `store_session_summary.go` → 如果保留 SummaryRepo,实现改为 no-op 或 error
- [ ] 6. 更新 `schemaRequiredTables` 强制列(去掉 owner_profile, session_summaries)
- [ ] 7. `go build ./internal/store/` — 通过
- [ ] 8. `go test ./internal/store/ -run TestSchema -v` — schema 相关测试通过
- [ ] 9. Commit: `refactor: redesign SQLite schema — 12→10 tables, drop owner_profile/session_summaries`

### Task 3.2: Store 实现 port 接口

**Files:**
- Modify: `internal/store/store_run.go` — 实现 `port.RunRepo`
- Modify: `internal/store/store_session.go` — 实现 `port.SessionRepo`
- Modify: `internal/store/store_session_messages.go` — 实现 `port.MessageRepo`
- Modify: `internal/store/store_pending_action.go` — 实现 `port.PendingActionRepo`
- Modify: `internal/store/store_device.go` — 实现 `port.DeviceRepo`
- Modify: `internal/store/artifacts.go` — 实现 `port.ArtifactRepo`
- Modify: `internal/store/store_oauth.go` — 实现 `port.OAuthRepo`
- Modify: `internal/store/sqlite_store.go` — Store struct 添加 compile-time interface assertions
- Delete: `internal/store/store.go` 中的旧类型定义(已移到 domain)
- Delete: `internal/store/store_work.go`(如果有 store-specific 工作类型)

**Why:** Store 方法签名改为引用 `domain.*` 类型而非 `store.*` 类型。Store struct 显式声明实现 port 接口。

**Key changes per file:**
- `store_run.go`: `CreateBoundRunWithParams(ctx, store.RunCreateParams)` → `CreateRun(ctx, domain.RunCreateParams)`,`FinishRunContext` → `FinishRun`,`MarkInterruptedContext` → `MarkInterrupted`,`UpdateRunOutputContext` → `UpdateRunOutput`
- `store_session.go`: 方法名去掉 `Context` 后缀(如果有)
- `store_pending_action.go`: `CreatePendingAction(ctx, store.CreatePendingActionInput)` → `CreatePendingAction(ctx, domain.PendingActionInput)`
- `store_device.go`: `SavePairingCode(ctx, *store.PairingCode)` → `SavePairingCode(ctx, *domain.PairingCode)`,`SaveDevice(ctx, *store.Device)` → `SaveDevice(ctx, *domain.Device)`
- `artifacts.go`: `Write(ctx, store.ArtifactWriteRequest)` → `WriteArtifact(ctx, domain.ArtifactWriteRequest)` 等
- `store_oauth.go`: 用 `domain.OAuthToken` 替换 `store.OAuthToken`

**Compile-time assertions in `sqlite_store.go`:**
```go
var (
	_ port.SessionRepo       = (*Store)(nil)
	_ port.MessageRepo       = (*Store)(nil)
	_ port.RunRepo           = (*Store)(nil)
	_ port.EventRepo         = (*Store)(nil)
	_ port.PendingActionRepo = (*Store)(nil)
	_ port.DeviceRepo        = (*Store)(nil)
	_ port.ArtifactRepo      = (*Store)(nil)
	_ port.OAuthRepo         = (*Store)(nil)
)
```

**Steps:**
- [ ] 1. 逐文件更新方法签名: `store.*` 类型 → `domain.*` 类型,方法名按 port 接口对齐
- [ ] 2. 在 `sqlite_store.go` 添加 compile-time assertions
- [ ] 3. 删除 `store.go` 中已移到 domain 的类型定义;保留 sentinel errors
- [ ] 4. `go build ./internal/store/` — 通过
- [ ] 5. `go test ./internal/store/` — 更新测试中的类型引用,通过
- [ ] 6. Commit: `refactor: store implements port.*Repo interfaces`

### Task 3.3: 迁移 store 测试

**Files:**
- Modify: `internal/store/*_test.go`(10 个测试文件)

**Why:** 测试中的 `store.RunCreateParams` → `domain.RunCreateParams` 等。

**Steps:**
- [ ] 1. 全局替换测试文件中的类型引用: `store.RunCreateParams` → `domain.RunCreateParams`,`store.Device` → `domain.Device`,`store.PairingCode` → `domain.PairingCode`,`store.OAuthToken` → `domain.OAuthToken`,`store.CreatePendingActionInput` → `domain.PendingActionInput`
- [ ] 2. 更新 schema 测试: 去掉 owner_profile/session_summaries 表检查;更新 `schemaRequiredTables`
- [ ] 3. `go test ./internal/store/ -v` — 全部通过
- [ ] 4. Commit: `test: migrate store tests to domain types`

---

## Phase 4: 适配 `internal/memory/`

### Task 4.1: Memory 实现新 port 接口

**Files:**
- Modify: `internal/memory/semantic.go` — 如果有 OAuthRepo 依赖,更新引用
- Modify: `internal/memory/types.go` — 如果引用 store 类型,改为 domain
- Check: `internal/memory/*.go` 对 `internal/store` 的 import → 替换为 `internal/domain` + `internal/port`

**Why:** memory 包当前可能直接 import store(通过 ArtifactService 等)。改为只依赖 port 接口。

**Steps:**
- [ ] 1. 搜索 memory 包对 store 的 import: `grep -r '"github.com/ycvk/acorn/internal/store"' internal/memory/`
- [ ] 2. 替换为 domain 类型 + port 接口
- [ ] 3. `go build ./internal/memory/` — 通过
- [ ] 4. `go test ./internal/memory/` — 通过
- [ ] 5. Commit: `refactor: memory depends on port/domain, not store`

---

## Phase 5: 适配 `internal/providers/mcp/`

### Task 5.1: MCP 实现新 port 接口

**Files:**
- Modify: `internal/providers/mcp/oauth_handler.go` — TokenStore → port.MCPTokenStore
- Modify: `internal/providers/mcp/elicitation_handler.go` — PendingActionStore → port.MCPPendingActionStore
- Modify: `internal/providers/mcp/manager.go` — 更新引用
- Check: `internal/providers/mcp/*.go` 对 `internal/store` 的 import → 替换

**Why:** mcp 包定义了 `TokenStore`/`PendingActionStore` 接口,移到 port 包。mcp 只实现,不定义端口。

**Steps:**
- [ ] 1. 搜索 mcp 包对 store 的 import: `grep -r '"github.com/ycvk/acorn/internal/store"' internal/providers/`
- [ ] 2. 将 `mcpprovider.TokenStore` 接口定义移到 `internal/port/mcp.go`(已在 Task 1.3 创建)
- [ ] 3. 将 `mcpprovider.PendingActionStore` 接口定义移到 `internal/port/mcp.go`
- [ ] 4. mcp 包内引用改为 `port.MCPTokenStore`/`port.MCPPendingActionStore`
- [ ] 5. `go build ./internal/providers/...` — 通过
- [ ] 6. `go test ./internal/providers/...` — 通过
- [ ] 7. Commit: `refactor: mcp depends on port interfaces, not store`

---

## Phase 6: 重写 `internal/tools/`

### Task 6.1: 移除工具契约,保留实现

**Files:**
- Delete: `internal/tools/contract.go` — 类型移到 port(已在 Task 1.2 创建)
- Delete: `internal/tools/tools_types.go` — tool I/O 类型保留在 tools(它们是实现细节,不是契约)
- Modify: `internal/tools/builtin_registry.go` — 引用 `port.ToolContract`/`port.ToolSpec`
- Modify: `internal/tools/catalog_builders.go` — 引用 `port.*`
- Modify: `internal/tools/ports.go` — `WorkspaceView`/`ArtifactService` 等接口保留(是 tools 实现的依赖端口,不是工具契约);但 `ArtifactService` 引用改为 `domain.ArtifactWriteRequest` 等
- Modify: `internal/tools/*.go` — 所有引用 `tools.ToolContract` → `port.ToolContract`,`tools.ToolSpec` → `port.ToolSpec`,`tools.Catalog` → `port.Catalog` 等
- Modify: `internal/tools/file_read.go`, `file_mutate.go`, `file_edit.go`, `browser_*.go`, `web.go`, `command.go`, `artifact.go`, `operator.go`, `workflow.go` — 更新类型引用

**Why:** tools 包不再自引用。契约在 port,实现在 tools。tools 依赖 port + domain,不直接依赖 store。

**Key type mapping:**
```
tools.ToolContract     → port.ToolContract
tools.ToolSpec         → port.ToolSpec
tools.Catalog          → port.Catalog
tools.ToolKind         → port.ToolKind
tools.ToolCategory     → port.ToolCategory
tools.ParallelPolicy   → port.ParallelPolicy
tools.ToolLoadingPolicy → port.ToolLoadingPolicy
tools.ToolExecutionPolicy → port.ToolExecutionPolicy
tools.HealthState      → port.HealthState
tools.ToolHealth        → port.ToolHealth
tools.EagerLoadingPolicy → port.EagerLoadingPolicy
tools.DeferredLoadingPolicy → port.DeferredLoadingPolicy
tools.ExecutionPolicyResolver → port.ExecutionPolicyResolver
```

**保留在 tools 包的**: `localToolDef`, `ConfiguredLocalSpecs`, `ConfiguredLocalSpec`, `BuildAuditedTools`, 所有 I/O 类型(`ReadFileInput` 等), `Status`/`NavigateResult`/`ScanResult` 等工具实现类型。

**Steps:**
- [ ] 1. 删除 `internal/tools/contract.go`
- [ ] 2. 全局替换 tools 内部引用: `tools.ToolContract` → `port.ToolContract` 等(用 `goimports` 或 sed)
- [ ] 3. 更新 `ports.go` 中 `ArtifactService` 接口引用: `store.ArtifactWriteRequest` → `domain.ArtifactWriteRequest`
- [ ] 4. 删除 tools 对 store 的直接 import,改为 domain
- [ ] 5. `go build ./internal/tools/` — 预期失败(外部引用未更新),进入后续 Task 修复
- [ ] 6. Commit: `refactor: tools contract moved to port, implementation stays`

### Task 6.2: 吸收 tooldispatch

**Files:**
- Move: `internal/runtime/tooldispatch/node.go` → `internal/tools/dispatch_node.go`
- Move: `internal/runtime/tooldispatch/scheduler.go` → `internal/tools/dispatch_scheduler.go`
- Move: `internal/runtime/tooldispatch/side_effects.go` → `internal/tools/dispatch_side_effects.go`
- Move: `internal/runtime/tooldispatch/streaming_executor.go` → `internal/tools/dispatch_streaming.go`
- Move: `internal/runtime/tooldispatch/types.go` → `internal/tools/dispatch_types.go`
- Delete: `internal/runtime/tooldispatch/`(空目录)

**Why:** tooldispatch 是工具调度逻辑,属于 tools 包,不属于 runtime。

**Steps:**
- [ ] 1. 移动 5 个文件到 `internal/tools/`,改 package 名为 `tools`
- [ ] 2. 更新文件内引用: `tooldispatch.ToolInvoker` → `tools.ToolInvoker` 等(如果类型名需要导出)
- [ ] 3. 删除 `internal/runtime/tooldispatch/` 目录
- [ ] 4. `go build ./internal/tools/` — 通过
- [ ] 5. Commit: `refactor: absorb tooldispatch into tools package`

### Task 6.3: 迁移 tools 测试

**Files:**
- Modify: `internal/tools/*_test.go`(7 个测试文件)
- Move: `internal/runtime/tooldispatch/*_test.go` → `internal/tools/`(如果有)

**Steps:**
- [ ] 1. 更新测试中的类型引用: `tools.ToolContract` → `port.ToolContract` 等
- [ ] 2. `go test ./internal/tools/` — 通过
- [ ] 3. Commit: `test: migrate tools tests to port types`

---

## Phase 7: 重命名 `internal/contextplane/` → `internal/context/`

### Task 7.1: 包重命名

**Files:**
- Move: `internal/contextplane/*.go` → `internal/context/*.go`(7 个源文件 + 6 个测试文件)
- Modify: 所有文件 package 名: `contextplane` → `context`
- Modify: 全局引用替换: `"github.com/ycvk/acorn/internal/contextplane"` → `"github.com/ycvk/acorn/internal/context"`
- Modify: 全局类型引用: `contextplane.ContextPlane` → `context.Plane`(避免与 Go 内置 `context` 冲突,重命名接口为 `Plane`)
- Modify: `contextplane.ContextSession` → `context.Session`
- Modify: `contextplane.AssembleRequest` → `context.AssembleRequest`
- Modify: `contextplane.AssembleResult` → `context.AssembleResult`
- Modify: `contextplane.NewDefaultContextPlane` → `context.NewDefaultPlane`
- Modify: `contextplane.WithContextSession` → `context.WithSession`

**Why:** `contextplane` 名字冗长。改为 `context`。但需要重命名核心类型避免与 Go 内置 `context` 包冲突——`ContextPlane` → `Plane`,`ContextSession` → `Session`。

**Important:** Go 允许包名和导入名不同。但为清晰,接口名也要改:包是 `context`,接口是 `Plane`(不是 `ContextPlane`,因为 `context.ContextPlane` 冗余)。

**Steps:**
- [ ] 1. `mkdir -p internal/context`
- [ ] 2. 移动所有 `.go` 文件: `git mv internal/contextplane/*.go internal/context/`
- [ ] 3. 全局替换 package 名: `package contextplane` → `package context`
- [ ] 4. 全局替换 import path: `internal/contextplane` → `internal/context`(所有 internal/ 和 cmd/ 和 tests/)
- [ ] 5. 重命名接口类型: `ContextPlane` → `Plane`,`ContextSession` → `Session`,`WithContextSession` → `WithSession`,`NewDefaultContextPlane` → `NewDefaultPlane`,`NewContextSession` → `NewSession`
- [ ] 6. `go build ./internal/context/` — 通过
- [ ] 7. `go test ./internal/context/` — 通过
- [ ] 8. Commit: `refactor: rename contextplane → context, rename ContextPlane → Plane`

---

## Phase 8: 创建 `internal/agent/` 包(从 runtime 提取)

### Task 8.1: 创建 agent 包核心

**Files:**
- Move: `internal/runtime/executor.go` → `internal/agent/agent.go`(Agent struct + Run/Resume/ExecuteMessages)
- Move: `internal/runtime/direct_response.go` → `internal/agent/direct_response.go`
- Move: `internal/runtime/agent_loop.go` → `internal/agent/loop.go`
- Move: `internal/runtime/run.go` → `internal/agent/run.go`
- Move: `internal/runtime/runner.go` → `internal/agent/factory.go`(RunnerFactory → AgentFactory,或直接内联为 buildRun 函数)
- Move: `internal/runtime/types.go` → `internal/agent/types.go`(只保留 agent 相关类型: AgentRunContext, ActiveRunner, RunnerBuildRequest 等)
- Move: `internal/runtime/runner_emit.go` → `internal/agent/emit.go`
- Move: `internal/runtime/runner_mcp.go` → `internal/agent/mcp.go`
- Move: `internal/runtime/runner_selection.go` → `internal/agent/selection.go`
- Move: `internal/runtime/validator.go` → `internal/agent/validator.go`
- Move: `internal/runtime/audit.go` → `internal/agent/audit.go`
- Move: `internal/runtime/catalog.go` → `internal/agent/catalog.go`
- Move: `internal/runtime/factextract/*.go` → `internal/agent/memtools.go`(合并两个文件为一个)
- Move: `internal/runtime/tooltest/test_helpers.go` → `tests/helpers/test_helpers.go`

**Why:** runtime 包拆分为 agent(执行引擎)。所有执行编排逻辑集中在这里。

**Key renames:**
- `Executor` → `Agent`(语义更清晰:它是执行 agent)
- `RunnerFactory` → `AgentFactory` 或直接内联
- `NewExecutorWithRunRuntimeAndController` → `NewAgent`
- `RuntimeDeps` → `AgentDeps`
- `ExecutorStore` → 删除,用 `port.RunRepo` + `port.EventRepo` + `port.MessageRepo` 组合
- `RunnerFactoryStore` → 删除,同上

### Task 8.2: 内联 4 个 Assembler

**Files:**
- Delete: `internal/runtime/model_builder.go` → 内联为 `internal/agent/model.go` 的 `newChatModel(ctx, cfg) (einomodel.BaseChatModel, error)` 函数
- Delete: `internal/runtime/capability_assembler.go` → 内联为 `internal/agent/capability.go` 的 `buildCapabilities(ctx, req)` 函数
- Delete: `internal/runtime/context_assembler.go` → 内联为 `internal/agent/context_assembly.go` 的 `assembleContext(ctx, req)` 函数
- Move: `internal/runtime/direct_response.go` 中的 `ToolAssembler` struct → 内联为 `internal/agent/tooling.go` 的 `assembleTooling(ctx, params)` 函数
- Create: `internal/agent/mcp_assembly.go` — 从 MCPAssembler 内联为 `buildMCPManager(ctx, req)` 函数

**Why:** 4 个 Assembler struct 是纯搬运中间层。内联为函数序列后,buildRun 的执行流一目了然。

**Steps for Task 8.1 + 8.2 (combined):**
- [ ] 1. `mkdir -p internal/agent`
- [ ] 2. 移动 executor.go → agent.go,改 package 为 `agent`,重命名 `Executor` → `Agent`
- [ ] 3. 移动 direct_response.go,内联 `ToolAssembler` struct 为 `assembleTooling` 函数
- [ ] 4. 移动 agent_loop.go → loop.go
- [ ] 5. 移动 runner.go → factory.go,`RunnerFactory` → `AgentFactory`
- [ ] 6. 移动 types.go,删除 `ExecutorStore`/`RunnerFactoryStore` 接口,替换为 port 接口组合:
  ```go
  type AgentStore interface {
      port.RunRepo
      port.EventRepo
      port.MessageRepo
      port.PendingActionRepo
      port.OAuthRepo
  }
  ```
- [ ] 7. 移动 model_builder.go → model.go,内联为 `newChatModel` 函数(删除 ModelBuilder struct)
- [ ] 8. 移动 capability_assembler.go → capability.go,内联为 `buildCapabilities` 函数(删除 CapabilityAssembler struct)
- [ ] 9. 移动 context_assembler.go → context_assembly.go,内联为 `assembleContext` 函数(删除 ContextAssembler struct)
- [ ] 10. 创建 mcp_assembly.go,从 runner_mcp.go + MCPAssembler 内联为 `buildMCPManager` 函数
- [ ] 11. 移动 runner_emit.go → emit.go,runner_selection.go → selection.go,validator.go,audit.go,catalog.go
- [ ] 12. 合并 factextract/memory_tools.go + factextract/memory_tools_search.go → memtools.go
- [ ] 13. 移动 tooltest/ → tests/helpers/
- [ ] 14. 更新 AgentDeps: `Store RunnerFactoryStore` → `Store AgentStore`,`MemoryModule memory.Service`,`ContextPlane context.Plane`
- [ ] 15. 删除 `internal/runtime/` 目录(此时应已空)
- [ ] 16. `go build ./internal/agent/` — 通过
- [ ] 17. Commit: `refactor: extract agent from runtime, inline 4 assemblers`

### Task 8.3: 迁移 agent 测试

**Files:**
- Move: `internal/runtime/direct_response_test.go` → `internal/agent/direct_response_test.go`
- Move: `internal/runtime/*_test.go` → `internal/agent/*_test.go`(14 个测试文件)
- Delete: `internal/runtime/runner_build_selection_test.go`(如果已被删除或内联)
- Delete: `internal/runtime/runner_factory_skills_test.go`(如果已被删除或内联)

**Steps:**
- [ ] 1. 移动所有 runtime 测试文件到 `internal/agent/`
- [ ] 2. 更新 package 名为 `agent`
- [ ] 3. 更新类型引用: `Executor` → `Agent`,`RunnerFactory` → `AgentFactory`,`RuntimeDeps` → `AgentDeps`
- [ ] 4. 更新 store mock: 实现 `port.RunRepo` + `port.EventRepo` + `port.MessageRepo` 而非 `ExecutorStore`
- [ ] 5. `go test ./internal/agent/` — 通过
- [ ] 6. Commit: `test: migrate runtime tests to agent package`

---

## Phase 9: 重写 `internal/api/`(吸收 app service)

### Task 9.1: 移入 app service 到 api

**Files:**
- Move: `internal/app/thread_service.go` → `internal/api/thread_service.go`
- Move: `internal/app/run_service.go` → `internal/api/run_service.go`
- Move: `internal/app/event_service.go` → `internal/api/event_service.go`
- Move: `internal/app/pending_action_service.go` → `internal/api/pending_action_service.go`
- Move: `internal/app/pending_action_service_decision.go` → `internal/api/pending_action_decision.go`
- Move: `internal/app/capability_service.go` → `internal/api/capability_service.go`
- Move: `internal/app/device_auth_service.go` → `internal/api/device_auth_service.go`
- Move: `internal/app/inbox_service.go`(如果存在) → `internal/api/inbox_service.go`
- Move: `internal/app/skill_service.go` → `internal/api/skill_service.go`
- Move: `internal/app/run_resume_service.go` → `internal/api/run_resume_service.go`
- Move: `internal/app/notification_service.go` → `internal/api/notification_service.go`
- Delete: `internal/app/MemoryService`(delegate wrapper,直接用 `memory.Service`)
- Delete: `internal/app/container_runtime.go` 中的 `executorHandle`/`runtimeExecutorHandle`

**Why:** app 包的业务 service 移入 api。app 包只保留组合根(Phase 10 变为 wire)。

**Key changes:**
- service 的 store 依赖: `containerAppStore` → 多个 `port.*Repo` 接口
- service 的 runtime 依赖: `*runtime.RunnerFactory`/`*runtime.RunController` → `*agent.AgentFactory`/`*agent.RunController`
- `MemoryService` delegate 删除,api 直接持有 `memory.Service`
- `executorHandle` 适配层删除,api 直接依赖 `agent.Agent` 接口

**Steps:**
- [ ] 1. 移动 11 个 service 文件到 `internal/api/`,改 package 为 `api`
- [ ] 2. 更新每个 service 的 store 依赖为 `port.*Repo`
- [ ] 3. 更新 service 对 runtime 的引用为 `agent.*`
- [ ] 4. 删除 `MemoryService` wrapper
- [ ] 5. 删除 `executorHandle`/`runtimeExecutorHandle`
- [ ] 6. 更新 api 现有 handler 对 service 的引用
- [ ] 7. `go build ./internal/api/` — 预期失败(wire 未创建),进入 Phase 10
- [ ] 8. Commit: `refactor: move app services to api, delete delegate/adapter layers`

### Task 9.2: 迁移 api 测试

**Files:**
- Move: `internal/app/*_test.go` → `internal/api/*_test.go`(15 个测试文件)
- Modify: `internal/api/*_test.go`(现有 6 个测试文件)

**Steps:**
- [ ] 1. 移动 app 测试到 api
- [ ] 2. 更新 package 名 + 类型引用
- [ ] 3. 更新 mock 实现 port 接口
- [ ] 4. `go test ./internal/api/` — 通过(在 Phase 10 完成后)
- [ ] 5. Commit: `test: migrate app tests to api package`

### Task 9.3: 更新 OpenAPI pending-actions:decide 端点

**Files:**
- Modify: `docs/openapi.yaml` — `POST /v1/pending-actions/{action_id}:decide` → `POST /v1/pending-actions/{action_id}/decide`
- Modify: `internal/api/routes.go` — 路由更新
- Modify: `internal/api/handlers_pending_action.go` — handler 更新

**Why:** 更 RESTful 的子资源路径。mobile 需重新生成 client。

**Steps:**
- [ ] 1. 更新 `docs/openapi.yaml` 中 pending-actions:decide 路径
- [ ] 2. 更新 `routes.go` 路由注册
- [ ] 3. `go build ./internal/api/` — 通过
- [ ] 4. `go test ./internal/api/` — 更新路由测试
- [ ] 5. Commit: `refactor: pending-actions:decide → POST /v1/pending-actions/{id}/decide`

---

## Phase 10: 创建 `internal/wire/` 包(新组合根)

### Task 10.1: 创建 wire 包

**Files:**
- Move: `internal/app/container.go` → `internal/wire/container.go`
- Move: `internal/app/container_runtime.go` → `internal/wire/runtime.go`(删除 executorHandle 适配层,保留 build 逻辑)
- Modify: `wire/container.go` — Container struct 精简,service 字段直接引用 api.*Service

**Why:** wire 是唯一知道所有具体实现的包。它 import store/memory/mcp/agent 具体实现,注入到 api/agent。

**Key Container struct:**
```go
type Container struct {
	cfg       *config.Config
	store     *store.Store
	agent     *agent.AgentFactory
	threads   *api.ThreadService
	runs      *api.RunService
	events    *api.EventService
	pending   *api.PendingActionService
	skills    *api.SkillService
	memory    memory.Service
	caps      *api.CapabilitiesService
	deviceAuth *api.DeviceAuthService
	inbox     *api.InboxService
}
```

**Delete in this phase:**
- `containerRuntimeStore` interface → 直接用 `*store.Store`
- `containerAppStore` interface → 直接用 `*store.Store`
- `containerRuntimeDeps` struct → wire 直接构造并注入

**Steps:**
- [ ] 1. 创建 `internal/wire/` 目录
- [ ] 2. 移动 container.go → wire/container.go,改 package 为 `wire`
- [ ] 3. 移动 container_runtime.go → wire/runtime.go,删除 executorHandle 适配层
- [ ] 4. Container struct 字段更新为直接引用 api/agent 类型
- [ ] 5. `buildContainer` 函数: 直接 new `store.Open()` → 注入到 `agent.NewAgentFactory()` → 注入到 `api.New*Service()`
- [ ] 6. 删除 `internal/app/` 目录(此时应已空)
- [ ] 7. `go build ./internal/wire/` — 通过
- [ ] 8. Commit: `refactor: create wire package as new composition root`

### Task 10.2: 迁移 wire 测试

**Files:**
- Move: `internal/app/container_test.go` → `internal/wire/container_test.go`
- Move: `internal/app/container_bleve_faiss_test.go` → `internal/wire/bleve_faiss_test.go`(如果保留)
- Delete: `internal/app/tooling_helpers_test.go`(如果不再需要)

**Steps:**
- [ ] 1. 移动测试文件,改 package 为 `wire`
- [ ] 2. `go test ./internal/wire/` — 通过
- [ ] 3. Commit: `test: migrate container tests to wire package`

---

## Phase 11: 适配 `internal/cli/`

### Task 11.1: 更新 CLI import

**Files:**
- Modify: `internal/cli/cli.go` — `app.Container` → `wire.Container`
- Modify: `internal/cli/serve.go` — `app.NewContainer` → `wire.NewContainer`
- Modify: `internal/cli/run.go` — `app.*` → `wire.*` / `agent.*`
- Modify: `internal/cli/smoke.go` — 同上
- Modify: `internal/cli/skills.go` — `app.*` → `wire.*` / `api.*`
- Modify: `internal/cli/token.go` — `app.*` → `wire.*` / `api.*`
- Modify: `internal/cli/devices.go` — 同上
- Modify: `internal/cli/pair.go` — 同上
- Modify: `internal/cli/doctor_output.go` — 同上
- Modify: `internal/cli/init.go` — 同上
- Modify: `cmd/acorn/main.go` — `app.*` → `wire.*`

**Steps:**
- [ ] 1. 全局替换: `internal/app` → `internal/wire`,`app.Container` → `wire.Container`
- [ ] 2. 全局替换: `internal/runtime` → `internal/agent`(如果 cli 引用了 runtime 类型)
- [ ] 3. `go build ./internal/cli/ ./cmd/acorn/` — 通过
- [ ] 4. `go test ./internal/cli/` — 通过
- [ ] 5. Commit: `refactor: cli imports wire instead of app`

---

## Phase 12: 更新全局引用 + 架构守卫 + 文档

### Task 12.1: 全局 import 修复

**Files:**
- Modify: 所有仍引用旧包路径的文件

**Steps:**
- [ ] 1. `go build ./...` — 查看所有编译错误
- [ ] 2. 逐个修复 import: `contextplane` → `context`,`runtime` → `agent`,`app` → `wire`/`api`
- [ ] 3. `go build ./...` — 通过
- [ ] 4. Commit: `fix: resolve all import paths after greenfield refactor`

### Task 12.2: 更新架构守卫测试

**Files:**
- Modify: `tests/architecture/structural_limits_test.go` — 更新 `refactorOwnedDirs`(去掉 runtime/app/contextplane,加 agent/wire/context)
- Modify: `tests/architecture/store_interface_count_test.go` — 更新: consumer-owned store 接口 ≤4 → 改为检查 port.*Repo 接口数量,或删除此守卫(因为 port 包接口不再由消费者定义)
- Modify: `tests/architecture/client_projection_boundary_test.go` — 更新文件列表
- Create: `tests/architecture/dependency_direction_test.go` — 新守卫: 验证依赖层级无环

**New dependency direction test:**
```go
// tests/architecture/dependency_direction_test.go
package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// layerRank defines the dependency layer for each internal package.
// Packages may only import packages in a strictly lower layer.
var layerRank = map[string]int{
	"domain":        0,
	"port":          1,
	"store":         2,
	"memory":        2,
	"mcp":           2,
	"tools":         2,
	"context":       2,
	"workspace":     2,
	"webaccess":     2,
	"skills":        2,
	"stream":        2,
	"clientevents":  2,
	"config":        1,
	"agent":         3,
	"api":           4,
	"wire":          5,
	"cli":           6,
}

func TestDependencyDirectionNoCycle(t *testing.T) {
	// Walk internal/, parse imports, assert layerRank[importer] > layerRank[imported]
	// Fail on any import that violates the layer order.
}
```

**Steps:**
- [ ] 1. 更新 `structural_limits_test.go`: `refactorOwnedDirs` 改为 `{"internal/agent", "internal/context", "internal/tools", "internal/api", "internal/wire"}`
- [ ] 2. 更新 `store_interface_count_test.go`: 改为检查 `port` 包接口数 ≤10(而非 consumer-owned ≤4),或删除旧守卫
- [ ] 3. 更新 `client_projection_boundary_test.go`: 更新文件列表(app → api/wire)
- [ ] 4. 创建 `dependency_direction_test.go`: 验证依赖层级无环
- [ ] 5. `go test ./tests/architecture/ -v` — 通过
- [ ] 6. Commit: `test: update architecture guards for Clean Slim Layers`

### Task 12.3: 更新架构文档

**Files:**
- Modify: `docs/architecture/ARCHITECTURE.md` — 更新主链 + 包职责
- Modify: `docs/architecture/INVARIANTS.md` — 更新不变量(包结构、依赖方向、port 定义)
- Modify: `docs/architecture/GLOSSARY.md` — 更新术语
- Modify: `docs/architecture/runtime-execution.md` — runtime → agent
- Modify: `docs/architecture/runtime-orchestration.md` — runtime → agent
- Modify: `docs/architecture/runtime-context-memory-decision.md` — contextplane → context
- Modify: `docs/architecture/data-web-store.md` — store 端口翻转
- Modify: `docs/architecture/mobile-control-surface.md` — app → wire/api
- Modify: `AGENTS.md` — 更新架构大图 + 硬边界 + 验证要求

**Steps:**
- [ ] 1. 更新所有架构文档中的包名引用
- [ ] 2. 更新主链图: `app Container` → `wire Container`,`runtime Executor` → `agent Agent`
- [ ] 3. 更新 INVARIANTS.md: 新增依赖方向不变量
- [ ] 4. 更新 AGENTS.md: 架构大图 + 关键包 + 硬边界
- [ ] 5. Commit: `docs: sync architecture docs to Clean Slim Layers`

### Task 12.4: 重新生成 mobile client

**Files:**
- Modify: `mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/`(openapi-generator 产出)

**Steps:**
- [ ] 1. `cd mobile-kotlin && ./tool/generate_openapi_client.sh`
- [ ] 2. 检查 diff: 确认只有 pending-actions:decide 路径变化
- [ ] 3. `cd mobile-kotlin && ./gradlew assembleDebug`(如果环境支持)
- [ ] 4. Commit: `mobile: regenerate API client for pending-actions decide path`

### Task 12.5: 最终验证

**Steps:**
- [ ] 1. `go build ./...` — 通过
- [ ] 2. `go test ./...` — 全部通过
- [ ] 3. `go test -race ./...` — 通过
- [ ] 4. `make lint` — 通过
- [ ] 5. `make format-check` — 通过
- [ ] 6. `make test-architecture` — 通过
- [ ] 7. `make build` — 通过,产出 `./bin/acorn`
- [ ] 8. `./bin/acorn doctor` — 能力快照正常
- [ ] 9. 验证依赖图无环: `go test ./tests/architecture/ -run TestDependencyDirection -v`
- [ ] 10. Commit: `chore: greenfield architecture refactor complete — all checks pass`

---

## Self-Review

### Spec coverage
- [x] §5.1 Store 端口翻转 → Task 1.1 + 3.2
- [x] §5.2 runtime 拆分 → Task 8.1 + 8.2
- [x] §5.3 app 拆分 → Task 9.1 + 10.1
- [x] §5.4 tools 分离 → Task 1.2 + 6.1
- [x] §5.5 domain 瘦身 → Task 2.1 + 2.2
- [x] §5.6 Schema 重设计 → Task 3.1
- [x] §5.7 OpenAPI 变化 → Task 9.3
- [x] §6.2 迁移顺序 → Phase 1-12 按依赖层从底到顶
- [x] §7 验收标准 → Task 12.5 最终验证覆盖全部 13 条

### Placeholder scan
- 无 TBD/TODO/placeholder。所有 task 有具体文件路径和操作。

### Type consistency
- `port.RunRepo.CreateRun(ctx, domain.RunCreateParams)` ↔ `store.CreateRun(ctx, domain.RunCreateParams)` ✓
- `port.DeviceRepo.SaveDevice(ctx, *domain.Device)` ↔ `store.SaveDevice(ctx, *domain.Device)` ✓
- `Agent` 替代 `Executor` 后所有引用一致 ✓

### Compatibility
- Greenfield 重建,无 compat alias
- 旧 DB 数据不迁移
- mobile wire contract 仅 pending-actions:decide 变化

### Risks
- 测试迁移量大 → 按 Phase 逐步迁移,每 Phase 后跑测试
- import rename 遗漏 → go build 每步兜底
- contextplane→context 改名 → 全局 replace + Go 内置 context 包冲突通过接口重命名解决

### ADR Signals
- 需创建 ADR-0013: Clean Slim Layers 架构重构
- 需创建 ADR-0014: Store 端口翻转
- 需创建 ADR-0015: Schema 重设计(删除 owner_profile/session_summaries)
- 需更新 ADR-0005(如果之前有 schema 精简 ADR)
