# Acorn 架构重构设计方案

**对应扫描**: `2026-05-24-runtime-cleanup-scan.md`
**日期**: 2026-05-24
**状态**: draft（待用户审批）

## 执行顺序（按依赖拓扑排序）

| 步骤 | 条目 | 方法 | 前置条件 | 退出信号 | 验证方 | 风险 |
|---|---|---|---|---|---|---|
| 1 | R8 合并 store/sqlite 文件 | M-L3-01 | 无 | 45 文件 → 20 文件 | AI | 低 |
| 2 | R6 合并 web handler/dto 文件 | M-L3-01 | 无 | 37 文件 → 15 文件 | AI | 低 |
| 3 | R5 合并 app service 文件 | M-L3-01 | 无 | 48 文件 → 15 文件 | AI | 低 |
| 4 | R1 合并 runtime 核心文件 | M-L3-01 | Step 1-3 完成 | 135 文件 → 25 文件 | AI | 中 |
| 5 | R12/R13/R14 内联子包 | M-L3-04 | Step 4 完成 | graph/api/stream 子包消失 | AI | 低 |
| 6 | R4 合并 store 类型层 | M-L3-02 | Step 1 完成 | 3 文件 → 1 文件 | AI | 低 |
| 7 | R2 删除微型接口 | M-L1-01 | Step 3-5 完成 | 20+ 接口 → 0 | AI | 中 |
| 8 | R3 简化 Stream 事件系统 | M-L2-03 | Step 4-5 完成 | 40+ payload → 1 | AI | 中 |
| 9 | R7 简化 ContextPlane | M-L2-01 | Step 1-8 完成 | 10 参数 → 4 | AI | 高 |

## 详细执行方案

---

### Step 1: 合并 store/sqlite 文件（R8）

**目标**: 将 45 个文件合并为 ~20 个

**具体合并策略**:

```
store_sessions.go
store_session_messages.go
store_session_summaries.go
store_session_result_summary.go
→ store_session.go (保留所有函数，按类型分组)

store_runs.go
store_run_archives.go
store_context_snapshots.go
store_context_boundaries.go
→ store_run.go

store_artifacts.go
store_tool_results.go
store_workingstate.go
→ store_work.go

store_notifications.go
store_device_auth.go
store_oauth.go
store_pending_actions.go
store_decision.go
store_plan.go
store_provider_usage.go
→ store_meta.go

store_schema.go
store_schema_validate.go
store_schema_test.go
store_sqlite_config.go
→ store_schema.go

store_terminal_sessions.go
store_terminal_sessions_test.go
→ store_terminal.go

store_elicitation_test.go
store_artifacts_test.go
store_checkpoint_test.go
store_memory_fail_loud_test.go
store_notifications_test.go
store_oauth_token_test.go
store_plan_test.go
store_provider_usage_test.go
store_runs_test.go
store_scan_helpers.go
→ 保留独立（测试文件不合并到主文件，可以合并到 tests）
```

**注意**:
- 不合并测试文件到被测试的文件中
- 可以创建 `store_test.go` 合并所有小型测试文件
- `store.go`（Open/Close/配置）保持独立

**退出信号**:
- `go test ./internal/store/sqlite/...` 全部通过
- `go build ./...` 通过

---

### Step 2: 合并 web handler/dto 文件（R6）

**目标**: 将 37 个文件合并为 ~15 个

**具体合并策略**:

```
handlers_thread.go
handlers_message.go
handlers_run.go
handlers_pending_action.go
handlers_inbox.go
handlers_devices.go
→ handlers.go

handlers_skills.go
handlers_skills_test.go
handlers_memory.go
handlers_memory_test.go
handlers_system.go
handlers_system_test.go
→ handlers_resource.go

dto_devices.go
dto_memory.go
dto_plans.go
dto_skills.go
dto_system.go
inbox_dto.go
message_dto.go
message_dto_test.go
pending_action_dto.go
request_decode.go
respond.go
run_dto.go
runtime_workbench_dto.go
runtime_workbench_dto_test.go
session_state_dto.go
settings_dto.go
thread_dto.go
→ dto.go

client_handlers_test.go → 保留
client_sse.go → 保留
errors.go → 保留
handler_helpers.go → 保留
openapi_test.go → 保留
routes.go → 保留
server.go → 保留
```

