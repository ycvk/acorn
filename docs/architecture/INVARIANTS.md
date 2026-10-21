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
- **run 不会一直停在 running**：Acorn 停止时，`Container.Close` 拒绝新的 run 与续跑，以 `core.ErrShuttingDown` 取消在跑的 run，至多等 10 秒让它们写下 `run.failed` 与 failed 状态，再关闭 store；run 在登记到 `RunController` 之后才建 run 或认领续跑，被拒绝的续跑留在 interrupted，下次启动照常续跑。`serve` 等 HTTP 请求结束后再停 worker、run 与 store。每个打开的 store 持有 `{storage_dir}/processes/` 下以其进程命名的加锁文件，`run_processes` 记下执行每个 running run 的进程；容器启动时，进程已停止（锁文件缺失或未被持有）的 running run 以 `run.failed` 记为 failed 并删除 checkpoint，它的线程重新可删，仍在运行的 Acorn 进程的 run 不受影响。
  - `internal/runtime/run_control_test.go`
  - `internal/store/store_run_process_test.go`
  - `internal/api/run_resume_ready_test.go`
  - `internal/wire/run_shutdown_e2e_test.go`

- **Decision Card 扩展 ask_operator payload**：`OperatorQuestionPayload` 增 `considered_options/rationale/risk/recommendation` 可选维度。它给 `ask_operator` 的提问补上决策依据；工具调用审批由 `approval.require` 规则和 approval middleware 负责。
  - `internal/core/decision_card_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire` 直接 import `internal/store`；其他包只依赖 core 定义的接口或 `internal/store` 导出的记录与错误。
  - `tests/architecture/dependency_direction_test.go`
- **数据库结构有版本**：`PRAGMA user_version` 记录结构版本。空库在一个事务里按当前结构建好并写入版本；旧版本库在独占存储锁下按顺序迁移，每一步与版本号在同一事务提交，外键在迁移期间关闭、每一步结束时不得有失效外键；没有版本号或版本更新的库拒绝打开。每个版本的结构记录在 `internal/store/testdata/schema/vN.sql`，从任一记录版本升级的结果必须与新建库的结构完全一致。
  - `internal/store/schema_version_test.go`
- **备份不打断运行，恢复要求停机**：`acorn backup` 写入 `backup.dir`（为空表示关闭备份）、保留最新 `backup.keep` 份快照，经只读连接 `VACUUM INTO` 生成数据库快照，校验完整性与结构版本后以原子重命名发布；附件与产出文件在快照之后镜像，只增不删，快照引用的文件都在镜像里。`acorn restore` 持有独占存储锁，Acorn 运行时拒绝执行，先校验快照再替换数据库并清除 WAL。
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
- **称呼只由 owner 设置**：Acorn 怎么称呼 owner 存在 SQLite 的 `owner` 表，去掉首尾空白后至多 40 个字、不含控制字符，空表示没有称呼。owner 经 `/v1/owner` 与 `acorn owner set` 设置，agent 没有写称呼的工具。每个 run 开始时与每次记忆抽取时读当前称呼：前者写进 Instruction，后者用它写关于 owner 的事实，没有称呼时写成 owner。配置不读 `owner.name`，写了按未知字段拒绝。
  - `internal/store/store_owner_test.go`
  - `internal/presence/owner_test.go`
  - `internal/api/handlers_owner_test.go`
  - `internal/wire/owner_e2e_test.go`
  - `internal/memory/extraction_dedup_test.go`
  - `internal/config/config_load_test.go`
- **Instruction = 人格 + 称呼 + 内置规则 + 技能说明**：人格是 run 开始时的当前人格，有称呼时其后一句说明 owner 是谁；内置 operating rules 说明工作记忆、约定、知识库、审批和工具发现的用法；Eino skill middleware 追加技能说明并提供 `skill` 工具，工具描述列出本 run 可用（eligible）的技能，按名加载正文。这些都在 Instruction 或工具描述里，不参与总结。context 快照记录的是 skill middleware 追加之后的 Instruction。
  - `internal/runtime/agent_test.go`
  - `internal/runtime/skill_backend_test.go`
  - `internal/wire/persona_e2e_test.go`
- **临时上下文有独立预算和来源**：每次模型调用追加 `<memory_context>` 与 `<presence>`；保存上下文快照及 source/record 引用。近期原文与线程摘要按 token 选择,线程摘要占历史预算的八分之一并限制在 1024 到 8192 token 之间（历史预算不足 8192 时取四分之一、不超过 1024），生成的摘要超过该预算即报错,最终模型输入检查容量。念头、关切和约定按各自生命周期显示。
  - `internal/runtime/memory_visibility_test.go`
  - `internal/runtime/presence_test.go`
  - `internal/memory/context_test.go`
