# AGENTS.md

本文件是 Acorn 的 **AI 协作硬约束入口**,同时也是 Claude Code 的指南——`CLAUDE.md` 软链接至本文件,**单一真相源,只维护这一份**。所有临时修复、重构、文档更新和提交前检查默认遵守本文件。

## 项目概览

Acorn 是 **Go 1.26 + 字节 Eino ADK 的单用户自托管 AI agent 后端**,module `github.com/ycvk/acorn`;当前产品主线是 mobile control surface。一个 owner 在自己的 VPS 上跑后端,用 Flutter App 配对手机后远程发起任务、看运行、批审批、管理 agent 的持久状态。当前可用入口:CLI、authenticated `/v1` remote client API、mobile inbox aggregate、persisted RunEvent SSE、Flutter mobile MVP。

## 常用命令

Go 命令在仓库根目录执行;Flutter 命令在 `mobile/` 下执行。

```bash
# 构建与运行
make build                 # 编译 ./bin/acorn(普通构建,不含 FAISS)
make serve                 # build 后用 CONFIG 启动 /v1 API(默认 configs/acorn.local.yaml)
make doctor                # build 后跑环境自检
./bin/acorn pair --server-url <url> --qr   # 生成一次性配对码 + 终端二维码
make generate              # 重新生成签入的生成代码(go generate ./internal/web)

# 测试 / lint / format(= CI Go 门禁:format-check → lint → test -race)
make test                          # go test ./...(普通构建,无需 FAISS)
go test -race ./...                # CI 用的带竞争检测版本
go test ./internal/runtime         # 单个包
go test -run TestXxx ./internal/runtime   # 单个测试函数
make test-architecture             # 仅架构边界守卫(tests/architecture)
make lint                          # golangci-lint run ./...
make format / make format-check    # goimports+gofmt 自动修复 / 只检查(CI 用)

# Mobile(在 mobile/ 下)
python3 mobile/tool/generate_openapi_client.py --check   # 校验 Dart client 与 openapi 同步(CI 门禁)
python3 mobile/tool/generate_openapi_client.py           # 改 openapi 后重新生成 acorn_api.dart
flutter pub get && flutter test && flutter analyze && flutter build apk --debug

# 发布
make release-linux-amd64 / make release-linux-arm64      # release tarball 到 ./dist(内嵌 FAISS .so)
```

**FAISS 陷阱**:普通 `make build`/`make test` **不带** `bleve_faiss vectors` build tags,此时语义检索 fail-loud(`NewBleveSemanticIndex` 返回 `ErrBleveFAISSSupportNotBuilt`)。绝大多数单测不碰 FAISS,普通 `make test` 即可;要真正运行 memory 语义检索(`serve`/`doctor`/`acorn memory semantic rebuild`)用 macOS arm64 本地 dev 链:

```bash
make dev-faiss-artifacts   # 从 pinned commit 构建本地 FAISS .so 到 .artifacts/faiss-native(一次性)
make dev-build-faiss       # 构建带 FAISS tags 的 ./bin/acorn-dev
make dev-serve-faiss / make dev-doctor-faiss   # 用 FAISS dev 二进制 serve / doctor
```

## 架构大图

- **单一组合根 + 依赖倒置**:`internal/app.Container`(`internal/app/container.go`)是唯一实例化具体实现(SQLite store、RunnerFactory、Bleve/FAISS、OpenAI client)的地方;其余层只依赖窄接口(`store_ports.go`/`container_store_ports.go` 把同一个 `*sqlite.Store` 切成各消费方所需方法子集)。`cmd/acorn → internal/cli → app.Container → {web, runtime, store, ...}`;`serve` 是唯一长驻命令。
- **运行时主链**:`Executor → RunnerFactory/runBuilder → run selection policy + ContextPlane + OrchestrationPlane → ContextSession → SQLite/file-backed memory`。
- **3 个 root mode**(`resolveRootOrchestrationMode` 纯函数路由,`internal/runtime/resume.go`):显式 mode 优先 → 有 `parent_run_id` 走 `single_agent` → 有 `skill_id` 走 `plan_execute` → 默认 `direct_response`。`plan_execute` 的 ActNode 与 `single_agent` 共用 `internal/orchestration` 的 `ExecuteRound` 回合原语。
- **职责边界**:OrchestrationPlane(`internal/orchestration`)只做「装配+执行编排」,不拥有上下文事实;ContextPlane(`internal/contextplane`)拥有「上下文事实」(首轮装配、tool lifecycle、tool-result ledger、`ContextSession`)。
- **两套真相,不要混**:SQLite(`internal/store`,纯 Go `modernc.org/sqlite`,单连接串行化)是 runtime 真相(runs/events/tool_results/pending_actions/context_boundaries/artifacts 元数据/devices 等约 23 张表;schema 集中在 `store/sqlite/store_schema.go`,打开时严格校验列存在,**缺列 fail-loud**);文件型长期记忆(`internal/memorymodule`)是 `facts/`/`skills/`/`history/`;Bleve+FAISS 只是可重建语义索引,不是 SQLite 真相也不是 L0 memory truth。
- **API 契约 & 事件流**:`docs/openapi.yaml` 是唯一 wire contract,`mobile/lib/src/api/acorn_api.dart` 由它生成。客户端只收 `internal/clientevents` 投影的 13 类 live RunEvent(tool/memory/plan/MCP/subagent 等诊断事件对客户端隐藏);RunEvent SSE 用 `follow=true` 轮询(非 WebSocket)+ `after_seq` 游标续读。

