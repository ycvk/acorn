# Acorn 架构术语表

| 术语 | 当前含义 |
|---|---|
| **core** | `internal/core`（Layer 0）。核心 domain 类型 + store ports + 工具契约。零内部导入。 |
| **runtime** | `internal/runtime`（Layer 3）。Executor、RunnerFactory、buildRun、direct_response、ExecuteRound、Plane、Session、masking、auto-compact、StreamItem 投影。 |
| **Executor** | `internal/runtime.Executor`，接收 remote client / CLI / trigger 请求，创建 run，调用 RunnerFactory，执行 run lifecycle。 |
| **RunnerFactory** | `internal/runtime.RunnerFactory`，持有 runtime 共享依赖、ToolRegistry、MCP manager cache；每次 run 的具体装配由 `buildRun` 执行。 |
| **buildRun** | `internal/runtime/runner.go` 的 per-run assembly 入口，按固定主链接 model、tool catalog、prepared memory、Plane、direct_response，返回 `ActiveRunner`。 |
| **direct_response** | 唯一编排模式。model → tool loop → record → 下一轮。 |
| **ExecuteRound** | `internal/runtime` 的执行回合原语；direct_response 通过它执行模型回合。 |
| **Plane** | `internal/runtime.ContextPlane`，负责 context assembly（skill/memory context、deferred tool lifecycle）。 |
| **Session** | root-run model input 的唯一 owner。消息分为不可压缩的 prefix（assembled context + instruction）与 conversation；`BeforeModelCall` 执行 masking → apply pending compact → count tokens → 必要时启动后台 compact。 |
| **Observation masking** | tool result 超 `mask_after_turns`（默认 2）轮后用占位符替换。纯内存操作，不写 SQLite。 |
| **LLM auto-compact** | token 超 `window_tokens - compact_margin_tokens`（默认 margin 13000）时后台用一次 model 调用总结 conversation 前段，下一轮替换。Circuit breaker：连续 3 次失败停止。 |
| **Tool lifecycle state** | run-scoped 工具可见性状态（loaded/deferred tools）；执行前由 `SafeParallelToolsNode` 校验。 |
| **Risk gate** | `tools.ClassifyRisk` 的规则判定；高风险工具调用在执行前被拦截，模型须通过 `ask_operator` 申请批准。 |
| **Store ports** | `internal/core` 定义的 consumer-owned 持久化接口（SessionStore/IdentityStore/ArtifactStore）；`internal/wire/container.go` 是唯一允许直接持有 sqlite adapter 的 composition root。 |
| **Device Auth** | Single-owner self-hosted auth boundary：`acorn pair` 写一次性 pairing code hash，`POST /v1/devices:pair` 换取一次性展示的 bearer token；SQLite 只保存 token hash。 |
| **Client RunEvent** | `/v1` 的 client-facing live event envelope；mobile client 只消费 mobile live subset（run lifecycle、assistant delta/message、terminal status、resume、elicitation/operator question、decision_blocked）；由 `internal/api/projection.go` 从 `core.EventRecord` 投影。 |
| **SQLite persisted truth** | 后端 runtime 事实来源（10 张表）：events、runs、sessions、session_messages、pending_actions、mcp_oauth_tokens、devices、pairing_codes、artifacts、schema_migrations。长期 memory 的 active truth 是 `internal/memory` 文件。 |
| **WorldState** | `internal/memory.WorldState`，跨 run 的 file-backed key-value 决策投影，唯一变更路径是 `ApplyDelta`。 |
| **Trigger** | `internal/triggers` 的 webhook（HMAC 验签）或 cron 唤醒；每次 fire 起一个新的短命 run。 |
| **Mobile Control Surface** | `mobile-kotlin/` Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 `/v1`；不执行 runtime、不维护第二套 message lifecycle、不做 offline-first truth。 |
