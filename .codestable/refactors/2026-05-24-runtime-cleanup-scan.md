# Acorn 架构重构扫描报告

**扫描范围**: `internal/runtime/` (109 文件), `internal/app/` (48 文件), `internal/web/` (37 文件), `internal/store/sqlite/` (45 文件), `internal/contextplane/` (44 文件)
**发现条数**: 高优先级 8 条，中优先级 6 条，低优先级 4 条
**按分类分布**: L3 结构拆分 (8), L2 代码级重构 (6), L1 行为等价迁移 (4)
**按风险分布**: 高 (3), 中 (8), 低 (7)
**建议先做**: runtime 包合并 → 接口删除 → Stream 简化（按依赖顺序）
**慎做**: 涉及公开 API 契约的改动（OpenAPI/mobile 同步）

---

## 高优先级条目

### R1. `internal/runtime` 109 个文件过度拆分

- **位置**: `internal/runtime/*.go`, `internal/runtime/stream/*.go`, `internal/runtime/graph/*.go`, `internal/runtime/api/*.go`
- **问题**: 一个 package 包含 135 个文件（含子目录），主执行流程被分散在 30+ 个文件中，导航困难、编译慢、认知负担极高
- **影响范围**: 编译时间、代码导航、新开发者 onboarding
- **风险**: 中（文件合并不改变行为，但大量移动代码容易出错）
- **验证**: `go test ./internal/runtime/...` 必须全部通过
- **推荐方法**: M-L3-01 包内文件合并 — 按职责聚合成 15-20 个核心文件
- **具体方案**:
  - `executor.go` + `executor_loop.go` + `executor_run.go` + `executor_terminal.go` → `executor.go`
  - `runner_factory.go` + `runner_factory_init.go` + `runner_factory_build.go` + `runner_factory_assemblers.go` + `runner_factory_orchestration.go` + `runner_factory_mcp.go` + `runner_factory_toolset.go` + `runner_factory_capabilities.go` + `runner_factory_provider.go` + `runner_factory_skills.go` + `runner_factory_compression_test.go` → `runner.go`
  - `run_builder.go` + `run_context.go` + `run_control.go` + `run_types.go` → `run.go`
  - `plan_node.go` + `plan_store.go` + `plan_evidence.go` + `plan_runtime.go` + `plan_act_observe_e2e_test.go` + `plan_execute_graph.go` + `plan_execute_graph_test.go` + `plan_gate_test.go` → `plan.go`
  - `stream/stream_types.go` + `stream/payloads.go` + `stream/item_json.go` + `stream/accessors.go` + `stream/projection.go` → `stream_types.go`
  - `stream/agent.go` + `stream/context.go` + `stream/helpers.go` + `stream/plan_helpers.go` + `stream/plan_payloads.go` → `stream_helpers.go`
  - `graph/types.go` + `graph/agent.go` + `graph/context_session.go` + `graph/observe_node.go` + `graph/plan_state.go` → `graph.go`
  - `api/ports.go` + `api/plan.go` + `api/session.go` + `api/evidence.go` + `api/execute.go` + `api/errors.go` + `api/context.go` → `api.go`
  - 保留 `tool_*.go` 和 `checkpoint_json.go` 等独立模块
- **预期结果**: 135 → ~25 个文件

### R2. 接口爆炸（微型接口 1:1 映射实现）

- **位置**: `internal/app/store_ports.go`, `internal/web/server.go`, `internal/runtime/api/ports.go`
- **问题**: 定义了 20+ 个微型接口（`sessionStore`, `traceStore`, `decisionStore`, `clientStore`, `inboxStore`, 等），但实现者都是同一个 `*sqlite.Store`。接口没有带来抽象价值，反而增加了改动成本
- **影响范围**: 所有 service 层代码、web handler 层
- **风险**: 中（删除接口后编译失败，修复方向明确）
- **验证**: `go build ./...` + `go test ./internal/app/...` + `go test ./internal/web/...`
- **推荐方法**: M-L1-01 删除虚假抽象 — 直接依赖具体实现，直到真有第二个实现
- **具体方案**:
  - 删除 `internal/app/store_ports.go` 中所有接口定义
  - 让各 Service 直接依赖 `*sqlite.Store`
  - 删除 `internal/web/server.go` 中的重复接口定义
  - 删除 `internal/runtime/api/ports.go` 中的 `EventAppender` 接口（如果只有一个实现）
