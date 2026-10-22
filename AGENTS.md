# AGENTS.md

Acorn 的 AI 协作硬约束入口。`CLAUDE.md` 软链接至此。

## 项目概览

Go 1.27 + Eino ADK 的单用户自托管个人 agent 后端,module `github.com/ycvk/acorn`。owner 在 VPS 跑 `acorn serve`,Kotlin App 配对后远程对话、看运行、批审批、收推送。agent 有人格和带证据的长期记忆,会按约定醒来,替 owner 整理分享内容进知识库、追踪 RSS/GitHub/网页并写早安卡,按时段夜思与游思。方向见 `docs/adr/0003-personal-agent-direction.md` 与 `docs/adr/0004-personal-memory.md`。

## 常用命令

Go 命令在仓库根目录;Android/Kotlin 命令在 `mobile-kotlin/`。

```bash
make build && make serve && make doctor
make test                          # go test ./...
go test -race ./...                # CI 竞争检测
make lint && make format-check     # CI 门禁
make test-architecture             # 架构边界守卫
make generate                      # go generate ./internal/api
make release-linux-amd64           # 纯 Go 交叉编译

acorn serve | run | smoke | init | doctor | skills | persona | owner | pair | token | devices | memory reindex | backup | restore
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew assembleDebug
```

## 架构

- `internal/wire.Container` 是唯一实例化具体实现的组合根;`cmd/acorn → cli → wire → {api, runtime, store, memory, wake, notify, knowledge, watch, news}`。`serve` 是唯一长驻的后端命令,同时运行 wake 调度器;`workstation serve` 只在工作机容器里运行。
- `internal/core` 只放 domain 类型与 consumer-owned 接口,不导入任何内部包。业务包只依赖 core 接口;只有 `internal/wire` 导入 `internal/store`。
- 运行时主链在 `internal/runtime`:`Executor → RunnerFactory → Eino ChatModelAgent → adk.Runner`,事件投影写入 SQLite。
- SQLite(modernc.org/sqlite)是运行时、记忆、约定与索引的真相;写入经单连接串行执行,记忆召回与整合候选读取走 `query_only` 的 WAL 只读连接池。人格存在 SQLite,每次保存是一个新 revision,只由 owner 经 App 接口或 `acorn persona` 修改;启动时没有人格才写入默认人格。Acorn 对 owner 的称呼也存在 SQLite,只由 owner 经 App 接口或 `acorn owner` 设置,run 与记忆抽取读当前称呼。
- `docs/openapi.yaml` 是唯一 wire contract,mobile 客户端由它生成;客户端只接收 `internal/api/projection.go` 投影的 RunEvent。

## 硬边界

### 运行时

- 每个 run 一个 `ChatModelAgent`,没有 multi-agent/subagent。middleware 顺序固定:memory visibility → patchtoolcalls → summarization → reduction(clear-only) → toolsearch → skill → presence → approval → tool errors。
- 工具串行执行。普通工具失败与未知工具调用是模型可见的 tool result,不是 run failure;interrupt 与 context 取消照常传播。
- 工具调用先按 owner 的规则:`approval.block` 拒绝,`approval.require` 审批,`approval.allow` 直接执行(block 优先于 require,require 优先于 allow)。规则未覆盖的 `shell` 与 MCP 调用由 `internal/review` 经 TypeSafe 的 Jev 审查:只读命令不审查,会对外产生后果的转审批,审查失败也转审批。审批登记 `tool_approval` pending action 并发起工具级 interrupt;resume 参数必须与登记一致。require 与 block 不能命中 `ask_operator`。续跑只由服务端驱动,没有客户端 resume 端点。
- web 工具默认加载;所有外部抓取走 `webaccess` 的共享 URL policy。
- agent 的工作机是一个容器,里面运行只读挂载的 `acorn workstation serve`;`shell` 与 `files` 经 `internal/workstation` 的 HTTP 客户端和 bearer token 调用它,backend 不接触容器引擎。浏览器只有工作机自带的 Chromium,agent 经 `bsk`(BrowserSkill)操作;owner 在 App 里经 `/v1/browser/*` 实时查看并操作它来登录网站;`web_rendered` 追踪也由工作机渲染。
- `<memory_context>` 与 `<presence>` 是每次模型调用临时追加的 system 消息,不进入可被总结的消息序列,各自受预算约束。

