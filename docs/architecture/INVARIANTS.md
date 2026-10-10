# Acorn 架构不变量

每条不变量标注执行它的测试文件。新增不变量须同步加测试。

## 核心层 (core)

- **core 有零内部导入**：`internal/core` 不导入任何 `github.com/ycvk/acorn/internal/*` 包；core 是 Layer 0，只依赖外部 SDK（Eino schema/adk）。
  - `tests/architecture/dependency_direction_test.go`
- **持久化接口归 core**：各业务包依赖的 store 接口由 core 定义，`*store.Store` 在编译期断言实现它们。
  - `internal/core/store.go`
  - `internal/core/presence.go`
  - `internal/core/knowledge.go`
  - `internal/core/watch.go`
  - `internal/core/core_test.go`

## 运行时与编排

- **每个 run 一个 Eino ChatModelAgent**：`buildAgentRunner` 组装 `adk.TypedRunner[*schema.AgenticMessage]{ChatModelAgent, EnableStreaming, CheckPointStore}`；handlers 依次为 memory visibility → patchtoolcalls → summarization → reduction(clear-only) → toolsearch(有 deferred 工具时) → skill → presence → approval → tool errors；工具串行执行（`ExecuteSequentially`），普通工具失败与未知工具调用作为模型可见的 tool result 返回。Executor 只负责把 `AgentEvent` 投影成 RunEvent。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/events_test.go`
- **模型使用原生协议并保留推理状态**：Responses、Chat Completions、Anthropic Messages 都经 Eino AgenticModel；推理内容块与签名参与下一轮工具结果回传和 checkpoint，公开事件投影不含签名。sampling 可省略，输出预算和上下文按模型能力验证。请求空闲超时随响应进度刷新，总请求与 run 时限可关闭。
  - `internal/runtime/model_protocol_test.go`
  - `internal/runtime/model_http_test.go`
  - `internal/config/config_models_test.go`
- **工具失败可见，主模型调用有界重试**：tool error middleware 把错误转成结果文本时按 call id 记录，projector 对这些调用发 `tool.call.failed`（其余发 `tool.call.succeeded`），两者都不进 live 契约。主模型调用失败最多重试 3 次，context 取消不重试；被重试的失败流不记 run 失败，只有成功那次的输出成为 assistant 消息；3 次重试都失败时 run 失败。
  - `internal/runtime/tool_errors_test.go`
  - `internal/runtime/model_retry_test.go`
- **回复先落库再报完成**：run 成功或失败结束时，Executor 先写 run output 和对应的 assistant 消息，再发 `run.completed`/`run.failed` 并把 run 标记为结束；写入失败让 run 返回错误。客户端在收到完成事件或看到结束状态后重新加载线程，一定能读到回复。
  - `internal/wire/wake_acceptance_e2e_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`
- **审批绑定具体调用并可跨重启恢复**：`approval.require` 命中的工具调用由 approval middleware 登记 `tool_approval` pending action 并发起工具级中断；resume 时校验参数与登记时一致，accept 才执行，decline 把拒绝说明作为工具结果返回。同一轮里排在被拦截调用之后的工具调用会先执行完，被拦截的调用等决策后再执行。`approval.require` 不允许命中 `ask_operator`。checkpoint 经 `core.SessionStore` 落 SQLite `agent_checkpoints`，run 成功或失败结束时删除。
  - `internal/runtime/approval_test.go`
  - `internal/store/store_checkpoint_test.go`

- **Decision Card 扩展 ask_operator payload**：`OperatorQuestionPayload` 增 `considered_options/rationale/risk/recommendation` 可选维度。它给 `ask_operator` 的提问补上决策依据；工具调用审批由 `approval.require` 规则和 approval middleware 负责。
  - `internal/core/decision_card_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire` 直接 import `internal/store`；其他包只依赖 core 定义的接口或 `internal/store` 导出的记录与错误。
  - `tests/architecture/dependency_direction_test.go`
- **写入单连接，记忆读取并行**：所有写入与事务经一个 SQLite 连接串行执行；记忆召回与整合候选的读取走 `query_only` 的 WAL 只读连接池，写事务进行中也能读到已提交的数据，只读池拒绝写入。
  - `internal/store/store_read_pool_test.go`
- **时间由调用方给出**：记忆与约定的 created_at/recorded_at/updated_at 与 `notifications` 的 created_at 由写入方的时钟决定，store 拒绝缺少时间的写入；时钟只在组合根注入，工具、presence、wake 调度器与推送 sender 共用一个。
  - `internal/store/store_presence_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`

## 上下文与记忆

- **上下文管理由 Eino middleware 承担**：summarization 在 token 超过 `window_tokens - 输出预留 - presence.max_tokens - compact_margin_tokens` 时总结历史；reduction 只做 clear（保留最近 `mask_after_turns` 轮工具调用原样）。public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`。
  - `internal/runtime/agent_test.go`
