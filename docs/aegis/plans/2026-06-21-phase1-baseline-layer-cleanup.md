# Acorn 重构 - Phase 1: 后端基础层清理

Date: 2026-06-21
Parent Spec: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md`
Status: Implementation Plan

## Goal

砍掉 plan_execute / single_agent / compaction / bleve+faiss 相关的**基础层数据结构、配置、事件类型和工具契约**,为后续 runtime / contextplane / memorymodule 的逻辑层清理扫清依赖。本阶段不碰 runtime 执行逻辑,只清理被它们引用的类型定义和配置。

## Architecture

```text
本阶段范围(基础层):
  config/     — 删除 plan/skill-lifecycle/bleve/serve/mcp-server 相关配置
  store/      — 删除 plan/context_boundary/tool_result/archive 相关 records + sentinel
  store/sqlite/ — 精简 schema 到 ~8 表,drop 旧表
  events/     — 删除 plan/skill-lifecycle/procedure 相关 event types
  tooling/    — 简化 ToolContract(去掉 PlanPolicy/EvidencePolicy/ResourceScope/ToolProfile)
  providers/  — 删除 provider_usage 记录类型

不在本阶段(后续阶段):
  runtime/    — Phase 2
  contextplane/ — Phase 3
  memorymodule/ — Phase 4
  orchestration/ — Phase 2
  web/        — Phase 5
  app/        — Phase 5
  Flutter     — Phase 6
```

## Tech Stack

- Go 1.26
- modernc.org/sqlite(SQLite adapter)
- Eino ADK v0.8.13(保留 model + tool loop)
- 保留依赖:tiktoken-go(可选,token counting)

## Baseline / Authority Refs

- `docs/aegis/specs/2026-06-21-acorn-refactor-design.md` §2 决策记录、§3.4 SQLite Schema、§3.8 工具系统、§3.10 配置
- `docs/architecture/INVARIANTS.md` — store boundary 守卫、consumer-owned port 接口收敛
- `internal/store/sqlite/store_schema.go` — 当前 schema
- `internal/config/config.go` — 当前 config struct

## Compatibility Boundary

- **Hard cutover**: 旧数据清空,不写 migration。schema 直接重建。
- `internal/app/container.go` 仍是唯一允许 import sqlite 的地方(`store_boundary_test.go` 守卫)。
- Consumer-owned store 接口 ≤6 守卫(`store_interface_count_test.go`)需要在本阶段更新:删除 plan/skill snapshot store port 后,接口数应下降。

## Verification

每个 task 必须满足:
1. `go build ./...` 编译通过
2. `go test ./internal/config ./internal/store ./internal/store/sqlite ./internal/events ./internal/tooling ./internal/providers` 通过
3. `go test ./tests/architecture/...` 通过(更新守卫后)

---

## Task 1: 精简 config struct

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_defaults.go`
- Modify: `internal/config/config_validate.go`
- Modify: `internal/config/config_mcp.go`
- Modify: `internal/config/config_workspace.go`(如果引用了 ServeConfig)
- Modify: `internal/config/context_policy.go`
- Modify: `internal/config/config_providers.go`(删除 provider_usage 相关)
- Modify: `internal/config/config_load.go`
- Test: `internal/config/config_validation_test.go`
- Test: `internal/config/config_defaults_test.go`
- Test: `internal/config/config_load_test.go`
- Test: `internal/config/config_semantic_test.go`
- Test: `internal/config/installer_config_test.go`

**Why:** config 是所有包的依赖根。先精简 config,后续包的编译才不会引用已删除的类型。

**Impact/Compatibility:** 砍掉 `ServeConfig`、`ServeToolsConfig`、`BleveSemanticConfig`、`MemorySemanticConfig` 中的 bleve 字段、`AgentConfig.MaxSubagentDepth`、`ContextConfig` 的过度派生字段。保留新 `ContextConfig`(4 个简单字段)和新 `EmbeddingProviderConfig`。

**Verification:** `go build ./internal/config && go test ./internal/config`

### Steps

