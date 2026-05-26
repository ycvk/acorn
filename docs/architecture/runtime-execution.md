---
doc_type: architecture
status: current
last_reviewed: 2026-05-14
slug: runtime-execution
---

# Runtime Execution

## 现状

Acorn 的执行层由 `internal/runtime.Executor` 启动 run，并把 run 的 root orchestration mode 写入 persisted truth。用户执行入口来自 authenticated `/v1` remote client contract；operator CLI 只保留 `doctor`、`serve`、`pair`、skills、memory rebuild 等运维/诊断命令。app container 在 `internal/app/container.go` 装配 runtime executor、trace service、workbench service 和 web dependencies，Web handler 只调用 app/runtime service，不直接拼 runtime 状态。

## Run lifecycle

- `internal/runtime/executor.go` 创建或恢复 session/run，写入 `events.RunRecord`，并把选定的 orchestration mode 持久化到 `runs.orchestration_mode`。
- `RunnerFactory.New` 在 `internal/runtime/runner.go` 中只保留入口委托；`internal/runtime/run.go` 的 `runBuilder.Build` 构建 `ActiveRunner`。当前主链是：创建 chat model；bootstrap MCP 并构建 run tool catalog；调用 `memorymodule.Service.Prepare` 得到 file-backed prepared memory；assemble ContextPlane；再按 mode 把 root assembly 委托给 OrchestrationPlane。`direct_response` 不进入 run selection；`plan_execute` 和 internal `single_agent` 在 ContextPlane assembly 前通过 `internal/decision` policy 解析 selected skill、decision record 和 context priority。
- Run tool catalog is built from `tooling.ToolContract`. Each enabled tool has explicit loading, execution, result, boundary, and projection policy; incomplete contracts fail catalog construction, and MCP providers must declare `tool_safety`.
- Executor 在 model run 前通过 ContextSession Bootstrap 生成首轮 `ModelInput`。Bootstrap 合并 ContextPlane assembly 与 initial user messages；`direct_response` 额外把 stable instruction 作为 leading system message 交给 ContextSession。Executor 把 session 保存在 `ActiveRunner` 并绑定到 root runner execution context，root mode adapter 从 context 读取同一个 session。
- Chat model 只来自配置中唯一 enabled LLM provider；runtime 不再提供 priority、backoff、透明 failover 或 provider retry/switch/exhausted stream event。MCP provider 只暴露 startup health、catalog/auth lifecycle 和真实错误；不再维护 circuit breaker、half-open 状态或后台 reconnect loop。
- `internal/runtime/runner_orchestration.go` 是 OrchestrationPlane 调用边界：`buildDirectResponseAssembly`、`buildSingleAgentAssembly`、`buildPlanExecuteAssembly` 分别把 request 投给 concrete `orchestration.DefaultPlane`；runtime 本地 `orchestrationPlane` 只作为测试注入 seam。
- `internal/runtime/executor.go` 和 executor finalization 路径负责把 ADK events、assistant message、run terminal status、archive、session summary 和 memorymodule history append 收口到 persisted truth。
- Context compression boundary facts are persisted separately as runtime history. `context_boundaries` records compact sequence, root mode, trigger, covered/preserved message references, transcript ref, summary, token metrics, and effective window; stream events only project `boundary_id` and display metrics.
- Tool result truth is persisted separately from stream events. SQLite `tool_results` stores each tool call's durable `tool_result_ref`, run/session/call identity, arguments JSON, status, preview, full text, token estimate, side-effect refs, and evidence refs. Workspace mutation checkpoints, rollback results, run artifacts, operator questions, `multi_edit`, `run_verification`, and `git_summary` diff artifacts are projected through the same side-effect refs. The runtime writes this ledger from ContextPlane on every tool result, and plan evidence backlinks are appended against the same durable record.
- Interrupted run resume truth is inferred from persisted root interrupt contexts, not client-local buttons. `TraceService` currently recognizes empty/default interrupt kind plus `run_command_pause` when reconstructing resume targets for `/v1/runs/{id}:resume`.
- Context pressure is governed by effective-window thresholds. `BudgetGovernor` reserves provider output cap, summary tokens, and static overhead before computing derived warning/auto/blocking thresholds; the compression adapter compacts only for `auto_compact` or `blocking`, not `context_window * percent`.
- Context assembly uses the same token counter as pressure and compaction. Selected skill and memory messages are checked against the derived assembly budget; Acorn no longer string-trims or silently drops active context to make a model call fit.
- Current tool output is not character-compressed before the model sees it. The lifecycle middleware only replaces expired tool messages with a durable `tool_result_ref`; it does not rewrite still-live tool messages to head/tail previews.
- Tool execution is stream-first and unified through `AgentLoop`. `StreamingToolExecutor` interleaves model streaming with real-time tool submission via `Submit(call)` during the assistant stream, then collects final results with `GetRemainingResults`. Path-aware conflict detection (`ParallelPolicyWriteScoped`) resolves scheduling order at the executor level, not in the model loop. Old sync paths (`SafeParallelToolsNode.Invoke`/`Stream`, `toolExecutionScheduler.execute`/`stream`) are removed; all root modes use the same streaming model→tools loop. Tool progress is a side-channel surfaced as persisted `tool.call.progress` RunEvent; final model-visible truth remains the terminal `schema.ToolMessage`.
- Proactive compaction is executed by `internal/contextplane.CompactionEngine`. The ADK handler stack still provides the pre-model seam, but summary input generation, model response validation, final message rewrite, preserved tail/tool-pair handling, and `CompressionOutcome` generation are engine-owned.
- Post-compact rehydration is planned inside `internal/contextplane` as a concrete helper, not a public abstraction boundary. The compacted model input chain is `system messages -> continuation summary -> rehydrated packet messages -> preserved tail`; packets restore active skill, memory/checkpoint/session summary, tool lifecycle state, plan text, and explicit recent touched paths when present. Oversized packets fail compaction instead of being truncated.
- Provider context overflow recovery is reactive and narrow. If the model returns an explicit context-window/prompt-too-long error, direct_response asks `ContextSession.ReactiveCompact` for a compacted input and retries the same model call once; graph modes get the same one-retry behavior from the compaction middleware model wrapper. Non-overflow errors do not trigger compact or retry.
- The old run-wide `TokenBudget` hard stop and `token_budget.exceeded` stream event are removed. Acorn no longer fails a run based on cumulative provider usage percentage; context pressure is evaluated from the current model input by `BudgetGovernor`.

