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
- **工具失败可见，主模型调用有界重试**：tool error middleware 把错误转成结果文本时按 call id 记录，projector 对这些调用发 `tool.call.failed`（其余发 `tool.call.succeeded`）。主模型调用失败最多重试 3 次，context 取消不重试；被重试的失败流不记 run 失败，只有成功那次的输出成为 assistant 消息；3 次重试都失败时 run 失败。
  - `internal/runtime/tool_errors_test.go`
  - `internal/runtime/model_retry_test.go`
- **工具调用对客户端只露出名字和摘要**：assistant 消息里的每个工具调用在消息之后发 `tool.call.started`。`tool.call.started`/`succeeded`/`failed` 进入 live 契约，客户端只拿到工具名、取自参数的一行摘要（至多 80 字）和截断到 300 字的错误，工具输出留在服务端。消息列表把 run 的这些事件按开始顺序折叠成 assistant 消息的 `steps`。
  - `internal/runtime/events_test.go`
  - `internal/api/tool_steps_test.go`
  - `internal/store/store_thread_steps_test.go`
- **回复先落库再报完成**：run 成功或失败结束时，Executor 先写 run output 和对应的 assistant 消息，再发 `run.completed`/`run.failed` 并把 run 标记为结束；写入失败让 run 返回错误。客户端在收到完成事件或看到结束状态后重新加载线程，一定能读到回复。
  - `internal/wire/wake_acceptance_e2e_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`
- **规则先于审查，审查失败转审批**：`approval.block` 命中的调用不执行并把拒绝说明作为工具结果返回；`approval.require` 命中的调用审批；`approval.allow` 命中的直接执行；规则未覆盖的 `shell` 与 MCP 调用在审查开启时送审，原生工具不送审。只读的 shell 命令不调用审查，`find` 不带执行、删除或写文件的表达式时算只读；`bsk` 页面操作带着本 run 观察到的元素名与页面送审，关于页面文字的判断只用于页面操作；审查出错按需要审批处理。每次审查记一条 `tool_review` 诊断事件，不进入客户端契约。
  - `internal/runtime/approval_review_test.go`
  - `internal/review/review_test.go`
- **审批绑定具体调用并可跨重启恢复**：规则或审查要求审批的工具调用由 approval middleware 登记 `tool_approval` pending action 并发起工具级中断；resume 时校验参数与登记时一致，accept 才执行，decline 把拒绝说明作为工具结果返回。同一轮里排在被拦截调用之后的工具调用会先执行完，被拦截的调用等决策后再执行。`approval.require` 与 `approval.block` 不允许命中 `ask_operator`。checkpoint 经 `core.SessionStore` 落 SQLite `agent_checkpoints`，run 成功或失败结束时删除。
  - `internal/runtime/approval_test.go`
  - `internal/store/store_checkpoint_test.go`

- **Decision Card 扩展 ask_operator payload**：`OperatorQuestionPayload` 增 `considered_options/rationale/risk/recommendation` 可选维度。它给 `ask_operator` 的提问补上决策依据；工具调用审批由 `approval.require` 规则和 approval middleware 负责。
  - `internal/core/decision_card_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire` 直接 import `internal/store`；其他包只依赖 core 定义的接口或 `internal/store` 导出的记录与错误。
  - `tests/architecture/dependency_direction_test.go`
- **数据库结构有版本**：`PRAGMA user_version` 记录结构版本。空库在一个事务里按当前结构建好并写入版本；旧版本库在独占存储锁下按顺序迁移，每一步与版本号在同一事务提交，外键在迁移期间关闭、每一步结束时不得有失效外键；没有版本号或版本更新的库拒绝打开。每个版本的结构记录在 `internal/store/testdata/schema/vN.sql`，从任一记录版本升级的结果必须与新建库的结构完全一致。
  - `internal/store/schema_version_test.go`
- **备份不打断运行，恢复要求停机**：`acorn backup` 写入 `backup.dir`（为空表示关闭备份）、保留最新 `backup.keep` 份快照，经只读连接 `VACUUM INTO` 生成数据库快照，校验完整性与结构版本后连同人格文件以原子重命名发布；附件与产出文件在快照之后镜像，只增不删，快照引用的文件都在镜像里。`acorn restore` 持有独占存储锁，Acorn 运行时拒绝执行，先校验快照再替换数据库并清除 WAL。
  - `internal/wire/backup_test.go`
