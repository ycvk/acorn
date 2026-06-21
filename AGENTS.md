# AGENTS.md

Acorn 的 AI 协作硬约束入口。`CLAUDE.md` 软链接至此,单一真相源。

## 项目概览

Go 1.26 + Eino ADK 的单用户自托管 AI agent 后端,module `github.com/ycvk/acorn`。owner 在 VPS 跑后端,Flutter App 配对手机后远程发起任务、看运行、批审批。入口:CLI、authenticated `/v1` API、mobile inbox、persisted RunEvent SSE、Flutter mobile。

## 常用命令

Go 命令在仓库根目录;Flutter 命令在 `mobile/`。

```bash
make build && make serve && make doctor
make test                          # go test ./...(普通构建,无需 FAISS)
go test -race ./...                # CI 竞争检测版本
make lint && make format-check     # CI 门禁
make test-architecture             # 架构边界守卫
make generate                      # go generate ./internal/web

# Mobile(在 mobile/)
python3 mobile/tool/generate_openapi_client.py --check   # CI 门禁
flutter test && flutter analyze && flutter build apk --debug

# FAISS 本地 dev(make build/test 不含 FAISS)
make dev-faiss-artifacts && make dev-build-faiss && make dev-serve-faiss
```

## 架构大图

- **组合根**:`internal/app.Container` 是唯一实例化具体实现的地方(SQLite store、RunnerFactory、Bleve/FAISS、OpenAI client)。`cmd/acorn → cli → app.Container → {web, runtime, store}`。`serve` 是唯一长驻命令。
- **运行时主链**:`Executor → RunnerFactory.buildRun → run selection + ContextPlane + OrchestrationPlane → ContextSession → SQLite/file-backed memory`。
- **3 个 root mode**(`resolveRootOrchestrationMode`,`internal/runtime/resume.go`):显式 mode 优先 → 有 `parent_run_id` 走 `single_agent` → 有 `skill_id` 走 `plan_execute` → 默认 `direct_response`。
- **职责边界**:OrchestrationPlane(`internal/orchestration`)做装配+执行编排;ContextPlane(`internal/contextplane`)拥有上下文事实(首轮装配、tool lifecycle、tool-result ledger、`ContextSession`)。
- **两套真相**:SQLite(`internal/store`,modernc.org/sqlite,单连接串行化)是 runtime 真相(~23 张表,schema 在 `store/sqlite/store_schema.go`,缺列 fail-loud);文件型长期记忆(`internal/memorymodule`)是 `facts/`/`skills/`/`history/`;Bleve+FAISS 是可重建语义索引。
- **API 契约**:`docs/openapi.yaml` 是唯一 wire contract,`mobile/lib/src/api/acorn_api.dart` 由它生成。客户端只收 `internal/clientevents` 投影的 live RunEvent;RunEvent SSE 用 `follow=true` 轮询 + `after_seq` 游标续读。

## 硬边界

### 运行时 & 编排

- public root modes:`direct_response`、`plan_execute`;`single_agent` 是内部 child-run 模式。
- `ContextSession` 是 root-run model input 的唯一 owner,不允许绕过它维护第二套 message lifecycle。
- 工具执行:`ToolContract → ToolExecutionScheduler → ToolResultLedger`,按 parallel policy + 路径冲突执行。tool result refs、side effects、plan evidence backlinks 是持久化事实。普通工具失败是模型可见 failed result,不是 run failure。
- workspace mutation 恢复是 scoped checkpoint + 显式 `rollback_workspace_checkpoint`。
- plan_execute subagent 递归深度受 `agent.max_subagent_depth`(默认 3)限制。

### 工具 & 技能

- native skill truth 是 `internal/skills` file-backed loader。repo `./skills` 是 release seed pack;release installer 安装到 `~/.acorn/skills`;generated skills 写入 `{storage_dir}/skills/generated`;workspace skills 写入 `./.acorn/skills/workspace`。**不要把 generated skill 写回 repo root `skills/`**。
- `skill_assess` 是唯一 active runtime lifecycle action;`skill.lifecycle` 是 RunEvent visibility truth,OpenAPI/mobile 必须同步。
- `lifecycle_status: verified` 对非 builtin skill 必须有 `evidence_refs`。

### 记忆 & 检索

- 长期 memory 是 `internal/memorymodule` 的 file-backed `facts/`/`skills/`/`history/`。Canonical Memory Record V2 frontmatter。fact 写入走结构化 `remember`/`CreateFact` 工具;raw `memory_create_file` 仍要求完整 frontmatter。
- 语义检索配置 `memory.semantic` 是语义检索前提,但**惰性接线**:embedder/FAISS 在首次 `Search`/`Prepare`/rebuild 时才构造,serve 启动不被阻塞。语义检索是可选增强,不是发任务的前置闸门。未配置 embedding 时 `Prepare` 降级为返回空 memory 结果(零召回是合法 baseline)。
- **不要引入 pgvector/LanceDB 或第二套 retrieval store**。

### 上下文 & 压缩

