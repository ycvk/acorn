---
doc_type: refactor-design
refactor: 2026-05-25-contextplane-simplify
status: approved
scope: ContextPlane compression/budget internals only; no OpenAPI, config schema, SQLite schema, mobile DTO, or runtime mode behavior changes
summary: Hard-cut pseudo budgets and one-implementation compression interfaces while preserving ContextSession/ContextPlane protocol behavior.
---

# contextplane-simplify refactor design

## 1. 本次范围

- 从 scan 勾选：#1 删除半生效的 section `BudgetAllocator`；#2 把 micro/reactive compact 接口内联到 pipeline；#3 收窄单实现接口的导出表面；#4 同步 current-truth 文档里的命名边界。
- 明确不做：不改变 context pressure 阈值；不改变 compact summary prompt；不改变 rehydrate packet 行为；不改 public `context:` config；不新增 fallback / compat alias / feature flag。
- 预估总工作量：中等，主要是类型签名和测试同步。
- 总风险档位：中。行为应保持等价，但会删除 internal exported symbols，属于 destructive hard cut。

## 2. 前置依赖

- 测试覆盖：已有 `context_session_test.go`、`compression_test.go`、`budget_governor_test.go`、`rehydration_planner_test.go`、`reactivecompact_test.go`、`pipeline_metrics_test.go`、`assembly_test.go`，足够支撑 refactor。
- 调用方搜索：已用 `rg` 确认 `BudgetUsed` / `BudgetAllocator` 无生产消费者；micro/reactive interfaces 只在 `internal/contextplane` 内部；pipeline constructors 的 production call sites 在 runtime/app/orchestration tests。
- 工作树注意：当前已有 `.codestable/refactors/` 目录；缺失 `.codestable/attention.md`，本次按 live repo hard-cut design 继续，不依赖旧 workflow 骨架。

## 3. 执行顺序

### 步骤 1：删除 section BudgetAllocator 和 BudgetUsed

- 引用方法：M-L2-02 Inline Function / 删除空壳抽象
- 具体操作：
  - 从 `ContextPlane` interface 删除 `Budget(...)`。
  - 从 `AssembleResult` 删除 `BudgetUsed`。
  - 删除 `budget.go` 和 `budget_test.go`。
  - 从 `assembly.go` 删除 `p.Budget(...)`、`assemblePresentSections(...)` 和 `BudgetUsed` 赋值。
  - 更新 `assembly_test.go` 去掉 `BudgetUsed` sum 断言。
- 退出信号：
  - `rg "BudgetUsed|BudgetStatus|BudgetRequest|NewBudgetAllocator|SectionSkill|SectionMemory|SectionToolDef|SectionConversation" internal/contextplane internal/runtime internal/app internal/orchestration` 无生产残留。
  - `go test ./internal/contextplane` 通过。
- 验证责任：AI 自证。
- 回滚：回退本步骤修改文件即可；未触及外部 schema。

### 步骤 2：内联 micro/reactive compact layer

- 引用方法：M-L2-02 Inline Function / 删除空壳包装函数
- 具体操作：
  - 删除 `MicrocompactEngine` / `ReactiveCompactEngine` interface 和 `ReactiveCompactRequest` / `ReactiveCompactResult`。
  - `defaultContextCompressionPipeline` 字段改为 `microcompact *defaultMicrocompactEngine`、`autocompact *defaultCompactionEngine` 或当前步骤暂保留 compaction seam，`reactivecompact` 字段删除。
  - `newReactiveCompactEngine(...).Recover(...)` 逻辑搬进 `defaultContextCompressionPipeline.runReactiveCompact(...)`。
  - `NewDefaultContextCompressionPipeline` 对 required deps fail-loud：`Governor`、`CompactionEngine`、`TokenCounter` 不满足时不再静默跳过 layer。若保持 constructor 不返回 error，则在 `Compress()` 开头验证并返回明确错误；后续步骤再考虑改 constructor 签名。
  - 删除 `reactivecompact.go`，把 reactive tests 合并进 `compression_test.go` 或重写为 pipeline-level tests。
