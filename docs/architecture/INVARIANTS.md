# Acorn 架构不变量

每条不变量标注执行它的测试文件。新增不变量须同步加测试。

## 核心层 (core)

- **core 有零内部导入**：`internal/core` 不导入任何 `github.com/ycvk/acorn/internal/*` 包；core 是 Layer 0，只依赖外部 SDK（Eino schema/adk）。
  - `tests/architecture/dependency_direction_test.go`
- **core 拥有 6 个 store 接口**：`SessionStore`/`IdentityStore`/`ArtifactStore`/`PresenceStore`/`NotificationStore`/`KnowledgeStore` 是 core 定义的 consumer-owned 持久化接口。
  - `internal/core/store.go`
  - `internal/core/presence.go`
  - `internal/core/knowledge.go`
  - `internal/core/core_test.go`

## 运行时与编排

- **每个 run 一个 Eino ChatModelAgent**：`buildAgentRunner` 组装 `adk.Runner{ChatModelAgent, EnableStreaming, CheckPointStore}`；handlers 依次为 patchtoolcalls → summarization → reduction(clear-only) → toolsearch(有 deferred 工具时) → skill → presence → approval → tool errors；工具串行执行（`ExecuteSequentially`），普通工具失败与未知工具调用作为模型可见的 tool result 返回。Executor 只负责把 `AgentEvent` 投影成 RunEvent。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/events_test.go`
- **工具失败可见，主模型调用有界重试**：tool error middleware 把错误转成结果文本时按 call id 记录，projector 对这些调用发 `tool.call.failed`（其余发 `tool.call.succeeded`），两者都不进 live 契约。主模型调用失败最多重试 3 次，context 取消不重试；被重试的失败流不记 run 失败，只有成功那次的输出成为 assistant 消息；3 次重试都失败时 run 失败。
  - `internal/runtime/tool_errors_test.go`
  - `internal/runtime/model_retry_test.go`
- **回复先落库再报完成**：run 成功或失败结束时，Executor 先写 run output 和对应的 assistant 消息，再发 `run.completed`/`run.failed` 并把 run 标记为结束；写入失败让 run 返回错误。客户端在收到完成事件或看到结束状态后重新加载线程，一定能读到回复。
  - `internal/wire/wake_acceptance_e2e_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`
- **审批绑定具体调用并可跨重启恢复**：`approval.require` 命中的工具调用由 approval middleware 登记 `tool_approval` pending action 并发起工具级中断；resume 时校验参数与登记时一致，accept 才执行，decline 把拒绝说明作为工具结果返回。同一轮里排在被拦截调用之后的工具调用会先执行完，被拦截的调用等决策后再执行。`approval.require` 不允许命中 `ask_operator`。checkpoint 经 `core.SessionStore` 落 SQLite `agent_checkpoints`，run 成功或失败结束时删除。
  - `internal/runtime/approval_test.go`
  - `internal/store/store_checkpoint_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire/container.go` 直接 import `internal/store`；其他包只依赖 consumer-owned ports（`core.SessionStore`/`core.IdentityStore`/`core.ArtifactStore`/`core.PresenceStore`/`core.NotificationStore`/`core.KnowledgeStore`）或 `internal/store` shared records/errors。
  - `tests/architecture/dependency_direction_test.go`
- **时间由调用方给出**：`memory_items` 的 created_at/updated_at 与 `notifications` 的 created_at 由写入方的时钟决定，store 拒绝缺少时间的写入；时钟只在组合根注入，工具、presence、wake 调度器与推送 sender 共用一个。
  - `internal/store/store_presence_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`
- **Consumer-owned store 接口收敛**：`internal/runtime` + `internal/wire` 顶层定义的 consumer-owned store 接口（Store/Port/Repository/Ledger）≤4（RuntimeStore）。
  - `tests/architecture/store_interface_count_test.go`

## 上下文与记忆

- **上下文管理由 Eino middleware 承担**：summarization 在 token 超过 `window_tokens - compact_margin_tokens` 时总结历史；reduction 只做 clear（保留最近 `mask_after_turns` 轮工具调用原样）。public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`。
  - `internal/runtime/agent_test.go`