- **事实修订保留历史，来源排除约束所有读取**：精确证据引用、revision CAS、known_at 与 as_of 分离；更正使派生认识待复核。遗忘按独立证据判断：当前 revision 引用被排除来源（整体或片段匹配）的记录，去掉这些证据后仍有未排除的支持证据时写一个新 revision（证据、只由被排除证据提及的实体与别名、根来源重算，reason 为 `evidence excluded by owner`，入队 embed），否则整条排除并沿 derives_from 级联；被剥离的存活记录不级联，其派生认识标 needs_review 并入队 consolidate。旧 revision 仍引用被排除来源，`memoryRevisionVisibility` 让历史读取、向量与别名看不到它；整合经记忆视图读来源，无法把剥离的证据加回来。遗忘事务同时处理来源片段、向量和摘要。来源的整体排除传给由它派生的全部来源；片段排除只传给在排除之前读过它的 run 派生的来源，之后写成的派生来源与线程摘要读到的是去掉片段的正文，照常可读。旧 worker、checkpoint 与模型输入受 epoch 约束。确认前等待在排除之前读过被排除内容的在途 run 退出。
  - `internal/store/memory_contract_test.go`
  - `internal/store/memory_state_test.go`
  - `internal/store/memory_forget_test.go`
  - `internal/store/memory_merge_test.go`
  - `internal/runtime/memory_visibility_test.go`
- **记忆只读脱敏后的来源**：store 解析来源时有两个视图。记忆视图把 `internal/redact` 识别的凭据替换为 `[REDACTED:<类型>]`，抽取输入、证据校验、召回里的未整理来源、`memory_read` 与 App 的记忆详情都用它；线程视图只应用排除，供线程历史、线程摘要输入与当前输入使用。遗忘的引用在脱敏正文里定位后映射回原文，两个视图排除同一段话，排除范围碰到的凭据整段排除。记录的正文、条件或实体带凭据时以 `ErrMemoryCredential` 拒绝提交。
  - `internal/redact/redact_test.go`
  - `internal/store/memory_redaction_test.go`
  - `internal/memory/redaction_test.go`
- **App 里的记忆改动与 agent 同路**：`/v1/memories` 只列出未排除的 current/disputed/open 记录，详情略去已排除来源的证据。owner 在 App 里的修改与遗忘各登记一个 `request` 来源（speaker 为 owner、从不抽取）：修改以它为新事实的证据并 supersede 原事实，只接受 fact 且校验读到的 revision；遗忘以它为请求来源走同一个排除事务，并在回应前经同一个屏障等待受影响的 run 退出。
  - `internal/wire/memory_page_e2e_test.go`
  - `internal/store/memory_request_source_test.go`
- **删除对话即遗忘**：`DELETE /v1/threads/{id}` 以 owner 的删除请求为来源，先经遗忘事务排除该对话的全部 message 与 event 来源及其派生记录、向量和摘要，经同一屏障等待看过它们的 run 退出，再删除对话、消息、对话文件、视图、run（连同 `runs_fts`）、事件、待处理请求、checkpoint、通知、产物文件和只被这些 run 引用的上下文快照；记忆来源行保留以维持排除。整体排除的来源读取时直接返回 `ErrMemoryExcluded`，不再解析已删除的原文。最新 run 仍在执行时以 `thread_busy`（409）拒绝。v5 迁移以同样方式补完此前只删了消息的对话。在该对话里建立的关切随之排除：不再列出，修订返回 `ErrMemoryExcluded`，约定不能再关联它；之后每次 run 前，工作机经 `/v1/work` 列出 `~/work/` 下以关切 id 命名的目录，属于已删除对话的关切的目录被删除，其他目录保留，删除失败时 run 继续，下次 run 前重试。
  - `internal/store/store_thread_delete_test.go`
  - `internal/wire/thread_delete_e2e_test.go`
  - `internal/api/client_thread_run_handlers_test.go`
  - `internal/store/memory_state_test.go`
  - `internal/runtime/concern_work_test.go`
  - `internal/workstation/files_test.go`
  - `internal/wire/concern_acceptance_e2e_test.go`
- **自动抽取只读 owner 的话，insight 需要独立的 owner 证据**：只有 speaker 为 owner 的来源登记 extract 任务；工具结果、watch 条目与笔记 revision 登记为来源供证据引用，不自动抽取，v8 迁移结束此前排队的非 owner 抽取。整合新建或修订 insight 时，其父记录经 `memory_revision_sources` 解析到的根来源中必须有至少两个不同的 owner 来源，否则整批拒绝。
  - `internal/store/memory_contract_test.go`
  - `internal/memory/process_test.go`
  - `internal/memory/consolidation_evidence_test.go`
