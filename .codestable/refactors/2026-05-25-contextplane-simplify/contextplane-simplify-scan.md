---
doc_type: refactor-scan
refactor: 2026-05-25-contextplane-simplify
status: user-reviewed
scope: internal/contextplane context protocol compression/budget internals plus direct runtime/orchestration call sites
summary: 4 findings: structure 3, readability 1; risk low 2, medium 2
---

# contextplane-simplify scan

## 总览

- 扫描范围：`internal/contextplane/{budget.go,budget_governor.go,types.go,assembly.go,compression.go,compression_pipeline.go,context_session.go,compaction_engine.go,rehydration_planner.go,microcompact.go,reactivecompact.go}`，对应 tests，以及直接调用点 `internal/runtime/context_session_bridge.go`、`internal/runtime/runner_build.go`、`internal/app/container.go`、`internal/orchestration/direct_response_builder_test.go`。
- 发现 4 条优化点：结构 3 / 性能 0 / 可读性 1。
- 按风险：低 2 / 中 2 / 高 0。
- 建议先做：#1 #2 #3。它们都是内部 hard cut，有测试可自证，不改 `/v1`、OpenAPI、SQLite schema 或外部 CLI 行为。
- 建议慎做 / 后做：#4。文档同步必须在代码定型后做，避免把中间设计写成 current truth。
- 前置检查 7 条：第 6 条按整个 `internal/contextplane` 会超范围；本次已收窄到 context protocol compression/budget internals，实际扫描文件 12 个生产文件 + 对应测试，关键路径有单测覆盖，允许进入 design。

## 条目

### #1 删除半生效的 section BudgetAllocator ✓

- **位置**：`internal/contextplane/budget.go:8-176`，`internal/contextplane/types.go:27-45`，`internal/contextplane/assembly.go:22-72`，`internal/contextplane/assembly_test.go:106-108`，`internal/contextplane/budget_test.go:1-98`
- **分类**：结构
- **现状**：`Assemble()` 先调用 `p.Budget(...)` 计算 skill/memory/tool_def/conversation 的 20/30/20/30 allocation，并把结果放进 `AssembleResult.BudgetUsed`；后续实际装配只使用 `LayeredMemoryBudget` 和 `budgetedContextMessages(...)` 的总 token 检查，没有按这些 allocation 限制 section。
- **问题**：`BudgetUsed` 全仓库只有 `assembly_test.go` 断言 sum；`BudgetAllocator` 只有测试和 `Assemble()` 内部使用。它制造了“section budget 已生效”的错觉，但实际不驱动裁剪或失败条件。
- **建议**：删除 `BudgetAllocator`、`BudgetRequest`、`BudgetStatus`、`Section*`、`ContextPlane.Budget`、`AssembleResult.BudgetUsed` 和 `budget_test.go`；`Assemble()` 直接构造 context packet 并依靠真实 token counter 的总预算 fail-loud。
- **建议映射的方法**：M-L2-02（Inline Function / 删除空壳抽象）
- **风险**：中（删除 exported internal symbols，会影响同包测试和可能的内部调用点；全仓 `rg` 显示没有外部生产消费者）
- **验证**：AI 自证（`go test ./internal/contextplane ./internal/runtime ./internal/orchestration`，`rg "BudgetUsed|BudgetStatus|BudgetRequest|NewBudgetAllocator|SectionSkill|SectionMemory|SectionToolDef|SectionConversation"` 只允许无生产残留）
- **范围**：约 260 行 / 5 文件

### #2 把 micro/reactive compact 接口内联到 pipeline ✓