- **Instruction = 人格 + 内置规则 + 技能说明**：人格来自 `{storage_dir}/persona.md`，缺失或为空时 run 失败并给出路径；内置 operating rules 说明工作记忆、约定、知识库、审批和工具发现的用法；Eino skill middleware 追加技能说明并提供 `skill` 工具，工具描述列出本 run 可用（eligible）的技能，按名加载正文。这些都在 Instruction 或工具描述里，不参与总结。context 快照记录的是 skill middleware 追加之后的 Instruction。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/skill_backend_test.go`
  - `internal/presence/presence_test.go`
- **临时上下文有独立预算和来源**：每次模型调用追加 `<memory_context>` 与 `<presence>`；保存上下文快照及 source/record 引用。近期原文与线程摘要按 token 选择,最终模型输入检查容量。念头、关切和约定按各自生命周期显示。
  - `internal/runtime/memory_visibility_test.go`
  - `internal/runtime/presence_test.go`
  - `internal/memory/context_test.go`
- **事实修订保留历史，来源排除约束所有读取**：精确证据引用、revision CAS、known_at 与 as_of 分离；更正使派生认识待复核。遗忘事务处理来源片段、派生记录、向量和摘要；旧 worker、checkpoint 与模型输入受 epoch 约束。确认前等待受影响的在途 run 退出。
  - `internal/store/memory_contract_test.go`
  - `internal/store/memory_state_test.go`
  - `internal/runtime/memory_visibility_test.go`
- **后台处理可恢复，外部调用可计量**：来源与任务原子登记、租约 token 和游标防止重复/过期提交；每个模型、embedding 与压缩调用按预算记录用量。索引模型与维度匹配方可检索，离线维护使用独占数据目录锁。
  - `internal/memory/process_test.go`
  - `internal/store/memory_index_test.go`
  - `internal/store/memory_vector_quality_test.go`
  - `internal/store/memory_vector_history_test.go`
  - `internal/store/memory_aliases_test.go`
  - `internal/memory/read_test.go`
- **记忆检索融合四路候选**：关键词、向量、关系与时间采用 RRF 排名，deep 调用主模型核对相关性；结果保留事实、认识、念头的类型和来源。当前状态、历史已知时间、有效时间与线程范围分别过滤；语义候选在排名前选定对应 revision，明确别名按来源和范围展开；未整理来源及数量显式返回。
  - `internal/store/memory_state_test.go`
  - `internal/memory/process_test.go`
  - `internal/memory/live_evaluation_test.go`
  - `internal/memory/consolidation_revision_test.go`
  - `internal/memory/semantic_history_test.go`
  - `internal/memory/research_evaluation_test.go`
- **知识记忆绑定不可变版本**：每个笔记 revision 在写入事务里登记为记忆来源，展开读取对应 revision 的正文；agent 写入保留来源关系，后续派生输出继承已提交的遗忘。agent 知识工具过滤排除，owner 的原文读取保留。
  - `internal/knowledge/memory_sources_test.go`

## 知识库与 Capture

- **笔记的每次写入是一个 revision**：笔记只经 `knowledge.Vault` 写入 SQLite；`knowledge_write`/`knowledge_edit` 各产生一个新 revision，保留创建时间、正文哈希与写入它的 run。编辑基于读到的 revision，期间被改过则以 `ErrKnowledgeConflict` 失败。搜索与列表读当前 revision。笔记路径必须是相对 `.md` 路径，不能含 `..`、隐藏段或位于 `attachments/`。
  - `internal/knowledge/knowledge_test.go`
  - `internal/store/store_knowledge_test.go`
  - `internal/tools/knowledge_tools_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`
- **Capture 是 owner 发起的 run**：`POST /v1/captures` 先把图片（JPEG/PNG/WebP/GIF，≤10 MiB，类型按内容判断）存入 `{storage_dir}/attachments/`，再为这次分享新建线程，以 role `capture` 的输入立即起 run；模型把它当作 user 消息读取，客户端单独显示。capture 不记 `wake.fired`，不计入 `wake.daily_limit`。历史里的 capture 消息把 `Image:` 行指向的附件作为图片输入随消息交给模型，附件读取失败则 run 失败；token 预算按每张 `core.ImageInputTokens` 计。`GET /v1/knowledge/attachment` 只返回符合 `attachments/YYYY/MM/<16 位 hex>.<jpg|png|webp|gif>` 的文件，其他路径按无效请求拒绝；笔记以 `![描述](attachments/...)` 引用图片。
  - `internal/knowledge/knowledge_test.go`
  - `internal/api/capture_knowledge_test.go`
  - `internal/runtime/tokens_test.go`
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
- **此刻页只读后端事实，改动与 agent 同路**：`GET /v1/now` 返回最新 `briefings/` 笔记、scheduled/due 约定及未结 occurrence、active/waiting 关切（已遗忘来源不出现）和全部追踪项（failing 在前），任一部分读取失败整个请求失败。`:cancel` 走与 `settle` 相同的 `SettleCommitment` 事务，已结束的约定返回 409；`:pause`/`:resume` 与 `watch_update` 共用 `core.Watch.Pause`/`Resume`。
  - `internal/api/now_test.go`
  - `internal/wire/now_acceptance_e2e_test.go`
- **Mobile 是 control surface 不是 runtime**：mobile 不执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle；context pressure/boundary/run status 都消费后端 projection。
  - `mobile-kotlin/app/src/test/...`（JUnit）

## 约定、唤醒与追踪

- **约定发生只认领一次，执行前绑定来源**：约定规则与 occurrence 独立。条件认领后，Executor 在模型调用前绑定执行 run；周期沿同一规则排下一次。五分钟未绑定的认领可恢复，迟到启动受状态校验拦截。完成约定必须引用 owner 确认或成功执行结果。
  - `internal/store/commitment_contract_test.go`
  - `internal/wake/scheduler_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`
- **每日唤醒上限按 owner 时区计算**：`wake.daily_limit` 限制 owner 本地每天的唤醒次数，计数来自 events 表中当天的 `wake.fired`，重启后依然有效；超限的约定保持 scheduled，次日再处理；为 0 时关闭自主唤醒。
  - `internal/wake/scheduler_test.go`
- **推送有上限、守免打扰、随设备失效**：`notify_owner` 经 `notify.Sender` 发送；每小时超过 `notify.max_per_hour` 时返回错误；免打扰时段内的通知排队到时段结束，由 wake 调度器每次 tick 发出；没有任何已登记设备时返回错误。一个 FCM token 只归最近登记它的设备（同一部手机重新配对不会收到重复推送）；FCM 回 404 或 UNREGISTERED 的 token 被删除，设备吊销时其 push token 一并删除。至少一台设备收到即记 sent，否则记 failed 并返回错误。未配置服务账号时工具以 disabled 注册并给出原因。
  - `internal/notify/notify_test.go`
  - `internal/store/store_presence_test.go`
  - `internal/api/push_token_handler_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`
- **追踪项只报新东西**：`watch_create` 的首次抓取失败则不建立；成功的首次检查只建基线（条目记 baseline），之后的检查才产生新条目。条目按 (watch, key) 唯一，同一条目只入库一次；网页类追踪项比较选中内容的快照，变化产生一条带前后值的条目，回到旧值同样算变化。
  - `internal/watch/watch_test.go`
  - `internal/store/store_watch_test.go`
  - `internal/tools/watch_tools_test.go`
- **抓取只经 URL policy**：feed、GitHub 与网页都经 `webaccess.FetchRaw`（与 `web_fetch` 共用 policy、超时与大小上限），渲染页面经浏览器服务的 policy；loopback 一律拒绝。
  - `internal/watch/watch_test.go`
- **追踪项检查与唤醒只在 wake 调度器**：到期追踪项以条件更新加租约认领，多个调度器只有一个检查。检查结果只写检查字段（下次与上次检查、错误、失败次数、快照、active 与 failing 之间的切换），与新条目在同一事务里，且仅当追踪项的状态、失败次数与上次检查仍是检查开始时读到的值；检查期间 owner 的暂停、恢复或重设基线优先，这次检查的结果与条目都不记录。只有 immediate 追踪项的新条目起 wake run（在建立追踪项的线程里），计入 `wake.daily_limit` 并记 `wake.fired{watch_id}`；超限、起 run 失败与 digest 追踪项的条目都留给早安卡。一个追踪项失败不影响约定和其他追踪项；连续 5 次失败标为 failing，退避上限 24 小时。
  - `internal/wake/watches_test.go`
  - `internal/watch/watch_test.go`
  - `internal/store/store_watch_test.go`
  - `internal/wire/watch_acceptance_e2e_test.go`
- **每个本地日一次早安卡**：过了 `briefing.at`（owner 时区）后，`routine_runs` 表按 (`briefing`,日期) 认领，跨进程只触发一次；起 run 失败释放认领，下个 tick 重试。早安卡在 Briefings 线程里运行，输入列出全部待简报条目和 failing 追踪项，条目随之标为 briefed；它不计入每日唤醒上限。
  - `internal/wake/watches_test.go`
  - `internal/store/store_watch_test.go`
  - `internal/wire/watch_acceptance_e2e_test.go`

## 空闲思考、用量与手机通知

- **例行唤醒一次认领**：`routine_runs` 的 (routine, slot) 唯一;准备/启动失败释放,预算跳过与空夜思保留,启动成功后保持认领。夜思与游思复用 Thoughts 线程。
  - `internal/wake/thinking_test.go`
- **原始通知只有一个写入入口**：鉴权设备身份与服务器 received_at 由 API 决定;批次全量校验后事务写入,按 (device,key,posted_at) 去重,不修改调用方输入。
  - `internal/api/phone_notification_service_test.go`
  - `internal/store/store_phone_notifications_test.go`
- **通知窗口连续且有界**：“当下”为最近 6 小时最多 10 条,预算裁减时通知最先丢;早安卡窗口为上次成功认领至本次认领的半开区间,最多 50 条并准确标记剩余数;原始信号七天后清理。
  - `internal/presence/phone_notifications_test.go`
  - `internal/wake/thinking_test.go`
  - `internal/store/store_routine_test.go`
- **自主预算按调用时间计**：当日本地零点起的 `model.usage` 累计,关联 run 的 `wake.fired` 判定自主用量;早安卡与 owner 运行不计。主模型成功调用与记忆处理/召回/摘要/压缩各次尝试分别计量,缺失 usage 可观察,不改变 live 契约。
  - `internal/runtime/usage_test.go`
  - `internal/store/store_phone_notifications_test.go`
  - `internal/wire/thinking_acceptance_e2e_test.go`
- **跨模块闭环**：夜思能通过技能整理工作记忆,手机通知能经过 HTTP 进入 presence 与简报,简报笔记带 run 身份写入知识库并推送。
  - `internal/wire/thinking_acceptance_e2e_test.go`
- **手机队列归属清晰**：队列绑定服务器与设备,身份变更清空;取消白名单移除待传项;请求使用批次自己的凭证;失败保留,成功只删除发送的 ID;持续入队保留第一条的上传截止时间。
  - `mobile-kotlin/app/src/test/java/io/ycvk/acorn/core/notifications/NotificationQueueTest.kt`
