---
doc_type: dev-guide
slug: agent-plan-loop
component: agent-plan-loop
status: current
summary: 面向 Acorn 贡献者说明 Plan-Act-Observe 执行循环、plan 存储和 stream/API 合同
tags: [runtime, graph, plan, stream]
last_reviewed: 2026-05-09
---

# Agent Plan Loop 开发者指南

## 概述

Agent Plan Loop 是 Acorn 面向工具任务的 agent graph 执行循环。未显式指定 mode 的 root run 由 root router 进入 `direct_response`，不会生成 plan，但仍能跑最小 conversational tool-call loop；带 `skill_id` 的 root run 或显式 `mode=plan_execute` 的入口才进入结构化 plan/execute 循环，由运行时生成结构化计划，再按 plan step 执行工具，最后观察当前结果并决定继续、重规划或结束。计划不再通过 `plan_write` / `plan_read` 这类 agent 工具暴露，而是由 graph 内的 `PlanNode`、`ActNode` 和 `ObserveNode` 直接管理。

## 前置依赖

- Go 1.26。
- 可用的 Eino chat model；配置了工具时，模型必须支持 tool calling。
- 可用的 SQLite store；agent graph 构建时必须传入 `PlanStore`。
- 运行路径需要 `session_id` 和 `run_id` 写入 context，否则 `PlanNode` / `ActNode` 会直接报错。

## 快速上手

贡献者通常不直接 new graph，而是走 RunnerFactory 装配路径。用户可见执行入口是 authenticated `/v1` remote client contract；先启动后端并用 `acorn pair` 换取 device token，再创建 thread/message/run：

```bash
go run ./cmd/acorn serve -c configs/acorn.example.yaml
go run ./cmd/acorn pair -c configs/acorn.example.yaml --server-url http://127.0.0.1:8080 --json

curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Plan loop check"}' \
  http://127.0.0.1:8080/v1/threads

curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":{"type":"text","text":"请读取 README.md，并总结当前项目骨架。"}}' \
  http://127.0.0.1:8080/v1/threads/THREAD_ID/messages

curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"plan_execute"}' \
  http://127.0.0.1:8080/v1/threads/THREAD_ID/runs
```

Plan state is a backend store fact, not a RunEvent family. Debug plan creation, status changes, and evidence through `PlanStore` / SQLite plan rows; do not reintroduce plan/step stream diagnostics or public PlanDTO/workbench projections.

如果需要远程核对一次 run 的用户可见事实，可通过标准 `/v1` run detail 查看 run/thread、live event activity 和 artifacts：

```bash
curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  http://127.0.0.1:8080/v1/runs/RUN_ID/detail
```

## 核心概念

`PlanNode` 负责用专用 plan prompt 生成 JSON plan steps，校验后保存到 `PlanStore`。如果 session 已有可继续的 runnable plan，且当前不是 replan 决策，它会复用已有计划，不覆盖未完成状态。

当需要生成新计划或 replan 时，`PlanNode` 用专用 system prompt、当前 conversation context 和可见工具名生成计划。

`ActNode` 每次只推进一个 runnable step。它先把 step 标记为 `in_progress`，再用 `orchestration.ExecuteRound` 让模型为该 step 选择工具调用并收集终态 tool results；工具实际执行仍通过 `SafeParallelToolsNode` / `StreamingToolExecutor`。高风险工具的 active-plan policy 在 `BeforeToolCall` pre-submit hook 中检查，校验失败时工具不会被提交。工具结果会写入 step-scoped evidence ledger：基础 tool evidence、`run_command` 的 command/test evidence、`inspect_git_diff` 的 diff evidence、workspace mutation checkpoint / rollback evidence，以及到 durable tool result record 的 `tool_result_ref` backlink。普通工具错误仍作为 tool result 返回给模型，同时写 failed evidence 并把 step 标成 `failed`；没有 tool error 时还要检查 `verification_intent` coverage，只有 evidence 覆盖到位才会把 step 标成 `completed`。

当 step 调用 `delegate_task` 时，tool 输入必须是结构化 `DelegationSpec`，至少包含 `task` 和非空 `acceptance_criteria`。runtime default child-agent adapter 会把 child run 的 `succeeded` / `failed` / `interrupted` 终态转换成结构化 `ChildAgentResult`：包含 `child_run_id`、`child_session_id`、`final_status`、`output_summary`、`acceptance`、`evidence_summaries` 和 `effective_tool_names`。`ActNode` 解析这个结果后会追加一条 `kind=subagent` evidence；如果 `acceptance.status=failed`，当前 step 会带着 acceptance reasons 直接失败，而不是退化成泛化的 verification gap。