- **重复事实合并，存活者由代码决定**：整合可以把两条表达同一主张且可同时成立的 current fact 合并：`consolidateRecords` 要求两条都在本次输入里、都是 current fact，恰有一条 pinned 时存活者是它，否则是 `recorded_at` 更早的那条，草稿必须带被合并记录的全部证据，否则整批拒绝；`CommitMemory` 校验被合并记录的 revision 与 current 状态，写入 `merged` 状态的新 revision（有效时间不变）、`merged_into` 链接与存活记录的新 revision，并为存活记录入队整合。`merged` 记录不进 current 模式的列表、搜索与召回，history 模式可见。抽取输入列出本来源已支持的 current fact，与其中某条正文完全相同的草稿被丢弃。
  - `internal/memory/consolidation_merge_test.go`
  - `internal/memory/extraction_dedup_test.go`
  - `internal/store/memory_merge_test.go`
- **后台处理可恢复，外部调用可计量**：来源与任务原子登记、租约 token 和游标防止重复/过期提交；一次整合调用最多对齐 8 条一起变化的记录，批内每个任务的租约都在提交事务里校验并结束，调用失败时批内任务一起记失败，预算耗尽时批内任务与 lead 一起推迟到次日本地零点且不计尝试次数，升级时此前因预算耗尽而永久失败的任务回到队列；每个模型、embedding 与压缩调用按预算记录用量。索引模型与维度匹配方可检索，离线维护使用独占数据目录锁。
  - `internal/memory/process_test.go`
  - `internal/memory/consolidation_batch_test.go`
  - `internal/store/memory_contract_test.go`
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

- **笔记的每次写入是一个 revision**：笔记只经 `knowledge.Vault` 写入 SQLite；`knowledge_write`/`knowledge_edit` 各产生一个新 revision，保留创建时间、正文哈希与写入它的 run。编辑基于读到的 revision，期间被改过或笔记已删除则以 `ErrKnowledgeConflict` 失败。同一路径在删除后重建，revision 接着该路径已登记来源的最大版本编号，来源 ID 不复用。搜索与列表读当前 revision。笔记路径必须是相对 `.md` 路径，不能含 `..`、隐藏段或位于 `attachments/`。
  - `internal/knowledge/knowledge_test.go`
  - `internal/store/store_knowledge_test.go`
  - `internal/tools/knowledge_tools_test.go`
  - `internal/wire/capture_acceptance_e2e_test.go`
- **删除笔记是 owner 的遗忘请求**：`DELETE /v1/knowledge/note` 先读出当前 revision，以 owner 请求排除该路径每个 revision 的记忆来源及其派生记录，经遗忘屏障等用过它们的 run 结束，再按读到的 revision 删除笔记、全部 revision 与搜索索引；期间笔记被改过则以 409 失败，再删一次会连新 revision 一起遗忘。记忆来源保留，排除持续生效；笔记引用的附件保留。agent 没有删除笔记的工具。
  - `internal/api/capture_knowledge_test.go`
  - `internal/store/store_knowledge_test.go`
  - `internal/wire/note_delete_e2e_test.go`
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
- **写技能的审批显示全文**：starter config 与默认配置把 `skill_write`、`skill_import`、`skill_retire` 放进 `approval.require`；`skill_write` 的待批准项摘要是技能名与 description，详情的 `tool_detail` 是将写入的 `SKILL.md`、增改的文件内容与删除的文件；`skill_import` 的详情是暂存包的 `SKILL.md` 与文件内容。包带 `acorn-package.yaml` 时详情先列出它在工作机上运行的东西：MCP 服务器的命令与参数、每个触发器的时间表、送达方式与命令、密钥名；写入新清单、沿用 base revision 的清单（标为 unchanged）或删除清单各有说明，解析不了的清单说明写入会失败。
  - `internal/config/config_validation_test.go`
  - `internal/api/tool_steps_test.go`
- **包清单只声明 MCP 服务器、触发器与密钥**：包根目录的 `acorn-package.yaml` 是一个 YAML 文档，以 KnownFields 严格解析，至少声明 `mcp` 或一个触发器：`mcp` 的 `command` 必填，可带 `args`；`triggers` 每项要求 name（技能名格式，包内不重复）、可解析的 cron `schedule`、`run` 与 `deliver`（immediate 或 digest），没有 webhook 字段；包级 `secrets` 是互不相同的大写环境变量名；`skill_write`、`skill_inspect`、`skill_import` 与 `acorn skills import` 写入前校验，解析失败的写入不落库。声明了 MCP 服务器的包名与 `mcp.providers` 的工具命名空间相同时写入、暂存与导入都失败；在用技能与后来配置的 provider 同名时容器启动失败，停用的技能不算。
  - `internal/skills/manifest_test.go`
  - `internal/wire/package_tools_e2e_test.go`
