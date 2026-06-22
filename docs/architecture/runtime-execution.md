---
doc_type: architecture
status: current
last_reviewed: 2026-06-21
slug: runtime-execution
---

# Runtime Execution

## 现状

Acorn 的执行层由 `internal/runtime.Executor` 启动 run。用户执行入口来自 authenticated `/v1` remote client contract；operator CLI 只保留 `doctor`、`serve`、`pair`、skills、memory rebuild 等运维/诊断命令。app container 在 `internal/app/container.go` 装配 runtime executor、run resume service 和 web dependencies，Web handler 只调用 app/runtime service，不直接拼 runtime 状态。

## Run lifecycle

- `internal/runtime/executor.go` 创建或恢复 session/run，写入 `events.RunRecord`。只有一个编排模式 `direct_response`。
- `RunnerFactory.New` 在 `internal/runtime/runner.go` 中只保留入口委托；`internal/runtime/run.go` 的 `RunnerFactory.buildRun` 构建 `ActiveRunner`。主链是：创建 chat model；bootstrap MCP 并构建 run tool catalog；调用 `memorymodule.Service.Prepare` 得到 file-backed prepared memory；assemble ContextPlane；委托给 `orchestration.DefaultPlane.BuildDirectResponse`。
- Run tool catalog is built from `tooling.ToolContract`. Each enabled tool has explicit identity, source, kind, category, loading policy, and execution policy; incomplete contracts fail catalog construction.
- Executor 在 model run 前通过 ContextSession Bootstrap 生成首轮 `ModelInput`。Bootstrap 合并 ContextPlane assembly 与 initial user messages；`direct_response` 额外把 stable instruction 作为 leading system message 交给 ContextSession。
- Chat model 只来自配置中唯一 enabled LLM provider；runtime 不提供 priority、backoff、透明 failover 或 provider retry/switch。MCP provider 只暴露 startup health、catalog/auth lifecycle 和真实错误。
- `internal/runtime/executor.go` 和 executor finalization 路径负责把 ADK events、assistant message、run terminal status 和 memorymodule history append 收口到 persisted truth。
- Interrupted run resume truth is inferred from persisted root interrupt contexts. `RunResumeService` recognizes empty/default interrupt kind plus `run_command_pause` when reconstructing resume targets for `/v1/runs/{id}:resume`.

## Hybrid Context

- ContextSession 在 `BeforeModelCall` 中执行 observation masking + LLM auto-compact（token 超阈值时生成 summary，circuit breaker 3 次失败停止）。
- Tool result 不再持久化为 durable ledger。结果留在 message stream 中，由 masking 按 `mask_after_turns` 轮数替换为占位符。
- Context boundary 不持久化（compact 边界是内存状态）。
- 不再有 CompactionEngine、BudgetGovernor、reactive compact、rehydration packet。

## Tool Execution

- Tool execution is stream-first and unified through `AgentLoop` / `ExecuteRound`. `StreamingToolExecutor` interleaves assistant streaming with tool submission via `Submit(call)`, then collects final results with `GetRemainingResults`.
- `read_only` tools execute in parallel; `serial` tools execute serially. No path conflict detection — serial tools without `PathArg` (like `ask_operator`, `load_tools`, `remember`) execute without path validation.
- Tool progress callbacks are ephemeral and are not persisted as run events; durable tool truth is the terminal `schema.ToolMessage` and run events.