Verifier child run 使用同一 `ChildAgentExecutor` lineage，但 origin 固定为 `verifier` 且默认只读。`plan_execute` 的 `ExecuteDispatchNode` 只在 step 显式声明 `verification_intent.kind=verifier` 时运行它；primary child execution 先写入 `kind=subagent` evidence，verifier 再消费 plan、acceptance criteria、tool result refs、evidence refs 和只读 inspection tools，返回 `passed | failed | inconclusive` verdict。`inconclusive` 不是成功；verdict 会作为 `kind=verifier` evidence 回填 plan，failed/inconclusive 会让当前 step 显式失败，不会升级成全局 closeout policy。

Workspace mutation tools 会创建 scoped mutation checkpoint 并把 checkpoint id 写入 tool output / tool result side effects。显式调用 `rollback_workspace_checkpoint` 才会 rollback；rollback result 同样通过 tool result ledger 和 PlanEvidence 保留为后端事实。前端不能从 git/local state 或 assistant 文本推断 checkpoint / rollback truth，也不能要求 RunDetail 暴露完整 checkpoint/rollback workbench 聚合。

`ObserveNode` 读取当前 plan 和消息历史，返回 `next`、`replan` 或 `done`。如果所有 steps 都已经是终态，Observe 会直接结束，不再调用模型。

`PlanStore` 是 runtime domain 接口，当前 SQLite adapter 复用已有 plan 表。不要在 storage 层加入业务判断；step 选择、状态推进和 risky tool 检查属于 runtime graph。

## 接口参考

### Go domain 类型

| 类型 | 位置 | 用途 |
|---|---|---|
| `Plan` | `internal/model/plan.go` | session/run 当前执行计划 |
| `PlanStep` | `internal/model/plan.go` | 单个执行步骤，含 `id`、`action`、`status`、`depends_on`、`repo_targets`、`verification_intent`、`risk`、`tool_hints`、`evidence` |
| `PlanRepoTarget` | `internal/model/plan.go` | step 声明的 workspace 相对目标路径、可选符号、理由和 confidence |
| `VerificationIntent` | `internal/model/plan.go` | step 执行前声明的验证方式，kind 为 test / build / lint / diff / read / manual / subagent / verifier / checkpoint / rollback |
| `PlanStepRisk` | `internal/model/plan.go` | step 风险分类，取值 read / write / execute / delegate |
| `PlanEvidence` | `internal/model/plan.go` | step 的结构化执行证据，包含 tool/command/diff/test/manual/subagent/verifier/checkpoint/rollback，并可携带 `tool_result_ref` backlink；runtime 验证逻辑在 `internal/runtime/plan_evidence.go`。 |
| `DelegationSpec` | `internal/tools/delegate_task.go` | `delegate_task` 的结构化父子任务合同，含 child task、tool allowlist、acceptance criteria 和 expected evidence |
| `VerificationRequest` / `VerificationResult` | `internal/orchestration/verifier.go` | verifier child run 的只读验证合同 |
| `PlanStore` | `internal/runtime/plan_store.go` | `LoadPlan`、`SavePlan`、`AppendStepEvidence` |

`PlanStep.status` 当前取值：

| 状态 | 含义 |
|---|---|
| `pending` | 尚未执行 |
| `in_progress` | 当前 step 正在执行 |
| `completed` | step 已完成 |
| `failed` | step 执行失败，等待 Observe 决定是否 replan |
| `skipped` | step 被跳过 |

### Graph 装配合同

`buildAgentGraph` 需要显式传入 `PlanStore`、`planPrompt` 和当前工具 catalog。没有 `PlanStore` 时 graph 构建失败，不会回退到旧 ReAct loop。

```go
func buildAgentGraph(
    ctx context.Context,
    agentName string,
    chatModel einomodel.BaseChatModel,
    safeToolNode *SafeParallelToolsNode,
    maxIterations int,
    checkpointStore compose.CheckPointStore,
    handlers []adk.ChatModelAgentMiddleware,
    planStore PlanStore,
    planPrompt string,
    eagerToolNames []string,
    toolSpecs []tooling.ToolSpec,
) (compose.Runnable[*agentGraphInput, *schema.Message], error)
```

### Diagnostics

Plan diagnostics are store-level facts: plan rows, step status, and `PlanEvidence`. There are no `plan.*` or `step.*` RunEvents. Remote clients must not consume plan execution state from the foreground SSE stream.

### Remote Client API