## System Readiness

- App-level readiness truth is assembled in `internal/app.CapabilitiesService.Snapshot`. The snapshot carries typed `RuntimeReadiness` / `ProviderReadinessSummary` projections; legacy `ExecutionReady` / `ExecutionError` fields have been removed.
- `runtime_readiness.status` answers only whether the current config can execute a root run at all. It is `ready` or `blocked`; missing execution prerequisites stay explicit in `reason`.
- `provider_readiness[]` is provider-scoped truth, currently projected from MCP provider capability facts. A provider can be `passed`, `failed`, or `blocked` without silently collapsing the whole runtime into blocked.
- `runtime_readiness=ready` can legitimately coexist with provider `failed` or `blocked` when that provider is not the hard prerequisite for root execution. This is the contract boundary that keeps runtime viability separate from subsystem degradation.

## AgentLoop 统一化

所有 root mode 的模型→工具循环现在由 `orchestration.AgentLoop` 统一实现：

- `ExecuteRound` 是导出的纯执行函数（无 `ContextSession` 依赖），负责 `模型流式调用 → 实时工具提交 → 终态结果收集` 的完整回合。它接收 `AssistantStreamer` 和 `ToolInvoker` 作为接口参数，不感知上层 orchestration 模式。
- `AgentLoop.RunOneIteration` 在 `ExecuteRound` 之上封装 `ContextSession` 生命周期：调用 `BeforeModelCall` 准备输入、执行 `ExecuteRound`、成功后 `RecordAssistant` + `RecordToolResults`、失败时触发 `ReactiveCompact` 并重试一次。reactive compact 恢复后的消息重新投入 `ExecuteRound`，结果再次通过 session 记录。
- `direct_response` 直接使用 `AgentLoop`（通过 `direct_response_builder.go` 的 `RunOneIteration` 循环）。
- `single_agent` 的 ActNode 通过注入的 `AssistantStreamer` 调用 `StreamAssistantMessage`，再用 `StreamingToolExecutor` 完成工具执行。ActNode 内部循环从 `GenerateWithToolInfos` + `streamToolMessages` 替换为对 `AssistantStreamer` + `StreamingToolExecutor` 的等价组合调用，保证行为与 `AgentLoop` 一致。
- `plan_execute` 的 `ExecuteDispatchNode` 通过 `ChildAgentExecutor` 委托给 `single_agent` 子执行器，间接共享同一条流式路径。
- `AssistantStreamRequest` 新增 `CallSite` 字段，允许 `direct_response`（`CallSiteAssistant`）和 ActNode（`CallSiteAct`）在 provider usage 追踪中区分来源。
- 前端 `LiveRunView` 新增 `toolProgress` 字段，`applyRunEvent` 按 `call_id` 累进 `tool.call.progress` delta，并在工具终态事件（succeeded/failed/interrupted）上清除对应条目。`deriveLiveWorkSurfaceView` 将活跃的工具进度块注入到 work surface 的 assistant text 与 evidence 之间。

