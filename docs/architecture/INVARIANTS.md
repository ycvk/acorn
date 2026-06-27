# Acorn 架构不变量

每条不变量标注执行它的测试文件。新增不变量须同步加测试。

## 核心层 (core)

- **core 有零内部导入**：`internal/core` 不导入任何 `github.com/ycvk/acorn/internal/*` 包；core 是 Layer 0，只依赖外部 SDK（Eino schema/adk）。
  - `tests/architecture/dependency_direction_test.go`
- **core 拥有 3 个 store 接口**：`SessionStore`/`IdentityStore`/`ArtifactStore` 是 core 定义的 consumer-owned 持久化接口，取代旧的 SessionRepo/MessageRepo/RunRepo/EventRepo/PendingActionRepo/DeviceRepo/ArtifactRepo/SummaryRepo/OAuthRepo。
  - `internal/core/store.go`
  - `internal/core/core_test.go`

## 运行时与编排

- **单一编排模式 direct_response**：`directResponseAgent.runFromState` 直接调用 `ExecuteRound` 执行模型回合（`AgentLoop` 中间层已内联删除）。plan_execute/single_agent 模式已删除。Session 在 BeforeModelCall 中执行 masking + auto-compact。runtime 包合并了 `agent`+`context`+`stream`，依赖方向无环。
  - `tests/architecture/structural_limits_test.go`
- **runtime 执行链自包含**：`internal/runtime` 拥有 executor、per-run assembly（内联函数,非 struct）、direct_response、ExecuteRound、Plane、Session、masking、auto-compact、StreamItem 投影；不依赖 `internal/agent`/`internal/context`/`internal/stream`（已全部合并删除）。
  - `tests/architecture/structural_limits_test.go`
- **Partial tool-call rejection 不丢弃已执行结果**：当 `BeforeToolCall` 拒绝一批 tool call 中的一个（如 `file_delete` 需审批）,已提交的 calls（如 `file_read`）结果必须保留。`consumeInterleavedForAgentLoop` 不 Discard executor,记录 `rejectedErr` 后继续消费 stream 到 finalMessage。`ExecuteRound` 在 rejection 时调 `GetRemainingResults` 收集已提交 calls 的结果。`direct_response.go` 先记录已执行 tool results,再记录 approval-required message。
  - `internal/runtime/agent_loop_partial_rejection_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire/container.go` 直接 import `internal/store`；其他包只依赖 consumer-owned ports（`core.SessionStore`/`core.IdentityStore`/`core.ArtifactStore`）或 `internal/store` shared records/errors。
  - `tests/architecture/dependency_direction_test.go`
- **Consumer-owned store 接口收敛**：`internal/runtime` + `internal/wire` 顶层定义的 consumer-owned store 接口（Store/Port/Repository/Ledger）≤4（RuntimeStore）。
  - `tests/architecture/store_interface_count_test.go`

## 上下文与记忆