---

## 硬边界:当前项目真相

> 改行为前先读 live code、确认主调用链和持久化事实边界。以下是不可破坏的边界。

### 运行时 & 编排

- public root modes 是 `direct_response`、`plan_execute`;`single_agent` 只作为内部 child-run / verifier / eval 执行模式,不对外暴露。
- `ContextSession` 是 root-run model input 的唯一 owner,root mode 不允许绕过它维护第二套 message lifecycle。
- 工具执行合同是 `ToolContract -> ToolExecutionScheduler -> ToolResultLedger`(合同/策略定义在 `internal/tooling`,调度由 `SafeParallelToolsNode`/`internal/runtime/tool` 按 parallel policy + 路径冲突执行);tool result refs、side effects、plan evidence backlinks 是持久化事实。普通工具失败是模型可见 failed result(让模型自纠),不是 run failure。
- workspace mutation 恢复是 scoped mutation checkpoint + 显式 `rollback_workspace_checkpoint`,不是旧 snapshot / auto rollback。

### 工具 & 技能

- native skill truth 是 `internal/skills` file-backed loader:repo `./skills` 是 release seed pack 源和本地开发 builtin source;release installer 把 bundled seed skills 安装到 installer-owned `~/.acorn/skills`;generated skills 写入 `{runtime.storage_dir}/skills/generated`,workspace skills 写入 `./.acorn/skills/workspace`;**不要把 generated skill 写回 repo root `skills/`**。
- procedure 使用事实是 `memorymodule` file-backed skills + `procedure.activation` RunEvent;verifier 是只读 child run contract。executable native skills 归 `internal/skills`,learned procedures 归 `internal/memorymodule/skills` 的 `ProcedureRecord`,两者可互相引用但不能混成一个 durable truth。
- `lifecycle_status: verified` 对非 builtin skill 必须有 `evidence_refs`;无 evidence 只能是 `draft`/`unverified`/`needs_eval`。skill 质量默认由 LLM 基于 durable evidence 判断,不要求用户确认「好不好」;用户 override 必须是显式、可审计的 lifecycle action。
- `skill_assess` 是唯一 active runtime lifecycle action;`skill_create`/`skill_assess`/`skill.lifecycle` RunEvent 维护生命周期。**不要恢复 `skill_eval`/`skill_curate` / judge child-run 评测平台,不要把 gbrain skillpack 格式当兼容目标**。
- skill health / routing fixture 是 deterministic 检查和 operator contract,不是 lifecycle promotion;release seed updates 走 release installer。
- `skill.lifecycle` 是 RunEvent visibility truth;OpenAPI/mobile client/projection 必须同步,不允许客户端从 prose 推断 skill 状态。

### 记忆 & 检索