## Root mode routing

Root mode routing 的当前事实在 `internal/runtime` 测试和 executor 路径中固定：

- runtime-internal 显式 supported mode 原样保留；未知或已删除 mode 直接报 unsupported mode。Public `/v1` create-run request 只接受 `direct_response` / `plan_execute`。
- child run 默认 `single_agent`。
- root run 未显式指定 mode 时默认 `direct_response`；runtime 不再根据输入文本猜测 `plan_execute`。
- 带 `skill_id` 的 root run 默认 `plan_execute`，因为 skill 执行需要结构化计划和 evidence。
- 需要计划执行的产品入口必须显式传 `mode=plan_execute`。
选定 mode 写入 run record，resume 时不重新猜测。

## Streaming 与 finalization

- `internal/runtime/assistant_stream.go` 是直接模型 streaming seam，当前用于 budget grace summary 等需要真实 `assistant.delta` 的路径，不伪造 token delta。
- `direct_response` 当前由 `AgentLoop.RunOneIteration` 驱动：每轮先调用 `ContextSession.BeforeModelCall` 取得模型输入，再委托 `ExecuteRound` 完成模型流式调用→工具提交→结果收集的完整回合。`ExecuteRound` 内部使用 `AssistantStreamer.StreamAssistantInterleaved` 获取交错流（文本增量 + 工具调用帧），并实时通过 `StreamingToolExecutor.Submit` 下发工具调用；工具完成后 `GetRemainingResults` 返回终态 `schema.ToolMessage`。成功后 `AgentLoop` 通过 `RecordAssistant` / `RecordToolResults` 将结果交回 `ContextSession`。上下文溢出时触发 `ReactiveCompact`，恢复后重试同一模型调用一次。`direct_response` 不维护本地 messages slice，也不使用 ADK runner input 作为历史 fallback。
- internal `single_agent` / public `plan_execute` 的 ActNode 现在共享同一条流式路径：通过注入的 `AssistantStreamer` 调用 `StreamAssistantMessage`（带 `CallSiteAct` 以区分 provider usage），获得完整 assistant message 后再用 `StreamingToolExecutor` 执行工具。ActNode 的循环体（`GenerateWithToolInfos` + 手动 `streamToolMessages`）已被替换为对 `orchestration.ExecuteRound` 等价行为的直接调用，保证 single-agent graph 与 direct_response 在模型→工具循环上完全一致。step evidence 只读取终态 tool message；progress 只作为用户可见运行过程。
- Mobile 当前通过 `mobile/lib/src/api/run_event_stream.dart` 消费 `/v1` persisted `RunEvent` SSE，并由 chat projection 做 live assistant/activity 投影；legacy `/api` request-local StreamItem stream 已删除，terminal stream 后 reload server truth。
- `internal/stream` 是 runtime stream item、kind、payload 和 projection helper 的 canonical 包；`internal/runtime` 不再 re-export stream types。Web/OpenAPI/mobile types 只是投影，不是第二套运行时事实。
- `tool.call.progress` carries tool name/call id/arguments JSON/progress delta/sequence and is projected through OpenAPI generated mobile types and mobile live activity rows. Builtin progress-capable tools implement `tooling.ProgressTool`; `run_command` emits stdout/stderr chunks while preserving the final structured JSON tool result, and workflow tools (`multi_edit`, `run_verification`, `git_summary`) emit progress while keeping the final model-visible result structured.
- `context.pressure` and `context.compressed` are backend-owned visibility events. Pressure payloads come from `BudgetGovernor` state/token/window facts; compressed payloads come from `CompressionOutcome` boundary/token/snippet facts. OpenAPI and mobile generated types carry both events, and mobile projection renders these fields without estimating pressure locally.
- `context.compressed` is a projection of compression visibility. When compaction writes a boundary, the payload carries `boundary_id`; resume/rehydration must load the boundary from SQLite rather than reconstructing it from the stream payload.
- `procedure.activation` is the persisted visibility event for procedure usage trace. It carries procedure ref/title/kind, phase, reason, score, status, origin, source refs, and evidence refs; it is a trace event only and does not create a second procedure truth.
- `skill.lifecycle` is the persisted visibility event for native skill lifecycle work. It carries skill id, lifecycle action, status/verdict, `assessment_id`, evidence refs, and optional `assessment` payloads. It projects lifecycle truth from runtime tools and does not infer skill quality from assistant prose. `skill_assess` is the active lifecycle action; missing required evidence refs or invalid verdicts remain inconclusive evidence, not a promotion signal.
- ContextPlane initial assembly budget is derived from the governor warning threshold, not a fixed percentage of the raw context window.

