# AGENTS.md

Acorn 的 AI 协作硬约束入口。`CLAUDE.md` 软链接至此,单一真相源。

## 项目概览

Go 1.26 + Eino ADK 的单用户自托管 AI agent 后端,module `github.com/ycvk/acorn`。owner 在 VPS 跑后端,Kotlin App 配对手机后远程发起任务、看运行、批审批。入口:operator CLI(`serve` 长驻 / `run`·`smoke` 一次性 direct_response / `init`·`pair`·`devices`·`token` 运维 / `skills`·`memory`·`doctor` 诊断)、authenticated `/v1` API、mobile inbox、persisted RunEvent SSE、Kotlin mobile。

## 常用命令

Go 命令在仓库根目录;Android/Kotlin 命令在 `mobile-kotlin/`。

```bash
make build && make serve && make doctor
make test                          # go test ./...
go test -race ./...                # CI 竞争检测版本
make lint && make format-check     # CI 门禁
make test-architecture             # 架构边界守卫
make generate                      # go generate ./internal/web
make release-linux-amd64           # 纯 Go 交叉编译(无 CGO)

# acorn CLI(根目录 acorn / make build 产出 ./bin/acorn)
acorn serve [-c path] [--listen addr]   # 长驻 remote API,唯一常驻命令
acorn run [-c path] [--json] "task"      # 一次性 direct_response 执行
acorn smoke [-c path] [--json] "task"   # 安装探活:真实跑一次 run,非零退出即失败
acorn init [-c path] [--force] [--print] # 生成 starter config
acorn doctor [-c path] [--json]          # 能力快照 + MCP 健康探活
acorn skills {list|inspect|check|create|patch|delete} [-c path] [--json]
acorn memory semantic rebuild [-c path]  # 重建 embedding 向量库(memory_vectors)
acorn pair [-c path] [--qr] [--server-url url]  # 生成设备配对码
acorn token issue [-c path] [--name n] [--ttl d]  # 颁发 device token
acorn devices {list|revoke} [-c path]

# Mobile(在 mobile-kotlin/)
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check   # CI 门禁
./gradlew assembleDebug  # in mobile-kotlin/
```

## 架构大图

- **组合根**:`internal/app.Container` 是唯一实例化具体实现的地方(SQLite store、RunnerFactory、embedding client)。`cmd/acorn → cli → app.Container → {web, runtime, store}`。`serve` 是唯一长驻命令。
- **运行时主链**:`Executor → RunnerFactory.buildRun → ContextPlane + direct_response → ContextSession → SQLite/file-backed memory`。
- **单一编排模式**:`direct_response`。model → tool loop → record → 下一轮。`AgentLoop.RunOneIteration` 每轮 `BeforeModelCall → RunActionRound(ExecuteRound) → RecordAssistant/RecordToolResults`。plan_execute/single_agent/child_agent/verifier 已全部删除。
- **职责边界**:`internal/runtime` 做装配+执行编排;ContextPlane(`internal/contextplane`)拥有上下文事实(首轮装配、tool lifecycle、`ContextSession`)。
- **关键包**:`internal/domain` 拥有核心 domain 类型(`RunRecord`/`EventRecord`/`SessionRecord`/`PendingActionRecord`/`SessionSummary`)+ context plumbing(`WithRunID`/`WithSessionID`/`WithCallSite`)+ ports(`EventAppender`/`RunContextBridge`/`ToolCallContextBridge`);`internal/toolkit` 拥有 `ToolContract`/`Catalog`/`ToolSpec`;`internal/toolset` 拥有工具实现(file/git/browser/web/command/artifact);`internal/runtime` 拥有 `StreamItem` + 投影逻辑;`internal/clientevents` 把 live RunEvent 投影为 mobile live subset;`internal/workspace` 拥有 mutation checkpoint + worktree;`internal/webaccess` 拥有 `web_search`/`web_fetch`/`browser` 工具与共享 URL policy;`internal/providers/mcp` 拥有 MCP provider lifecycle + OAuth token store + pending action store。
- **两套真相**:SQLite(`internal/store`,modernc.org/sqlite,单连接串行化)是 runtime 真相(12 张表:runs/events/sessions/session_messages/pending_actions/mcp_oauth_tokens/owner_profile/devices/pairing_codes/artifacts/session_summaries/schema_migrations;schema 在 `store/sqlite/store_schema.go`,`schemaRequiredTables` 强制列存在、缺列 fail-loud);文件型长期记忆(`internal/memory`)是 `facts/`/`history/`;embedding 向量存 SQLite `memory_vectors` 表。
- **API 契约**:`docs/openapi.yaml` 是唯一 wire contract,`mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/` 由它生成。客户端只收 `internal/clientevents` 投影的 live RunEvent;RunEvent SSE 用 `follow=true` 轮询 + `after_seq` 游标续读。