| endpoint | 返回 | 用途 |
|---|---|---|
| `GET /v1/runs/{run_id}/detail` | `RunDetail` | 查询 run detail 聚合，包含 run/thread、live events 和 artifacts |
| `GET /v1/runs/{run_id}/events` | `RunEvent` SSE/历史事件 | 查询 mobile live event subset，不包含 plan/step diagnostics |

`RunDetail` 和 `RunEvent` 的字段以 `docs/openapi.yaml` 为准，mobile Dart client 由 `mobile/tool/generate_openapi_client.py` 生成。不要恢复 legacy `/api/sessions/*/plan`、`/api/runs/*/plan`、public `PlanDTO` 或 runtime workbench 平行查询面。

## 常见场景

### 观察一轮执行的 plan 生命周期

```bash
curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  http://127.0.0.1:8080/v1/runs/RUN_ID/detail
```

Run detail 是 remote client 的 run/thread、activity 和 artifact source。完整 run 成败以 run status 和 terminal live event 为准。

### 让 remote client 消费 plan state

当前 mobile control surface 通过 `/v1/runs/{run_id}/detail` 消费 run/thread 和 artifacts，通过 `/v1/runs/{run_id}/events` 只消费 foreground live subset。扩展 plan state 时必须先明确新的 product surface 和 OpenAPI contract，不要恢复旧 frontend dispatcher、legacy `StreamItem` reducer、public PlanDTO/workbench aggregate，或把 plan/step diagnostics 重新塞进 live RunEvent。

新增 public plan surface 时必须同步三处：

- `docs/openapi.yaml`
- `mobile/lib/src/api/acorn_api.dart`
- 对应的 explicit projection/view model

新增 plan step、evidence、verifier、checkpoint 或 rollback 字段时还必须同步 runtime/store/Web DTO/OpenAPI/mobile types，并覆盖 SQLite roundtrip 与 stream payload clone，避免 metadata 在中途丢失。

### 处理工具失败

普通工具错误不是 graph failure。`SafeParallelToolsNode` 会把非 interrupt 工具错误保留为 tool result，`ActNode` 将当前 step 标记为 `failed`，然后 `ObserveNode` 可以选择 `replan`。只有模型调用失败、plan JSON 无法解析、plan 持久化失败、graph 装配错误、SQLite 等 Acorn 运行时错误才让 run failed。

### 检查 risky tool 计划约束

高风险工具仍通过 `ToolSpec.PlanPolicyRequireActivePlan` 约束执行。消费点在 `ActNode.enforceToolCalls`，它会在工具执行前检查 session 是否存在 active in-progress plan step。不要重新引入 `plan_write` 或 `plan_read` 工具来满足这个约束。

## 已知限制与注意事项

- Root router 会把普通问答留在 `direct_response`；public root request 只有显式 `mode=plan_execute` 或 `skill_id` 才生成执行计划。`single_agent` 只保留为内部 child-run / verifier / eval 执行模式。
- 初始 plan 和每次 replan 都消耗一次 plan iteration；超过 `maxIterations` 会显式报错。
- PlanNode 只重试一次无效 JSON 或无效 step 结构，第二次仍失败会返回 `new plan format` 错误。
- plan 使用现有 SQLite plan 表，不新增 migration。
- 调试 plan payload 时优先使用 authenticated `/v1/runs/{run_id}/detail`；不要恢复 CLI streaming 或 `/v1` live stream 作为第二套 plan client contract。
- 当前没有 repo-map/codeintel planning context；`repo_targets` 由 PlanNode 根据用户请求、conversation context 和可见工具生成。
- `tool_hints` 不是 allowlist；真正的工具边界仍由 tool catalog 和 `ToolSpec.PlanPolicy` 执行。
- `Risk=delegate` 仍只是 plan step risk 分类；真正的委派合同在 `delegate_task` 的 `DelegationSpec`，不会被内联进 `PlanStep` schema。
- `delegate_task` 必须显式提供 `acceptance_criteria`，且 `allowed_tools` 是 child run 的真实 allowlist；运行时不会静默追加额外恢复工具。
- child run 的 `failed` / `interrupted` 终态仍会回成结构化 `ChildAgentResult` 并写 failed subagent evidence；只有 child session bootstrap、executor 初始化、plan 读取、stream 持久化等 Acorn 编排故障才会让 run 直接报 runtime error。

## 相关文档

- [Agent Plan Loop 用户指南](../user/agent-plan-loop.md)
- [`docs/openapi.yaml`](../openapi.yaml)
- [Architecture overview](../architecture/ARCHITECTURE.md)
- [Runtime orchestration architecture](../architecture/runtime-orchestration.md)
