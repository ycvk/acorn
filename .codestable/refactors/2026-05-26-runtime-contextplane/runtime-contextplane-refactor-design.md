# Acorn P0 重构设计文档

## 调研结论

### Go 模块拆分最佳实践（来源：Dave Cheney、Ardan Labs、Google Go Style Guide）

1. **按职责而非按类型拆分** — 包应该围绕行为边界组织，而非数据类型
2. **包数量少但职责单一** — "Consider fewer, larger packages"（Dave Cheney），但当超过 ~30 文件时应拆分
3. **使用 internal/ 子包拆分，保持公共接口在父包** — Go 的 internal/ 机制天然支持这种拆分
4. **避免接口膨胀** — Go 的哲学是"小接口、隐式实现"，显式定义大量单实现接口是反模式
5. **向后兼容迁移** — 使用 type alias 做 Parallel Change，但用户已接受破坏性重构，可直接硬切

### contextplane/ 压缩系统分析

**当前问题**：
- 25个非测试文件，4,327行代码
- 压缩本质：判断压力 → LLM总结 → 替换历史消息
- 实际被拆成：BudgetGovernor + CompactionEngine + RehydrationPlanner + MicrocompactEngine + CompressionPipeline + CompressionTokenCounter + CompressionState + 大量辅助文件
- `microcompact.go` 148行，核心逻辑仅70行：遍历消息，把旧tool result替换成 `"[Previous tool result content cleared]"`
- `rehydration_planner.go` 312行，本质：把几个变量（working_checkpoint、skill、plan等）拼成消息
- `compression_state.go` 19行：记录上次摘要和压缩次数
- `nil.go` 16行：一个反射判断接口nil的辅助函数

**工业界对比**：
- LangChain的压缩：ContextWindowTokenBuffer（滑动窗口）或 StuffDocumentsChain（直接截断），无分层engine
- LlamaIndex：ChatMemoryBuffer（简单token计数+截断），无microcompact概念
- 工业界共识：上下文压缩应该是简单、可预测的，不需要5+个抽象层

---

## 重构计划 A：runtime/ 模块拆分

### 目标
将 `internal/runtime/`（52个文件，13,075行）拆分为职责清晰的子模块，每个模块不超过20个文件。

### 拆分策略

#### 步骤 A1：创建 `internal/run/` — Run 构建模块

**迁移文件**：
```
internal/runtime/runner.go           → internal/run/factory.go
internal/runtime/run.go              → internal/run/builder.go
internal/runtime/runner_build.go     → internal/run/deps.go
internal/runtime/runner_catalog.go   → internal/run/catalog.go
internal/runtime/runner_mcp.go       → internal/run/mcp.go
internal/runtime/runner_orchestration.go → internal/run/orchestration.go
internal/runtime/runner_toolset.go   → internal/run/toolset.go
internal/runtime/runner_toolset_build.go → internal/run/toolset_build.go
internal/runtime/runtime_deps.go     → internal/run/deps.go（合并）
internal/runtime/registry.go         → internal/run/registry.go
internal/runtime/store_ports.go      → internal/run/store.go
```