**退出信号**:
- `go test ./internal/web/...` 全部通过
- `go build ./...` 通过

---

### Step 3: 合并 app service 文件（R5）

**目标**: 将 48 个文件合并为 ~15 个

**具体合并策略**:

```
session_service.go
session_state_service.go
chat_service.go
client_service.go
client_runs.go
client_threads.go
client_projection.go
→ session.go

run_service.go
resume_service.go
trace_service.go
runtime_workbench_service.go
runtime_workbench_plan_store.go
→ run.go

skill_service.go
memory_service.go
capabilities_service.go
decision_service.go
→ skill.go

pending_action_service.go
inbox_service.go
notification_service.go
device_auth_service.go
→ action.go

working_checkpoint_service.go
tooling_helpers.go
→ helpers.go

container.go
container_bootstrap.go
→ container.go

保留测试文件和独立文件
```

**退出信号**:
- `go test ./internal/app/...` 全部通过
- `go build ./...` 通过

---

### Step 4: 合并 runtime 核心文件（R1）

**目标**: 将 135 个文件（含子目录）合并为 ~25 个

**这是最关键的步骤，需要仔细规划**:

#### 4a. 核心执行流程文件

```
executor.go
executor_loop.go
executor_run.go
executor_terminal.go
executor_stream.go
executor_finalization_test.go
executor_lifecycle_test.go
executor_crystallization_test.go
→ executor.go（保留所有核心执行逻辑）
```

#### 4b. Runner 生命周期文件

```
runner_factory.go
runner_factory_init.go
runner_factory_build.go
runner_factory_assemblers.go
runner_factory_orchestration.go
runner_factory_mcp.go
runner_factory_toolset.go
runner_factory_capabilities.go
runner_factory_provider.go
runner_factory_skills.go
runner_factory_compression_test.go
runner_factory_test_helpers_test.go
runner_factory_mcp_test.go
runner_factory_provider_test.go
runner_factory_skills_test.go
runner_factory_toolset_test.go
→ runner.go（所有 RunnerFactory 相关）
```

#### 4c. Run 上下文和类型

```
run_builder.go
run_context.go
run_control.go
run_types.go
→ run.go
```

#### 4d. Plan 执行

```
plan_node.go
plan_store.go
plan_evidence.go
plan_runtime.go
plan_stream.go
plan_execute_graph.go
plan_act_observe_e2e_test.go
plan_execute_graph_test.go
plan_gate_test.go
plan_node_test.go
plan_evidence_test.go
→ plan.go
```

#### 4e. Stream 子目录（见 Step 8）

暂不合并，等 Step 8 一起处理

#### 4f. 工具相关

```
tool_audit.go
tool_execution_scheduler.go
tool_naming.go
tool_schema_dedup.go
tool_specs.go
toolresult_ledger_test.go
→ tool.go

load_tools.go → 保留（如果独立）
```

#### 4g. 辅助文件

```
helpers.go
helpers_test.go
context.go
alias.go
alias_stream.go
validator.go
validator_test.go
→ helpers.go
```

#### 4h. 状态与 Trace

```
trace_types.go
trace_projector.go
subagent_executor.go
pending_resume.go
orchestration_mode_test.go
root_orchestration_router.go
root_orchestration_router_test.go
→ trace.go
```

#### 4i. 保留独立文件