- **包的工具只在技能在用的 run 里**：每次 run 在包同步之后让工作机恰好运行在用且密钥齐全的技能的 MCP 服务器，其余停止；backend 为每个起来的服务器连接 `/v1/mcp/<name>`，工具名 `mcp__<name>__<tool>`、类别 integration，run 结束时连接关闭，照常经审批规则与审查。资格判断时技能对自己 `mcp__<name>__` 工具的要求留到连上之后检查；包声明的密钥缺任一个时，不论有没有 MCP 服务器，该技能本次都不列出；缺密钥（`missing_secrets`）、服务器起不来（`mcp_server_failed`）、连不上（`mcp_connect_failed`）、工作机不可用（`mcp_unavailable`）或缺自己要求的工具时，该技能与它的工具本次不列出，run 照常进行。`acorn doctor` 与 App 的技能页读同一个 `skills.Status`：每个服务器的状态、失败原因与缺的密钥，每个触发器的状态；读状态不启停服务器，问工作机至多等 10 秒，没回应时状态为 unknown。
  - `internal/skills/manifest_test.go`
  - `internal/runtime/skill_tools_test.go`
  - `internal/wire/package_tools_e2e_test.go`
  - `internal/cli/doctor_output_test.go`
  - `internal/api/handlers_skills_test.go`
- **包的密钥只交给包的进程**：密钥存在 SQLite 的 `skill_secrets`，经 `acorn skills secret set <skill> <NAME>` 从标准输入或 App 的 `PUT /v1/skills/{name}/secrets/{secret}` 写入，经 App 的 `DELETE` 清除；两条路径规则相同：名字必须是技能当前清单声明的密钥，值非空且不超过 64 KiB。密钥只写不读，API 只返回声明的密钥哪些已设置。值只随运行请求交给工作机，作为环境变量传给该包的 MCP 进程与触发器命令，不进入技能快照、模型输入、审批详情、API 响应与日志；密钥随数据库备份与恢复。
  - `internal/store/store_skills_test.go`
  - `internal/api/handlers_skills_test.go`
  - `internal/skills/manifest_test.go`
  - `internal/workstation/mcp_test.go`
  - `internal/wire/package_tools_e2e_test.go`
  - `internal/wire/backup_test.go`
  - `internal/cli/skills_test.go`
- **带文件的技能同步到工作机**：每次 run 前，backend 按包哈希把在用且带文件的技能写入工作机的 `~/skills/<name>/`，删除不再需要的技能目录，只动带 `.acorn-package` 标记的目录；工作机先写临时目录再整体替换，`scripts/` 下的文件可执行。没有工作机时这些技能因缺少 `files` 不列出；同步失败时它们本次不列出，run 照常进行。
  - `internal/skills/library_test.go`
  - `internal/workstation/skills_test.go`
  - `internal/runtime/skill_backend_test.go`
  - `internal/tools/web_skill_eligibility_test.go`
- **内置技能随二进制发布**：`internal/skills/seed` 编进二进制；启动时缺少的内置技能写入 revision 1，内容与上次 release 写入的哈希不同时在当前 revision 之上写新 revision 并保留停用状态，相同则不写。
  - `internal/skills/library_test.go`
- **owner 的改动基于读到的 revision**：App 的回滚把选中 revision 的 `SKILL.md` 与附带文件写成在用的新 revision，恢复把停用技能的当前内容写成在用的新 revision，停用写一个停用 revision；作者都是 owner，不经审批。三者都带 base revision，不等于当前 revision 时以 `ErrSkillConflict` 失败（409 `skill_changed`），什么也不写；回滚与恢复的内容经过与其他写入相同的检查（frontmatter、附带文件、清单、MCP 服务器与 provider 重名），状态不对（停用已停用的、恢复在用的）或检查不过以 `ErrSkillInvalid` 拒绝（400 `skill_change_invalid`）。下一次 run 用写入后的内容。
  - `internal/skills/owner_test.go`
  - `internal/api/handlers_skills_test.go`
  - `internal/wire/skills_owner_e2e_test.go`
- **夜思回顾技能**：夜思输入列出上次夜思以来 agent 写过或加载过的技能及其 revision；release 写入的 revision 不列出。
  - `internal/wake/thinking_test.go`

## Remote API 与 mobile