- [ ] **1.1 重写 `config.go`**:删除 `ServeConfig` / `ServeToolsConfig` / `BleveSemanticConfig`。简化 `ContextConfig` 为 4 字段:`EffectiveWindow int`、`CompactMarginTokens int`、`MaskAfterTurns int`、`PreserveRecentTurns int`。简化 `MemorySemanticConfig`:删除 `Bleve` 字段,只保留 `Embedding`。简化 `AgentConfig`:删除 `MaxSubagentDepth`。删除 `MCPConfig` 中 server 相关字段。
- [ ] **1.2 重写 `config_defaults.go`**:更新 `defaultConfig()`:新 `ContextConfig` 默认值 `EffectiveWindow: 200000, CompactMarginTokens: 13000, MaskAfterTurns: 2, PreserveRecentTurns: 3`。删除 bleve 默认配置。删除 `MaxSubagentDepth` 默认值。
- [ ] **1.3 重写 `config_validate.go`**:删除 FAISS / bleve / semantic-prep / plan / mcp-server 相关校验。保留 provider validation、embedding key 独立性校验、新 `ContextConfig` 字段范围校验。
- [ ] **1.4 重写 `config_mcp.go`**:删除 server 相关配置。只保留 client provider 配置。
- [ ] **1.5 重写 `context_policy.go`**:删除 `ModelProfileFromContextPolicy` 和 `ContextAssemblyTokenLimitFromContextPolicy`(BudgetGovernor 将在 Phase 3 删除)。如果 config_validate 引用它们,内联简化逻辑。
- [ ] **1.6 更新测试**:删除 `config_semantic_test.go` 中 bleve 相关测试。更新 `config_validation_test.go` 删除 server/bleve/maxsubagentdepth 测试用例。更新 `config_defaults_test.go` 匹配新默认值。
- [ ] **1.7 运行验证**:`go build ./internal/config && go test ./internal/config`。修复编译错误直到通过。
- [ ] **1.8 Commit**:`refactor(config): strip plan/serve/bleve/subagent-depth config fields`

---

## Task 2: 精简 events 类型

**Files:**
- Modify: `internal/events/events.go`
- Modify: `internal/events/operator_question.go`(保留,不动)
- Test: `internal/events/`(如果有 events 测试)

**Why:** events 包定义 RunEvent kinds。删掉 plan / skill-lifecycle / procedure / context-boundary 相关 kind 定义,后续 runtime/contextplane 编译才不会引用它们。

**Impact/Compatibility:** 删除以下 event kind 常量(如果存在):`plan.*`、`skill.lifecycle`、`procedure.activation`、`context.boundary`、`memory.prepared` 中的 procedure 部分。保留 `run.*`、`assistant.*`、`agent.message`、`tool.*`、`operator_question.*`、`elicitation.*`、`decision_blocked`、`memory.prepared`(简化 payload)。

**Verification:** `go build ./internal/events && go test ./internal/events`

### Steps

- [ ] **2.1 审查 `events.go`**:列出所有 event kind 常量。标记保留/删除。
- [ ] **2.2 删除 plan / skill-lifecycle / procedure / context-boundary 相关 kind 常量和 payload 类型**。保留 `RunStatus`、`OrchestrationMode`(但删除 `OrchestrationModePlanExecute` 和 `OrchestrationModeSingleAgent` 常量,只留 `OrchestrationModeDirectResponse`)。保留 `PendingActionKind`、`PendingActionStatus`。
- [ ] **2.3 运行验证**:`go build ./internal/events`。此时会有其他包编译失败(runtime/contextplane 引用了已删除的 kind),这是预期的——本阶段只保证 events 包自身编译通过。
- [ ] **2.4 Commit**:`refactor(events): remove plan/skill-lifecycle/procedure/context-boundary event kinds`

---

## Task 3: 精简 tooling ToolContract

**Files:**
- Modify: `internal/tooling/contracts.go`
- Modify: `internal/tooling/specs.go`
- Modify: `internal/tooling/catalog.go`
- Modify: `internal/tooling/builtin_registry.go`
- Test: `internal/tooling/policy_test.go`
- Test: `internal/tooling/catalog_test.go`