- `stream.go`（可能为空或需要检查）
- `graph_agent_test.go`
- `graph_agent.go`
- `streaming_assistant_stream.go`
- `streaming_tool_executor.go`
- `act_node.go`
- `act_node_test.go`
- `observe_node_test.go`
- `checkpoint_json.go`
- `safe_parallel_tools_node.go`
- `safe_parallel_tools_node_test.go`
- `fact_extractor.go`
- `fact_extractor_test.go`
- `memory_tools.go`
- `memory_tools_test.go`
- `stream_*.go`（streaming 相关）
- `run_context_test.go`
- `run_control_test.go`
- `run_builder_test_helpers_test.go`
- `run_builder_interface.go`
- `context_session_bridge.go`
- `contextplane_bridge.go`
- `plan_store.go`（已合并到 plan.go）
- `agent_graph.go`
- `assistant_stream.go`
- `assistant_stream_test.go`
- `assistant_streamer.go`
- `elicitation.go`
- `elicitation_test.go`

**退出信号**:
- `go test ./internal/runtime/...` 全部通过
- `go build ./...` 通过

---

### Step 5: 内联子包（R12/R13/R14）

**目标**: 将 graph/api/stream 子包合并到 `internal/runtime`

**策略**:

```
internal/runtime/graph/types.go → internal/runtime/graph_types.go
internal/runtime/graph/agent.go → internal/runtime/graph_agent.go
internal/runtime/graph/context_session.go → internal/runtime/graph_agent.go（合并）
internal/runtime/graph/observe_node.go → internal/runtime/graph_agent.go（合并）
internal/runtime/graph/plan_state.go → internal/runtime/plan.go（合并）

internal/runtime/api/plan.go → internal/runtime/api_types.go
internal/runtime/api/ports.go → 删除（接口删除步骤）
internal/runtime/api/session.go → internal/runtime/api_types.go（合并）
internal/runtime/api/evidence.go → internal/runtime/api_types.go（合并）
internal/runtime/api/execute.go → internal/runtime/api_types.go（合并）
internal/runtime/api/errors.go → internal/runtime/api_types.go（合并）
internal/runtime/api/context.go → internal/runtime/api_types.go（合并）
internal/runtime/api/plan_test.go → internal/runtime/api_types_test.go

internal/runtime/stream/* → 见 Step 8
```

**退出信号**:
- 删除 `internal/runtime/graph/`, `internal/runtime/api/` 目录
- `go test ./internal/runtime/...` 全部通过
- 没有其他包 import 这些子包

---

### Step 6: 合并 store 类型层（R4）

**目标**: 3 个文件 → 1 个文件

**策略**:

```
internal/store/types.go
internal/store/errors.go
→ 合并为 internal/store/store.go

internal/store/sqlite/store_contracts.go → 删除
```

**修改影响**:
- `internal/store/sqlite/*.go` 中所有使用 `RunCreateParams = storecore.RunCreateParams` 的 alias，改为直接使用 `store.RunCreateParams`
- 所有 `ErrRunNotFound = storecore.ErrRunNotFound` 改为 `store.ErrRunNotFound`

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/store/...` 通过

---

### Step 7: 删除微型接口（R2）

**目标**: 删除 20+ 个虚假接口

**策略**:

```
# internal/app/store_ports.go — 删除整个文件
sessionStore → 删除，让 SessionService 直接依赖 *sqlite.Store
pendingActionDecisionStore → 删除
traceStore → 删除
decisionStore → 删除
sessionStateStore → 删除
clientStore → 删除
inboxStore → 删除
notificationStore → 删除
deviceAuthStore → 删除
// 等等所有接口

# internal/web/server.go — 删除接口定义，改为依赖具体类型
ClientService interface → 删除，使用 *app.ClientService
PendingActionService interface → 删除
RuntimeWorkbenchService interface → 删除
// 等等所有接口