- **写入单连接，记忆读取并行**：所有写入与事务经一个 SQLite 连接串行执行；记忆召回与整合候选的读取走 `query_only` 的 WAL 只读连接池，写事务进行中也能读到已提交的数据，只读池拒绝写入。
  - `internal/store/store_read_pool_test.go`
- **时间由调用方给出**：记忆与约定的 created_at/recorded_at/updated_at 与 `notifications` 的 created_at 由写入方的时钟决定，store 拒绝缺少时间的写入；时钟只在组合根注入，工具、presence、wake 调度器与推送 sender 共用一个。
  - `internal/store/store_presence_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`

## 上下文与记忆

- **上下文管理由 Eino middleware 承担**：summarization 在 token 超过 `window_tokens - 输出预留 - presence.max_tokens - compact_margin_tokens` 时总结历史；reduction 只做 clear（保留最近 `mask_after_turns` 轮工具调用原样）。public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`。
  - `internal/runtime/agent_test.go`
- **人格只由 owner 修改**：人格存在 SQLite 的 `persona_revisions`，最新 revision 是当前人格；每次保存是基于读到的 revision 的新 revision，并发修改显式失败；内容去掉首尾空白后不能为空，至多 8 KiB。启动时没有人格才以 release 身份写入默认人格，已有人格不被覆盖。owner 经 `/v1/persona` 与 `acorn persona` 保存，agent 没有写人格的工具。每个 run 开始时读当前人格。
  - `internal/store/store_persona_test.go`
  - `internal/presence/presence_test.go`
  - `internal/api/handlers_persona_test.go`
  - `internal/wire/persona_e2e_test.go`
- **Instruction = 人格 + 内置规则 + 技能说明**：人格是 run 开始时的当前人格；内置 operating rules 说明工作记忆、约定、知识库、审批和工具发现的用法；Eino skill middleware 追加技能说明并提供 `skill` 工具，工具描述列出本 run 可用（eligible）的技能，按名加载正文。这些都在 Instruction 或工具描述里，不参与总结。context 快照记录的是 skill middleware 追加之后的 Instruction。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/skill_backend_test.go`
  - `internal/wire/persona_e2e_test.go`
- **临时上下文有独立预算和来源**：每次模型调用追加 `<memory_context>` 与 `<presence>`；保存上下文快照及 source/record 引用。近期原文与线程摘要按 token 选择,最终模型输入检查容量。念头、关切和约定按各自生命周期显示。
  - `internal/runtime/memory_visibility_test.go`
  - `internal/runtime/presence_test.go`
  - `internal/memory/context_test.go`
- **事实修订保留历史，来源排除约束所有读取**：精确证据引用、revision CAS、known_at 与 as_of 分离；更正使派生认识待复核。遗忘事务处理来源片段、派生记录、向量和摘要；旧 worker、checkpoint 与模型输入受 epoch 约束。确认前等待受影响的在途 run 退出。
  - `internal/store/memory_contract_test.go`
  - `internal/store/memory_state_test.go`
  - `internal/runtime/memory_visibility_test.go`
- **记忆只读脱敏后的来源**：store 解析来源时有两个视图。记忆视图把 `internal/redact` 识别的凭据替换为 `[REDACTED:<类型>]`，抽取输入、证据校验、召回里的未整理来源、`memory_read` 与 App 的记忆详情都用它；线程视图只应用排除，供线程历史、线程摘要输入与当前输入使用。遗忘的引用在脱敏正文里定位后映射回原文，两个视图排除同一段话，排除范围碰到的凭据整段排除。记录的正文、条件或实体带凭据时以 `ErrMemoryCredential` 拒绝提交。
  - `internal/redact/redact_test.go`
  - `internal/store/memory_redaction_test.go`
  - `internal/memory/redaction_test.go`
- **App 里的记忆改动与 agent 同路**：`/v1/memories` 只列出未排除的 current/disputed/open 记录，详情略去已排除来源的证据。owner 在 App 里的修改与遗忘各登记一个 `request` 来源（speaker 为 owner、从不抽取）：修改以它为新事实的证据并 supersede 原事实，只接受 fact 且校验读到的 revision；遗忘以它为请求来源走同一个排除事务，并在回应前经同一个屏障等待受影响的 run 退出。
  - `internal/wire/memory_page_e2e_test.go`
  - `internal/store/memory_request_source_test.go`
