# Acorn 架构术语表

| 术语 | 当前含义 |
|---|---|
| **Executor** | `internal/runtime.Executor`，接收 remote client 请求，创建 run，写入 root orchestration mode，调用 RunnerFactory，执行 run lifecycle。 |
| **RunnerFactory** | `internal/runtime.RunnerFactory`，持有 runtime 共享依赖、registry、workspace、provider/cache 和 concrete orchestration builder；每次 run 的具体装配由 `buildRun` 执行。 |
| **buildRun** | `internal/runtime/run.go` 的 per-run assembly 入口，按固定主链接 model、tool catalog、prepared memory、run selection policy、ContextPlane、OrchestrationPlane，返回 `ActiveRunner`。 |
| **Root orchestration mode** | 持久化到 `runs.orchestration_mode` 的 run 模式。Public root request 只接受 `direct_response` / `plan_execute`；`single_agent` 是内部 child-run / verifier 执行模式。 |
| **RunActionRound** | `internal/orchestration` 的共享执行回合原语（ExecuteRound + reactive-compact-retry）；`direct_response` 与 `plan_execute`/`single_agent` 两条路径都调用它。 |
| **ContextPlane** | `internal/contextplane` 的运行时上下文边界，负责 context assembly、budget、deferred load、tool lifecycle；`compaction/` 子包管 compression handlers。 |
| **BudgetGovernor** | `internal/contextplane` 的 context pressure 计算器；用 model window、provider output cap、summary cap、derived buffers 得出 `ok`/`warning`/`auto_compact`/`blocking`。 |
| **CompactionEngine** | `internal/contextplane/compaction/` 的 proactive compact 执行所有者；生成 structured continuation summary、保护 preserved tail/tool pairs、产出 `context_boundaries` token/summary metrics。 |
| **ContextBoundary** | `internal/model.ContextBoundary` 的 persisted compact boundary fact；SQLite `context_boundaries` 保存 root mode、trigger、covered/preserved refs、transcript ref、summary、token metrics。 |
| **Tool lifecycle state** | `internal/contextplane` 管理的 run-scoped 工具可见性状态（loaded/deferred tools、tool result refs）；执行前由 `SafeParallelToolsNode` 校验，执行后写入 durable `ToolResultLedger`。 |
| **ToolResultLedger** | `internal/store` ledger contract + SQLite `tool_results` 的工具结果事实层；保存 result ref、arguments、status、preview/full text、side-effect refs、evidence refs。 |
| **ToolExecutionScheduler** | `internal/runtime/tool/` 的共享调度核；direct_response 和 graph tool execution 都通过它消费 `ToolContract.Execution`，处理 read-only 并发、write-scoped path conflict、exclusive sequencing。 |
| **OrchestrationPlane** | `internal/orchestration` 的编排入口；RunnerFactory 委托它构建 public direct/plan-execute assembly 和内部 single-agent child assembly。 |
| **assembleContext / buildAssembly** | `internal/runtime` 的单一 context assembly + single assembly helper（mode 分发）；取代旧的 `assembleToolContext`/`assembleDirectContext` + 3 个 `build*Assembly`。 |
| **assembleTooling** | `internal/orchestration` 的共享 tool/handler/instruction/run-context assembly helper；`BuildDirectResponse`、`BuildSingleAgent`、`BuildPlanExecute` 都调用它。 |
| **MemoryModule** | `internal/memorymodule` 的 file-backed memory 模块；管理 facts、skills、history、search、prepare、memory file tools。Canonical Memory Record V2 metadata 包含 validity、provenance、typed relations、lifecycle timestamps。 |
| **Bleve+FAISS semantic index** | 必接入的 rebuildable retrieval index，由 `memory.semantic` 配置、`acorn memory semantic rebuild` 重建，为 `memorymodule.Search`/`Prepare` 提供 hybrid text/vector semantic candidates。 |
| **Run selection policy** | runtime 内联的选择逻辑（`internal/runtime/runner_build_selection.go:resolveRunSelectionByDecision`），在 tool-enabled mode 消费 explicit skill、skill candidates、working context；不持久化 decision record（selected skill id 持久化在 `runs.skill_id`）。 |
| **Store ports** | app/runtime 顶层定义的 consumer-owned persistence 接口（ExecutorStore、RunnerFactoryStore、containerRuntimeStore、containerAppStore 等）；`internal/app/container*.go` 是唯一允许直接持有 sqlite adapter 的 composition root。 |
| **PlanStore** | runtime graph 使用的计划持久化接口，消费 `internal/model.Plan`/`PlanEvidence`；`internal/runtime/plan/` 负责 step evidence/backlink 追加。 |
| **ChildAgent contract** | `internal/orchestration/child_agent.go` 的 `ChildAgentRequest`/`ChildAgentResult`/`ChildAgentExecutor`，被 `delegate_task`、plan_execute、verifier 共用。 |
| **Device Auth** | Single-owner self-hosted auth boundary：`acorn pair` 写一次性 pairing code hash，`POST /v1/devices:pair` 换取一次性展示的 bearer token；SQLite 只保存 token hash。 |
| **Client RunEvent** | `/v1` 的 client-facing live event envelope；mobile client 只消费 mobile live subset（run lifecycle、assistant delta/message、terminal status、resume、elicitation/operator question、`decision_blocked`）；由 SQLite `events` 表投影。 |
| **SQLite persisted truth** | 后端 runtime 事实来源；events、runs、plans、checkpoints、tool results、archives、session summaries、context boundaries 等由 SQLite 持久化。长期 memory 的 active truth 是 `memorymodule` 文件。 |
| **Mobile Control Surface** | `mobile/` Flutter app，通过 generated Dart client 消费 `/v1`；不执行 runtime、不维护第二套 message lifecycle、不做 offline-first truth。 |