# internal/runtime/api/ports.go — 删除 EventAppender 接口
```

**关键注意事项**:
- 需要确保 `*sqlite.Store` 确实实现了所有被删除接口的方法
- 如果有方法缺失，需要添加到 `*sqlite.Store`
- 这是一个破坏性变更，会影响所有 service 构造函数

**修改示例**:

```go
// 之前
type sessionStore interface {
    CreateSession(ctx context.Context, sessionID, title string) (*events.SessionRecord, error)
    ListSessionMessages(ctx context.Context, sessionID string, limit int) ([]events.SessionMessageRecord, error)
}

type SessionService struct {
    store sessionStore
}

// 之后
type SessionService struct {
    store *sqlite.Store  // 或 *store.Store（如果封装）
}
```

**退出信号**:
- `go build ./...` 通过（这是最强的验证）
- `go test ./internal/app/...` 通过
- `go test ./internal/web/...` 通过
- `go test ./internal/runtime/...` 通过

---

### Step 8: 简化 Stream 事件系统（R3）

**目标**: 40+ payload types → 1 个 `map[string]any`

**策略**:

```go
// 之前
type StreamItem struct {
    RunID     string         `json:"run_id"`
    Sequence  int64          `json:"sequence,omitempty"`
    Kind      StreamItemKind `json:"kind"`
    CreatedAt time.Time      `json:"created_at"`
    Payload   StreamPayload  `json:"-"`
}

type RunStartedPayload struct {
    Input string `json:"input,omitempty"`
}
// ... 40+ 个 payload struct

// 之后
type StreamItem struct {
    RunID     string         `json:"run_id"`
    Sequence  int64          `json:"sequence,omitempty"`
    Kind      StreamItemKind `json:"kind"`
    CreatedAt time.Time      `json:"created_at"`
    Payload   map[string]any `json:"-"`
}
```

**accessor 方法修改**:

```go
// 之前
func (item StreamItem) GetMessage() *StreamMessage {
    switch p := item.Payload.(type) {
    case *AssistantMessagePayload:
        return p.Message
    // ...
    }
}

// 之后
func (item StreamItem) GetMessage() *StreamMessage {
    msg, ok := item.Payload["message"].(*StreamMessage)
    if !ok {
        // 尝试从 map 反序列化
    }
    return msg
}
```

**JSON 序列化**:
- 现有的 `MarshalJSON` 已经使用 `map[string]any` 扁平化，所以修改后逻辑可以大幅简化
- `UnmarshalJSON` 需要改为从 `map[string]any` 中提取字段，而不是类型断言

**退出信号**:
- `go test ./internal/runtime/stream/...` 全部通过
- `go test ./internal/runtime/...` 全部通过
- JSON 序列化/反序列化行为不变（通过测试验证）

---

### Step 9: 简化 ContextPlane（R7）

**目标**: 10 个配置参数 → 4 个核心参数

**策略**:

```go
// 之前
type ContextPolicy struct {
    ContextWindowTokens     int
    ReservedOutputTokens    int
    StaticOverheadTokens    int
    WarningBufferTokens     int
    AutoCompactBufferTokens int
    BlockingBufferTokens    int
    PreserveRecentTurns     int
    MaxSummaryTokens        int
    TokenEncoding           string
    HandoffFrameDisabled    bool
}

