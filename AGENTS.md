# AGENTS.md

Acorn 的 AI 协作硬约束入口。`CLAUDE.md` 软链接至此,单一真相源。

## 项目概览

Go 1.27 + Eino ADK 的单用户自托管个人 agent 后端,module `github.com/ycvk/acorn`。owner 在 VPS 跑后端,Kotlin App 配对手机后远程对话、看运行、批审批、收推送。agent 有人格和带证据的长期记忆,会自己约时间醒来(约定),需要时经 FCM 推送找 owner;owner 从手机分享的链接、文字、图片由 agent 整理进 markdown 知识库(同时是 git 仓库);agent 替 owner 追踪 RSS、GitHub、网页(含价格),每天早上写简报笔记并推送;按固定时段夜思/游思,读取手机白名单 App 上报的通知。入口:operator CLI(`serve` 长驻 / `run`·`smoke` 一次性 run / `init`·`pair`·`devices`·`token` 运维 / `skills`·`doctor` 诊断 / `memory reindex` 离线索引维护)、authenticated `/v1` API、serve 进程内的唤醒调度器(约定、追踪项、早安卡、夜思、游思)、mobile inbox、persisted RunEvent SSE、Kotlin mobile(含系统分享入口与知识库页)。方向见 `docs/adr/0003-personal-agent-direction.md`。

## 常用命令

Go 命令在仓库根目录;Android/Kotlin 命令在 `mobile-kotlin/`。

```bash
make build && make serve && make doctor
make test                          # go test ./...
go test -race ./...                # CI 竞争检测版本
make lint && make format-check     # CI 门禁
make test-architecture             # 架构边界守卫
make generate                      # go generate ./internal/api
make release-linux-amd64           # 纯 Go 交叉编译(无 CGO)

# acorn CLI(根目录 acorn / make build 产出 ./bin/acorn)
acorn serve [-c path] [--listen addr]   # 长驻 remote API,唯一常驻命令
acorn run [-c path] [--json] "task"      # 一次性执行一个 run
acorn smoke [-c path] [--json] "task"   # 安装探活:真实跑一次 run,非零退出即失败
acorn init [-c path] [--force] [--print] # 生成 starter config 和默认 persona.md
acorn memory preflight [-c path] [--json] # 只读迁移完整性和活跃任务预检
acorn memory reindex [-c path] [--json] # 停止其他 Acorn 进程后重建 embedding 索引
acorn doctor [-c path] [--json]          # 能力快照 + MCP 健康探活
acorn skills {list|inspect|check} [-c path] [--json]
acorn pair [-c path] [--qr] [--server-url url]  # 生成设备配对码
acorn token issue [-c path] [--name n] [--ttl d]  # 颁发 device token
acorn devices {list|revoke} [-c path]

# Mobile(在 mobile-kotlin/)
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check   # CI 门禁
./gradlew assembleDebug  # in mobile-kotlin/
```

## 架构大图