- **预期结果**: 20+ 个接口 → 0 个虚假接口

### R3. Stream 事件系统 40+ 种强类型 payload

- **位置**: `internal/runtime/stream/stream_types.go`, `internal/runtime/stream/payloads.go`
- **问题**: 定义了 40+ 种 `StreamItemKind`，每种都有独立的 payload struct + `StreamKind()` 方法 + accessor 方法。新增一种事件需要改 4-5 个地方
- **影响范围**: 所有 event 处理代码、web projection、mobile client
- **风险**: 中（类型系统改动，需要确保序列化/反序列化行为不变）
- **验证**: 所有 `*_test.go` 中涉及 StreamItem 的测试 + `go test ./internal/runtime/stream/...`
- **推荐方法**: M-L2-03 类型扁平化 — 用统一的 `map[string]any` payload + kind discriminator 替代强类型 struct
- **具体方案**:
  - 保留 `StreamItem` 结构体（含 `RunID`, `Sequence`, `Kind`, `CreatedAt`）
  - `Payload` 字段改为 `map[string]any`
  - 删除所有独立的 payload struct（`RunStartedPayload`, `RunCompletedPayload`, ...）
  - accessor 方法改为从 `map[string]any` 中提取字段
  - JSON 序列化/反序列化逻辑大幅简化（已经是 flat JSON，没有嵌套 payload wrapper）
- **预期结果**: 40+ payload types → 1 个 `map[string]any`

### R4. Store 三层类型系统

- **位置**: `internal/store/types.go`, `internal/store/errors.go`, `internal/store/sqlite/store_contracts.go`
- **问题**: 类型定义分布在三层：domain (`internal/store/types.go`) → error (`internal/store/errors.go`) → alias (`internal/store/sqlite/store_contracts.go`)。`store_contracts.go` 只是类型别名和 error 变量的重导出，没有实际价值
- **影响范围**: store 层、所有依赖 store 的包
- **风险**: 低（纯类型移动，编译错误容易修复）
- **验证**: `go build ./...`
- **推荐方法**: M-L3-02 合并分层 — 将 alias 层合并到主定义层
- **具体方案**:
  - 删除 `internal/store/sqlite/store_contracts.go`
  - 让 `sqlite` 包直接 import `internal/store` 并使用其类型
  - 合并 `internal/store/errors.go` 到 `internal/store/types.go`
- **预期结果**: 3 个文件 → 1 个文件

### R5. `internal/app` 48 个 service 文件过度拆分

- **位置**: `internal/app/*.go`
- **问题**: 每个 service 一个文件，很多只有 30-80 行（如 `session_service.go` 23 行, `run_service.go` 36 行, `decision_service.go` 33 行）。文件数量多但每个都很薄
- **影响范围**: app 层 service 组织
- **风险**: 低（纯文件合并，不改变类型/接口）
- **验证**: `go test ./internal/app/...`
- **推荐方法**: M-L3-01 包内文件合并
- **具体方案**:
  - 按 domain 合并：
    - `session_service.go` + `session_state_service.go` + `chat_service.go` + `client_service.go` + `client_runs.go` + `client_threads.go` + `client_projection.go` → `session.go`
    - `run_service.go` + `resume_service.go` + `trace_service.go` + `runtime_workbench_service.go` + `runtime_workbench_plan_store.go` → `run.go`
    - `skill_service.go` + `memory_service.go` + `capabilities_service.go` + `decision_service.go` → `skill.go`
    - `pending_action_service.go` + `inbox_service.go` + `notification_service.go` + `device_auth_service.go` → `action.go`
    - `working_checkpoint_service.go` + `container.go` + `container_bootstrap.go` → `container.go`
- **预期结果**: 48 → ~15 个文件

### R6. `internal/web` 37 个 handler 文件过度拆分