- 长期 memory 是 `internal/memorymodule` 的 file-backed `facts/`、`skills/`、`history/`。Canonical Memory Record V2 frontmatter 必须承载 status/tags/created/updated、validity window、`source_run`、`source_refs`、`evidence_refs` 和 typed relations(`supports`、`derived_from`、`supersedes`、`contradicts`)。Search、Prepare、list 和 semantic projection 默认按 active records 工作;inactive/retired 只能通过显式 include 参数查看,不能由 client 自行推断。
- retrieval/eval truth 是 `memorymodule.Search/Prepare` 的可选 `SearchExplain`、source-backed replay fixtures、显式 opt-in retrieval capture、`source_ref_backlink` deterministic boost,以及必接入的 Bleve+FAISS semantic retrieval index。**不要引入 pgvector/PGLite、LanceDB 或第二套 retrieval store**。
- semantic retrieval 配置 `memory.semantic`(独立 OpenAI-compatible embedding base_url/model/api_key/dimensions/timeout/batch_size + Bleve path/index_name)是语义检索运行的前提,但**惰性接线**:embedder/FAISS index 在首次 `Search`/`Prepare`/rebuild 时才构造,所以 `acorn pair`/`doctor`/serve 启动不被 embedding 配置或 FAISS 可用性阻塞。embedding 未配置时不接线语义运行时,`Search`/`Prepare` 直接 fail-loud(`semantic search runtime is required`)。**不存在 `memory.semantic.enabled` 开关,也不静默降级**;`acorn memory semantic rebuild` 显式从 canonical Memory Record V2 records 重建 index,semantic text/hash/Bleve document 必须包含 v2 metadata;**semantic `Search`/`Prepare` 失败时不能 fallback 到关键词搜索或 fake vectors**(关键词检索路径已物理移除)。

### 上下文 & 压缩(Context Protocol)

- `BudgetGovernor` 是 context pressure 的唯一计算入口。不要恢复 `threshold_pct`、raw percentage trigger、字符估算或 client-local pressure 估算。
- context assembly / memory context / rehydration packet 预算必须使用后端统一 token counter;不要恢复字符串级 `trimToBudget`、`rune/4` 估算或 silent drop active context。
- tool output 是模型可见 tool result truth;不要恢复字符数 `toolOutputCompressor` 或在 audit wrapper 里截断真实工具输出。需要回收上下文时只用 durable `tool_result_ref` 过期替换。
- `CompactionEngine` 拥有 compact 规则(summary prompt、structured continuation validation、preserved tail、tool-call/tool-result pair preservation、compression metrics),不能散落回 middleware;post-compact rehydration helper 拥有 packet 恢复,compact 后不能只靠 summary 继续,也不能扫描 workspace 猜 recent files。
- `ContextBoundary`(SQLite `context_boundaries`)是 durable compact/resume boundary truth。不要恢复 `context.compressed` RunEvent projection,也不能从 RunEvent payload 恢复 boundary。
- reactive compact 只处理真实 provider/model context overflow,且只允许同 provider/options 一次重试;其他 provider/runtime/tool/parser 错误必须显式失败。
- tool lifecycle fail-loud:unknown、disabled、deferred-before-load 是模型可见 failed tool result;runtime wiring/storage/model failure 是 run failure。tool result lifecycle 必须写入 durable ledger;ledger wiring/storage 失败是 run failure;workspace checkpoint/rollback side effects 只能从后端 ledger/store-owned projection 消费。

### Remote API & Mobile

- remote client wire contract 是 `docs/openapi.yaml`,mobile generated client 必须从它生成。Remote clients 只走 `/v1`、`/healthz` 和 serve-time `/mcp` mount;**不新增 legacy `/api` alias、debug-only API、mobile fake type 或绕过 OpenAPI 的 wire shape**。改 mobile DTO/RunEvent/stream payload/OpenAPI schema 必须同步 `docs/openapi.yaml`、generated mobile client(`mobile/lib/src/api/acorn_api.dart`,不得手写 parallel DTO)和相关 parser/projection tests。
- remote client auth 是 single-owner device auth:`acorn pair` 生成一次性 pairing code,`POST /v1/devices:pair` 换取一次性展示的 bearer token,SQLite 只保存 pairing code / device token hash;除 `/healthz` 和 pairing exchange 外的 `/v1` 路由都必须通过 device bearer auth。token 缺失、格式错误、未知或 revoked 必须显式失败,**不能 fallback 到 local/dev access**。
- pairing onboarding:`acorn pair --server-url <url> --qr` 渲染包含 `server_url`/`pairing_code`/`expires_at` 的终端二维码;Flutter mobile MVP 提供内置相机扫码器(`mobile/lib/src/features/pairing/pairing_qr_scanner_screen.dart`,基于 `mobile_scanner`,扫码回填 server URL+pairing code)与手动输入两种方式,扫码后仍需用户手动确认完成配对。
- mobile inbox truth 是 `GET /v1/inbox`:后端聚合 pending action summaries、active runs、recent terminal runs 和 system status,复用 SQLite runs/events/pending_actions 和 `CapabilitiesService`,不是第二套 event store 或 pending action source endpoint。
- pending approval remote truth 是 `GET /v1/pending-actions`、`GET /v1/pending-actions/{action_id}`、`POST /v1/pending-actions/{action_id}:decide`;list/detail/decide 都消费 SQLite `pending_actions`,**不从 assistant prose 或 RunEvent 反推审批状态**。
- Mobile 是后端事实的 remote control surface:不本地执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle;不从 local state、message length 或 token counter 猜测后端事实(context pressure/boundary/run status 都消费后端 projection,diagnostic summary/raw trace 不属于 mobile/public contract)。mobile memory list/search 只消费 `/v1/memory/*`,`include_inactive`/`include_retired`、relations、validity、source/evidence refs 都来自 generated client,不允许 mobile 解析 memory markdown 或自算 active status。
- 涉及 mobile 视觉/交互的改动必须在真机或模拟器人工验证;无法连接设备时必须明确说明未验证。