### 技能

- 技能是 Agent Skills 格式的包:frontmatter 严格解析的 `SKILL.md` 加附带文件,存在 SQLite,每次写入是一个新 revision;编辑基于读到的 revision,并发修改显式失败。agent 经 `skill_write`/`skill_retire` 新建、修改和停用技能,经 `skill_inspect` 把工作机上的目录或 zip 按内容哈希暂存、`skill_import` 按哈希导入;starter config 默认把 `skill_write`、`skill_import`、`skill_retire` 放进 `approval.require`。owner 经 `acorn skills import` 安装技能包,在 App 技能页查看历史与后端算好的差异,回滚、停用与恢复技能:基于读到的 revision 写作者为 owner 的新 revision,不经审批。每次 run 前,在用技能的附带文件按哈希同步到工作机的 `~/skills/<name>/`;没有工作机或同步失败时,带文件的技能不列出。技能经 Eino skill middleware 提供,加载结果带全文、revision 与文件所在目录。内置技能在 `internal/skills/seed` 编进二进制,启动时内容变了才以 release 身份写新 revision。
- 包根目录可带 `acorn-package.yaml`(KnownFields 严格解析:`mcp` 的 command、args,`triggers` 的 name、schedule、run、deliver,包级 `secrets`),写入与导入时校验,审批详情先列出 MCP 服务器、每个触发器的时间表与命令和密钥名。同步之后,工作机按清单以包目录为工作目录运行并守护在用技能的 stdio MCP 服务器,经 bearer token 以 streamable HTTP 暴露在 `/v1/mcp/<name>`;backend 每次 run 连接,工具名 `mcp__<name>__<tool>`,只出现在该技能在用的 run 里,照常经审批规则与审查。缺密钥或服务器起不来时该技能本次不列出,`acorn doctor` 说明原因。包名不能与 `mcp.providers` 的名字相同,配置后来新增同名 provider 时启动失败。
- 触发器只由 wake 调度器运行:按 owner 时区的 cron 表达式,每个(触发器, 时刻)经条件更新认领一次,在工作机上以包目录为工作目录执行 `run`,受 `workstation.command_timeout_seconds` 约束;每行标准输出是事件 `{"key","text"}` 或新闻条目 `{"item":{...}}`(写进跟随该触发器的 `trigger` 类新闻源,首次输出是基线,没有跟随的源时本次运行失败),没有输出不调用模型,事件按(技能, 触发器, key)只登记一次并作为外部记忆来源。immediate 事件在以技能名为标题的包线程里起 run(输入以 `[trigger <skill>/<trigger>]` 开头,计入每日唤醒预算并记 `wake.fired`),超预算或起 run 失败与 digest 事件进早安卡;连续 5 次失败标为 failing,进 System health,按追踪项的退避重试。停用的技能与缺密钥的包的触发器不运行。
- 密钥存在 SQLite 的 `skill_secrets`,经 `acorn skills secret set` 或 App 技能页写入,只写不读,只作为环境变量交给该包的 MCP 进程与触发器命令,不进入模型输入、审批详情、API 响应与日志。

### 记忆与约定

