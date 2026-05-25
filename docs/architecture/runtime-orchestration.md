---
doc_type: architecture
status: current
last_reviewed: 2026-05-09
slug: runtime-orchestration
---

# Runtime Orchestration

## OrchestrationPlane

`internal/orchestration` 是 root 编排唯一 builder 入口。当前实现是 concrete `DefaultPlane`，由 `NewDefaultPlane(DefaultPlaneOptions)` 构造；RunnerFactory 在 `internal/runtime/runner_orchestration.go` 注入 tool builder、tool node factory、graph builders、handlers builder 和 context binders。生产代码不再暴露跨包 `Plane` interface；runtime 只保留本地 `orchestrationPlane` 测试 seam，不新增第二套 plane abstraction。

三个 build 方法：

- `BuildDirectResponse`：`internal/orchestration/direct_response_builder.go`，用于纯对话/普通问答，同时支持轻量 tool-call loop。
- `BuildSingleAgent`：`internal/orchestration/single_agent_builder.go`，构建 PlanNode -> ActNode -> Observe -> Final 的 internal child-run graph；不作为 public root create-run mode 暴露。
- `BuildPlanExecute`：`internal/orchestration/single_agent_builder.go` 调用 runtime 注入的 plan-execute graph builder，父 run 计划后派发 child single-agent run。

## direct_response

`direct_response` 读取 run tool catalog、绑定 ContextPlane tool lifecycle state，并构建 `SafeParallelToolsNode`。它不启动 PlanNode，也不调用 child-agent executor；执行时按 `ContextSession.BeforeModelCall -> AssistantStreamer.StreamAssistantInterleaved -> StreamingToolExecutor.Submit/GetRemainingResults -> ContextSession.RecordAssistant/RecordToolResults` 的 session-owned loop 推进，直到模型返回无 tool call 的最终 assistant message。`AssistantStreamer` 负责把模型 stream chunk 持久化为 `assistant.delta`，再保留最终完整 assistant message。

`direct_response` 是 Acorn-specific ADK agent，不是 Eino `adk.NewChatModelAgent` 的薄封装。保留自定义 loop 的原因是：普通问答必须产出 Acorn RunEvent / StreamItem truth，tool lifecycle 必须和 ContextPlane 的 loaded/deferred state 绑定，普通 tool failure 必须继续作为模型可见 failed tool result。缺 lifecycle wiring、缺 catalog、缺 tool node 或模型 streaming 失败是 runtime failure。

ContextSession 拥有所有 root mode 的首轮 model input，并且 direct_response 的深层 tool loop 已经不再维护 loop-local messages。缺少 root ContextSession binding 时 direct_response 直接失败，不回退到 `input.Messages`。

CompactionEngine 拥有 proactive compact 的 summary/rewrite 规则。ADK handler stack 和 ContextSession direct loop 都通过 BudgetGovernor pressure 触发 engine-owned compact；summary shape、tail preservation、contextplane rehydration packets 和 token metrics 不由 adapter 自己决定。

Reactive compact 是 provider overflow 专用恢复路径。`direct_response` 在 `AssistantStreamer.StreamAssistantMessage` 返回明确 context overflow 后，通过 ContextSession 执行 `CompactTriggerReactive` 并用同一个 message id、model、tool infos 重试一次；普通 model error 不重试。ADK graph modes 不把内部控制 prompt 写入 ContextSession，而是在 compaction middleware 的 `WrapModel` seam 对 `Generate` / `Stream` 的直接 overflow error 做同样的一次 reactive compact retry。

single_agent / plan_execute 内部的 PlanNode plan JSON、ObserveNode decision JSON、ActNode step instruction 和 plan-execute dispatch summaries 是 graph control state，不是 user-facing conversation history。它们继续由 graph state / PlanStore / evidence ledger 持有，不写入 ContextSession，避免把内部控制消息污染为会话事实。

## single_agent

`single_agent` 是内部 child-run / verifier / skill-eval 执行模式，使用 runtime graph：

- `internal/runtime/agent_graph.go` 构建 PlanNode / ActNode / ObserveNode / FinalNode。
- graph builder 必须拿到 runtime `PlanStore`；缺 plan store 直接构建失败。
- Procedure activation 不在 graph 内生成 synthetic plan。Learned procedures now enter the run as file-backed memory skill entries from `memorymodule.Prepare`; ordinary executable skills still come from `internal/skills` selection and ContextPlane injection.
- ActNode 用 `SafeParallelToolsNode` 执行工具，并把 tool result 写入 step evidence ledger；同一结果同时由 ContextPlane 写入 durable `tool_results` ledger，再由 PlanStore 把 step evidence backlink 回写到同一条 tool result 记录。workspace mutation checkpoint / rollback 的 side-effect refs 也沿这条链路进入 ledger 和 workbench projection。
- `SafeParallelToolsNode` 是 Acorn-specific tool dispatch adapter；实际批次、路径冲突和结果顺序由 `internal/runtime/tool.go` 的 shared scheduler core 处理，并通过 `internal/runtime/streaming_tool_executor.go` 暴露实时提交接口。它从 `tooling.ExecutionPolicyResolver` 读取 `ToolContract.Execution`，保留 policy-aware parallelism、ContextPlane tool lifecycle 和 plan evidence recorder；已加载工具没有 execution policy 是 runtime wiring failure，不会默认成 read-only。真实工具执行时会显式触发 Eino Tool component callbacks，因此外部 Eino callback/DevOps handler 能看到 tool OnStart/OnEnd/OnError。模型调用 unknown/deferred tool 仍是模型可见 failed tool result，不伪造真实工具 callback success。