- **组合根**:`internal/wire.Container` 是唯一实例化具体实现的地方(SQLite store、RunnerFactory、wake scheduler、FCM sender、知识库 vault 与系统 git、watch checker 与它的浏览器、时钟)。`cmd/acorn → cli → wire.Container → {api, runtime, store, wake, notify, knowledge, watch}`。`serve` 是唯一长驻命令,同时运行 wake scheduler。
- **运行时主链**:`Executor → RunnerFactory.buildRun → buildAgentRunner(Eino ChatModelAgent) → adk.Runner`,事件经 `agentEventProjector` 写 SQLite events。全部在 `internal/runtime`。
- **单一编排模式**:每个 run 一个 Eino `ChatModelAgent`(ReAct:model → tools → model),`EnableStreaming`,checkpoint 经 `core.SessionStore` 落 SQLite `agent_checkpoints`。
- **职责边界**:`internal/runtime` 做装配(工具 catalog、persona + operating rules 写进 Instruction、middleware 链)+ 执行(run/resume)+ StreamItem 投影;run 历史与自动召回由 memory 服务提供;上下文压缩、延迟加载工具、技能加载、工具调度、主模型重试交给 Eino middleware、ToolsNode 与 `ModelRetryConfig`。
- **关键包**(17 个 internal 包):`internal/core`(Layer 0,零内部导入,纯类型+契约:核心 domain 类型 + context plumbing + 11 个 store 接口 + 工具契约,无 service struct);`internal/runtime`(Layer 3)拥有 Executor、RunnerFactory、buildAgentRunner、presence/approval/tool-error middleware、skill middleware 的 backend、StreamItem 投影;`internal/tools` 拥有工具实现(artifact/operator/presence/notify/knowledge/web/browser 工具 + ToolRegistry);`internal/store` 拥有 SQLite adapter + ArtifactService(依赖 `core.ArtifactService`,无重复接口);`internal/memory` 拥有来源处理、证据整合、四路召回、线程摘要和调用预算;`internal/presence` 拥有"当下"渲染、persona 读取与 cron 解析(纯函数);`internal/wake` 拥有 serve 进程内的唤醒调度器(约定、追踪项、早安卡、夜思、游思);`internal/notify` 拥有 FCM HTTP v1 client 与推送 sender(频率上限、免打扰排队);`internal/knowledge` 拥有知识库 vault(路径校验、frontmatter 读写、经 `Git` 接口提交、索引同步);`internal/watch` 拥有追踪项的抓取、解析与比对(rss/atom、GitHub、网页快照,不调用模型);`internal/mcp` 拥有 MCP provider manager;`internal/api` 拥有 `/v1` client surface + live RunEvent 投影(`projection.go`);`internal/webaccess` 拥有 web fetcher、Tavily search、内容抽取与共享 URL policy(工具本身在 `internal/tools`);`internal/skills`/`internal/config`/`internal/cli`/`internal/wire` 各司其职。
- **真相归属**:SQLite(`internal/store`,modernc.org/sqlite,写入单连接串行化,记忆召回与整合候选读取走 `query_only` 的 WAL 只读连接池)保存 runtime、记忆来源引用/记录/修订/关系/排除/任务/向量/用量、关切和约定发生记录,共 43 张业务与元数据表,另有 FTS5 索引 `memory_records_fts`/`runs_fts`/`knowledge_notes_fts`。schema 在 `store_schema_bootstrap.go` 与 `memory_schema.go`,两组 required-table 校验强制列存在。`{storage_dir}/persona.md` 是 owner 可编辑的人格,每个 run 读取,缺失或为空时 run 失败;知识库 markdown 与 Git 版本是知识原文的真相,`knowledge_notes` 是可重建索引。
- **API 契约**:`docs/openapi.yaml` 是唯一 wire contract,`mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/` 由它生成。客户端只收 `internal/api/projection.go` 投影的 live RunEvent;RunEvent SSE 用 `follow=true` 轮询 + `after_seq` 游标续读。

## 硬边界

### 运行时 & 编排

- 每个 run 只有一个 `ChatModelAgent`,不存在 multi-agent/subagent。agent 状态由 Eino 持有,run 间历史来自 `session_messages`,按 token 预算选择近期原文与有来源游标的线程摘要。
- middleware 顺序固定:memory visibility → patchtoolcalls → summarization → reduction(clear-only) → toolsearch(有 deferred 工具时) → skill → presence → approval → tool errors。越靠前包得越外层。
- 工具串行执行(`ExecuteSequentially`)。普通工具失败与调用不存在的工具都是模型可见的 tool result,不是 run failure;interrupt 与 context 取消照常传播。普通工具失败记为 `tool.call.failed` 事件。
- 主模型调用失败最多重试 3 次(context 取消不重试);中途断开的流被重试时不算 run 失败,只有成功那次的输出成为 assistant 消息,已推给客户端的 delta 不撤回。
- 审批:`approval.require` 是工具名 glob 列表(默认 `browser`、`mcp__*`)。命中的调用由 approval middleware 登记 `tool_approval` pending action 并发起工具级 interrupt;resume 时参数必须与登记时一致,accept 才执行,decline 把拒绝说明作为工具结果返回。同一轮里排在被拦截调用之后的工具调用会先执行,被拦截的调用等决策后执行。`approval.require` 不能命中 `ask_operator`(config 校验拒绝)。approval 自身的存储失败直接让 run 失败。
- 续跑由服务端驱动:`RunResumeService.ResumeIfReady` 只在 run 为 interrupted 且没有 pending 的 pending action 时续跑;每次决策后和每次 run 停在 interrupted 时各检查一次,in-flight 集合防重复。
- web 工具(`web_search`/`web_fetch`/`browser`)是 deferred,经 toolsearch middleware 的 `tool_search`(`select:<name>`)加载;共享 URL policy 只放行公网 http(s),private 网段需 `web_access.allow_private_networks`。