- 退出信号：
  - `go test ./internal/contextplane -run 'TestContextCompressionPipeline|TestContextSessionReactiveCompact|TestCompressionOutcomeIncludesLayersApplied|TestReactive'` 通过。
  - `go test ./internal/contextplane` 通过。
- 验证责任：AI 自证。
- 回滚：恢复 `reactivecompact.go`、types 和 pipeline 字段。

### 步骤 3：把单实现接口收窄为具体类型或未导出 seam

- 引用方法：M-L3-07 Single Responsibility Split
- 具体操作：
  - `NewBudgetGovernor` 返回 `*BudgetGovernor` 或具体未导出实现按 Go idiom 重命名为 `NewBudgetGovernor(...) *BudgetGovernor`；原 interface 删除。
  - `NewDefaultCompactionEngine` 返回 `*CompactionEngine` 或 `*defaultCompactionEngine`，根据包内命名统一；如果 production 需要 exported concrete type，优先 `type CompactionEngine struct`。
  - `NewDefaultRehydrationPlanner` 返回具体 planner；如果 planner 只由 compaction engine 内部默认构造，可直接使用 zero-value concrete helper。
  - `NewCompressionPipeline` / `NewDefaultContextCompressionPipeline` 返回具体类型；`ContextSessionOptions.Pipeline` 改为具体 pipeline 或未导出 minimal interface，只保留测试必须的 seam。
  - 同步 runtime/orchestration test call sites。
- 退出信号：
  - `rg "type (BudgetGovernor|CompactionEngine|RehydrationPlanner|CompressionPipeline|ContextCompressionPipeline) interface" internal/contextplane` 无 exported interface 残留。
  - `go test ./internal/contextplane ./internal/runtime ./internal/orchestration` 通过。
- 验证责任：AI 自证。
- 回滚：恢复接口类型和 constructor 返回签名。

### 步骤 4：同步文档 current truth

- 引用方法：M-L2-02 Inline Function / 删除旧命名抽象后同步引用
- 具体操作：
  - 更新 `AGENTS.md`、`docs/architecture/runtime-execution.md`、`docs/architecture/runtime-context-memory-decision.md`、`docs/architecture/ARCHITECTURE.md`。
  - 保留行为约束，不保留旧接口名作为架构边界。
- 退出信号：
  - `rg "BudgetAllocator|MicrocompactEngine|ReactiveCompactEngine|RehydrationPlanner" AGENTS.md docs internal/contextplane` 只剩允许的 behavior/test references。
  - `git diff --check` 通过。
- 验证责任：AI 自证。
- 回滚：恢复文档措辞。

### 步骤 5：全量验证

- 引用方法：M-L1-04 Characterization Test（用现有测试固化行为）
- 具体操作：
  - 跑 targeted package tests。
  - 跑 repo 标准检查。
- 退出信号：
  - `go test ./internal/contextplane ./internal/runtime ./internal/orchestration ./internal/app`
  - `make test`
  - `make format-check`
  - `make lint`
  - `git diff --check`
- 验证责任：AI 自证。
- 回滚：如果 targeted tests 失败，回到对应步骤；如果 broad tests 暴露外部 contract 问题，停止并重新设计，不加兼容层。

## 4. 风险与看点

- 最大风险是把“测试 seam”误删成难测代码。处理原则：可以保留未导出小 seam，但不保留 exported pseudo interface。
- `ContextSession` 和 `ContextPlane` 是真实协议边界，不能为了减少接口数把它们并进具体 runtime。
- `RehydrationPlanner` 的行为约束不能丢：compact 后 summary 不是唯一恢复上下文；packets 必须继续 token-counted、oversized fail-loud。
- `BudgetAllocator` 删除后，`ContextPriority` 在 context assembly 内不再影响 pseudo section allocation。当前 live code 本来也没有用 allocation 驱动实际预算；如果未来要做 priority-aware assembly，应另开 feature，不在 refactor 中夹带行为。