## 不变量

- run status、mode、lineage、events 和 plans 以 SQLite 为准。
- context boundary chain、summary、transcript reference 和 preserved segment references 以 SQLite `context_boundaries` 为准；RunEvent 只展示投影。
- context pressure 以 BudgetGovernor 的 effective-window calculation 为准；OpenAPI/mobile 只消费后端事实。
- context visibility 以 `context.pressure` / `context.compressed` RunEvent payload 为准；mobile 不使用本地 token estimate、message count 或 assistant 文本推断 pressure/boundary。
- initial model input 以 ContextSession Bootstrap 输出为准；ContextPlane assembly 不是由 executor 裸 prepend 成运行事实。
- direct_response stable instruction 和后续 assistant/tool history 以 ContextSession 为准；缺 ContextSession 是 runtime failure。
- provider context overflow recovery 只允许 `CompactTriggerReactive` + same-model single retry；retry 失败继续是显式 model/runtime error。
- run lifecycle 不再有累计 usage 百分比 `TokenBudget` 硬停；如果模型输入接近窗口，必须走 context pressure/compact protocol。
- proactive compaction 以 CompactionEngine 输出为准；Eino/ADK middleware 只负责触发 seam，不是 compact summary 或 rewrite truth。
- post-compact rehydration 以 contextplane rehydration packets 为准；packet 超过 token limit 直接失败，recent files 不做 workspace scan。
- 工具执行错误和 lifecycle rejection 是模型可见 failed tool result；runtime 自身装配、缺 tool lifecycle context/state/plane、graph、store 或工具边界损坏才让 run failed。
- Tool execution scheduling is centralized in `internal/runtime/tool.go` (`toolExecutionScheduler`) and consumed through `SafeParallelToolsNode.NewStreamingExecutor` / `internal/runtime/streaming_tool_executor.go` by both direct_response and graph execution. The executor enforces `maxParallel` via semaphore and handles unknown tools as model-visible failed results without silently dropping calls. Known loaded tools missing `ToolContract.Execution` are runtime wiring failures, not read-only defaults; model-called unknown/deferred tools remain model-visible failed tool results.
- Tool progress never enters model context directly. The model sees the final tool message; users see progress through persisted RunEvent projection.
- `direct_response` 绑定 run tools 和 tool lifecycle context，但不进入 PlanNode/ActNode、不启动 child run。
- `plan_execute` 的 execute child run 使用 `RequestedMode=single_agent`，child result 以 subagent evidence 回填父计划。当 step 显式声明 `verification_intent.kind=verifier` 时，runtime 会在 primary child execution 成功后运行只读 verifier child run，并把 verdict 回填为 `kind=verifier` plan evidence。Verifier failed/inconclusive 只让当前 step 失败，不会自动把 closeout 变成 blocking policy；`inconclusive` 不是成功。