- **删除对话即遗忘**：`DELETE /v1/threads/{id}` 以 owner 的删除请求为来源，先经遗忘事务排除该对话的全部 message 与 event 来源及其派生记录、向量和摘要，经同一屏障等待看过它们的 run 退出，再删除对话、消息、run（连同 `runs_fts`）、事件、待处理请求、checkpoint、通知、产物文件和只被这些 run 引用的上下文快照；记忆来源行保留以维持排除。整体排除的来源读取时直接返回 `ErrMemoryExcluded`，不再解析已删除的原文。最新 run 仍在执行时以 `thread_busy`（409）拒绝。v5 迁移以同样方式补完此前只删了消息的对话。
  - `internal/store/store_thread_delete_test.go`
  - `internal/wire/thread_delete_e2e_test.go`
  - `internal/api/client_thread_run_handlers_test.go`
- **自动抽取只读 owner 的话，insight 需要独立的 owner 证据**：只有 speaker 为 owner 的来源登记 extract 任务；工具结果、watch 条目与笔记 revision 登记为来源供证据引用，不自动抽取，v8 迁移结束此前排队的非 owner 抽取。整合新建或修订 insight 时，其父记录经 `memory_revision_sources` 解析到的根来源中必须有至少两个不同的 owner 来源，否则整批拒绝。
  - `internal/store/memory_contract_test.go`
  - `internal/memory/process_test.go`
  - `internal/memory/consolidation_evidence_test.go`
- **后台处理可恢复，外部调用可计量**：来源与任务原子登记、租约 token 和游标防止重复/过期提交；一次整合调用最多对齐 8 条一起变化的记录，批内每个任务的租约都在提交事务里校验并结束，调用失败时批内任务一起记失败；每个模型、embedding 与压缩调用按预算记录用量。索引模型与维度匹配方可检索，离线维护使用独占数据目录锁。
  - `internal/memory/process_test.go`
  - `internal/memory/consolidation_batch_test.go`
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

## 技能

- **技能的每次写入是一个 revision**：技能是 Agent Skills 格式的包，存在 SQLite 的 `skills`/`skill_revisions`/`skill_files`，每次写入记下 `SKILL.md`、哈希、附带文件、作者（agent、owner 或 release）与 run。写入必须基于当前 revision（新技能为 0），否则以 `ErrSkillConflict` 失败。frontmatter 严格解析，要求合法的 name、不超过 1024 字符的 description 和正文，`metadata.acorn-requires-tools` 经 `skill_write` 写入时只能列出本地工具、内置工具或 `mcp__` 工具；附带文件是包内相对路径，包有大小与文件数上限。`skill_write` 在 base revision 的文件上增删文件，写已停用的技能使它恢复；`skill_retire` 写一个停用 revision。run 只加载在用的技能，加载结果是全文加 revision，带文件的技能还给出文件所在目录。
  - `internal/store/store_skills_test.go`
  - `internal/skills/library_test.go`
  - `internal/tools/skill_tools_test.go`
  - `internal/runtime/skill_backend_test.go`
- **导入的就是检查过的包**：`skill_inspect` 读取工作机上的目录或 zip，只接受普通文件，按内容哈希暂存技能包；`skill_import` 只导入给定哈希的暂存包，包里的技能名必须与参数一致，导入后暂存移除，超过 7 天的暂存在下次暂存时清除。`acorn skills import` 以 owner 身份写入，同名技能只在 `--replace` 时替换。
  - `internal/skills/library_test.go`
  - `internal/store/store_skills_test.go`
  - `internal/tools/skill_tools_test.go`
  - `internal/workstation/skills_test.go`
- **写技能的审批显示全文**：starter config 与默认配置把 `skill_write`、`skill_import`、`skill_retire` 放进 `approval.require`；`skill_write` 的待批准项摘要是技能名与 description，详情的 `tool_detail` 是将写入的 `SKILL.md`、增改的文件内容与删除的文件；`skill_import` 的详情是暂存包的 `SKILL.md` 与文件内容。
  - `internal/config/config_validation_test.go`
  - `internal/api/tool_steps_test.go`