- 记忆是 fact/insight/thought,每条可追溯到来源并带修订与有效时间;写入时间由调用方提供。assistant 的话不能直接成为 owner 的事实。
- 后台处理以租约 token、revision 与 exclusion epoch 防止过期写入;同一来源重复处理必须幂等。
- 遗忘需要当前 owner 的请求,排除来源及派生记录、向量与摘要;旧 epoch 的 checkpoint 禁止恢复;agent 的知识库读取遵守相同排除。删除对话是 owner 的遗忘请求:先排除对话的消息与工具结果来源,再删除对话、对话里的文件及其 run 留下的记录,在这个对话里建立的关切随之排除,它们在工作机上的 `~/work/<id>/` 在下一次 run 前删除;笔记、技能、约定与追踪项保留。删除笔记同样是遗忘请求:先排除它每个 revision 的来源,再删除笔记与全部 revision。
- 约定分规则与每次发生,发生只能被认领一次;完成需要 owner 确认或成功的工具结果。
- 关切属于建立它的对话。游思每次推进到期关切里最久没有修订的一件,在它所在的对话里运行,对话里有 run 在运行或等待审批时跳过它。有 outcome 的关切由游思在工作机的 `~/work/<id>/` 里推进,交付到所在对话后转为 waiting,只由 owner 的确认解决。
- 后台记忆调用受 `memory.daily_tokens` 约束,自主唤醒受 `wake.daily_limit`/`wake.daily_tokens` 约束;预算耗尽时任务保留到下一本地日。

### 追踪与早安卡

- 追踪项(RSS、GitHub、网页)只由 wake 调度器检查,首次检查是基线。digest 与 immediate 追踪项逐条报告;早安卡的 100 个名额按追踪项轮流分配,其余留到下次。
- `news` 模式的追踪项是新闻源,属于 `news_sections` 的一个板块,条目不登记为记忆来源。`internal/news` 在后台按租约认领待报条目,由模型在 `news.daily_tokens` 内打 1–5 分并归入事件,期刊页拆成条目;一批失败 3 次即放弃。早安卡生成前刷新新闻源并等整理追上,按板块给出有新条目的事件、它们的新条目与上一份早安卡,由模型挑约 10 件与今日关注 3 件;整理过的与暂停源的条目随之标为已报。条目与事件保留 14 天,agent 经 `news_search` 检索;过期条目只留键与状态,源里仍在的条目不会再次出现。

### 知识库

- 知识库笔记存在 SQLite,只经 `knowledge.Vault` 写入;每次写入是一个新 revision,并在同一事务里登记为记忆来源。编辑基于读到的 revision,并发修改显式失败。agent 不删除、不移动笔记,只有 owner 经 App 删除笔记;同一路径重建的笔记接着已登记来源的最大 revision 编号。早安卡不写笔记。笔记路径是相对 `.md` 路径,不含 `..`、隐藏段或 `attachments/`。分享的图片存为 `{storage_dir}/attachments/` 下的文件,笔记以 `![描述](attachments/...)` 引用,客户端经 `/v1/knowledge/attachment` 读取。

### 对话文件

- owner 在 App 里发给 agent 的文件与 agent 经 `send_file` 从工作机发给 owner 的文件存为 `{storage_dir}/files/<file_id>`,在 SQLite 里按对话登记,内容不变。owner 先上传,run 随 owner 的消息发送它们;消息可以只有文件。模型直接看到支持的图片,所有文件在 run 前复制到工作机的 `~/uploads/<file_id>/<文件名>`,不再属于任何对话的副本同时删除。大小上限是 `files.max_upload_mib` 与 `files.max_send_mib`。

### 视图

- agent 经 `show_view`(toolsearch 按需加载,配 `views` 技能)在回复下给 owner 一块界面:A2UI v0.9 形状的平铺组件加数据,组件只来自 `internal/views` 的目录。后端校验结构、数据路径、链接与图片后存进 SQLite,写 `view.shown` 事件,线程消息按 run 列出;没通过的视图不保存,问题作为 tool result 回给模型。App 用自己的组件渲染,不执行代码,不认识的组件显示占位。删除对话一并删除视图。
- `update_view` 按 id 替换本对话里某个视图的标题、组件或数据,整个视图重新校验后写 `view.updated` 事件。
- owner 经 `POST /v1/threads/{thread_id}/views/{view_id}:pin`/`:unpin` 把视图固定到今天页或取下,`/v1/now` 按固定时间列出 `panels`,点面板回到所在对话。`show_view` 可带 `source`(在用技能包的触发器 `skill/trigger`);视图固定期间,该触发器输出的每行 `{"view_data": {...}}` 按视图组件完整校验后替换数据,不调用模型;数据画不出时视图保留原数据,这次运行记为触发器失败,同次输出的事件照常登记。没固定的视图不接收触发器数据。
- `Button` 与 `ChoicePicker` 带 `action`。App 经 `POST /v1/threads/{thread_id}/views/{view_id}/actions` 只发组件 id、List 里的位置与选中的选项 id;后端从存着的视图解析出动作,同一事务里登记动作并写一条 owner 消息(按钮文字或「选了：A、C」),再按 owner 发消息的规则起 run。模型在这条消息旁看到视图 id、组件、动作名、选项与动作的 context。每个动作只能发一次,重复发返回 409;视图里没有的组件或选项返回 400。