- **Remote client 必须设备认证**：除 `/healthz` 和 `POST /v1/devices:pair` 外，`/v1` 只接受 valid device bearer token；missing/malformed/unknown 返回 `unauthenticated`，revoked 返回 `device_revoked`。
  - `internal/store/store_schema_test.go`
- **OpenAPI 是 wire contract**：remote client DTO 只投影 core domain 类型；改 wire shape 须同步 `docs/openapi.yaml` + generated mobile client。投影逻辑在 `internal/api` 的 `projection.go`/`projection_helpers.go` 与 `dto_*.go` 中，不导入 `internal/runtime`。`thread_service.go`/`event_service.go` 合法导入 `internal/core`，不在 projection boundary 列表中。客户端生成器按 YAML 1.1 读文档，枚举值里会被读成布尔的词（如 `off`）必须加引号。
  - `tests/architecture/client_projection_boundary_test.go`
  - `internal/api/openapi_test.go`
- **Pending action 对客户端可达**：action ID 由 `core.NewActionID()` 生成，只含 `[a-z0-9_]`，能直接放进 `/v1/pending-actions/{action_id}:decide`。每种 pending action kind 都以 `<kind>.pending` / `<kind>.decided` 进入 live RunEvent，且 live 事件集合与 OpenAPI `RunEvent` discriminator 一致。
  - `internal/core/core_test.go`
  - `internal/api/openapi_test.go`
  - `internal/wire/approval_restart_e2e_test.go`
- **此刻页只读后端事实，改动与 agent 同路**：`GET /v1/now` 返回最近一次成功且有回复的早安卡 run（所在线程、日期、正文）、scheduled/due 约定及未结 occurrence、active/waiting 关切（带所在对话与 outcome，没有 outcome 的不输出该字段；来源已遗忘或所在对话已删除的不出现）和全部追踪项（failing 在前），任一部分读取失败整个请求失败。`:cancel` 走与 `settle` 相同的 `SettleCommitment` 事务，已结束的约定返回 409；`:pause`/`:resume` 与 `watch_update` 共用 `core.Watch.Pause`/`Resume`。
  - `internal/api/now_test.go`
  - `internal/wire/now_acceptance_e2e_test.go`
- **技能页只读后端事实**：`GET /v1/skills` 列出全部技能，已停用的也在内，带当前 revision、作者、更新时间、是否带清单、下一次 run 能否列出与原因（静态工具资格加缺的密钥）；当前 revision 的 `SKILL.md` 或清单解析不了的技能带 `problem`，列表与详情照常返回。详情给出当前 revision 的 `SKILL.md`、附带文件（不超过 64 KiB 的 UTF-8 文本带内容）与写入它的 run，有清单时给出清单内容、运行状态与密钥是否已设置；修订历史给出每个 revision 的作者、run、时间、是否停用与包哈希，任一 revision 可读。两个 revision 的差异由后端逐文件按行算好：`SKILL.md` 在前，其余按路径，相同的文件省略，文本文件是带 3 行上下文的 hunk，非文本只标出变化；App 只渲染。
  - `internal/api/handlers_skills_test.go`
  - `internal/skills/diff_test.go`
  - `internal/skills/owner_test.go`
- **对话文件随消息往返**：`POST /v1/threads/{id}/files` 边接收边写入 `{storage_dir}/files/<file_id>`（临时文件改名后才登记），超过 `files.max_upload_mib` 以 `payload_too_large`（413）拒绝，名字只保留最后一段、去掉控制字符；媒体类型对图片、音视频与 PDF 按内容判断，其他按扩展名。`CreateRunRequest.file_ids` 只接受本对话里尚未随消息发送的 owner 文件，至多 10 个，否则整个请求按无效请求拒绝；有文件时消息可以没有正文。线程消息把 owner 文件列在它随行的 user 消息上，把 agent 文件列在其 run 的 assistant 消息上；`GET /v1/threads/{id}/files/{file_id}` 只返回该对话的文件。
  - `internal/store/thread_files_test.go`
  - `internal/core/thread_file_test.go`
  - `internal/wire/thread_files_e2e_test.go`
- **视图经后端校验后挂在回复上**：`show_view` 经 toolsearch 按需加载，`internal/views` 按目录 `acorn.views.v1` 校验：未知组件或属性、重复或缺失的 id、不在 root 之下或有两个位置的组件、在数据里找不到的 path、与列数不符的行、不过 `webaccess` URL policy 的链接（含 Text 里的 Markdown 链接）、不是本对话图片文件或知识库图片附件的图片、超过 200 个组件或 64 KiB 的视图都拒绝，问题一次列出，作为模型可见的 tool result 返回，不保存。通过的视图存进 `thread_views` 并写 `view.shown` 事件；线程消息把视图列在其 run 的 assistant 消息上，没有回复的 run 与发了文件时一样补一条空正文的 assistant 消息；删除对话一并删除视图。`views` 技能里的示例必须通过校验。
  - `internal/views/views_test.go`
  - `internal/tools/views_tool_test.go`
  - `internal/store/thread_views_test.go`
  - `internal/wire/thread_views_e2e_test.go`