- **位置**：`internal/contextplane/types.go:237-269`，`internal/contextplane/compression_pipeline.go:20-149`，`internal/contextplane/microcompact.go:19-28`，`internal/contextplane/reactivecompact.go:9-46`，`internal/contextplane/reactivecompact_test.go:1-83`
- **分类**：结构
- **现状**：`MicrocompactEngine` 和 `ReactiveCompactEngine` 各只有一个内部实现；`ReactiveCompactEngine` 只把 `PreservePolicy.RecentTurns` 减半后再次调用 `CompactionEngine`；pipeline 还允许 `TokenCounter == nil` 时静默不创建 micro/reactive layer。
- **问题**：两个接口没有多实现或外部注入价值；`reactivecompact.go` 46 行，接口和 nil-engine 错误占了可观比例；`compression_pipeline.go:39-56` 的“nil layer skip”与 fail-loud 风格冲突。
- **建议**：删除 `MicrocompactEngine` / `ReactiveCompactEngine` 接口和 `defaultReactiveCompactEngine`；pipeline 保存具体 `*defaultMicrocompactEngine`，reactive recovery 改为 pipeline 私有方法 `runReactiveCompact(...)`；构造 pipeline 时要求 `TokenCounter` 和 `CompactionEngine` 显式存在，缺失直接在 `Compress()` 或构造后首次使用时报错，不允许静默跳过 compact layer。
- **建议映射的方法**：M-L2-02（Inline Function / 删除空壳包装函数）
- **风险**：中（需要同步多处 tests；reactive compact 的 layer metrics 和 recent-turn halving 必须保持等价）
- **验证**：AI 自证（`go test ./internal/contextplane -run 'TestContextCompressionPipeline|TestContextSessionReactiveCompact|TestCompressionOutcomeIncludesLayersApplied|TestReactive'`，再跑完整 `go test ./internal/contextplane`）
- **范围**：约 180 行 / 5 文件

### #3 收窄单实现接口的导出表面 ✓

- **位置**：`internal/contextplane/budget_governor.go:23-60`，`internal/contextplane/compaction_engine.go:58-112`，`internal/contextplane/rehydration_planner.go:29-70`，`internal/contextplane/compression.go:25-47`，`internal/contextplane/types.go:233-235`，`internal/runtime/context_session_bridge.go:38-55`，`internal/orchestration/direct_response_builder_test.go:580`
- **分类**：结构
- **现状**：`BudgetGovernor`、`CompactionEngine`、`RehydrationPlanner`、`CompressionPipeline`、`ContextCompressionPipeline` 都是 exported interface 或返回 interface 的 constructor；生产路径只有一个实现，测试用 fake 主要服务于同包 pipeline/session tests。
- **问题**：一接口一实现增加跳转成本和 nil 分支；`NewBudgetGovernor` / `NewDefaultCompactionEngine` / `NewDefaultRehydrationPlanner` 返回接口，让调用方看不到具体能力，也让内部演进被假抽象绑住。
- **建议**：保留 `ContextPlane` 和 `ContextSession` 两个主协议接口；把 `BudgetGovernor`、`CompactionEngine`、`RehydrationPlanner`、`CompressionPipeline`、`ContextCompressionPipeline` 改成具体类型依赖或未导出最小测试 seam。对 `CompactionEngine`，如果同包测试仍需要 fake，优先用具体 fake function field 或未导出小接口放在 pipeline 内部，而不是 exported package contract。
- **建议映射的方法**：M-L3-07（Single Responsibility Split / 去掉伪抽象后让协议边界只保留真实职责）
- **风险**：低（包内生产调用点少，外部 production 只通过 constructors；但测试 helpers 需要同步）
- **验证**：AI 自证（`go test ./internal/contextplane ./internal/runtime ./internal/orchestration`；`rg "type (BudgetGovernor|CompactionEngine|RehydrationPlanner|CompressionPipeline|ContextCompressionPipeline) interface"` 应无残留，除非保留未导出 test seam）
- **范围**：约 240 行 / 8 文件

### #4 同步 current-truth 文档里的命名边界 ✓

- **位置**：`AGENTS.md:60`，`docs/architecture/runtime-execution.md:31`，`docs/architecture/runtime-context-memory-decision.md:55`，`docs/architecture/ARCHITECTURE.md:49`
- **分类**：可读性
- **现状**：架构文档把 `RehydrationPlanner` 作为 current-truth 命名边界；如果 #3 把它降级为具体 helper 或未导出类型，文档仍会暗示它是公共架构组件。
- **问题**：代码 hard cut 后文档如果继续描述旧 exported interface，会让后续 review 误以为该接口必须保留。
- **建议**：代码定型后，把文档从“`RehydrationPlanner` owns ...”改成“contextplane post-compact rehydration helper owns ...”，保留行为约束：packet 恢复、token limit、oversized fail-loud、recent files 只来自显式输入。
- **建议映射的方法**：M-L2-02（Inline Function / 删除旧命名抽象后同步引用）
- **风险**：低（文档措辞变化，不改变 runtime）
- **验证**：AI 自证（`rg "RehydrationPlanner|BudgetAllocator|MicrocompactEngine|ReactiveCompactEngine"` docs AGENTS.md internal/contextplane` 确认只剩行为说明或测试必要引用）
- **范围**：约 20 行 / 4 文件