### 自托管发布 & 本地 dev

- self-hosted onboarding truth 是 GitHub Release 预构建 tarball + Linux binary + signed Android APK + `systemd` + installer:tag `v*` 触发 `.github/workflows/release.yml` 构建 `linux/amd64` 和 `linux/arm64` 必带 Bleve+FAISS 的后端资产,并构建 `acorn_mobile_${VERSION}_android.apk`。后端 Release build 固定 `-tags "bleve_faiss vectors"` + FAISS C API headers/libs + 目标平台 `lib/${GOOS}_${GOARCH}/libfaiss*.so*`;**缺失 artifact、CGO toolchain、build tags 或 packaged libs 必须显式失败,不发布 non-FAISS fallback 包**。Android release 必须通过 repository signing secrets 生成 keystore/key.properties,**缺 secrets 或本地 `mobile/android/key.properties` 显式失败,不回退 debug signing**。
- 后端包含 `acorn` binary、FAISS runtime libs、bundled `skills/`、`install-release.sh`、`deploy/systemd/acorn.service`、`deploy/systemd/acorn.env.example`、`configs/acorn.selfhosted.example.yaml`、`docs/user/self-hosted-onboarding.md`。installer 安装 `/opt/acorn/acorn`、`/opt/acorn/lib/${GOOS}_${GOARCH}/libfaiss*.so*` 和 installer-owned `~/.acorn/skills`,提供 `/usr/local/bin/acorn` wrapper;二进制默认读 `~/.acorn/acorn.yaml`;installer 以执行安装脚本的用户作为 systemd `User`/`HOME`,root VPS install 用 `/root/.acorn`,operator workspace 是 `/srv/acorn/workspace`。
- macOS arm64 本地 dev 是 `.artifacts/faiss-native` 的 pinned FAISS artifacts + `scripts/run-with-faiss-artifacts.sh` + `make dev-*`(见上「常用命令」),是 local dev runtime path,不是 Release target。
- compact/resume truth 是 SQLite `context_boundaries`,不是 `context.compressed` event 或旧 marker(已在 Context Protocol 述及,持久化层同样适用)。

### 不要复活的旧设计

旧终端界面、legacy `/api` route group、reflection review API、memory candidate review、SQLite-backed core memory UI、codeintel/repo_map runtime surface、fixed resident shelf、sliding-window marker compression、run-wide `TokenBudget`、旧单 agent skeleton 叙事。

---

## 工作方式

- 先读 live code 和 current docs 再下结论;旧 memory、旧评审、pasted critique 只能当线索。已知文件路径直接读;未知位置先用语义搜索或 `rg` 定位。
- 不为了「看起来更稳」添加 mock、fallback、compat alias、dual-read、dual-write、silent degradation 或吞错逻辑;surface 真实失败,修根因不修症状。
- 用户明确允许 destructive rewrite 时默认 hard cut:新路径落地时同步删除旧路径、旧配置、旧测试和死文档。
- 不要修改或回滚用户已有的 unrelated dirty worktree 改动。
- 业务逻辑不要硬 new service/infrastructure concrete implementation,通过参数、接口或 container 注入;优先 immutable value 和显式返回,不偷偷 mutate 输入参数或全局状态。

## 代码规范

- Go 1.26,模块路径 `github.com/ycvk/acorn`。Go 用 tab 缩进,前端 TS/JS/Dart 用 2 空格;import 按 goimports 标准分组排序。
- error 返回值必须显式处理,`_ = someFunc()` 只允许用于 fmt 打印类等明确无害场景;错误命名遵循 `ErrXxx`。
- SQLite 操作必须关闭 Rows/Stmt 并检查 `rows.Err()`;HTTP 请求必须带 context,response body 必须关闭。

## 配置和文档