## 硬边界

### 运行时 & 编排

- 只有一个 root mode:`direct_response`。不存在 plan_execute/single_agent/child_agent/subagent。
- `ContextSession` 是 root-run model input 的唯一 owner,不允许绕过它维护第二套 message lifecycle。
- 工具执行:`ToolContract → toolExecutionScheduler`,按 parallel policy(read_only 并行 / serial 串行)执行。普通工具失败是模型可见 failed result,不是 run failure。
- workspace mutation 恢复是 scoped checkpoint + 显式 `rollback_workspace_checkpoint`。

### 工具 & 技能

- native skill truth 是 `internal/skills` file-backed loader。repo `./skills` 是 release seed pack;release installer 安装到 `~/.acorn/skills`;generated skills 写入 `{storage_dir}/skills/generated`;workspace skills 写入 `./.acorn/skills/workspace`。**不要把 generated skill 写回 repo root `skills/`**。
- skill 是只读 markdown + 简单关键词匹配,无 lifecycle/evidence/assess。

### 记忆 & 检索

- 长期 memory 是 `internal/memory` 的 file-backed `facts/`/`history/`。Canonical Memory Record V2 frontmatter(简化:status / tags / created / updated / source_run / source_refs)。fact 写入走结构化 `remember` 工具;raw `memory_create_file` 仍要求完整 frontmatter。
- 语义检索配置 `memory.semantic.embedding` 是语义检索前提,但**惰性接线**:embedder/VectorStore 在首次 `Search`/`Prepare`/rebuild 时才构造,serve 启动不被阻塞。语义检索是可选增强,不是发任务的前置闸门。未配置 embedding 时 `Prepare` 降级为返回空 memory 结果(零召回是合法 baseline)。
- 语义检索实现:OpenAI embedding 调用 → SQLite `memory_vectors` 表 BLOB 存储 → 纯 Go 暴力余弦相似度检索。零 CGO。
- **不要引入 pgvector/LanceDB/Bleve/FAISS 或第二套 retrieval store**。

### 上下文 & 压缩

- **Hybrid context 方案**(替代旧 CompactionEngine):
  1. **Observation masking**:tool result 超 `mask_after_turns`(默认 2)轮后用占位符替换。纯内存操作,不写 SQLite。
  2. **LLM auto-compact**:token 超 `window_tokens - compact_margin`(默认 margin 13000)时用一次 model 调用生成 summary,替换旧消息。circuit breaker:连续 3 次失败停止。
  3. **关键上下文 re-inject**:compact 后从 disk/memory 重新注入 system prompt + memory context + skill context。
- `ContextSession` 接口:`Bootstrap` / `BeforeModelCall`(masking + auto-compact) / `RecordAssistant` / `RecordToolResults`。无 `ReactiveCompact`。
- context boundary 不持久化(compact 边界是内存状态)。

### Remote API & Mobile

- remote client wire contract 是 `docs/openapi.yaml`。Remote clients 只走 `/v1`、`/healthz`。`/mcp` server mode 已删除。改 mobile DTO/RunEvent/OpenAPI schema 必须同步 openapi.yaml、generated client 和相关测试。
- auth 是 single-owner device auth:pairing code → bearer token,SQLite 只存 hash。token 缺失/未知/revoked 必须显式失败。
- mobile inbox truth 是 `GET /v1/inbox`,后端聚合 pending actions + active/recent runs + system status。
- pending approval truth 是 `GET /v1/pending-actions` + `:decide`,消费 SQLite `pending_actions`。
- Mobile 不本地执行 run、不持 runtime truth、不从 local state 猜测后端事实。mobile memory 只消费 `/v1/memory/*`。
- 涉及 mobile 视觉/交互的改动必须在真机或模拟器验证;无法连接设备时必须说明未验证。

### 自托管发布

- GitHub Release 预构建 tarball + Linux binary + signed Android APK + `systemd`。Release build 是纯 Go 交叉编译(`CGO_ENABLED=0`),无 FAISS/CGO/build tags。
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
- public context config 只保留 `window_tokens`、`compact_margin_tokens`、`mask_after_turns`、`preserve_recent_turns`。删除配置字段不保留兼容读取。
- 架构现状 → `docs/architecture/`,用户指南 → `docs/user/`,开发者指南 → `docs/dev/`。不要把未来计划写成 current truth。

## 验证要求

提交前必须通过 `make format-check` 和 `make lint`。context/runtime 改动至少跑 `go test ./internal/config ./internal/contextplane ./internal/orchestration ./internal/runtime ./internal/cli ./internal/toolkit ./internal/toolset ./internal/store ./internal/memory ./internal/app ./internal/web`。

**CI 守卫**(`tests/architecture/`):`store_boundary_test.go`(只允许 container.go import sqlite)、`structural_limits_test.go`、`client_projection_boundary_test.go`。