// 之后
type ContextPolicy struct {
    WindowTokens        int
    CompactMarginTokens int
    PreserveRecentTurns int
    SummaryMaxTokens    int
}
```

**影响代码**:
- `internal/config/config.go` — 删除字段
- `internal/contextplane/budget_governor.go` — 简化预算计算
- `internal/contextplane/compaction_engine.go` — 删除 HandoffFrame 逻辑
- `configs/acorn.example.yaml` — 同步修改

**退出信号**:
- `go test ./internal/contextplane/...` 全部通过
- `go build ./...` 通过
- 端到端测试通过（如果存在）

---

## 验证策略总览

| 验证层级 | 工具 | 触发时机 | 通过标准 |
|---|---|---|---|
| L1 编译 | `go build ./...` | 每次文件合并后 | 零错误 |
| L2 单元测试 | `go test ./internal/{pkg}/...` | 每步完成后 | 测试通过数不下降 |
| L3 集成测试 | `go test ./...` | 每阶段完成后 | 全部通过 |
| L4 静态检查 | `go vet ./...` | 每阶段完成后 | 零问题 |
| L5 格式化 | `gofmt -l .` | 最后 | 零未格式化文件 |

## 回滚策略

1. **每步完成后创建 git checkpoint**: `git add . && git commit -m "refactor: Step N - 描述"`
2. **如果某步失败**: `git reset --hard HEAD~1` 回到上一步状态
3. **接口删除步骤**: 因为这是破坏性变更，如果编译修复工作量过大，可以保留部分必要接口，只删除完全没有价值的接口
4. **Stream 简化步骤**: 如果 `map[string]any` 方案导致太多运行时类型错误，可以回退到保留少量核心 payload struct（如 `AssistantMessagePayload`, `ToolCallPayload`），其余用 `map[string]any`

## 时间估算

| 步骤 | 估算时间 | 不确定性 |
|---|---|---|
| 1 store 合并 | 30 min | 低 |
| 2 web 合并 | 30 min | 低 |
| 3 app 合并 | 30 min | 低 |
| 4 runtime 合并 | 2-3 hours | 高（核心逻辑密集） |
| 5 子包内联 | 45 min | 中 |
| 6 store 类型合并 | 20 min | 低 |
| 7 接口删除 | 1-2 hours | 高（编译失败点多） |
| 8 Stream 简化 | 1-2 hours | 中 |
| 9 ContextPlane 简化 | 1-2 hours | 高 |
| **总计** | **8-12 hours** | |

## 不做的范围（明确排除）

1. 不改 OpenAPI schema（除非 Stream 简化影响 event types）
2. 不改 mobile 代码（除非必要同步）
3. 不改 SQLite schema（不删表/不迁移）
4. 不改 eino agent 核心逻辑（只改组织方式）
5. 不改技能/内存模块的业务逻辑
6. 不改任何外部可见的 HTTP API 行为

## 设计决策记录

### 决策 1: 为什么保留测试文件独立？

测试文件通常很长（几百到上千行），合并到主文件中会让主文件超过 2000 行，反而降低可读性。Go 社区惯例也是测试文件独立。

### 决策 2: 为什么先合并文件再删除接口？

文件合并是纯组织变更，不影响编译。接口删除是破坏性变更，会导致编译失败。先完成安全的合并，再处理需要修复编译错误的步骤，降低认知负担。

### 决策 3: 为什么不一次合并所有文件？

虽然可以一次操作，但 `internal/runtime` 135 个文件的合并涉及大量 import 和符号引用变化，一次性改动容易导致错误遗漏。分步合并允许每步后验证，更快定位问题。

### 决策 4: 为什么 Stream 简化用 `map[string]any` 而不是保留核心 struct？

调研显示 Go 社区对 event/streaming 系统的建议是：除非有强类型安全需求，否则用 `map[string]any` + kind discriminator 更灵活。Acorn 的 40+ payload 类型大部分只有 1-3 个字段，强类型的维护成本高于收益。

### 决策 5: 为什么接口删除是可选的？

如果编译修复工作量超过预期（比如 `*sqlite.Store` 缺少某些方法需要大量添加），可以只删除明显无价值的接口（如 `sessionStore` 这种只有一个消费者的小接口），保留跨多个包使用的接口。

---

## 审批

**用户确认勾选以下条目后进入执行阶段**:

- [ ] 同意执行顺序
- [ ] 同意合并策略
- [ ] 同意删除微型接口（理解这是破坏性变更）
- [ ] 同意 Stream 简化为 `map[string]any`
- [ ] 同意 ContextPlane 参数缩减
- [ ] 理解回滚策略
- [ ] 确认不做的范围可接受

**审批后**: 生成 `checklist.yaml` 进入 apply 阶段。