- `configs/acorn.local.yaml` 在 `.gitignore` 中,不要提交;`configs/acorn.example.yaml`(可提交开发示例)与 `configs/acorn.selfhosted.example.yaml`(可提交自托管示例,须保持 `~/.acorn` storage、`/srv/acorn/workspace` tool roots、`127.0.0.1:8080` default listen)修改时必须同步 config struct、defaults、validation 和 tests。
- provider `api_key` 与 `memory.semantic.embedding.api_key` 都支持环境变量展开;embedding key 是独立 key,**不能从 chat provider key 静默复用**。缺失密钥必须显式表现为 readiness/config error,不能 fallback 到其他 provider 或 fake success(systemd path 通过 `~/.acorn/acorn.env` 的 `OPENAI_API_KEY` 注入)。
- public context config 只保留 `context.window_tokens`、`context.compact_margin_tokens`、`context.preserve_recent_turns`、`context.summary_max_tokens`;reserved/static/warning/blocking/tokenizer/reduction 属于内部 derived policy。删除配置字段不保留兼容读取(除非用户明确要求),旧本地配置 strict-load 失败是可接受的 hard cut 结果。`decision.md` 是 Acorn runtime 决策配置文件,不是协作文档。
- 文档分区:架构现状 → `docs/architecture/`,用户指南 → `docs/user/`,开发者指南 → `docs/dev/`,不要把未来计划写成 current truth;README 面向使用者/新贡献者;AGENTS(本文件)面向 AI 协作;`docs/openapi.yaml` 只描述 active remote client contract,不为内部重构或旧路径补文档。

## 验证要求

提交前必须通过 `make format-check` 和 `make lint`。常用补充:`make test`、`python3 mobile/tool/generate_openapi_client.py --check`、`git diff --check`;mobile 改动在 `mobile/` 下跑 `flutter test`/`flutter analyze`/`flutter build apk --debug`;context/runtime 改动至少跑相关包,如 `go test ./internal/config ./internal/contextplane ./internal/orchestration ./internal/runtime ./internal/cli`。

**CI 守卫(`tests/architecture/`,改代码前知道能省一次红 CI)**:
- `store_boundary_test.go`:生产代码只允许 `internal/app/container.go` 直接 import `internal/store/sqlite`,其他地方导入即失败。
- `bleve_faiss_release_guard_test.go`:Bleve 导入只能出现在带 `bleve_faiss,vectors,cgo` build tag 的适配器文件。
- `client_projection_boundary_test.go`:客户端事件投影边界守卫。

**已知坑**:OpenAPI/generated mobile types 漏同步会让 mobile parser/analyzer 失败;不要从旧 `context.compressed` 事件或任何 RunEvent payload 恢复 context boundary(真相在 SQLite `context_boundaries`);`serve` 可在 execution-not-ready 状态启动,执行路径会显式返回 `execution_not_ready`,不要伪造可执行状态。

---

## Harness 自演化系统

Acorn 自带一套 AI 协作自演化 harness(状态/记忆/反思/技能治理)。**运行细节(触发条件、不变量、输出格式)以各 `.claude/skills/<name>/SKILL.md` 为真相源**;本节只给路由边界与硬约束。

- **状态与记忆**:项目状态在 `.acorn/harness/state/current.md`,模块共识记忆在 `.acorn/harness/memory/modules/*.md`,架构决策在 `.acorn/harness/memory/decisions/*.md`。新对话开始或用户说「继续」时,经 `orchestrate`/`harness-init` skill 加载状态再开工;涉及 sprint 进度/blocker/风险变更的 run 结束后经 `harness-update` 更新 `state/current.md`,架构/硬约束讨论结束后更新 `memory/decisions/` 或 `memory/modules/`。直接改写 Markdown,不破坏现有格式。
- **反思与升级**(`reflexion-extract`/`meta-review`/`pattern-updater`):reflexion 在「改 ≥2 模块 / 改了 `internal/web`·`docs/openapi.yaml`·`mobile/` / 测试非全绿 / 用户负面反馈 / 命中已有 RISK」时触发。meta-review 升级边界:**计数 ≥2 的 pattern 可自动升级为 RISK(无需确认);硬约束升级与新建决策必须等人确认**。
- **技能治理**(`auto-skill-create`/`skill-validator`/`bootstrap-verify`/`skill-health`/`decision-audit`):新 skill 走「AI 生成 → skill-validator 验不变量 → 人确认后激活」三层;**skill 不变量包含「不修改 AGENTS.md 或 hooks.json」「无 destructive 命令」「无循环依赖」**。自动生成的 skill 放 `.claude/skills/{name}/SKILL.md`,草稿未激活前不进 orchestrate 路由,回滚的移到 `.claude/skills/deprecated/drafts/`。
