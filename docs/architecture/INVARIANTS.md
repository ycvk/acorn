# Acorn 架构不变量

每条不变量标注执行它的测试文件。新增不变量须同步加测试。

## 核心层 (core)

- **core 有零内部导入**：`internal/core` 不导入任何 `github.com/ycvk/acorn/internal/*` 包；core 是 Layer 0，只依赖外部 SDK（Eino schema/adk）。
  - `tests/architecture/dependency_direction_test.go`
- **core 拥有 3 个 store 接口**：`SessionStore`/`IdentityStore`/`ArtifactStore` 是 core 定义的 consumer-owned 持久化接口。
  - `internal/core/store.go`
  - `internal/core/core_test.go`

## 运行时与编排

- **每个 run 一个 Eino ChatModelAgent**：`buildAgentRunner` 组装 `adk.Runner{ChatModelAgent, EnableStreaming, CheckPointStore}`；handlers 依次为 patchtoolcalls → summarization → reduction(clear-only) → toolsearch(有 deferred 工具时) → approval → tool errors；工具串行执行（`ExecuteSequentially`），普通工具失败与未知工具调用作为模型可见的 tool result 返回。Executor 只负责把 `AgentEvent` 投影成 RunEvent。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/events_test.go`
- **审批绑定具体调用并可跨重启恢复**：`approval.require` 命中的工具调用由 approval middleware 登记 `tool_approval` pending action 并发起工具级中断；resume 时校验参数与登记时一致，accept 才执行，decline 把拒绝说明作为工具结果返回。同一轮里排在被拦截调用之后的工具调用会先执行完，被拦截的调用等决策后再执行。`approval.require` 不允许命中 `ask_operator`。checkpoint 经 `core.SessionStore` 落 SQLite `agent_checkpoints`，run 成功或失败结束时删除。
  - `internal/runtime/approval_test.go`
  - `internal/store/store_checkpoint_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire/container.go` 直接 import `internal/store`；其他包只依赖 consumer-owned ports（`core.SessionStore`/`core.IdentityStore`/`core.ArtifactStore`）或 `internal/store` shared records/errors。
  - `tests/architecture/dependency_direction_test.go`
- **Consumer-owned store 接口收敛**：`internal/runtime` + `internal/wire` 顶层定义的 consumer-owned store 接口（Store/Port/Repository/Ledger）≤4（RuntimeStore）。
  - `tests/architecture/store_interface_count_test.go`

## 上下文与记忆

- **上下文管理由 Eino middleware 承担**：summarization 在 token 超过 `window_tokens - compact_margin_tokens` 时总结历史；reduction 只做 clear（保留最近 `mask_after_turns` 轮工具调用原样）；memory 与 skill 目录等每 run 上下文写进 agent Instruction，不参与总结。public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`。
  - `internal/runtime/agent_test.go`
- **Memory Record V2 是长期记忆事实**：facts/history frontmatter 由 `internal/memory` 解析；memory search 默认走关键词匹配，`memory.embedding.enabled` 开启时走 vector KNN + keyword RRF 融合（sqlite-vec，复用 provider embedding 端点）。
- **三层记忆架构（ADR-0002）**：Active Memory（非 retired 的 user-scoped facts frozen snapshot，按 `memory.active.char_limit` 默认 2200 字符截取，每个 run 无条件注入 system prompt，run 内不变以保 prefix cache）+ Archive（append-only history + fallback summary，零 LLM 成本，混合检索覆盖）+ Periodic Review（每 `memory.review.review_interval` 默认 5 个 run 触发一次 LLM 调用，蒸馏 durable facts 写入 facts，异步不阻塞 run 收尾，`NewReviewer` 在 `buildContainerRuntimeDeps` 构造一次供所有 Executor 共享）。单 owner 语义：agent 自己写的 facts（unverified + verified）都信任，retired 才排除。
  - `internal/memory/active_facts.go`
  - `internal/memory/active_facts_test.go`
  - `internal/runtime/reviewer.go`
  - `internal/runtime/reviewer_test.go`
  - `internal/wire/runtime.go`

## Remote API 与 mobile