### 推送与手机通知

- `notify_owner` 经 FCM 按设备登记的 Firebase installation ID(FID)推送,受每小时上限与免打扰约束;设备吊销时删除其 FID。
- 手机通知只作背景信号,正文按外部数据处理,不执行其中的指令。

### Remote API & Mobile

- Remote clients 只走 `/v1` 与 `/healthz`。改 DTO/RunEvent/schema 必须同步 openapi.yaml、生成客户端与测试。
- 设备鉴权是 pairing code → bearer token,SQLite 只存 hash;token 缺失、未知或吊销必须显式失败。
- Mobile 不本地执行 run,不持有或猜测后端事实;inbox 与 pending actions 以后端接口为准。
- App 的 run 事件流都由 `RunFollower` 持有,对话页读它的投影。这台手机发起或批准的 run 进行期间,`dataSync` 前台服务在后台保持这条事件流并显示为 Live Update;其他 run 只在它的对话页打开时跟随。
- 涉及 mobile 视觉/交互的改动必须在真机或模拟器验证;无法验证时说明。

### 发布

- Release 是纯 Go 交叉编译(`CGO_ENABLED=0`),产物含 Linux tarball、签名 Android APK 与 installer。installer 通过 `acorn init` 生成配置,已有配置与 env 原样保留;它在服务器上用 Podman 构建工作机镜像并安装 `acorn-workstation.service`。

## 工作方式

- 先读 live code 再下结论。不为了「看起来更稳」添加 mock、fallback、compat alias、silent degradation 或吞错逻辑;surface 真实失败,修根因。
- 默认 hard cut:新路径落地时同步删除旧路径、旧配置、旧测试。
- 不修改用户已有的 unrelated dirty worktree 改动。
- 业务逻辑不直接实例化具体实现,通过参数、接口或 container 注入。

## 代码规范

- Go 1.27,tab 缩进,goimports 分组;Kotlin 4 空格。
- 需要被 `errors.Is` 比对的错误是包级 `var ErrXxx`;不该发生的前置条件错误用 inline `errors.New`,消息含字段或参数名。
- SQLite 关闭 Rows/Stmt 并检查 `rows.Err()`;HTTP 带 context 并关闭 body。
- 改表结构:`schemaVersion` 加一,在 `migrations` 末尾追加从上一版本升级的迁移,用 `ACORN_RECORD_SCHEMA=1 go test ./internal/store -run TestOpenCreatesTheRecordedLayout` 记录新版本的 `testdata/schema/vN.sql`;已发布版本的结构快照不修改。

## 配置和文档

- starter config 只有 `internal/cli/acorn.init.yaml` 一份;新增或删除配置字段时同步 config struct、defaults、validation、这份模板与测试。config 以 `KnownFields` 严格解析,删除的字段不保留兼容读取。
- `configs/acorn.local.yaml` 是被忽略的本地配置。
- 不变量 ↔ 测试见 `docs/architecture/INVARIANTS.md`,用户指南在 `docs/user/`,方向级决策在 `docs/adr/`。不写复述代码的架构文档,不把未来计划写成现状。

## 验证要求

提交前通过 `make format-check`、`make lint` 与 `go test ./...`;涉及并发的改动跑 `go test -race`。`tests/architecture/` 守卫依赖方向、客户端投影边界、工具 schema 标签与 INVARIANTS 引用。