## plan_execute

`plan_execute` 是 root parent graph，由 `internal/runtime/plan_execute_graph.go` 管理。父 run 负责计划和 closeout；每个 runnable step 通过 `ChildAgentExecutor` 派发为 `RequestedMode=single_agent` 的 child run。child result 以 `kind=subagent` evidence 回填父计划，`verification_intent=subagent` 只有在 child evidence passed/confirmed 时才能完成。

Verifier child runs 仍走同一 `ChildAgentExecutor` 合同，但 origin 固定为 `verifier`，任务只读，返回 `VerificationResult` 的 `passed|failed|inconclusive` verdict。`plan_execute` 只在 step 显式声明 `verification_intent.kind=verifier` 时触发 verifier；verifier 结果会转成 `kind=verifier` plan evidence，failed/inconclusive 会让当前 step 失败。它不是自动 blocking closeout，也不会把验证本身提升成第二套 closeout policy。

`plan_execute` 对照 Eino `adk/prebuilt/planexecute` 的 plan/execute/replan 思路，但不是直接复用 prebuilt agent。Acorn 自定义 graph 的硬约束是 persisted `PlanStore`、step evidence ledger、child run lineage、workbench/trace truth 和 fail-loud runtime errors。后续如果吸收 Eino prebuilt 能力，只允许吸收 planner output/tool-calling planner 等局部模式，不能替换这套持久化和 child-agent contract。

Eino `adk/prebuilt/deep` 的 subagent/filesystem/shell 组合能力也只作为参考输入。Acorn 的多 agent 执行事实仍是 `ChildAgentExecutor` + child run/session + evidence summary；不能新增第二套 subagent task protocol。

## Child-agent contract

共享合同在 `internal/orchestration/child_agent.go`：

- `ChildAgentRequest` 包含 task、context messages、allowed tool names、acceptance、expected evidence 和 parent run/session 元数据。
- `ChildAgentResult` 包含 child run/session、final status、acceptance、evidence summaries 和 effective tool names。
- `ChildAgentExecutor` 被 `delegate_task`、plan_execute、verifier 和 run toolset 共用。
- `ChildAgentOriginVerifier` 标识只读 verifier child run；`VerificationRequest` / `VerificationResult` 是 verifier 专用 typed contract，`EvidenceKindVerifier` 是其 plan evidence bridge。

## Tool Boundary Validation

Acorn 不再有通用 orchestration guardrail 链，也不再维护命令名 allowlist。安全和输入校验落在具体工具边界：workspace mutation tools 通过 `workspace.ResolveWritePath` 限制相对路径、root escape、symlink escape 和 mutation denylist；`run_command` 只校验 workspace cwd、timeout 和显式 `pause_before_exec`。命令是否存在由宿主系统决定，缺失二进制或非零退出码作为真实 tool result 返回模型。缺 lifecycle context/state/plane、graph、store 或工具边界自身损坏才是 runtime failure。

Tool runtime contract lives in `internal/tooling.ToolContract`. Runtime tool builders must assign loading/execution/result/boundary/projection policy before catalog construction. MCP enabled providers must declare `tool_safety`; missing or invalid safety fails config/runtime build instead of silently assuming read-only.

## 装配边界

默认 OrchestrationPlane 的依赖来自 RunnerFactory，构造集中在 `internal/runtime/runner_orchestration.go`：

- tool builder：`internal/runtime/tool.go` 的 audited tools。
- tool node factory：`internal/runtime/safe_parallel_tools_node.go` 的 safe parallel tools node。
- graph builders：runtime 的 `buildAgentGraph` 与 `buildPlanExecuteGraph`。
- handlers builder：ContextPlane compaction middleware adapter + runtime custom handlers；旧 runtime sliding window middleware 已删除。
- context binders：store、session id、tool lifecycle context。
- context session：Executor Bootstrap 后挂在 `ActiveRunner` 并绑定到 root execution context；direct_response 内部 message loop 通过 ContextSession 统一，graph modes 的 internal control prompts 不进入 session history。

这些是装配细节，不改变 root mode 语义。
