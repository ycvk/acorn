# AGENTS.md

Acorn 的 AI 协作硬约束入口。`CLAUDE.md` 软链接至此,单一真相源。

## 项目概览

Go 1.27 + Eino ADK 的单用户自托管 AI agent 后端,module `github.com/ycvk/acorn`。owner 在 VPS 跑后端,Kotlin App 配对手机后远程发起任务、看运行、批审批。入口:operator CLI(`serve` 长驻 / `run`·`smoke` 一次性 run / `init`·`pair`·`devices`·`token` 运维 / `skills`·`memory`·`doctor` 诊断)、authenticated `/v1` API、cron triggers、mobile inbox、persisted RunEvent SSE、Kotlin mobile。方向见 `docs/adr/0003-personal-agent-direction.md`。

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
acorn init [-c path] [--force] [--print] # 生成 starter config
acorn doctor [-c path] [--json]          # 能力快照 + MCP 健康探活
acorn skills {list|inspect|check|create|patch|delete} [-c path] [--json]
acorn pair [-c path] [--qr] [--server-url url]  # 生成设备配对码
acorn token issue [-c path] [--name n] [--ttl d]  # 颁发 device token
acorn devices {list|revoke} [-c path]

# Mobile(在 mobile-kotlin/)
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check   # CI 门禁
./gradlew assembleDebug  # in mobile-kotlin/
```

## 架构大图

- **组合根**:`internal/wire.Container` 是唯一实例化具体实现的地方(SQLite store、RunnerFactory)。`cmd/acorn → cli → wire.Container → {api, runtime, store}`。`serve` 是唯一长驻命令。
- **运行时主链**:`Executor → RunnerFactory.buildRun → ContextPlane(memory/skill 上下文) → buildAgentRunner(Eino ChatModelAgent) → adk.Runner`,事件经 `agentEventProjector` 写 SQLite events。全部在 `internal/runtime`。
- **单一编排模式**:每个 run 一个 Eino `ChatModelAgent`(ReAct:model → tools → model),`EnableStreaming`,checkpoint 经 `core.SessionStore` 落 SQLite `agent_checkpoints`。
- **职责边界**:`internal/runtime` 做装配(工具 catalog、memory/skill 上下文写进 Instruction、middleware 链)+ 执行(run/resume)+ StreamItem 投影;上下文压缩、延迟加载工具、工具调度交给 Eino middleware 与 ToolsNode。
- **关键包**(14 个 internal 包):`internal/core`(Layer 0,零内部导入,纯类型+契约:核心 domain 类型 + context plumbing + 3 个 store 接口 + 工具契约,无 service struct);`internal/runtime`(Layer 3)拥有 Executor、RunnerFactory、buildAgentRunner、approval/tool-error middleware、ContextPlane、StreamItem 投影;`internal/tools` 拥有工具实现(artifact/operator/search_runs/worldstate/web/browser 工具,以及只服务 memory 根目录的文件工具 + ToolRegistry);`internal/store` 拥有 SQLite adapter + ArtifactService(依赖 `core.ArtifactService`,无重复接口);`internal/memory` 拥有 file-backed memory + WorldState;`internal/mcp` 拥有 MCP provider manager;`internal/triggers` 拥有 serve 进程内的 cron trigger scheduler;`internal/api` 拥有 `/v1` client surface + live RunEvent 投影(`projection.go`);`internal/workspace` 拥有 memory 文件写入用的路径约束 + mutation checkpoint(不依赖 git);`internal/webaccess` 拥有 web fetcher、Tavily search、内容抽取与共享 URL policy(工具本身在 `internal/tools`);`internal/skills`/`internal/config`/`internal/cli` 各司其职。
- **两套真相**:SQLite(`internal/store`,modernc.org/sqlite,单连接串行化)是 runtime 真相(11 张表:runs/events/sessions/session_messages/pending_actions/mcp_oauth_tokens/devices/pairing_codes/artifacts/agent_checkpoints/schema_migrations;schema 在 `store/store_schema_bootstrap.go`,`schemaRequiredTables` 强制列存在、缺列 fail-loud);文件型长期记忆(`internal/memory`)是 `facts/`/`history/`。
- **API 契约**:`docs/openapi.yaml` 是唯一 wire contract,`mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/` 由它生成。客户端只收 `internal/api/projection.go` 投影的 live RunEvent;RunEvent SSE 用 `follow=true` 轮询 + `after_seq` 游标续读。

## 硬边界

### 运行时 & 编排

- 每个 run 只有一个 `ChatModelAgent`,不存在 multi-agent/subagent。agent 状态由 Eino 持有,run 间历史来自 `session_messages`(最近 12 条 user/assistant 文本)。
- middleware 顺序固定:patchtoolcalls → summarization → reduction(clear-only) → toolsearch(有 deferred 工具时) → approval → tool errors。越靠前包得越外层。
- 工具串行执行(`ExecuteSequentially`)。普通工具失败与调用不存在的工具都是模型可见的 tool result,不是 run failure;interrupt 与 context 取消照常传播。
- 审批:`approval.require` 是工具名 glob 列表(默认 `browser`、`mcp__*`)。命中的调用由 approval middleware 登记 `tool_approval` pending action 并发起工具级 interrupt;resume 时参数必须与登记时一致,accept 才执行,decline 把拒绝说明作为工具结果返回。approval 自身的存储失败直接让 run 失败。
- 续跑由服务端驱动:`RunResumeService.ResumeIfReady` 只在 run 为 interrupted 且没有 pending 的 pending action 时续跑;每次决策后和每次 run 停在 interrupted 时各检查一次,in-flight 集合防重复。
- web 工具(`web_search`/`web_fetch`/`browser`)是 deferred,经 toolsearch middleware 的 `tool_search`(`select:<name>`)加载;共享 URL policy 只放行公网 http(s),private 网段需 `web_access.allow_private_networks`。

### 工具 & 技能

- native skill truth 是 `internal/skills` file-backed loader,`tools.workspace.root_dir` 指向存放 seed skills 与 workspace skills 的目录。repo `./skills` 是 release seed pack;release installer 安装到 `~/.acorn/skills`;generated skills 写入 `{storage_dir}/skills/generated`;workspace skills 写入 `./.acorn/skills/workspace`。**不要把 generated skill 写回 repo root `skills/`**。
- skill 是只读 markdown + 简单关键词匹配,无 lifecycle/evidence/assess。

### 记忆 & 检索

- 长期 memory 是 `internal/memory` 的 file-backed `facts/`/`history/`,按 ADR-0002 三层:Active Memory(每个 run 注入的 facts 快照)+ Archive(检索)+ Periodic Review(每 N 个 run 异步蒸馏 facts)。跨 run 状态是 `WorldState`(`{storage_dir}/worldstate/state.json`,只经 `ApplyDelta` 变更)。Canonical Memory Record V2 frontmatter(简化:status / tags / created / updated / source_run / source_refs)。fact 写入走结构化 `remember` 工具;raw `memory_create_file` 仍要求完整 frontmatter。
- **混合检索(opt-in)**:`memory.embedding.enabled` 开启时,写入自动生成 embedding(modernc.org/sqlite 内置 sqlite-vec,存 `{storage_dir}/vectors.db`),检索走 vector KNN + keyword RRF 融合(k=60)。复用 primary provider 的 OpenAI 兼容 `/v1/embeddings` 端点,不引入新依赖/新进程。关闭时(默认)退化为纯 keyword 检索,零行为变化。

### 上下文 & 压缩

- summarization middleware:token 超 `window_tokens - compact_margin_tokens` 时同步用一次 model 调用总结历史。
- reduction middleware 只做 clear:总 token 超 `window_tokens / 2` 时把较早的工具结果替换成占位符,最近 `mask_after_turns` 轮工具调用原样保留。
- memory 快照与 skill 目录等每个 run 的上下文写进 agent Instruction,不进入可被总结的消息序列。

### Remote API & Mobile

- remote client wire contract 是 `docs/openapi.yaml`。Remote clients 只走 `/v1`、`/healthz`。`/mcp` server mode 已删除。改 mobile DTO/RunEvent/OpenAPI schema 必须同步 openapi.yaml、generated client 和相关测试。
- auth 是 single-owner device auth:pairing code → bearer token,SQLite 只存 hash。token 缺失/未知/revoked 必须显式失败。
- mobile inbox truth 是 `GET /v1/inbox`,后端聚合 pending actions + active/recent runs + system status。
- pending approval truth 是 `GET /v1/pending-actions` + `:decide`,消费 SQLite `pending_actions`(kind:`elicitation`、`operator_question`、`tool_approval`)。决策后由服务端续跑,不存在客户端 resume 端点。
- Mobile 不本地执行 run、不持 runtime truth、不从 local state 猜测后端事实。mobile memory 只消费 `/v1/memory/*`。
- 涉及 mobile 视觉/交互的改动必须在真机或模拟器验证;无法连接设备时必须说明未验证。

### 自托管发布

- GitHub Release 预构建 tarball + Linux binary + signed Android APK + `systemd`。Release build 是纯 Go 交叉编译(`CGO_ENABLED=0`),无 CGO/build tags。
- installer 安装 `/opt/acorn`、`~/.acorn/skills`、`/usr/local/bin/acorn` wrapper;默认读 `~/.acorn/acorn.yaml`;root VPS 用 `/root/.acorn`,workspace 是 `/srv/acorn/workspace`。

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

提交前必须通过 `make format-check` 和 `make lint`。core/runtime 改动至少跑 `go test ./internal/core ./internal/runtime ./internal/cli ./internal/tools ./internal/store ./internal/memory ./internal/mcp ./internal/wire ./internal/api`。

**CI 守卫**(`tests/architecture/`):`structural_limits_test.go`、`client_projection_boundary_test.go`、`store_interface_count_test.go`、`dependency_direction_test.go`、`docs_structure_test.go`、`shipped_artifacts_test.go`。