- **位置**: `internal/web/*.go`
- **问题**: 几乎每个 HTTP handler 一个文件，加上独立的 DTO 文件（`dto_*.go`）。`handlers_run.go`, `handlers_thread.go`, `handlers_message.go` 等实际上可以合并
- **影响范围**: web 层 handler 组织
- **风险**: 低（纯文件合并）
- **验证**: `go test ./internal/web/...`
- **推荐方法**: M-L3-01 包内文件合并
- **具体方案**:
  - `handlers_thread.go` + `handlers_message.go` + `handlers_run.go` + `handlers_pending_action.go` + `handlers_inbox.go` → `handlers.go`
  - `handlers_skills.go` + `handlers_skills_test.go` + `handlers_memory.go` + `handlers_memory_test.go` + `handlers_system.go` + `handlers_system_test.go` → `handlers_resource.go`
  - `dto_*.go` 合并到 `dto.go`
  - `run_dto.go` + `thread_dto.go` + `message_dto.go` + `message_dto_test.go` + `session_state_dto.go` + `settings_dto.go` → `dto.go`
- **预期结果**: 37 → ~15 个文件

### R7. ContextPlane 过度复杂的压缩系统

- **位置**: `internal/contextplane/`
- **问题**: 44 个文件管理上下文压缩，包含 `BudgetGovernor`（10 个阈值参数）、`CompactionEngine`（3 种触发模式）、`RehydrationPlanner`、`CompressionPipeline`、`CompressionTokenCounter`。对 single-user 场景过度精细
- **影响范围**: 上下文组装、内存预算、运行时压缩逻辑
- **风险**: 高（改动容易引入运行时行为变化）
- **验证**: `go test ./internal/contextplane/...` + 端到端运行测试
- **推荐方法**: M-L2-01 参数简化 — 将 10 个阈值砍到 3-4 个核心参数
- **具体方案**:
  - `ContextPolicy` 从 10 个字段缩减到 4 个：`WindowTokens`, `CompactMarginTokens`, `PreserveRecentTurns`, `SummaryMaxTokens`
  - 删除 `ReservedOutputTokens`, `StaticOverheadTokens`, `WarningBufferTokens`, `AutoCompactBufferTokens`, `BlockingBufferTokens`, `TokenEncoding`, `HandoffFrameDisabled`
  - 保留 `BudgetGovernor` 但简化内部逻辑
  - 保留 `CompactionEngine` 但删除 `HandoffFrame` 相关逻辑
- **预期结果**: 更少的配置参数、更简单的预算计算

### R8. `internal/store/sqlite` 45 个文件每个 domain 一个

- **位置**: `internal/store/sqlite/store_*.go`
- **问题**: 几乎每张表/每个 domain 一个文件（`store_runs.go`, `store_sessions.go`, `store_session_messages.go`, `store_pending_actions.go`, ...）。加上 schema/migration/helpers 文件，数量爆炸
- **影响范围**: store 层维护、新增表操作
- **风险**: 低（纯文件合并，SQL 逻辑不变）
- **验证**: `go test ./internal/store/sqlite/...`
- **推荐方法**: M-L3-01 包内文件合并
- **具体方案**:
  - `store_sessions.go` + `store_session_messages.go` + `store_session_summaries.go` + `store_session_result_summary.go` → `store_session.go`
  - `store_runs.go` + `store_run_archives.go` + `store_context_snapshots.go` + `store_context_boundaries.go` → `store_run.go`
  - `store_artifacts.go` + `store_tool_results.go` + `store_workingstate.go` + `store_checkpoint_test.go` → `store_work.go`
  - `store_notifications.go` + `store_device_auth.go` + `store_oauth.go` + `store_pending_actions.go` + `store_decision.go` + `store_plan.go` + `store_provider_usage.go` → `store_meta.go`
  - `store_schema.go` + `store_schema_validate.go` + `store_schema_test.go` + `store_sqlite_config.go` → `store_schema.go`
- **预期结果**: 45 → ~20 个文件

---

## 中优先级条目

### R9. 投影链条重复转换