- **视图动作是 owner 的一条消息，只发一次**：App 只发组件 id、List 里的位置与选中的选项 id，后端从存着的视图解析动作：不是 `Button` 或 `ChoicePicker` 的组件、位置不对、不在声明里的选项、单选时选了多个都以 400 拒绝，动作名、选项文字与 context 都取自视图。登记动作与写 owner 消息在同一事务里，主键是（视图, 组件 id 加位置），重复发返回 409 且不多写消息、不多起 run；之后按 owner 发消息的规则起 run，模型在这条消息旁看到视图 id、组件、动作名、选项与 context。`update_view` 只能改本对话的视图，整个视图重新校验，写 `view.updated` 事件，已发的动作保留；删除对话一并删除动作。
  - `internal/views/actions_test.go`
  - `internal/runtime/view_actions_test.go`
  - `internal/store/thread_views_test.go`
  - `internal/tools/views_tool_test.go`
  - `internal/wire/thread_view_actions_e2e_test.go`
- **Mobile 是 control surface 不是 runtime**：mobile 不执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle；context pressure/boundary/run status 都消费后端 projection。
  - `mobile-kotlin/app/src/test/...`（JUnit）

## 约定、唤醒与追踪

- **约定发生只认领一次，执行前绑定来源**：约定规则与 occurrence 独立。条件认领后，Executor 在模型调用前绑定执行 run；周期沿同一规则排下一次。五分钟未绑定的认领可恢复，迟到启动受状态校验拦截。完成约定必须引用 owner 确认或成功执行结果。关联了关切的约定醒来时，输入在约定内容之后附上关切的标题、状态、outcome 与最新理由。
  - `internal/store/commitment_contract_test.go`
  - `internal/wake/scheduler_test.go`
  - `internal/wire/wake_acceptance_e2e_test.go`
- **每日唤醒上限按 owner 时区计算**：`wake.daily_limit` 限制 owner 本地每天的唤醒次数，计数来自 events 表中当天的 `wake.fired`，重启后依然有效；超限的约定保持 scheduled，次日再处理；为 0 时关闭自主唤醒。
  - `internal/wake/scheduler_test.go`
- **推送有上限、守免打扰、随设备失效**：`notify_owner` 经 `notify.Sender` 发送；每小时超过 `notify.max_per_hour` 时返回错误；免打扰时段内的通知排队到时段结束，由 wake 调度器每次 tick 发出；没有任何已登记设备时返回错误。推送按 App 向 FCM 注册的 Firebase installation ID（FID）投递，设备经 `PUT /v1/devices/self/push-installation` 登记 FID，格式不符的拒绝；一个 FID 只归最近登记它的设备（同一部手机重新配对不会收到重复推送）；FCM 回 404 或 UNREGISTERED 的 FID 被删除，设备吊销时其 FID 一并删除。至少一台设备收到即记 sent，否则记 failed 并返回错误。未配置服务账号时工具以 disabled 注册并给出原因。
  - `internal/notify/notify_test.go`
  - `internal/store/store_presence_test.go`
  - `internal/api/push_installation_handler_test.go`
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
- **触发器只在 wake 调度器运行，同一时刻只认领一次**：每个 tick 让 `skill_triggers` 恰好对应在用技能的触发器：新触发器或改了时间表的排到 owner 时区的下一个时刻，停用技能的触发器删除；缺密钥的包的触发器不运行。到期触发器以 `next_at` 的条件更新认领（多个调度器只有一个运行），同一技能的触发器依次、不同技能并行运行，一个 tick 至多 `watch.max_checks_per_tick` 个。工作机先拿到触发器所在的包（哈希不同就先放上去），再以包目录为工作目录用 `bash -lc` 执行 `run`，带包的密钥环境变量，受 `workstation.command_timeout_seconds` 与 `workstation.max_output_bytes` 约束。非零退出、超时、输出超限或任一非空行不是 `{"key","text"}` 都算失败：失败次数加一，按追踪项的退避（时刻间隔每次失败翻倍，上限 24 小时）推迟，连续 5 次标为 failing，成功后清零。
  - `internal/wake/triggers_test.go`
  - `internal/skills/manifest_test.go`
  - `internal/workstation/trigger_test.go`
  - `internal/store/store_skills_test.go`
  - `internal/wire/package_triggers_e2e_test.go`