- **带文件的技能同步到工作机**：每次 run 前，backend 按包哈希把在用且带文件的技能写入工作机的 `~/skills/<name>/`，删除不再需要的技能目录，只动带 `.acorn-package` 标记的目录；工作机先写临时目录再整体替换，`scripts/` 下的文件可执行。没有工作机时这些技能因缺少 `files` 不列出；同步失败时它们本次不列出，run 照常进行。
  - `internal/skills/library_test.go`
  - `internal/workstation/skills_test.go`
  - `internal/runtime/skill_backend_test.go`
  - `internal/tools/web_skill_eligibility_test.go`
- **内置技能随二进制发布**：`internal/skills/seed` 编进二进制；启动时缺少的内置技能写入 revision 1，内容与上次 release 写入的哈希不同时在当前 revision 之上写新 revision 并保留停用状态，相同则不写。
  - `internal/skills/library_test.go`
- **夜思回顾技能**：夜思输入列出上次夜思以来 agent 写过或加载过的技能及其 revision；release 写入的 revision 不列出。
  - `internal/wake/thinking_test.go`

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
- **等待 owner 的操作会推送**：run 以 interrupted 结束后，它每个仍 pending 的操作经 `notify.Sender` 推送一次，同样受每小时上限与免打扰约束，推送失败只记录日志、不改变 run 结果。这类推送是只含 data 的 FCM 消息（`type=pending_action`，带 `action_id`、`thread_id`、`run_id`），通知记录保存 `action_id`；App 收到后读取操作当前状态，已决定或已不存在的操作不显示。
  - `internal/wire/approval_push_e2e_test.go`
  - `internal/notify/notify_test.go`
  - `internal/store/store_push_action_test.go`
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
- **每个本地日一次早安卡**：过了 `briefing.at`（owner 时区）后，`routine_runs` 表按 (`briefing`,日期) 认领，跨进程只触发一次；起 run 失败释放认领，下个 tick 重试。早安卡在 Briefings 线程里运行，输入列出全部待简报条目和 failing 追踪项，条目随之标为 briefed；它不计入每日唤醒上限。输入只在有问题时带 System health：`backup.dir` 已设置而最新快照缺失、超过 36 小时或目录不可读，记忆任务重试耗尽失败，或待处理记忆任务等待超过 24 小时。
  - `internal/wake/health_test.go`
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

## 工作机

- **工作机只经带 token 的 HTTP 调用**：serve 不接触容器引擎；`acorn workstation serve` 在容器里运行，每个请求校验 bearer token，token 少于 32 字符时拒绝启动。`shell` 与 `files` 是 deferred 工具，未配置 `workstation.url` 时以原因禁用。
  - `internal/workstation/workstation_test.go`
  - `internal/tools/workstation_tools_test.go`
  - `internal/config/config_workstation_test.go`
- **伴随进程常驻**：`acorn workstation serve --keep` 的每个伴随进程在独立进程组里运行，退出后按退避重启，serve 停止时连同其子进程一起结束；日志按名字加前缀并去掉终端颜色码。
  - `internal/workstation/supervisor_test.go`
- **页面渲染隔离**：`web_rendered` 追踪经工作机 `/v1/render` 渲染，每次用临时配置目录启动一次性浏览器，只接受 http(s)；backend 先按共享 URL policy 校验目标。
  - `internal/workstation/render_test.go`
- **浏览器画面只给已认证的 owner**：工作机的 DevTools 只在容器内开放；`/v1/browser/*` 经设备认证转发到工作机，未配置工作机时返回 `browser_unavailable`，未知标签页返回 `browser_tab_not_found`；导航只接受 http(s)。画面流可按 owner 的手机尺寸排版，宽高要么都给、要么都不给。
  - `internal/workstation/view_test.go`
  - `internal/api/handlers_browser_test.go`
- **命令有界**：超时或请求取消时杀掉命令的整个进程组；工具超时不能超过 `workstation.command_timeout_seconds`；stdout 与 stderr 超出上限时各保留开头和结尾并标出省略的字节数。
  - `internal/workstation/workstation_test.go`
  - `internal/tools/workstation_tools_test.go`
