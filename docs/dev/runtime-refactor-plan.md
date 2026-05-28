# Runtime 包拆分计划 (修订版)

## 已完成

### Phase 1: api/ 公共契约包
- 创建 `internal/runtime/api/` 目录
- 迁移纯类型定义（无依赖运行时逻辑）：
  - `errors.go` — 错误常量
  - `execute.go` — `ExecuteRequest`
  - `plan.go` — `Plan`, `PlanStep`, `PlanStore`, `PlanPersistenceStore`
  - `evidence.go` — `PlanEvidence`, `EvidenceKind`, `EvidenceStatus`
  - `session.go` — `SessionState`, `DeriveSessionState`
  - `context.go` — `WithRunID`, `GetRunID`, `WithSessionID`, `SessionIDFromContext`, `WithStore`, `WithTurnIndex`
  - `stream.go` — `StreamItem`, `StreamItemKind`, `StreamPayload`, `StreamSink`, MarshalJSON/UnmarshalJSON
  - `ports.go` — `EventAppender` 接口
- `alias.go` facade 层重新导出外部需要的类型

### 关键导出（消除子包拆分命名障碍）
- `ActiveRunner` 全部 11 个字段导出
- graph 相关标识符导出：`BuildAgentGraph`, `BuildPlanExecuteGraph`, `NewGraphAgent`, `GraphAgentContextBinder`
- `AppendStreamItem` 导出, `EventAppender` 接口导出
- 上下文辅助函数导出：`WithStore`, `WithSessionID`, `SessionIDFromContext`, `AppendStreamItem`

### 验证
- `go build ./...` ✅
- `go test ./...` ✅ (33 个包全部通过)

---

## 阻塞项

### Blocker 1: Stream Payload 类型 (~650 行)

**问题**: `graph/` 子包物理迁移被 `AppendStreamItem` 阻塞。`AppendStreamItem` 依赖 `projectStreamItemToEvent` 投影逻辑，后者引用 30+ 个 payload 类型（`RunStartedPayload`, `AssistantMessagePayload`, `ToolCallStartedPayload`, `ContextCompressedPayload` 等），这些类型分布在：

- `internal/runtime/stream_payloads.go` (517 行)
- `internal/runtime/stream_accessors.go` (138 行)
- `internal/runtime/stream_types.go`
- `internal/runtime/stream_payload_decode.go`

**影响**: 如果要移动 `act_node.go`, `plan_node.go`, `plan_execute_graph.go` 到 `graph/`，它们调用 `AppendStreamItem`，迫使 `AppendStreamItem` 及其投影逻辑进入 `graph/` 或 `api/`。但投影逻辑引用的 payload 类型又是 `runtime/` 根定义的，形成循环依赖。

**解决方案** (需要专门会话):
1. 创建 `internal/runtime/stream/` 子包
2. 迁移所有 stream payload 类型、accessors、projection 逻辑到 `stream/`
3. `api/` 只保留 `StreamItem`, `StreamItemKind`, `StreamPayload` 核心类型
4. `graph/` 导入 `api/` + `stream/`
5. 然后迁移 graph 文件

### Blocker 2: Graph 类型与实现耦合

**问题**: `AgentGraphState`, `agentGraphInput`, `graphPhase`, `ObserveDecision` 等类型定义在 `agent_graph.go` 中，但该文件也包含 `buildAgentGraph` 实现。实现引用了 `StreamMessage` 等 payload 类型（Blocker 1）。

**解决方案**: 需要先把类型定义提取到独立的 `graph/types.go`（不依赖 payload），然后移动实现文件。

---

## 剩余工作 (需要 2-3 个专门会话)

### Session A: Stream 子包拆分
- 创建 `internal/runtime/stream/`
- 迁移 `stream_payloads.go`, `stream_accessors.go`, `stream_types.go`, `stream_payload_decode.go`, `stream_projection.go`, `stream_item_json.go`, `stream_agent.go`
- 更新 `runtime/` 根和所有外部包的 import
- 验证编译和测试

### Session B: Graph 子包拆分
- 创建 `internal/runtime/graph/types.go` (提取 `AgentGraphState`, `ObserveDecision`, `graphPhase`)
- 迁移 `agent_graph.go`, `graph_agent.go`, `graph_context_session.go`, `act_node.go`, `observe_node.go`, `plan_node.go`, `plan_execute_graph.go`, `plan_runtime.go`, `safe_parallel_tools_node.go`, `plan_state.go`, `plan_evidence.go`
- 更新 `runner_factory_orchestration.go` 导入 `graph/`
- 验证编译和测试

### Session C: Runner + Executor 子包拆分
- 创建 `internal/runtime/runner/`，迁移 11 个 runner_factory 文件 + `run_builder.go`
- 创建 `internal/runtime/executor/`，迁移 5 个 executor 文件 + `context_session_bridge.go`
- 更新 `runtime/` 根 facade 层
- 验证编译和测试

---

## 当前项目状态

**可编译、可测试、无回归。**

`runtime/` 包结构保持原有 59 个文件位置，但已具备物理拆分的基础：
- `api/` 子包编译通过
- 关键标识符已导出
- `alias.go` facade 层保持外部兼容性

**等待 Stream Payload 类型迁移后即可解锁 graph/runner/executor 物理拆分。**