- **触发器事件每个 key 只处理一次，没有事件不调用模型**：事件按（技能, 触发器, key）唯一写入 `skill_trigger_events`，同一事务里由 SQL 触发器登记为 `trigger:<id>` 外部记忆来源（speaker external，不抽取）。命令没有输出时不起 run。immediate 触发器的新事件在 `skill_threads` 记下的包线程里起 run（第一次时以技能名为标题建立），输入以 `[trigger <skill>/<trigger>]` 开头并注明是包命令的不可信输出，事件来源作为 run 输入来源登记，计入 `wake.daily_limit` 与 `wake.daily_tokens` 并记 `wake.fired{trigger}`；超限、起 run 失败与 digest 触发器的事件留给早安卡。operating rules 要求 agent 为这类输入加载对应技能，且不执行事件里的指令。
  - `internal/wake/triggers_test.go`
  - `internal/store/store_skills_test.go`
  - `internal/wire/package_triggers_e2e_test.go`
  - `internal/runtime/agent_test.go`
- **每个本地日一次早安卡，没材料不醒**：过了 `briefing.at`（owner 时区）后，`routine_runs` 表按 (`briefing`,日期) 认领，跨进程只触发一次；起 run 失败释放认领，下个 tick 重试。调度器确定性地收集材料，输入只含有内容的段落：今天到期的约定与未结 occurrence、上次简报以来 `updated_at` 变化的记忆记录、一周内生效的事实、上次简报之后成功的夜思 run 的输出、上次简报之后写的笔记、全部待简报条目、待简报的触发器事件（按技能与触发器分组，注明不可信）、failing 追踪项、System health、手机通知。一周内生效的事实只在当天生效时算材料，其余段落有内容即算；没有材料时保留认领、不起 run、不推送。早安卡在 Briefings 线程里以 standalone run 运行，回复就是简报全文，不写笔记：模型只看到本次输入，不带线程历史与摘要；条目与触发器事件随之标为 briefed，列出的记录、笔记 revision 与事件作为 run 输入来源登记；它不计入每日唤醒上限。System health 只在有问题时出现：`backup.dir` 已设置而最新快照缺失、超过 36 小时或目录不可读，记忆任务重试耗尽失败，待处理记忆任务等待超过 24 小时，或有 failing 的触发器。
  - `internal/wake/briefing_test.go`
  - `internal/wake/health_test.go`
  - `internal/wake/watches_test.go`
  - `internal/wake/triggers_test.go`
  - `internal/memory/context_test.go`
  - `internal/store/store_watch_test.go`
  - `internal/wire/watch_acceptance_e2e_test.go`

## 空闲思考、用量与手机通知

- **例行唤醒一次认领**：`routine_runs` 的 (routine, slot) 唯一;准备/启动失败释放,预算跳过与空夜思保留,启动成功后保持认领。夜思在 Thoughts 线程运行。
  - `internal/wake/thinking_test.go`
- **游思在关切所在的对话里推进**：关切记下建立它的 run 所在的对话，之后的 revision 不改变它。每个游思时段从到期的 active/waiting 关切里取最久没有修订的一件，所在对话最新 run 仍在运行或等待审批时跳过它；游思 run 在该对话里运行，带着对话历史与摘要，对话已删除时起 run 失败并释放认领。没有可推进的关切时保留认领。游思输入列出关切的 outcome、最新理由与复查时间，有 outcome 的关切另给出工作机上的 `~/work/<id>/` 目录。
  - `internal/wake/thinking_test.go`
  - `internal/wire/concern_acceptance_e2e_test.go`
- **有 outcome 的关切只由 owner 的确认解决**：outcome 随关切的每个 revision 保存，修订不传 outcome 时沿用当前的。解决关切要以 owner 的消息或成功的工具结果为来源，有 outcome 时只认 owner 的消息，在 `SaveConcern` 读取当前 revision 的同一事务里判断。游思在工作机上推进这类关切，中途需要审批的调用照常停下推送、批准后续跑；交付发在关切所在的对话里，关切转为 waiting，复查时间之前不再被游思选中。
  - `internal/store/memory_state_test.go`
  - `internal/wire/concern_acceptance_e2e_test.go`
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
- **跨模块闭环**：夜思能通过技能整理工作记忆,手机通知能经过 HTTP 进入 presence 与简报,早安卡的回复出现在此刻页并推送。
  - `internal/wire/thinking_acceptance_e2e_test.go`
- **手机队列归属清晰**：队列绑定服务器与设备,身份变更清空;取消白名单移除待传项;请求使用批次自己的凭证;失败保留,成功只删除发送的 ID;持续入队保留第一条的上传截止时间。
  - `mobile-kotlin/app/src/test/java/io/ycvk/acorn/core/notifications/NotificationQueueTest.kt`