- **位置**: `internal/app/client_projection.go`, `internal/runtime/stream/projection.go`, `internal/web/dto_*.go`
- **问题**: 数据从 `events.EventRecord` → `app.RunEvent` → `web.DTO` → `mobile model` 需要经过 3-4 层投影。每层都是手写的，字段变更需要改多处
- **风险**: 中
- **方法**: M-L2-02 统一投影层

### R10. Mobile 端手写 SSE 解析与生成代码不同步

- **位置**: `mobile/lib/src/api/run_event_stream.dart`
- **问题**: OpenAPI 生成的 `acorn_api.dart` 是自动的，但 SSE 流解析是手写的。如果 Event 类型变更，生成代码会更新但手写解析器不会
- **风险**: 中
- **方法**: M-L2-04 代码生成覆盖

### R11. Crystallization 模块职责边界不清

- **位置**: `internal/crystallization/`
- **问题**: 和 `internal/skills/` 以及 `internal/memorymodule/` 的 procedure 创建有重叠。如果自动化 skill 提取价值不高，会成为 dead code
- **风险**: 低
- **方法**: M-L3-03 职责合并或删除

### R12. `graph` 子目录可以内联到 runtime

- **位置**: `internal/runtime/graph/`
- **问题**: 6 个文件的子包，只被 `internal/runtime` 内部使用，没有其他消费者。不必要的子包增加了 import 复杂度
- **风险**: 低
- **方法**: M-L3-04 子包内联

### R13. `api` 子目录可以内联到 runtime

- **位置**: `internal/runtime/api/`
- **问题**: 8 个文件的子包，主要是 Plan 类型和接口。被 `internal/runtime` 和 `internal/orchestration` 使用，但完全可以合并到 `internal/runtime`
- **风险**: 低
- **方法**: M-L3-04 子包内联

### R14. `stream` 子目录可以保留或内联

- **位置**: `internal/runtime/stream/`
- **问题**: 15 个文件的子包。合并 payload 类型后可以大幅缩减，然后决定是否保留为子包或内联
- **风险**: 低
- **方法**: M-L3-04 子包内联（合并后）

---

## 低优先级条目

### R15. 配置系统参数过度精细

- **位置**: `internal/config/config.go`
- **问题**: `ContextPolicy` 10 个参数，`AgentConfig` 多个字段。对 single-user 场景过度配置化
- **风险**: 低
- **方法**: M-L2-01 参数简化

### R16. `alias.go` 和 `alias_stream.go` 的内容过少

- **位置**: `internal/runtime/alias.go`, `internal/runtime/alias_stream.go`
- **问题**: 每个文件只有几个 alias 定义，可以合并到 `types.go`
- **风险**: 低
- **方法**: M-L2-05 小文件合并

### R17. `tool_*.go` 文件可以进一步合并

- **位置**: `internal/runtime/tool_*.go`
- **问题**: `tool_audit.go`, `tool_execution_scheduler.go`, `tool_naming.go`, `tool_schema_dedup.go`, `tool_specs.go`, `load_tools.go` 等可以合并为 2-3 个文件
- **风险**: 低
- **方法**: M-L3-01 包内合并

### R18. `orchestrationmode` 独立包过于简单

- **位置**: `internal/orchestrationmode/`
- **问题**: 只有 1-2 个文件定义 mode 枚举。可以合并到 `internal/runtime` 或 `internal/orchestration`
- **风险**: 低
- **方法**: M-L3-04 子包内联

---

## 方法映射表

| 方法编号 | 方法名称 | 应用条目 |
|---|---|---|
| M-L3-01 | 包内文件合并 | R1, R5, R6, R8, R17 |
| M-L3-02 | 合并分层 | R4 |
| M-L3-03 | 职责合并或删除 | R11 |
| M-L3-04 | 子包内联 | R12, R13, R14, R18 |
| M-L2-01 | 参数简化 | R7, R15 |
| M-L2-02 | 统一投影层 | R9 |
| M-L2-03 | 类型扁平化 | R3 |
| M-L2-04 | 代码生成覆盖 | R10 |
| M-L2-05 | 小文件合并 | R16 |
| M-L1-01 | 删除虚假抽象 | R2 |