### 工具 & 技能

- native skill truth 是 `internal/skills` file-backed loader,`tools.workspace.root_dir` 指向存放 seed skills 与 workspace skills 的目录。repo `./skills` 是 release seed pack;release installer 安装到 `~/.acorn/skills`;workspace skills 放在 `./.acorn/skills/workspace`。agent 不创建或修改 skill。
- 技能经 Eino skill middleware 提供:`skill` 工具的描述列出本 run eligible 的技能(summary + trigger hints),按名加载 SKILL.md 正文;只有 inline 模式,不 fork 子 agent。技能是只读 markdown,无 lifecycle/evidence/assess。

### 个人记忆、约定 & 推送

- `MemoryStore` 保存 fact/insight/thought 及证据、修订、有效时间与排除版本。fact 的有效性由证据和时间决定;thought 用 open/resolved/released 表达进展;concern 用 active/waiting/resolved/released 跟踪持续事项。每条可用记忆可以展开来源。记忆与约定写入时间由调用方提供。
- 对话、工具终态事件与 watch 条目随原始记录登记来源和持久化任务;知识库按 Git 提交游标登记版本。后台 worker 分批抽取、整合和 embedding;整合候选融合语义、实体邻居与关键词,以租约 token、revision 与 exclusion epoch 防止过期写入。owner 原话与客观结果可以支持 fact;assistant 文本保留归属,认识通过 supporting parents 追溯。
- `recall` 融合 FTS5 trigram/短词 LIKE、Voyage 向量、实体与关系、时间检索;向量以 int8 索引筛选 2048 个候选,再用保留的 float32 原向量精确重排;向量按 revision 保存,`known_at` 在排名前选择当时版本;`deep` 使用主模型重排。每个 run 自动 standard recall。结果报告待处理来源与索引 generation;`memory_read` 按预算分页展开来源与修订。`keep` 同步保存当前 owner 的精确引用;`memory_correct` 绑定目标 revision 和当前更正;`think` 保存或结束念头;`concern` 管理持续事项。
- 显式实体别名必须包含规范名、别名和来源精确引用;有范围时引用必须包含范围。同名别名仅在身份唯一或查询给出可区分范围时展开,历史查询与遗忘约束同样适用。
- `memory_forget` 要求当前 owner 请求,事务排除目标来源或片段及派生记录,移除向量并使相关摘要失效。runtime 在 Eino 压缩前应用排除,旧 epoch checkpoint 禁止恢复,取消并等候受影响的其他 run 退出后确认遗忘。agent 的知识库读取与搜索遵守相同排除;owner 仍可读取原始聊天和知识文件。
- `<memory_context>` 与 `<presence>` 是每次调用临时追加的 system 消息。前者含本次检索与当前 run 来源 ID,后者含时间、唤醒原因、约定、开放念头、关切与手机通知。两者分别受预算控制,快照保存结构化来源引用;已到期约定超出必要预算时明确报错。最终模型输入再次检查总容量。
- `CommitmentStore` 分开保存约定规则与每次 occurrence。调度器条件认领,Executor 在首个模型调用前绑定执行 run 并排下次周期。未完成启动绑定的认领 5 分钟后恢复,迟到的启动请求在调用模型前失败;启动失败推迟 5 分钟。`settle` 完成具体 occurrence 需要当前 owner 确认或成功工具结果,取消作用于约定规则。
- `memory.daily_tokens` 约束后台处理;run 内检索、线程摘要和 Eino 压缩计入相应 run,自主调用同时受 `wake.daily_tokens` 约束。每次调用含独立 ID、operation、模型、token 与 reported 标志;预算耗尽保留任务至下一本地日。`doctor` 展示处理队列、失败、最近完成与索引状态。模型/维度变更经 `acorn memory reindex` 离线重建;数据目录共享锁与维护独占锁互斥。