## 工作机

- **工作机只经带 token 的 HTTP 调用**：serve 不接触容器引擎；`acorn workstation serve` 在容器里运行，每个请求校验 bearer token，token 少于 32 字符时拒绝启动。`shell` 与 `files` 是 deferred 工具，未配置 `workstation.url` 时以原因禁用。
  - `internal/workstation/workstation_test.go`
  - `internal/tools/workstation_tools_test.go`
  - `internal/config/config_workstation_test.go`
- **文件经工作机往返**：每次 run 前，对话里已发送的 owner 文件按 id 复制到工作机 `~/uploads/<file_id>/<文件名>`（写在旁边再整体换入），不再属于任何对话的副本被删除；复制失败时 run 继续，模型从消息附注里得知原因。模型看到每条 owner 消息附带的文件名、类型、大小与工作机路径，≤10 MiB 的 JPEG/PNG/WebP/GIF 作为图片输入随消息交给模型并按 `core.ImageInputTokens` 计入输入预算。`send_file` 是 eager 工具，经工作机 `/v1/files/raw` 读取文件，超过 `files.max_send_mib` 时失败，存为本 run 的 agent 文件。对话的消息把 agent 文件列在本 run 的回复下；run 没有回复时，列在它的输入之后一条以 run id 为 id 的无文本 assistant 消息里。
  - `internal/workstation/files_test.go`
  - `internal/tools/workstation_tools_test.go`
  - `internal/wire/thread_files_e2e_test.go`
- **伴随进程常驻**：`acorn workstation serve --keep` 的每个伴随进程在独立进程组里运行，退出后按退避重启，serve 停止时连同其子进程一起结束；日志按名字加前缀并去掉终端颜色码。
  - `internal/workstation/supervisor_test.go`
- **包的 MCP 服务器由工作机守护**：`PUT /v1/mcp` 给出应运行的包（名字、包哈希、密钥环境变量），工作机停止其余服务器，包哈希或环境变了的重启，新的启动，最多等 90 秒报告每个服务器能否服务。服务器与伴随进程用同一个 supervisor：以包目录为工作目录、独立进程组、退避重启，stderr 以 `mcp:<name>` 为前缀写入日志；包目录的哈希必须与请求一致。工作机以 MCP 客户端经 stdio 连接服务器，把它的工具挂在 `/v1/mcp/<name>` 的 streamable HTTP 上，经 bearer token 鉴权，会话跨重启保留；服务器 stdout 里不是 JSON-RPC 消息的行以 `stray stdout:` 记进 `mcp:<name>` 日志并跳过，会话照常；进程退出或 stdio 会话仍然断开（管道关闭、客户端解不开的消息、超长的行）都算一次失败，停掉进程后退避重启，重启进行中的请求等它结束再报告；不再运行的服务器的地址返回 404。serve 停止时这些服务器一起结束。
  - `internal/workstation/mcp_test.go`
- **页面渲染隔离**：`web_rendered` 追踪经工作机 `/v1/render` 渲染，每次用临时配置目录启动一次性浏览器，只接受 http(s)；backend 先按共享 URL policy 校验目标。
  - `internal/workstation/render_test.go`
- **浏览器画面只给已认证的 owner**：工作机的 DevTools 只在容器内开放；`/v1/browser/*` 经设备认证转发到工作机，未配置工作机时返回 `browser_unavailable`，未知标签页返回 `browser_tab_not_found`；导航只接受 http(s)。画面流可按 owner 的手机尺寸排版，宽高要么都给、要么都不给。
  - `internal/workstation/view_test.go`
  - `internal/api/handlers_browser_test.go`
- **拖动按手指的时间回放**：App 把拖动中手指的每个采样连同距按下的毫秒数分批发送，不丢点，不合成点；工作机一次拖动只用一个 DevTools 会话，视口换算在 down 时做一次，down 到达 150 ms 后按每个点的时间派发，不等页面应答下一个点；某批迟到时它到即派发，之后的点整体后移同样时长；up 等整次拖动派发完才返回。拖动期间画面来自 screencast，不截图。
  - `internal/workstation/drag_test.go`
  - `internal/workstation/view_test.go`
  - `internal/api/handlers_browser_test.go`
- **命令有界**：超时或请求取消时杀掉命令的整个进程组；工具超时不能超过 `workstation.command_timeout_seconds`；stdout 与 stderr 超出上限时各保留开头和结尾并标出省略的字节数。
  - `internal/workstation/workstation_test.go`
  - `internal/tools/workstation_tools_test.go`