**Why:** ToolContract 被 runtime / contextplane / tools 引用。先简化契约,后续包编译才不会引用已删除字段。

**Impact/Compatibility:** `ToolContract` 简化为:`Name`、`Source`、`Kind`、`Category`、`ParallelPolicy`、`Loading`。删除 `PlanPolicy`、`ResourceScope`、`Profiles`、`ToolProfile`(`run`/`serve`)。`ParallelPolicy` 简化为二选一:`read_only` / `serial`(删除 `write_scoped` 和 `never_parallel`,合并为 `serial`)。`ToolLoadingPolicy` 保留(`eager`/`deferred`/`hidden`)。

**Verification:** `go build ./internal/tooling && go test ./internal/tooling`

### Steps

- [ ] **3.1 重写 `contracts.go`**:`ToolContract` 删除 `PlanPolicy`、`ResourceScope`、`Profiles` 字段。`Validate()` 更新为只校验 `Name`/`Source`/`Kind`/`Category`/`ParallelPolicy`/`Loading`。删除 `EagerLoadingPolicy` / `DeferredLoadingPolicy` 之外的所有 helper。删除 `PlanPolicy`、`ResourceScope`、`ToolProfile` 类型及其常量。
- [ ] **3.2 重写 `specs.go`**:删除 `ToolKind` 中的 `ToolKindMCPResource`、`ToolKindMCPPrompt`(合并到 `ToolKindMCP`)。删除 `ToolCategory` 中不用的 category。`ParallelPolicy` 只留 `ParallelPolicyReadOnly` 和 `ParallelPolicySerial`(替换 `write_scoped` 和 `never_parallel`)。更新 `ParseParallelPolicy`。删除 `PlanPolicy`、`FactPolicy`、`HealthState`、`ToolHealth`、`ResourceScope` 相关。`ToolSpec` 简化:嵌入 `ToolContract` + `Tool` + `Health`。
- [ ] **3.3 重写 `catalog.go`**:删除 `EnabledSpecsForProfile` 方法中的 `ToolProfile` 参数(改为 `EnabledSpecs()` 无参数)。删除 serve profile 相关逻辑。
- [ ] **3.4 重写 `builtin_registry.go`**:更新所有 builtin tool spec 构建调用,去掉 `PlanPolicy`、`ResourceScope`、`Profiles` 参数。
- [ ] **3.5 更新测试**:`policy_test.go` 删除 `write_scoped`/`never_parallel`/`PlanPolicy` 测试。`catalog_test.go` 更新 `EnabledSpecsForProfile` → `EnabledSpecs`。
- [ ] **3.6 运行验证**:`go build ./internal/tooling && go test ./internal/tooling`。修复编译错误直到通过。
- [ ] **3.7 Commit**:`refactor(tooling): simplify ToolContract - remove PlanPolicy/ResourceScope/Profiles`

---