- `notify_owner` 经 FCM 推送,每小时上限 `notify.max_per_hour`,免打扰时段内排队、结束后由 wake 调度器发出;FCM 不认的 token 删除;没配 `notify.fcm.service_account_file` 时工具以 disabled 注册并给出原因。设备吊销时同时删除其 push token。

### 知识库 & Capture

- 知识库只经 `knowledge.Vault` 写入(工具 `knowledge_write`/`knowledge_edit`、Capture 的图片附件)。每次写入提交一次,只提交本次涉及的文件,作者固定为 Acorn,agent 的写入带 `Acorn-Run` trailer;owner 未提交的其他改动不受影响。agent 不删除、不移动笔记。
- 笔记路径是知识库内的相对 `.md` 路径,不含 `..` 与隐藏段,不在 `attachments/`;frontmatter 由 Acorn 管理 title/tags/source/created/updated,其他字段改写时原样保留。没有 frontmatter 的笔记以第一个 `# ` 标题或文件名为标题。
- 索引每次列表、搜索前按 mtime/size 与文件同步;读不了或解析失败的笔记让同步失败并给出路径。检索是 FTS5 trigram,少于 3 个字走 LIKE。
- `POST /v1/captures` 先把图片(JPEG/PNG/WebP/GIF,≤10 MiB,按内容判断类型)存进 `attachments/YYYY/MM/` 并提交,再新建线程,以 role `capture` 的输入立即起 run;模型把它当 user 消息读。capture 不计入 `wake.daily_limit`。分享内容的整理流程写在种子技能 `capture_to_note`。
- 知识库目录配置 `receive.denyCurrentBranch=updateInstead`,owner 可以 clone 到本地(如用 Obsidian 打开)改完 push 回去。

### Watch & 早安卡

- 追踪项由 agent 用 `watch_create`/`watch_update`/`watch_list` 管理,存在 `watches`;四类信息源:`rss`(含 `rsshub:/route`,需 `watch.rsshub_base_url`)、`github`(releases 或新开的 issue)、`web`(CSS 选择器或正文快照)、`web_rendered`(需配置浏览器)。所有抓取经 `webaccess` 的 URL policy。
- `watch_create` 先抓一次,失败不建立;成功的第一次检查只建基线,之后只报新条目。条目按 (watch, key) 只入库一次;网页类追踪项比较选中内容的快照,变化时产生一条带前后值的条目。
- 调度只在 `wake.Scheduler`:每次 tick 在约定之后认领到期追踪项(条件更新加 10 分钟租约)并检查,单项超时 60s,每 tick 最多 `watch.max_checks_per_tick` 个。`digest` 的新条目等早安卡;`immediate` 的新条目在建立追踪项的线程里起 wake run,计入 `wake.daily_limit` 并记 `wake.fired{watch_id}`,超限时转给早安卡。
- 抓取失败按间隔 ×2^n 退避(上限 24h),连续 5 次失败标为 failing,仍继续检查,成功后恢复。
- 早安卡按 `briefing.at`(owner 时区,空则关闭)每个本地日触发一次:`routine_runs` 表以 (`routine=briefing`,本地日期) 为主键保证多进程只触发一次,准备输入或起 run 失败则释放当天的认领。run 在 Briefings 线程里,输入以 `[briefing YYYY-MM-DD]` 开头,按追踪项列出全部待简报条目和 failing 追踪项;条目标为 briefed。早安卡不计入每日唤醒上限,记 `briefing.fired`。写笔记与推送的做法在种子技能 `morning_briefing`。

### 空闲思考 & 手机通知

