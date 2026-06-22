# Acorn 架构术语表

| 术语 | 当前含义 |
|---|---|
| **Executor** | `internal/runtime.Executor`，接收 remote client 请求，创建 run，调用 RunnerFactory，执行 run lifecycle。 |
| **RunnerFactory** | `internal/runtime.RunnerFactory`，持有 runtime 共享依赖、registry、workspace、provider 和 concrete orchestration builder；每次 run 的具体装配由 `buildRun` 执行。 |
| **buildRun** | `internal/runtime/run.go` 的 per-run assembly 入口，按固定主链接 model、tool catalog、prepared memory、ContextPlane、OrchestrationPlane，返回 `ActiveRunner`。 |
| **direct_response** | 唯一编排模式。model → tool loop → record → 下一轮。不存在 plan_execute/single_agent/child_agent。 |
| **RunActionRound** | `internal/orchestration` 的执行回合原语（ExecuteRound）；direct_response 通过它执行模型回合。 |
| **ContextPlane** | `internal/contextplane` 的运行时上下文边界，负责 context assembly、observation masking、LLM auto-compact、deferred tool loading、tool lifecycle。 |
| **ContextSession** | root-run model input 的唯一 owner。`BeforeModelCall` 中执行 masking → count tokens → auto-compact → return ModelInput。 |
| **Observation masking** | tool result 超 `mask_after_turns`（默认 2）轮后用占位符替换。纯内存操作，不写 SQLite。 |
| **LLM auto-compact** | token 超 `window_tokens - compact_margin`（默认 13000）时用一次 model 调用生成 summary。Circuit breaker：连续 3 次失败停止。 |
| **Tool lifecycle state** | `internal/contextplane` 管理的 run-scoped 工具可见性状态（loaded/deferred tools）；执行前由 `SafeParallelToolsNode` 校验。不写 durable ledger。 |
| **ToolExecutionScheduler** | `internal/runtime/tool/` 的共享调度核；通过 `ToolContract.Execution` 消费 parallel policy（read_only 并行 / serial 串行）。 |
| **OrchestrationPlane** | `internal/orchestration` 的编排入口；RunnerFactory 委托它构建 direct_response assembly。只有一个 `BuildDirectResponse` 方法。 |
| **MemoryModule** | `internal/memorymodule` 的 file-backed memory 模块；管理 facts、history、search、prepare、memory file tools。Canonical Memory Record V2 frontmatter（简化：status / tags / created / updated / source_run / source_refs）。 |
| **Embedding+SQLite semantic search** | 可选的 rebuildable retrieval index，由 `memory.semantic.embedding` 配置、`acorn memory semantic rebuild` 重建，为 `memorymodule.Search`/`Prepare` 提供 embedding 向量 + 暴力余弦相似度检索。零 CGO。 |
| **Store ports** | app/runtime 顶层定义的 consumer-owned persistence 接口（ExecutorStore、RunnerFactoryStore、containerRuntimeStore、containerAppStore 等）；`internal/app/container*.go` 是唯一允许直接持有 sqlite adapter 的 composition root。 |
| **Device Auth** | Single-owner self-hosted auth boundary：`acorn pair` 写一次性 pairing code hash，`POST /v1/devices:pair` 换取一次性展示的 bearer token；SQLite 只保存 token hash。 |
| **Client RunEvent** | `/v1` 的 client-facing live event envelope；mobile client 只消费 mobile live subset（run lifecycle、assistant delta/message、terminal status、resume、elicitation/operator question、decision_blocked）；由 SQLite `events` 表投影。 |
| **SQLite persisted truth** | 后端 runtime 事实来源（~8 张表）；events、runs、messages、sessions、pending_actions、devices、pairing_codes、owner_profile。长期 memory 的 active truth 是 `memorymodule` 文件 + `memory_vectors` 向量。 |
| **Mobile Control Surface** | `mobile-kotlin/` Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 `/v1`；不执行 runtime、不维护第二套 message lifecycle、不做 offline-first truth。 |
