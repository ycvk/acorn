# Acorn 重构 - Phase 2: Runtime + Orchestration 清理

Date: 2026-06-21
Parent Spec: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md`
Depends on: Phase 1(基础层清理)

## Goal

砍掉 `plan_execute` / `single_agent` 编排模式、`ChildAgentExecutor`、`SubagentExecutor`、verifier child run、plan evidence ledger。只保留 `direct_response` + `AgentLoop.RunOneIteration` + `ExecuteRound`。简化 `Executor` 为单一模式执行。

## Architecture

```text
本阶段范围:
  runtime/        — 删除 plan/ subagent childagent,简化 executor/runner
  orchestration/  — 删除 single_agent_builder child_agent,简化 direct_response
  runtime/plan/   — 整个目录删除
  runtime/graph/  — 整个目录删除(如果只服务 plan/single_agent)

不在本阶段:
  contextplane/   — Phase 3
  memorymodule/   — Phase 4
  web/ app/       — Phase 5
```

## Baseline / Authority Refs

- Spec §3.5 编排层简化、§3.8 工具系统简化
- `internal/runtime/executor_run.go` — 当前 run lifecycle
- `internal/runtime/runner_orchestration.go` — 当前 mode 分发
- `internal/orchestration/direct_response_builder.go` — direct_response 实现

## Compatibility Boundary

- `orchestration.ExecuteRound` 和 `AgentLoop.RunOneIteration` 保留(direct_response 用)
- `direct_response_builder.go` 保留并简化(删除引用已删除类型的代码)
- `Executor.Run` / `ExecuteMessages` 接口签名不变(外部 caller 不受影响)
- 工具执行保留 `toolExecutionScheduler`(简化后)

## Verification

- `go build ./internal/runtime/... ./internal/orchestration/...` 通过
- `go test ./internal/runtime/... ./internal/orchestration/...` 通过
- `go build ./...` 通过(contextplane/memorymodule 可能在 Phase 3/4 修复)

---

## Task 1: 删除 plan/ 和 graph/ 目录

**Files:**
- Delete: `internal/runtime/plan/`(整个目录,15 个 .go 文件 + 测试)
- Delete: `internal/runtime/graph/`(整个目录,如果只服务 plan/single_agent)
- Delete: `internal/runtime/api/plan.go`
- Modify: `internal/runtime/api/api.go`(删除引用 plan.go 的代码)

**Why:** plan/ 目录是 plan_execute 的核心实现。graph/ 是 single_agent/plan_execute 的 graph builder。两者都是完整删除。

**Impact/Compatibility:** 删除后 `runtime/api` 中的 `PlanStore` 接口和 `plan.go` 不再存在。`api.go` 需要删除对 `PlanStore` 的引用。

**Verification:** `go build ./internal/runtime/api`(只编译 api 包)

### Steps

- [ ] **1.1 删除 `internal/runtime/plan/` 目录**:`rm -rf internal/runtime/plan/`
- [ ] **1.2 删除 `internal/runtime/graph/` 目录**:`rm -rf internal/runtime/graph/`
- [ ] **1.3 删除 `internal/runtime/api/plan.go`**
- [ ] **1.4 修改 `internal/runtime/api/api.go`**:删除 `PlanStore` 接口、`WithPlanStore`、`GetPlanStore` 等。删除 import `internal/model` 中的 plan 相关类型引用。
- [ ] **1.5 运行验证**:`go build ./internal/runtime/api`。修复编译错误。
- [ ] **1.6 Commit**:`refactor(runtime): delete plan/ graph/ api/plan.go`

---

## Task 2: 删除 subagent_executor + child_agent

**Files:**
- Delete: `internal/runtime/subagent_executor.go`
- Delete: `internal/runtime/subagent_executor_run.go`
- Delete: `internal/runtime/subagent_executor_plan.go`
- Delete: `internal/runtime/subagent_executor_factory.go`
- Delete: `internal/runtime/subagent_executor_eval.go`
- Delete: `internal/runtime/subagent_executor_test.go`
- Delete: `internal/runtime/runner_childagent.go`
- Delete: `internal/runtime/subagent_adapter.go`
- Delete: `internal/orchestration/child_agent.go`
- Delete: `internal/orchestration/single_agent_builder.go`
- Delete: `internal/orchestration/single_agent_builder_test.go`
- Delete: `internal/orchestration/verifier.go`
- Delete: `internal/orchestration/verifier_test.go`

**Why:** SubagentExecutor + ChildAgentExecutor + verifier 是 plan_execute / single_agent 的执行机制。direct_response 不需要它们。

**Impact/Compatibility:** `orchestration.ChildAgentExecutor` 接口删除。`orchestration.VerificationRequest` / `VerificationResult` 类型删除。`orchestration.SingleAgentRequest` / `SingleAgentAssembly` 类型删除。`orchestration.PlanExecuteRequest` 类型删除。`orchestration.PlanStore` 标记接口删除。

**Verification:** `go build ./internal/orchestration`(会有编译错误,因为 direct_response_builder 引用了部分类型——Task 3 修复)

### Steps

- [ ] **2.1 删除所有 subagent_executor*.go 文件**
- [ ] **2.2 删除 `runner_childagent.go`**
- [ ] **2.3 删除 `subagent_adapter.go`**
- [ ] **2.4 删除 `orchestration/child_agent.go`**
- [ ] **2.5 删除 `orchestration/single_agent_builder.go` + 测试**
- [ ] **2.6 删除 `orchestration/verifier.go` + 测试**
- [ ] **2.7 Commit**:`refactor(runtime/orchestration): delete subagent/child_agent/single_agent/verifier`

---

## Task 3: 简化 orchestration types + direct_response

**Files:**
- Modify: `internal/orchestration/types.go`
- Modify: `internal/orchestration/direct_response_builder.go`
- Modify: `internal/orchestration/agent_loop.go`
- Modify: `internal/orchestration/action_round.go`
- Modify: `internal/orchestration/registry.go`
- Test: `internal/orchestration/direct_response_builder_test.go`
- Test: `internal/orchestration/agent_loop_test.go`

**Why:** types.go 和 direct_response_builder 引用了已删除的类型。需要清理引用,只保留 direct_response 路径。

**Impact/Compatibility:** `types.go` 删除:`SingleAgentRequest`、`PlanExecuteRequest`、`RunAssembly` 中的 plan 相关字段、`PlanStore` 接口、`GraphBuildRequest`、`PlanExecuteGraphBuildRequest`、`GraphBuilder`、`PlanExecuteGraphBuilder`、`ToolNodeFactory`、`ToolBuilder`、`HandlersBuilder`、`InstructionBuilder`、`ToolLifecycleBinder`。保留:`DirectResponseRequest`、`AssistantStreamRequest`、`AssistantStreamer`、`ToolInvoker`、`StreamingExecutor`、`AssistantStreamResult`、`InterleavedStream`、`ToolLifecycleStateView`、`AssembleResultView`。`direct_response_builder.go` 简化:`BuildDirectResponse` 删除 plan/single_agent 分发。`DefaultPlane` 简化:删除 `BuildSingleAgent` / `BuildPlanExecute` 方法。`DefaultPlaneOptions` 简化:删除 graph builders、child executor factory。

**Verification:** `go build ./internal/orchestration && go test ./internal/orchestration`

### Steps

- [ ] **3.1 重写 `types.go`**:只保留 direct_response 需要的类型。删除所有 plan/single_agent/graph 相关类型。保留 `ToolInvoker`、`StreamingExecutor`、`AssistantStreamer` 等。`DefaultPlane` 只保留 `BuildDirectResponse`。
- [ ] **3.2 重写 `direct_response_builder.go`**:`DefaultPlane` 删除 `BuildSingleAgent` / `BuildPlanExecute`。`DefaultPlaneOptions` 删除 graph builders / child executor factory / tool builder / tool node factory / handlers builder / context binders 等注入字段——这些改为 direct_response 内部直接构建(简化装配)。`BuildDirectResponse` 保留核心逻辑:构建 tool catalog → 绑定 tool lifecycle → 构建 `directResponseAgent`。
- [ ] **3.3 审查 `agent_loop.go`**:`AgentLoop.RunOneIteration` 保留。`ExecuteRound` 保留。`ExecuteToolCalls` 保留。删除引用已删除类型的代码。
- [ ] **3.4 审查 `action_round.go`**:`RunActionRound` 如果引用 ActNode 或 plan_execute,简化或删除。如果 direct_response 不用 `RunActionRound`(只用 `RunOneIteration`),删除文件。
- [ ] **3.5 更新 `direct_response_builder_test.go`**:删除 plan/single_agent/verifier 测试。保留 direct_response 路径测试。
- [ ] **3.6 运行验证**:`go build ./internal/orchestration && go test ./internal/orchestration`。修复编译错误直到通过。
- [ ] **3.7 Commit**:`refactor(orchestration): keep only direct_response, remove plan/single_agent types`

---

## Task 4: 简化 runner_orchestration + runner_build

**Files:**
- Modify: `internal/runtime/runner_orchestration.go`
- Modify: `internal/runtime/runner_build.go`
- Modify: `internal/runtime/runner_build_selection.go`
- Modify: `internal/runtime/runner_build_selection_test.go`
- Modify: `internal/runtime/runner_build_mcp.go`
- Modify: `internal/runtime/runner.go`
- Modify: `internal/runtime/run.go`
- Modify: `internal/runtime/run_build_helpers.go`

**Why:** runner_build 的 `buildAssembly` 当前按 mode 分发到三个 builder。砍掉 plan/single_agent 后只剩 direct_response。run selection 简化为:有 explicit skill → 加载 skill context;否则无。

**Impact/Compatibility:** `buildAssembly` 删除 mode 分发,直接调 `directResponseRequest`。删除 `buildSingleAgentRun` / `buildPlanExecuteRun`。`resolveRunSelection` 简化:删除 `resolveRunSelectionByDecision` 的复杂逻辑,保留简单 skill 匹配。`RunnerFactory` 删除 `ChildAgentExecutorFactory` 字段。`runner_orchestration.go` 删除 `BuildRuntimePlanExecuteGraph`、`runtimeGraphDependencies`、`wrapGraphAgent`。保留 `BuildRuntimeAgentGraph`(如果 direct_response 用)或简化。

**Verification:** `go build ./internal/runtime && go test ./internal/runtime`

### Steps

- [ ] **4.1 重写 `runner_orchestration.go`**:删除 `BuildRuntimePlanExecuteGraph`、`runtimeGraphDependencies`、`wrapGraphAgent`、`orchestrationPlane` 接口(如果只服务测试)。保留 `BuildRuntimeAgentGraph` 如果 direct_response 需要,否则删除。简化 `defaultOrchestrationPlaneDeps`。
- [ ] **4.2 重写 `runner_build.go`**:`buildAssembly` 删除 mode 分发,直接构建 `directResponseRequest`。删除 `buildSingleAgentRun` / `buildPlanExecuteRun` / `baseAssemblyFields`(如果不再需要共享)。`assembleRuntimeDeps` 删除 plan store / child agent executor factory 装配。
- [ ] **4.3 重写 `runner_build_selection.go`**:`resolveRunSelection` 简化:如果有 `req.SkillID` → 加载 skill;否则无 skill。删除 `resolveRunSelectionByDecision`、`blockRun`、`resolveExplicitSkill`、`resolveTopSkill`、`hasCapabilityFailure` 等复杂逻辑。保留 `retrieveSkillCandidates` 的简单匹配。
- [ ] **4.4 重写 `runner.go`**:`RunnerFactory` 删除 `ChildAgentExecutorFactory` 字段。`ChildAgentRuntimeDeps` 类型删除。`NewRunnerFactory` 删除 child executor 相关参数。
- [ ] **4.5 重写 `run.go`**:`RunnerFactory.buildRun` 简化:创建 chat model → bootstrap MCP → build tool catalog → Prepare memory → assemble ContextPlane → `BuildDirectResponse`。删除 mode 判断。
- [ ] **4.6 更新 `runner_build_selection_test.go`**:删除 decision/block/capability failure 测试。保留 simple skill match 测试。
- [ ] **4.7 运行验证**:`go build ./internal/runtime && go test ./internal/runtime`。修复编译错误直到通过。
- [ ] **4.8 Commit**:`refactor(runtime): simplify runner to direct_response only`

---

## Task 5: 简化 executor_run + executor_finalize

**Files:**
- Modify: `internal/runtime/executor_run.go`
- Modify: `internal/runtime/executor_finalize.go`
- Modify: `internal/runtime/executor_archive.go`
- Modify: `internal/runtime/executor_resume.go`
- Modify: `internal/runtime/executor_lifecycle.go`
- Modify: `internal/runtime/executor_state.go`
- Modify: `internal/runtime/executor_helpers.go`
- Modify: `internal/runtime/executor_finalization_test.go`
- Modify: `internal/runtime/executor_run_e2e_test.go`
- Modify: `internal/runtime/orchestration_mode_test.go`

**Why:** Executor 当前处理三模式 routing。砍掉后固定 direct_response,删除 mode 参数和 routing 逻辑。

**Impact/Compatibility:** `ExecuteMessages` 删除 `mode` 参数(或固定 `direct_response`)。`createBoundRun` 删除 `orchestration_mode` 参数。`persistSelectedSkillID` 简化(只在有 explicit skill 时持久化)。`executor_finalize.go` 删除 skill lifecycle / plan evidence 收口。`executor_archive.go` 删除 archive 相关(如果 archive 表已删除)。

**Verification:** `go build ./internal/runtime && go test ./internal/runtime`

### Steps

- [ ] **5.1 重写 `executor_run.go`**:`ExecuteMessages` 删除 mode routing。`prepareExecuteRequest` 删除 mode 解析。`createBoundRun` 删除 orchestration_mode 参数(使用简化后的 `RunCreateParams`)。`buildExecuteRunner` 直接调 `RunnerFactory.New`(无 mode 分发)。
- [ ] **5.2 重写 `executor_finalize.go`**:删除 `verifyAndRecordSkill`(skill lifecycle 收口)。`finishSucceededRun` / `finishFailedRun` / `finishInterruptedRun` 简化:不处理 plan evidence / skill lifecycle。保留 run status + output + memory history append。
- [ ] **5.3 删除或简化 `executor_archive.go`**:如果 archive 表已删除,删除文件。否则简化为只记录 run summary。
- [ ] **5.4 重写 `executor_resume.go`**:删除 plan/single_agent resume 逻辑。保留 direct_response resume。
- [ ] **5.5 更新测试**:`executor_finalization_test.go` 删除 skill lifecycle 测试。`orchestration_mode_test.go` 删除 plan/single_agent mode 测试,保留 direct_response。`executor_run_e2e_test.go` 更新:删除 mode 参数。
- [ ] **5.6 运行验证**:`go build ./internal/runtime && go test ./internal/runtime`。修复编译错误直到通过。
- [ ] **5.7 Commit**:`refactor(runtime): simplify executor to single-mode direct_response`

---

## Task 6: 简化 runner_toolset + tool 调度

**Files:**
- Modify: `internal/runtime/runner_toolset.go`
- Modify: `internal/runtime/runner_toolset_emit.go`
- Modify: `internal/runtime/runner_toolset_skill.go`
- Modify: `internal/runtime/runner_toolset_model.go`
- Modify: `internal/runtime/runner_catalog.go`
- Modify: `internal/runtime/tool/tool.go`
- Modify: `internal/runtime/tool/safe_parallel_tools_node.go`
- Modify: `internal/runtime/tool/streaming_tool_executor.go`
- Modify: `internal/runtime/tool/validator.go`
- Delete: `internal/runtime/runner_toolset_emit.go`(如果只服务 plan evidence)
- Delete: `internal/runtime/runner_toolset_skill.go`(skill lifecycle 相关)

**Why:** toolset 当前构建 plan evidence / skill lifecycle 相关的 tool。砍掉后简化为直接构建工具集。

**Impact/Compatibility:** `runner_toolset_emit.go` 如果只服务 plan evidence,删除。`runner_toolset_skill.go` 删除 skill lifecycle 工具构建。`tool.go` 简化 `toolExecutionScheduler`:删除路径冲突检测(`pathsOverlap` / `executionPathsFromArgs` / `normalizeExecutionPaths`),改为简单二选一(read_only 并行 / serial 串行)。`safe_parallel_tools_node.go` 简化:删除路径冲突检测逻辑。

**Verification:** `go build ./internal/runtime && go test ./internal/runtime`

### Steps

- [ ] **6.1 审查 `runner_toolset_emit.go`**:如果只服务 plan evidence,删除整个文件。否则保留并删除 plan evidence 相关代码。
- [ ] **6.2 删除 `runner_toolset_skill.go`**:skill lifecycle 工具(`skill_assess`)不再需要。
- [ ] **6.3 重写 `runner_toolset.go`**:删除 plan evidence / skill lifecycle 相关。保留 tool catalog 构建 + memory tools。
- [ ] **6.4 重写 `tool.go`**:`toolExecutionScheduler` 简化:删除 `pathsOverlap`、`executionPathsFromArgs`、`normalizeExecutionPaths`、`executionTrimmedPaths`。`classifiedCall` 简化:只分 `read_only` / `serial`。`BuildAuditedTools` 保留。MCP namespaced tool 保留。
- [ ] **6.5 重写 `safe_parallel_tools_node.go`**:删除路径冲突检测。简单调度:read_only 工具并行,serial 工具串行。
- [ ] **6.6 更新 `streaming_tool_executor.go`**:适配简化后的 scheduler。
- [ ] **6.7 运行验证**:`go build ./internal/runtime && go test ./internal/runtime`。修复编译错误直到通过。
- [ ] **6.8 Commit**:`refactor(runtime): simplify tool scheduling - remove path conflict detection`

---

## Task 7: 全量编译检查

**Files:** None(验证 task)

**Why:** 确认 Phase 2 runtime + orchestration 清理后,编译错误集中在 contextplane / memorymodule / app / web(后续阶段)。

### Steps

- [ ] **7.1 运行**:`go build ./internal/runtime/... ./internal/orchestration/...`。必须通过。
- [ ] **7.2 运行**:`go test ./internal/runtime/... ./internal/orchestration/...`。必须通过。
- [ ] **7.3 记录未通过的包**:`go build ./... 2>&1 | head -50`。记录 contextplane/memorymodule/app/web 的编译错误(Phase 3-5 修复)。
- [ ] **7.4 Commit**:`chore: phase 2 runtime+orchestration cleanup complete`