- **Instruction = 人格 + 内置规则 + 技能说明**：人格来自 `{storage_dir}/persona.md`，缺失或为空时 run 失败并给出路径；内置 operating rules 说明工作记忆、约定、知识库、审批和工具发现的用法；Eino skill middleware 追加技能说明并提供 `skill` 工具，工具描述列出本 run 可用（eligible）的技能，按名加载正文。这些都在 Instruction 或工具描述里，不参与总结。context 快照记录的是 skill middleware 追加之后的 Instruction。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/skill_backend_test.go`
  - `internal/presence/presence_test.go`
- **“当下”只存在于单次模型调用**：presence middleware 用 `WrapModel` 在每次模型调用的输入末尾追加 `<presence>` system 消息（owner 时区的时间、唤醒原因、约定、念头、owner 原话、倾向、关切、暂歇条目），不写回 agent 状态，因此不会进入历史或被总结。渲染前先执行衰减；超过 `presence.max_tokens` 时先丢暂歇条目，再丢各类最旧条目，woken 约定永不丢弃。每个不同的渲染结果按哈希存入 `context_snapshots`，并记一条 `presence.snapshot` 事件。
  - `internal/runtime/presence_test.go`
  - `internal/presence/presence_test.go`
- **工作记忆只有一条衰减路径**：`memory_items` 中的 said/thought/tendency/ruler 到期未续期时，由 `presence.Decay` 从 active 变为 resting，再变为 sunk；commitment 不衰减。续期、内化、放下、完成约定都只经 `settle` 工具。
  - `internal/presence/presence_test.go`
  - `internal/tools/presence_tools_test.go`

## 知识库与 Capture

- **知识库文件是真相，索引可重建**：笔记是 `knowledge.dir`（默认 `{storage_dir}/knowledge`）下的 markdown 文件，目录同时是 git 仓库。`knowledge_notes` 与 `knowledge_notes_fts` 只是索引：每次列表或搜索前按 mtime 和 size 与文件同步，在 Acorn 之外新建、修改、删除的笔记都会反映出来；读不了或解析失败的笔记让同步失败并给出路径。
  - `internal/knowledge/knowledge_test.go`
  - `internal/store/store_knowledge_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`
- **每次写入是一个只含本次文件的 commit**：笔记只经 `knowledge.Vault` 写入，每次 `knowledge_write`/`knowledge_edit` 与分享图片各提交一次，只提交本次涉及的文件，作者固定为 Acorn，agent 的写入在提交说明里带 `Acorn-Run`。工作区里 owner 未提交的其他改动不受影响。笔记路径必须是知识库内的相对 `.md` 路径，不能含 `..`、隐藏段或位于 `attachments/`。
  - `internal/knowledge/knowledge_test.go`
  - `internal/tools/knowledge_tools_test.go`
- **Capture 是 owner 发起的 run**：`POST /v1/captures` 先把图片（JPEG/PNG/WebP/GIF，≤10 MiB，类型按内容判断）存入 `attachments/` 并提交，再为这次分享新建线程，以 role `capture` 的输入立即起 run；模型把它当作 user 消息读取，客户端单独显示。capture 不记 `wake.fired`，不计入 `wake.daily_limit`。
  - `internal/api/capture_knowledge_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`

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

- **约定只由 wake 调度器触发，且只触发一次**：`wake.Scheduler` 住在 `serve` 进程内，每 30 秒先执行衰减，再处理到期的约定（`memory_items` 中 kind 为 commitment）。`ClaimDueCommitment` 用条件更新把 active 改为 woken，多个调度器并发时只有一个成功；随后经 `RunService.CreateWakeRun` 在约定所属线程里起 run（线程已删除时新建 Reminders 线程），并在该 run 上记录 `wake.fired`。醒来的输入以 role `wake` 记入线程：模型把它当作 user 消息读取，客户端把它和 owner 自己写的消息分开显示。起 run 失败时约定回到 active，唤醒时间推后 5 分钟。带 cron 的约定在触发后插入下一次的 active 条目。
  - `internal/wake/scheduler_test.go`
  - `internal/store/store_presence_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`
- **每日唤醒上限按 owner 时区计算**：`wake.daily_limit` 限制 owner 本地每天的唤醒次数，计数来自 events 表中当天的 `wake.fired`，重启后依然有效；超限的约定留在 active，次日再处理；为 0 时关闭自主唤醒。
  - `internal/wake/scheduler_test.go`
- **推送有上限、守免打扰、随设备失效**：`notify_owner` 经 `notify.Sender` 发送；每小时超过 `notify.max_per_hour` 时返回错误；免打扰时段内的通知排队到时段结束，由 wake 调度器每次 tick 发出；没有任何已登记设备时返回错误。一个 FCM token 只归最近登记它的设备（同一部手机重新配对不会收到重复推送）；FCM 回 404 或 UNREGISTERED 的 token 被删除，设备吊销时其 push token 一并删除。至少一台设备收到即记 sent，否则记 failed 并返回错误。未配置服务账号时工具以 disabled 注册并给出原因。
  - `internal/notify/notify_test.go`
  - `internal/store/store_presence_test.go`
  - `internal/api/push_token_handler_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`
- **Decision Card 扩展 ask_operator payload**：`OperatorQuestionPayload` 增 `considered_options/rationale/risk/recommendation` 可选维度。它给 `ask_operator` 的提问补上决策依据；工具调用审批由 `approval.require` 规则和 approval middleware 负责。
  - `internal/core/decision_card_test.go`
- **经历检索覆盖 run 和工作记忆**：`recall` 工具调用 `SearchExperience`，用 FTS5 trigram 检索 `runs` 的输入输出和全部 `memory_items`；少于 3 个字的查询改走 LIKE。FTS 表上线前的 run 在打开数据库时回填一次。
  - `internal/store/store_presence_test.go`
  - `internal/tools/presence_tools_test.go`

## 代码规范

- **Error 分两类**：Exported sentinel error（需要被 `errors.Is` 比对）必须是包级 `var ErrXxx`；precondition/internal-config error（不该发生的编程错误）用 inline `errors.New("...")` 直接返回。`.golangci.yml` 的 `errname` linter 强制导出 sentinel 命名。
  - `.golangci.yml`（errname linter）