- `BudgetGovernor` 是 context pressure 唯一计算入口。context assembly/rehydration 预算必须用后端统一 token counter。
- `CompactionEngine` 拥有 compact 规则;post-compact rehydration 拥有 packet 恢复。
- `ContextBoundary`(SQLite `context_boundaries`)是 durable compact/resume boundary truth。
- reactive compact 只处理真实 provider context overflow,只允许同 provider 一次重试。

### Remote API & Mobile

- remote client wire contract 是 `docs/openapi.yaml`。Remote clients 只走 `/v1`、`/healthz`、serve-time `/mcp`。改 mobile DTO/RunEvent/OpenAPI schema 必须同步 openapi.yaml、generated client 和相关测试。
- auth 是 single-owner device auth:pairing code → bearer token,SQLite 只存 hash。token 缺失/未知/revoked 必须显式失败。
- mobile inbox truth 是 `GET /v1/inbox`,后端聚合 pending actions + active/recent runs + system status。
- pending approval truth 是 `GET /v1/pending-actions` + `:decide`,消费 SQLite `pending_actions`。
- Mobile 不本地执行 run、不持 runtime truth、不从 local state 猜测后端事实。mobile memory 只消费 `/v1/memory/*`。
- 涉及 mobile 视觉/交互的改动必须在真机或模拟器验证;无法连接设备时必须说明未验证。

### 自托管发布

- GitHub Release 预构建 tarball + Linux binary + signed Android APK + `systemd`。Release build 固定 `-tags "bleve_faiss vectors"` + FAISS libs。**缺 artifact/CGO/build tags 显式失败,不发布 non-FAISS fallback**。
- installer 安装 `/opt/acorn`、`~/.acorn/skills`、`/usr/local/bin/acorn` wrapper;默认读 `~/.acorn/acorn.yaml`;root VPS 用 `/root/.acorn`,workspace 是 `/srv/acorn/workspace`。

## 工作方式

- 先读 live code 再下结论。不为了「看起来更稳」添加 mock、fallback、compat alias、silent degradation 或吞错逻辑;surface 真实失败,修根因不修症状。
- 用户明确允许 destructive rewrite 时默认 hard cut:新路径落地时同步删除旧路径、旧配置、旧测试。
- 不要修改用户已有的 unrelated dirty worktree 改动。
- 业务逻辑不硬 new concrete implementation,通过参数、接口或 container 注入。

## 代码规范

 - Go 1.26,tab 缩进;前端 2 空格;import 按 goimports 分组。
 - error 必须显式处理,分两类:
   - **Exported sentinel error**(需要被 `errors.Is` 比对):必须是包级 `var ErrXxx = errors.New(...)` 或 `fmt.Errorf("...: %w", ...)`;命名 `ErrXxx`;放在定义它的包的 errors.go 或对应文件顶部。
   - **Precondition/internal-config error**(不该发生的编程错误:依赖未注入、配置缺失、前置条件违反):用 inline `errors.New("...")` 直接返回,不需要 `errors.Is` 比对;消息要可定位(含字段名/参数名)。
 - SQLite 关闭 Rows/Stmt 并检查 `rows.Err()`;HTTP 带 context,关闭 body。

## 配置和文档

- `configs/acorn.local.yaml` 在 `.gitignore` 中。`configs/acorn.example.yaml` 和 `configs/acorn.selfhosted.example.yaml` 修改时同步 config struct、defaults、validation 和 tests。
- provider `api_key` 与 `memory.semantic.embedding.api_key` 都支持环境变量展开;embedding key 是独立 key,不能从 chat provider key 静默复用。
- public context config 只保留 `window_tokens`、`compact_margin_tokens`、`preserve_recent_turns`、`summary_max_tokens`。删除配置字段不保留兼容读取。
- 架构现状 → `docs/architecture/`,用户指南 → `docs/user/`,开发者指南 → `docs/dev/`。不要把未来计划写成 current truth。

## 验证要求

提交前必须通过 `make format-check` 和 `make lint`。context/runtime 改动至少跑 `go test ./internal/config ./internal/contextplane ./internal/orchestration ./internal/runtime ./internal/cli`。

**CI 守卫**(`tests/architecture/`):`store_boundary_test.go`(只允许 container.go import sqlite)、`bleve_faiss_release_guard_test.go`、`client_projection_boundary_test.go`。

## Harness 自演化系统

Acorn 自带 AI 协作 harness。运行细节以 `.claude/skills/<name>/SKILL.md` 为真相源。

- 状态在 `.acorn/harness/state/current.md`,模块记忆在 `.acorn/harness/memory/modules/`,架构决策在 `.acorn/harness/memory/decisions/`。
- reflexion 在改 ≥2 模块 / 改了 web·openapi·mobile / 测试非全绿 / 用户负面反馈时触发。meta-review:计数 ≥2 的 pattern 可自动升级为 RISK;硬约束升级与新建决策必须等人确认。
- 新 skill 走「AI 生成 → skill-validator 验不变量 → 人确认后激活」三层。skill 不变量:不修改 AGENTS.md 或 hooks.json、无 destructive 命令、无循环依赖。