**公共接口留在 runtime/**：
- `RunBuilder` 接口保留在 `internal/runtime/`，由 `internal/run/builder.go` 实现
- `RunnerFactory` 保留在 `internal/runtime/`，委托给 `internal/run/factory.go`
- 这样 app/ 和 web/ 的 import 路径暂时不变

**风险**：中等 — 需要更新 app/container.go 中的依赖注入

#### 步骤 A2：创建 `internal/plan/` — Plan 编排模块

**迁移文件**：
```
internal/runtime/plan.go           → internal/plan/node.go
internal/runtime/plan_steps.go     → internal/plan/steps.go
internal/runtime/plan_evidence.go  → internal/plan/evidence.go
internal/runtime/plan_execute.go   → internal/plan/execute.go
internal/runtime/plan_store.go     → internal/plan/store.go
internal/runtime/api/plan.go       → internal/plan/api.go（合并）
```

**公共接口**：
- `PlanNode`、`PlanStore`、`PlanningPromptProvider` 保留在 `internal/plan/`
- runtime/executor.go 直接 import `internal/plan`

**风险**：低 — plan 系统相对独立，外部引用少

#### 步骤 A3：提升 `internal/runtime/stream/` → `internal/stream/`

**操作**：
- 将 `internal/runtime/stream/` 提升到 `internal/stream/`
- 更新所有 import 路径
- 删除 `internal/runtime/alias_stream.go`

**迁移文件**：
```
internal/runtime/stream/           → internal/stream/
internal/runtime/alias_stream.go    → 删除
```

**风险**：低 — stream/ 已经是独立子包，只是路径变更

#### 步骤 A4：合并辅助文件到 `internal/runtime/helpers.go`

**操作**：
- 将 `alias.go` 中的有效类型定义（非 alias 部分）合并到 helpers.go
- 删除 `alias.go`
- 将 `checkpoint_json.go` 合并到 helpers.go 或 executor.go
- 将 `elicitation.go` 合并到 executor.go
- 将 `fact_extractor.go` 合并到 executor.go 或 tool.go
- 将 `skill_types.go` 合并到 runner.go 或删除（如果只在内部使用）

**风险**：低

#### 步骤 A5：整合执行核心

**保留在 runtime/ 的文件**：
```
executor.go          — 核心执行编排器
context_session_bridge.go  — ContextSession 引导
contextplane_bridge.go   — 压缩/压力事件转换
helpers.go           — 通用辅助函数
tool.go              — 工具审计包装
safe_parallel_tools_node.go — 并行工具节点
streaming_tool_executor.go — 流式工具执行
streaming_assistant_stream.go — 流式助手
assistant_stream.go  — 助手流
assistant_streamer.go — 助手Streamer
checkpoint_json.go   — 或合并到 helpers
```

**迁移后 runtime/ 规模**：约 12 个文件（从 36 个减少）

---

## 重构计划 B：contextplane/ 压缩系统简化

### 目标
将 `internal/contextplane/`（25个文件，4,327行）简化为 15 个文件以内，消除过度抽象。

### 简化策略：硬切合并

用户已接受破坏性重构，因此采用硬切策略：直接删除过度拆分的文件，将逻辑合并到核心文件中。

#### 步骤 B1：删除 Microcompact Engine，内联到 Compression Pipeline

**删除**：`microcompact.go`（148行）

**合并位置**：`compression_pipeline.go`

**新函数**（替代整个 microcompact engine）：
```go
// microcompact 将旧的 tool result 替换为占位符以释放 token
func microcompact(messages []adk.Message, turnIndex int, catalog *tooling.Catalog) ([]adk.Message, int, []string) {
    const clearedPlaceholder = "[Previous tool result content cleared]"
    var cleared []string
    freed := 0
    // ... 70行核心逻辑
    return messages, freed, cleared
}
```

**风险**：低 — 逻辑简单，有测试覆盖

#### 步骤 B2：删除 RehydrationPlanner，合并到 CompactionEngine

**删除**：`rehydration_planner.go`（312行）

**合并位置**：`compaction_engine.go`

**新函数**（替代 RehydrationPlanner）：
```go
func buildRehydratePackets(messages []adk.Message, toolState *ToolLifecycleState, currentPlan string, recentPaths []string) []RehydratePacket {
    // 直接从消息中提取 tagged content，组装 packets
    // 约 80 行，无需独立 struct 和 builder 模式
}
```

**风险**：中等 — rehydration 逻辑较复杂，需要保留测试

#### 步骤 B3：合并过度拆分的辅助文件

**删除**：`compression_state.go`（19行）
**合并到**：`context_session.go` — 将 `CompressionState` 作为 `defaultContextSession` 的字段

**删除**：`nil.go`（16行）
**合并到**：`compression_helpers.go` 或直接内联到使用处

**删除**：`context_session_context.go`（20行）
**合并到**：`context_session.go`

**删除**：`compression_summary_model.go`（284行）
**拆分合并**：
- 摘要构建逻辑 → `compaction_engine.go` 的 `buildSummarizerInput` 附近
- 结构化验证逻辑 → 内联到 `compaction_engine.go` 的 `validateStructuredContinuationSummary`
- 保留范围计算 → 内联到 `compaction_engine.go`

**风险**：低 — 都是纯函数，行为等价

#### 步骤 B4：合并 compression 辅助函数

**删除**：`compression_helpers.go`（196行）
**合并到**：`compaction_engine.go`

**删除**：`compression_prompt.go`（99行）
**合并到**：`compaction_engine.go` 或 `compression.go`

**风险**：低

#### 步骤 B5：合并 compression.go 和 compression_pipeline.go

**操作**：
- `compression.go`（394行）定义了 `CompressionPipeline` 和中间件适配
- `compression_pipeline.go`（221行）定义了 `defaultContextCompressionPipeline` 和三层压缩协调
- 两者都是压缩管道的不同侧面，应该合并为一个文件

**合并到**：`compression.go` — 保留为压缩系统的单一入口

**风险**：中等 — 两个文件交叉引用，需要仔细合并

#### 步骤 B6：保留的核心文件

简化后 contextplane/ 的文件结构：
```
assembly.go              — 上下文组装（保留）
budget_governor.go       — 预算治理（保留，194行，职责清晰）
compaction_engine.go     — 压缩引擎（合并后约 500-600 行）
compression.go           — 压缩管道入口（合并 compression_pipeline.go 后约 500 行）
compression_token_counter.go — Token计数器（保留，121行，独立职责）
context_session.go       — Session管理（合并 compression_state + context_session_context 后约 420 行）
tool_lifecycle.go        — 工具生命周期（保留，539行，复杂但必要）
tool_lifecycle_middleware.go — 工具中间件（保留，45行）
types.go                 — 类型定义（保留，231行）
memory_provider.go       — 内存提供者（保留，313行）
skill_provider.go        — 技能提供者（保留，58行）
skill_catalog_provider.go — 技能目录提供者（保留，84行）
run_context_snapshot.go  — 运行上下文快照（保留，96行）
selected_skill.go        — 选中技能（保留，22行，或合并到 skill_provider.go）
envelope.go              — 信封消息（保留，33行，或合并到 assembly.go）
context_overflow_error.go — 溢出错误检测（保留，53行）
```

**简化后总数**：约 16 个文件（从 25 个减少）
**核心压缩系统文件数**：从 12 个减少到 4 个（compaction_engine.go、compression.go、budget_governor.go、compression_token_counter.go）

---

## 依赖影响分析

### runtime/ 拆分后的依赖变化

**app/ 包的影响**：
- `app/container.go` — 需要更新 import 路径（`internal/run`、`internal/plan`、`internal/stream`）
- `app/chat_service.go` — 引用 runtime.StreamSink，路径变为 stream.StreamSink
- `app/client_service_run.go` — 引用 runtime 类型，路径变更
- `app/trace_service.go` — 引用 runtime 类型
- `app/run_service.go` — 引用 runtime 类型
- `app/session_state_service.go` — 引用 runtime 类型

**web/ 包的影响**：
- `web/server.go` — 接口定义引用 runtime 类型
- `web/handler_helpers.go` — 引用 runtime 类型
- `web/dto_*.go` — 可能引用 runtime 类型

**orchestration/ 包的影响**：
- `orchestration/*.go` — 引用 runtime.StreamItemKind 等类型，变为 stream.StreamItemKind

### contextplane/ 简化后的依赖变化

**影响较小** — 主要是内部文件合并，对外接口不变。
- `ContextPlane` 接口保留在 `types.go`
- `defaultContextPlane` 保留
- 外部只关心 `BuildHandlers`、`Assemble`、`OnToolCall` 等方法，不变

---

## 执行顺序

1. **先做 B（contextplane/ 简化）** — 影响范围小，主要是内部合并，风险低
2. **再做 A（runtime/ 拆分）** — 影响范围大，涉及 20+ 外部文件的 import 更新

---

## 验证策略

每步完成后必须：
1. `go build ./...` — 编译通过
2. `go test ./internal/contextplane/...` 或 `go test ./internal/runtime/...` — 相关测试通过
3. `make lint` — 无 lint 错误
4. `make format-check` — 格式正确

---

## 回滚策略

- 每次文件迁移都通过 `git mv` 保留历史
- 每步完成后立即 commit，便于回滚
- 破坏性变更在 design 文档中记录，便于问题排查