- **Hybrid context: masking + non-blocking auto-compact**：Session 在 BeforeModelCall 中执行 observation masking（旧 tool result 替换为占位符）+ 非阻塞 auto-compact（token 超阈值时 `maybeStartCompact` 启后台 goroutine 生成 summary，turn 间由 `applyPendingCompact` 原子 splice，circuit breaker 3 次失败后停止）；public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`、`context.preserve_recent_turns`。
  - `internal/runtime/context_session_test.go`
  - `internal/runtime/masking_test.go`
  - `internal/runtime/auto_compact_test.go`
  - `internal/runtime/auto_compact_nonblocking_test.go`
- **Memory Record V2 是长期记忆事实**：facts/history frontmatter 由 `internal/memory` 解析；memory search 默认走关键词匹配，`memory.embedding.enabled` 开启时走 vector KNN + keyword RRF 融合（sqlite-vec，复用 provider embedding 端点）。

## Remote API 与 mobile

- **Remote client 必须设备认证**：除 `/healthz` 和 `POST /v1/devices:pair` 外，`/v1` 只接受 valid device bearer token；missing/malformed/unknown 返回 `unauthenticated`，revoked 返回 `device_revoked`。
  - `internal/store/store_schema_test.go`
- **OpenAPI 是 wire contract**：remote client DTO 只投影 core domain 类型；改 wire shape 须同步 `docs/openapi.yaml` + generated mobile client。`clientevents` 包已合并进 `internal/api`，投影逻辑在 `projection.go`/`projection_helpers.go` 中，不导入 `internal/runtime`。`thread_service.go`/`event_service.go` 合法导入 `internal/core`，不在 projection boundary 列表中。
  - `tests/architecture/client_projection_boundary_test.go`
- **Mobile 是 control surface 不是 runtime**：mobile 不执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle；context pressure/boundary/run status 都消费后端 projection。
  - `mobile-kotlin/app/src/test/...`（JUnit）

## Triggers (ambient)

- **Trigger scheduler 是 run 外常驻进程**：`internal/triggers.Scheduler` 住 `serve` 进程内，与 `Executor` 平级，不属任何 per-run 生命周期。trigger fire 时调 `RunService.CreateRun` 起新短命 run，不续 session。`/v1/triggers/{id}` 端点不经 device auth，用 HMAC 验签。`Stop()` 清理 pending debounce timers 避免 shutdown 孤儿 run。
  - `internal/triggers/scheduler_test.go`
  - `internal/triggers/webhook_test.go`
- **Trigger 成本控制**：debounce + duplicate-skip + daily quota 三护栏。`triggers.debounce_millis`（默认 0=禁用,推荐 2000）合并同 trigger 窗口内多次 fire 为一次 run（last input wins,per-trigger timer 独立）。`triggerRunCreator.shouldSkipRun` 在 WorldState 投影 + input 指纹（SHA-256,key 排序确定性）与上次相同时跳过 CreateRun——首次不跳过,nil WorldState 不跳过。`triggers.daily_quota`（默认 0=不限）限制每 UTC 日 trigger fire 起 run 次数,超限静默丢弃（warn log）,serve 重启归零。webhook spam 100 次相同 payload + 无状态变化 = 0 次 LLM 调用。
  - `internal/triggers/scheduler_debounce_test.go`
  - `internal/wire/trigger_skip_test.go`
  - `internal/wire/trigger_quota_test.go`
- **Trigger fire → 起新 run，不续 session**：trigger fire 走 `Executor.ExecuteMessages` 起新 run，`RunTimeoutSeconds`(默认 900s) + `direct_response` 同步 loop 决定长 run 不可行。WorldState 是跨 run 唯一状态，session 是 per-run 临时态。
  - `internal/triggers/scheduler_test.go`
- **WorldState 是跨 run 决策投影**：`internal/memory.WorldState` 是 file-backed key-value store（`{storage_dir}/worldstate/state.json`），只有 `ApplyDelta` 一条变更路径（upsert/delete）。填补 Session（per-run 临时）和 facts（显式 remember）之间空白。内存 cache + mutex 串行写，避开 SQLite 单连接瓶颈。agent 通过 `worldstate_update`/`worldstate_load` 工具读写。
  - `internal/memory/worldstate_test.go`
- **Ambient 身份指令硬编码进 base instruction**：`internal/runtime/runner.go` 的 `ambientAgentInstruction` 常量拼进 `buildStableInstruction`（用户 prompt 之后、capability discovery 之前），教 agent ambient 五步循环（orient → assess → act → record → stop）。用户 prompt 不可覆盖——ambient 循环是 agent 身份的核心，不是可配置行为。`triggerRunCreator.CreateRun` 在 input 前加 trigger 唤醒上下文，`formatWorldStatePrefix` 加引导语让 agent 理解注入的 KV 是自己的跨 run 记忆。
  - `internal/wire/trigger_worldstate_e2e_test.go`
- **Decision Card 扩展 ask_operator payload**：`OperatorQuestionPayload` 增 `considered_options/rationale/risk/recommendation` 维度（向后兼容，旧 payload 仍解码）。不是新建审批系统，是给 `ask_operator` 补决策依据。风险分级用规则（非 LLM）判定器 `internal/tools.ClassifyRisk`，硬编码高风险白名单不可降级。
  - `internal/core/decision_card_test.go`
  - `internal/tools/risk_gate_test.go`
- **search_runs 工具让 agent 检索自己 run 历史**：`SearchRuns(ctx, query, limit)` 对 `runs.input_text` 做 LIKE 关键词匹配,返回匹配 run 摘要。工具注册为 `core.ToolKindNative` / `ToolCategoryInspect` / 只读并行。让 agent 能"回忆自己做过什么",不依赖每次显式 `remember`。
  - `internal/store/store_search_runs_test.go`
## 代码规范

- **Error 分两类**：Exported sentinel error（需要被 `errors.Is` 比对）必须是包级 `var ErrXxx`；precondition/internal-config error（不该发生的编程错误）用 inline `errors.New("...")` 直接返回。`.golangci.yml` 的 `errname` linter 强制导出 sentinel 命名。
  - `.golangci.yml`（errname linter）
