---
doc_type: architecture
status: current
last_reviewed: 2026-10-01
slug: runtime-execution
---

# Runtime Execution

## 现状

Acorn 的执行层由 `internal/runtime.Executor` 启动 run。执行入口有三类：authenticated `/v1` remote client、operator CLI 的一次性 `acorn run` / `acorn smoke`、以及 `internal/triggers` 的 webhook/cron trigger。wire container 在 `internal/wire/container.go` 装配 runtime executor、run resume service、trigger scheduler 和 web dependencies；Web handler 只调用 runtime service，不直接拼 runtime 状态。

## Run lifecycle

- `internal/runtime/executor.go` 创建或恢复 session/run，写入 `core.EventRecord`。只有一个编排模式 `direct_response`。
- `RunnerFactory.New`（`internal/runtime/runner.go`）委托 `RunnerFactory.buildRun` 构建 `ActiveRunner`：创建 chat model；构建 run capabilities（unified ToolRegistry + MCP resource/prompt tools）；调用 `memory.Service.Prepare` 得到 prepared memory；assemble Plane；交给 `buildDirectResponse`。
- Run tool catalog 来自 `core.ToolContract`；不完整的 contract 让 catalog 构造失败。详见 [tools.md](tools.md)。
- Executor 在 model run 前通过 `Session.Bootstrap` 生成首轮 `ModelInput`：assembled context messages + stable instruction（system）+ 初始 user messages。
- Chat model 只来自配置中唯一 enabled LLM provider；runtime 不做 provider failover/retry。MCP provider 只暴露 startup health、catalog/auth lifecycle 和真实错误。
- Executor finalization 把 ADK events、assistant message、run terminal status 和 `internal/memory` history append 收口到 persisted truth；每 `memory.review.review_interval` 个 run 异步触发一次 periodic memory review。
- Interrupted run resume truth 从 persisted root interrupt contexts 推断。`RunResumeService` 识别空/default interrupt kind 与 `run_command_pause`，用于 `/v1/runs/{id}:resume`。
- `RunController` 按 run ID 记录 cancel 函数，支持中断在途 run。

## Context

Session 在 `BeforeModelCall` 中执行 observation masking + 非阻塞 LLM auto-compact，详见 [runtime-orchestration.md](runtime-orchestration.md)。Tool result 留在 message stream 中，不单独持久化；compact 边界是内存状态。

## Tool Execution

- Tool execution 是 stream-first 的，统一经过 `ExecuteRound`。`StreamingToolExecutor` 在 assistant streaming 过程中通过 `Submit(call)` 提交工具调用，再用 `GetRemainingResults` 收集结果。
- 调度策略（`read_only` 并行、`serial` 按 path 冲突串行）见 [tools.md](tools.md)。
- Tool progress callbacks 是临时的，不持久化为 run events；durable tool truth 是 terminal `schema.ToolMessage` 和 run events。