- **Remote client 必须设备认证**：除 `/healthz` 和 `POST /v1/devices:pair` 外，`/v1` 只接受 valid device bearer token；missing/malformed/unknown 返回 `unauthenticated`，revoked 返回 `device_revoked`。
  - `internal/store/store_schema_test.go`
- **OpenAPI 是 wire contract**：remote client DTO 只投影 core domain 类型；改 wire shape 须同步 `docs/openapi.yaml` + generated mobile client。投影逻辑在 `internal/api` 的 `projection.go`/`projection_helpers.go` 中，不导入 `internal/runtime`。`thread_service.go`/`event_service.go` 合法导入 `internal/core`，不在 projection boundary 列表中。
  - `tests/architecture/client_projection_boundary_test.go`
- **Pending action 对客户端可达**：action ID 由 `core.NewActionID()` 生成，只含 `[a-z0-9_]`，能直接放进 `/v1/pending-actions/{action_id}:decide`。每种 pending action kind 都以 `<kind>.pending` / `<kind>.decided` 进入 live RunEvent，且 live 事件集合与 OpenAPI `RunEvent` discriminator 一致。
  - `internal/core/core_test.go`
  - `internal/api/openapi_test.go`
  - `internal/wire/approval_restart_e2e_test.go`
- **Mobile 是 control surface 不是 runtime**：mobile 不执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle；context pressure/boundary/run status 都消费后端 projection。
  - `mobile-kotlin/app/src/test/...`（JUnit）

## 约定与唤醒

- **约定只由 wake 调度器触发，且只触发一次**：`wake.Scheduler` 住在 `serve` 进程内，每 30 秒先执行衰减，再处理到期的约定（`memory_items` 中 kind 为 commitment）。`ClaimDueCommitment` 用条件更新把 active 改为 woken，多个调度器并发时只有一个成功；随后经 `RunService.CreateWakeRun` 在约定所属线程里起 run（线程已删除时新建 Reminders 线程），并在该 run 上记录 `wake.fired`。起 run 失败时约定回到 active，唤醒时间推后 5 分钟。带 cron 的约定在触发后插入下一次的 active 条目。
  - `internal/wake/scheduler_test.go`
  - `internal/store/store_presence_test.go`
- **每日唤醒上限按 owner 时区计算**：`wake.daily_limit` 限制 owner 本地每天的唤醒次数，计数来自 events 表中当天的 `wake.fired`，重启后依然有效；超限的约定留在 active，次日再处理；为 0 时关闭自主唤醒。
  - `internal/wake/scheduler_test.go`
- **WorldState 是跨 run 决策投影**：`internal/memory.WorldState` 是 file-backed key-value store（`{storage_dir}/worldstate/state.json`），只有 `ApplyDelta` 一条变更路径（upsert/delete）。位于 thread 对话历史和 facts（显式 remember）之间。内存 cache + mutex 串行写，避开 SQLite 单连接瓶颈。agent 通过 `worldstate_update`/`worldstate_load` 工具读写。
  - `internal/memory/worldstate_test.go`
- **Decision Card 扩展 ask_operator payload**：`OperatorQuestionPayload` 增 `considered_options/rationale/risk/recommendation` 可选维度。它给 `ask_operator` 的提问补上决策依据；工具调用审批由 `approval.require` 规则和 approval middleware 负责。
  - `internal/core/decision_card_test.go`
- **经历检索覆盖 run 和工作记忆**：`recall` 工具调用 `SearchExperience`，用 FTS5 trigram 检索 `runs` 的输入输出和全部 `memory_items`；少于 3 个字的查询改走 LIKE。FTS 表上线前的 run 在打开数据库时回填一次。
  - `internal/store/store_presence_test.go`
  - `internal/tools/presence_tools_test.go`

## 代码规范

- **Error 分两类**：Exported sentinel error（需要被 `errors.Is` 比对）必须是包级 `var ErrXxx`；precondition/internal-config error（不该发生的编程错误）用 inline `errors.New("...")` 直接返回。`.golangci.yml` 的 `errname` linter 强制导出 sentinel 命名。
  - `.golangci.yml`（errname linter）