## Task 4: 精简 store shared records

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/toolresult_types.go`
- Modify: `internal/store/artifacts.go`(保留,不动)
- Test: `internal/store/toolresult_types_test.go`
- Test: `internal/store/artifacts_test.go`

**Why:** store 包的 shared records 被 runtime / contextplane / app 引用。先删除 plan / context_boundary / tool_result ledger 相关 records。

**Impact/Compatibility:** 删除 `RunCreateParams` 中的 `OrchestrationMode`、`ParentRunID`、`SkillID`、`Depth`、`CheckpointID` 字段(简化为 `RunID`、`SessionID`、`TurnIndex`、`Input`、`BoundMessageID`)。删除 `CreatePendingActionInput` 中的 `Mode`、`Rule` 字段(简化,保留 `ActionID`、`RunID`、`InterruptID`、`Kind`、`Subject`、`PayloadJSON`、`Status`、`Reason`)。删除 `toolresult_types.go` 中的 ledger record 类型。删除 sentinel errors 中 plan/context_boundary/tool_result 相关的(如果存在)。

**Verification:** `go build ./internal/store && go test ./internal/store`

### Steps

- [ ] **4.1 重写 `store.go`**:`RunCreateParams` 简化为 `RunID`、`SessionID`、`TurnIndex`、`Input`、`BoundMessageID`。删除 `OrchestrationMode`、`ParentRunID`、`SkillID`、`Depth`、`CheckpointID`。`CreatePendingActionInput` 简化:删除 `Mode`、`Rule`,保留 `ActionID`、`RunID`、`InterruptID`、`Kind`、`Subject`、`PayloadJSON`、`Status`、`Reason`。删除 sentinel `ErrPlanNotFound`(如果存在)。保留 `ErrRunNotFound`、`ErrSessionNotFound` 等。
- [ ] **4.2 删除 `toolresult_types.go`**:整个文件删除(如果 ledger record 类型只被 contextplane/runtime 引用)。如果 `ToolResultLedger` 接口在此文件中,移到 contextplane 或删除。
- [ ] **4.3 更新测试**:`toolresult_types_test.go` 删除。`artifacts_test.go` 保留(不动)。
- [ ] **4.4 运行验证**:`go build ./internal/store && go test ./internal/store`。
- [ ] **4.5 Commit**:`refactor(store): remove plan/context-boundary/tool-result records`

---

## Task 5: 精简 SQLite schema

**Files:**
- Modify: `internal/store/sqlite/store_schema.go`
- Modify: `internal/store/sqlite/store_schema_bootstrap.go`
- Modify: `internal/store/sqlite/store_schema_columns.go`
- Modify: `internal/store/sqlite/store_schema_drops.go`
- Modify: `internal/store/sqlite/store_run.go`
- Delete: `internal/store/sqlite/store_plan.go`
- Delete: `internal/store/sqlite/store_context_boundary.go`
- Delete: `internal/store/sqlite/store_run_archive.go`
- Delete: `internal/store/sqlite/store_provider_usage.go`
- Delete: `internal/store/sqlite/conversation_segment.go`
- Test: `internal/store/sqlite/store_memory_fail_loud_test.go`

**Why:** schema 是持久化基础。先精简表结构,后续 store adapter 方法删除才有 schema 支撑。

**Impact/Compatibility:** schema 从 ~23 表降到 ~8 表。`store_schema_drops.go` 主动 drop 旧表(plans/plan_evidence/plan_steps/tool_results/context_boundaries/conversation_segments/run_archives/working_checkpoints/provider_usage/run_decisions)。`runs` 表删除 `orchestration_mode`、`skill_id`、`depth`、`parent_run_id` 列。新增 `memory_vectors` 表(可选,如果 Phase 4 需要)。`schemaRequiredTables` 更新为只要求 8 张表。

**Verification:** `go build ./internal/store/sqlite && go test ./internal/store/sqlite`

### Steps

- [ ] **5.1 重写 `store_schema_bootstrap.go`**:新 schema 只创建 8 张表:`sessions`、`messages`、`runs`、`events`、`pending_actions`、`devices`、`pairing_codes`、`owner_profile`。`runs` 表删除 `orchestration_mode`、`skill_id`、`depth`、`parent_run_id`、`checkpoint_id` 列。`messages` 表保持。`pending_actions` 表删除 `mode`、`rule` 列。
- [ ] **5.2 重写 `store_schema_drops.go`**:添加 drop 语句 for: `plans`、`plan_evidence`、`plan_steps`、`tool_results`、`context_boundaries`、`conversation_segments`、`run_archives`、`working_checkpoints`、`provider_usage`、`run_decisions`、`conversation_segments_idx`、`conv_seg_*` triggers。
- [ ] **5.3 重写 `store_schema_columns.go`**:更新列常量,只包含新 schema 的列。
- [ ] **5.4 重写 `store_schema.go`**:`schemaRequiredTables` map 更新为 8 张表 + 各自必需列。`migrateV2` 简化(不再添加已删除的列)。`validateSchema` 只校验新 8 表。
- [ ] **5.5 删除文件**:`store_plan.go`、`store_context_boundary.go`、`store_run_archive.go`、`store_provider_usage.go`、`conversation_segment.go`。
- [ ] **5.6 重写 `store_run.go`**:删除 `CreateRun` 中的 `orchestration_mode`、`skill_id`、`depth`、`parent_run_id`、`checkpoint_id` 参数。`RunRecord` 结构体删除对应字段。
- [ ] **5.7 更新 `store_memory_fail_loud_test.go`**:确保 drop 旧表测试通过。
- [ ] **5.8 运行验证**:`go build ./internal/store/sqlite && go test ./internal/store/sqlite`。修复编译错误直到通过。
- [ ] **5.9 Commit**:`refactor(store/sqlite): reduce schema from ~23 to ~8 tables`

---

## Task 6: 更新架构守卫测试

**Files:**
- Modify: `tests/architecture/store_interface_count_test.go`
- Modify: `tests/architecture/store_boundary_test.go`(确认 container.go 仍是唯一 sqlite import)
- Modify: `tests/architecture/structural_limits_test.go`(从 `refactorOwnedDirs` 删除已删除目录)
- Modify: `tests/architecture/bleve_faiss_release_guard_test.go`(删除或标记 skip)
- Modify: `tests/architecture/client_projection_boundary_test.go`(删除 plan/skill-lifecycle schema 断言)
- Delete: `tests/architecture/assembly_consolidation_test.go`(如果引用已删除的 assembly 函数)
- Delete: `tests/architecture/action_round_sharing_test.go`(如果引用已删除的 ActNode/plan_execute)

**Why:** 架构守卫测试引用了已删除的代码。需要更新守卫以匹配新架构。

**Impact/Compatibility:** `store_interface_count_test.go` 更新:删除 plan/skill snapshot store port 后,期望接口数 ≤4(从 ≤6)。`structural_limits_test.go` 从 `refactorOwnedDirs` 删除 `internal/runtime/plan`、`internal/contextplane/compaction`。`bleve_faiss_release_guard_test.go` 删除(不再有 FAISS)。`client_projection_boundary_test.go` 删除 plan/skill-lifecycle schema 断言。

**Verification:** `go test ./tests/architecture/...`

### Steps

- [ ] **6.1 更新 `store_interface_count_test.go`**:删除 plan/skill snapshot store port。期望接口数 ≤4。
- [ ] **6.2 更新 `structural_limits_test.go`**:`refactorOwnedDirs` 删除 `internal/runtime/plan`、`internal/contextplane/compaction`。
- [ ] **6.3 删除 `bleve_faiss_release_guard_test.go`**。
- [ ] **6.4 更新 `client_projection_boundary_test.go`**:删除 plan / skill.lifecycle / procedure.activation schema 断言。
- [ ] **6.5 审查 `assembly_consolidation_test.go` 和 `action_round_sharing_test.go`**:如果引用已删除的 assembly 函数或 ActNode,删除文件或更新断言。
- [ ] **6.6 运行验证**:`go test ./tests/architecture/...`(此时可能有编译错误,因为 runtime/contextplane 尚未清理。暂时注释掉引用未清理包的断言,标注 `// TODO Phase 2: re-enable after runtime cleanup`)。
- [ ] **6.7 Commit**:`refactor(architecture-tests): update guards for simplified schema and removed packages`

---

## Task 7: 全量编译检查 + 提交

**Files:** None(验证 task)

**Why:** 确认 Phase 1 基础层清理后,被影响的包的编译错误集中在 runtime/contextplane/memorymodule/orchestration/app/web(后续阶段清理)。

**Verification:** `go build ./internal/config ./internal/store ./internal/store/sqlite ./internal/events ./internal/tooling` 必须全部通过。其他包允许有编译错误(后续阶段修复)。

### Steps

- [ ] **7.1 运行**:`go build ./internal/config ./internal/store ./internal/store/sqlite ./internal/events ./internal/tooling`
- [ ] **7.2 运行**:`go test ./internal/config ./internal/store ./internal/store/sqlite ./internal/events ./internal/tooling`
- [ ] **7.3 记录未通过的包**:运行 `go build ./... 2>&1 | head -50`,记录编译错误的包。这些错误应在 Phase 2-5 修复。
- [ ] **7.4 Commit**:`chore: phase 1 baseline layer cleanup complete`