- `wake.Scheduler` 的例行唤醒统一认领 `(routine, slot)`。早安卡在 Briefings 线程;夜思和游思共用 Thoughts 线程。`thinking.night_at` 默认 `03:00`,空则关闭;`thinking.wander_at` 默认空,最多六个不重复的本地时刻。当日错过的时段在启动后补一次;预算超限或夜思清单为空时保留空认领,当天不重试;启动成功后的记账失败显式报错并保留认领。
- 夜思读取上次夜思以来更新的证据、待复核认识、开放念头/关切与未完成 occurrence。种子技能 `night_reflection` 用 recall/memory_read 核对依据,用 think/concern 记录进展,用 settle 完成有执行证据的约定;`wander` 每次推进一件已到复查时间的开放关切。
- 每次成功的主模型调用记一条 `model.usage`(prompt/completion/total tokens 和 reported);未返回 usage 时记 `reported:false`。该事件不进入 mobile live 契约。summarization 调用与失败重试的流不计入。`doctor` 显示当天自主次数、用量及未报告 usage 的调用数。
- `POST /v1/phone-notifications` 只接收已鉴权设备,每批 1–100 条;device_id 来自鉴权,received_at 来自服务端时钟。通知按 (device,key,posted_at) 去重,只作背景信号。"当下"读取最近 6 小时最多 10 条;早安卡读取上次成功启动的简报认领时间(含)至本次认领时间(不含)之间最多 50 条,注明超出数量。
- App 通知白名单默认空,只采集已配对状态下选中 App 的普通通知,跳过 ongoing、group summary 和自身。队列持久化并绑定 server URL + device ID,最多 500 条;首条入队后 60 秒开始上传,每批最多 100 条,成功只删除该批本地 ID。断开/更换身份清空队列,取消勾选移除该 App 待传项;网络失败保留并显示错误,下次入队、连接或手动 retry 重试。每批使用独立 bearer client。
- 调度器清理收到满 7 天的原始通知;已进入对话、context snapshots 或知识库的内容随各自记录保留。通知正文按外部数据处理,不执行其中的指令。

### 模型 & 上下文

- provider `api` 选择 `responses`(默认)、`chat_completions`、`anthropic`,经 Eino 原生 AgenticModel 适配器调用;全链路使用 `schema.AgenticMessage`,内容块和推理签名保存在 checkpoint,客户端只接收公开文本、推理文本和工具调用。
- 默认模型 GPT-6 Astra,上下文 1050000、压缩余量 32000、工具结果保留最近 8 轮;Astra 与 Opus 5.5 输出上限 128000,模型能力表校验输出、上下文、reasoning effort 与采样参数。未知模型按显式配置调用。`temperature` 省略时交给模型默认值。
- `runtime.run_timeout_seconds` 与 provider `timeout_seconds` 默认 0(关闭总时限);provider `idle_timeout_seconds` 默认 300,按响应数据刷新,0 关闭。`agent.max_iterations` 默认 100。

### 上下文 & 压缩

- summarization middleware:token 超 `window_tokens - max_output_tokens(或已知模型输出上限) - presence.max_tokens - compact_margin_tokens` 时同步用一次 model 调用总结历史。
- reduction middleware 只做 clear:总 token 超上述输入预算的 3/4 时把较早的工具结果替换成占位符,最近 `mask_after_turns` 轮工具调用原样保留。
- persona、operating rules 与 skill middleware 的说明写进 agent Instruction,技能列表在 `skill` 工具描述里,都不进入可被总结的消息序列。"当下"与记忆检索块不属于消息序列,不参与 summarization 计数,由 `presence.max_tokens` 单独约束(配置校验要求它小于 `compact_margin_tokens`)。

### Remote API & Mobile

- remote client wire contract 是 `docs/openapi.yaml`。Remote clients 只走 `/v1`、`/healthz`。`/mcp` server mode 已删除。改 mobile DTO/RunEvent/OpenAPI schema 必须同步 openapi.yaml、generated client 和相关测试。
- auth 是 single-owner device auth:pairing code → bearer token,SQLite 只存 hash。token 缺失/未知/revoked 必须显式失败。
- mobile inbox truth 是 `GET /v1/inbox`,后端聚合 pending actions + active/recent runs + system status。
- pending approval truth 是 `GET /v1/pending-actions` + `:decide`,消费 SQLite `pending_actions`(kind:`elicitation`、`operator_question`、`tool_approval`)。决策后由服务端续跑,不存在客户端 resume 端点。
- Mobile 不本地执行 run、不持 runtime truth、不从 local state 猜测后端事实。
- 推送:App 以 `PUT /v1/devices/self/push-token` 上报 FCM token;点击推送按 `data.thread_id` 打开线程。App 的 Firebase 配置来自 `local.properties` 的 `acorn.firebase.*` 或 `ACORN_FIREBASE_*` 环境变量。
- 分享与知识库:App 注册 `ACTION_SEND`(text/plain、image/*)分享入口,以 multipart `POST /v1/captures` 上传;App 的知识库页只读,经 `GET /v1/knowledge/notes`(搜索/最近)与 `GET /v1/knowledge/note` 读取。
- 涉及 mobile 视觉/交互的改动必须在真机或模拟器验证;无法连接设备时必须说明未验证。

### 自托管发布

- GitHub Release 预构建 tarball + Linux binary + signed Android APK + `systemd`。Release build 是纯 Go 交叉编译(`CGO_ENABLED=0`),无 CGO/build tags。
- installer 安装 `/opt/acorn`、`~/.acorn/skills`、`/usr/local/bin/acorn` wrapper;默认读 `~/.acorn/acorn.yaml`;root VPS 用 `/root/.acorn`,workspace 是 `/srv/acorn/workspace`。服务器需要系统 `git`(知识库;installer 用 apt 安装),找不到时 serve 启动失败并给出安装提示。

## 工作方式

- 先读 live code 再下结论。不为了「看起来更稳」添加 mock、fallback、compat alias、silent degradation 或吞错逻辑;surface 真实失败,修根因不修症状。
- 用户明确允许 destructive rewrite 时默认 hard cut:新路径落地时同步删除旧路径、旧配置、旧测试。
- 不要修改用户已有的 unrelated dirty worktree 改动。
- 业务逻辑不硬 new concrete implementation,通过参数、接口或 container 注入。

## 代码规范

 - Go 1.27,tab 缩进,import 按 goimports 分组;Kotlin 4 空格。
 - error 必须显式处理,分两类:
   - **Exported sentinel error**(需要被 `errors.Is` 比对):必须是包级 `var ErrXxx = errors.New(...)` 或 `fmt.Errorf("...: %w", ...)`;命名 `ErrXxx`;放在定义它的包的 errors.go 或对应文件顶部。
   - **Precondition/internal-config error**(不该发生的编程错误:依赖未注入、配置缺失、前置条件违反):用 inline `errors.New("...")` 直接返回,不需要 `errors.Is` 比对;消息要可定位(含字段名/参数名)。
 - SQLite 关闭 Rows/Stmt 并检查 `rows.Err()`;HTTP 带 context,关闭 body。

## 配置和文档

- `configs/acorn.local.yaml` 在 `.gitignore` 中。`configs/acorn.example.yaml` 和 `configs/acorn.selfhosted.example.yaml` 修改时同步 config struct、defaults、validation 和 tests。
- provider `api_key` 支持环境变量展开。
- public context config 只保留 `window_tokens`、`compact_margin_tokens`、`mask_after_turns`。删除配置字段不保留兼容读取(config 以 `KnownFields` 严格解析,多余字段直接报错)。
- 架构现状 → 本文件 + `docs/architecture/INVARIANTS.md`(不变量 ↔ 测试),用户指南 → `docs/user/`,方向级决策 → `docs/adr/`。不要把未来计划写成 current truth,不要新增复述代码的架构文档。

## 验证要求

提交前必须通过 `make format-check` 和 `make lint`。core/runtime 改动至少跑 `go test ./internal/core ./internal/runtime ./internal/cli ./internal/tools ./internal/store ./internal/memory ./internal/presence ./internal/wake ./internal/notify ./internal/mcp ./internal/wire ./internal/api`。

**CI 守卫**(`tests/architecture/`):`structural_limits_test.go`、`client_projection_boundary_test.go`、`store_interface_count_test.go`、`dependency_direction_test.go`、`docs_structure_test.go`、`shipped_artifacts_test.go`。
