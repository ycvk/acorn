# AGENTS.md

本文件是 Acorn 的 AI 协作硬约束入口。所有临时修复、重构、文档更新和提交前检查默认遵守本文件。

## 当前项目真相

- Acorn 是 **Go + Eino single-user self-hosted agent backend**；当前产品主线是 mobile control surface。
- 当前可用入口是 CLI、authenticated `/v1` remote client API、mobile inbox aggregate、persisted RunEvent SSE 和 Flutter mobile MVP。
- 当前 self-hosted onboarding truth 是 GitHub Release 预构建 tarball + Linux binary + signed Android APK + `systemd` + installer script：tag `v*` 触发 `.github/workflows/release.yml` 构建 `linux/amd64` 和 `linux/arm64` 必带 Bleve+FAISS 的后端 release 资产，并构建 `acorn_mobile_${VERSION}_android.apk` signed Flutter mobile APK；`make release-linux-amd64` / `make release-linux-arm64` 通过 `scripts/build-release.sh` 提供 Linux build host 上的后端本地等价打包。后端 Release build 固定使用 `-tags "bleve_faiss vectors"`、FAISS C API headers/libs 和目标平台 `lib/${GOOS}_${GOARCH}/libfaiss*.so*` shared libraries；缺失 artifact 或 toolchain 是显式失败，不发布 non-FAISS fallback 包。Android release build 必须通过 repository signing secrets 生成 keystore/key.properties，缺失 secrets 或本地 `mobile/android/key.properties` 是显式失败，不回退 debug signing。后端包内包含 `acorn` binary、FAISS runtime libs、bundled `skills/`、`install-release.sh`、`deploy/systemd/acorn.service`、`deploy/systemd/acorn.env.example`、`configs/acorn.selfhosted.example.yaml` 和 `docs/user/self-hosted-onboarding.md`。installer 安装 `/opt/acorn/acorn`、`/opt/acorn/lib/${GOOS}_${GOARCH}/libfaiss*.so*` 和 installer-owned `~/.acorn/skills`，并提供 `/usr/local/bin/acorn` wrapper；二进制默认读取 `~/.acorn/acorn.yaml`；installer 以执行安装脚本的用户作为 systemd `User` 和 `HOME`，所以 root VPS install 使用 `/root/.acorn`，operator workspace 是 `/srv/acorn/workspace`。
- 当前 macOS arm64 本地开发启动 truth 是 `.artifacts/faiss-native` 里的 pinned Bleve-compatible FAISS artifacts + `scripts/run-with-faiss-artifacts.sh` + `make dev-faiss-artifacts` / `make dev-build-faiss` / `make dev-doctor-faiss` / `make dev-serve-faiss`。这是 local dev runtime path，不是 GitHub 后端 Release target；`scripts/build-release.sh` 和 `.github/workflows/release.yml` 的后端发布仍只发布 Linux release artifacts，mobile APK 由 Flutter Android release job 单独产出。
- 当前 runtime 主链是 `Executor -> RunnerFactory/runBuilder -> run selection policy + ContextPlane + OrchestrationPlane -> ContextSession -> SQLite/file-backed memory`。
- 当前 public root modes 是 `direct_response`、`plan_execute`；`single_agent` 只作为内部 child-run / verifier / eval 执行模式。
- 当前工具执行合同是 `ToolContract -> ToolExecutionScheduler -> ToolResultLedger`；tool result refs、side effects、plan evidence backlinks 是持久化事实。
- 当前 procedure 使用事实是 `memorymodule` file-backed skills + `procedure.activation` RunEvent；verifier 是只读 child run contract。
- 当前 native skill truth 是 `internal/skills` file-backed loader：repo `./skills` 是 release seed pack 源和本地开发 builtin source；release installer 把 bundled seed skills 安装到 installer-owned `~/.acorn/skills`；generated skills 写入 `{runtime.storage_dir}/skills/generated`，workspace skills 写入 `./.acorn/skills/workspace`；skill lifecycle 通过 `skill_create`、`skill_assess` 和 `skill.lifecycle` RunEvent 维护。
- 当前 retrieval/eval truth 是 `memorymodule.Search/Prepare` 的可选 `SearchExplain`、source-backed replay fixtures、显式 opt-in retrieval capture、`source_ref_backlink` deterministic boost，以及必接入的 Bleve+FAISS semantic retrieval index。Bleve+FAISS 只作为可重建 retrieval index，不是 SQLite persisted truth，也不是 `facts/`、`skills/`、`history/` 的 L0 memory truth；不要引入 pgvector/PGLite、LanceDB 或第二套 retrieval store。
- 当前 skill governance truth 是 `internal/skills` 的 deterministic health report、routing fixtures 和 Acorn-native skill pack dry-run/receipt；不要恢复 `skill_eval`/`skill_curate` 平台，也不要把 gbrain skillpack 格式当兼容目标。
- 当前 workspace mutation 恢复事实是 scoped mutation checkpoint 和显式 `rollback_workspace_checkpoint`，不是旧 snapshot/auto rollback。
- 当前 remote client wire contract 是 `docs/openapi.yaml`，mobile generated client 必须从它生成。
- 当前 mobile client truth 是 `mobile/` Flutter app：`mobile/tool/generate_openapi_client.py` 从 `docs/openapi.yaml` 生成 Dart client，`ConnectionController` 只消费 `/v1` server truth，connection profile 写入 secure storage。
- 当前 remote client auth truth 是 single-owner device auth：`acorn pair` 生成一次性 pairing code，`POST /v1/devices:pair` 换取一次性展示的 bearer token，SQLite 只保存 pairing code/device token hash，除 `/healthz` 和 pairing exchange 外的 `/v1` 路由都必须通过 device bearer auth。
- 当前 pairing onboarding truth 是 `acorn pair --server-url <url> --qr` 渲染包含 `server_url`、`pairing_code`、`expires_at` 的终端二维码；Flutter mobile MVP 当前仍用输入框接收 server URL 和 pairing code，没有内置相机扫码器。
- 当前 mobile inbox truth 是 `GET /v1/inbox`：由后端聚合 pending action summaries、active runs、recent terminal runs 和 system status；它复用 SQLite runs/events/pending_actions 和 `CapabilitiesService`，不是第二套 event store 或 pending action source endpoint。
- 当前 pending approval remote truth 是 `GET /v1/pending-actions`、`GET /v1/pending-actions/{action_id}` 和 `POST /v1/pending-actions/{action_id}:decide`；list/detail/decide 都消费 SQLite `pending_actions`，不从 assistant prose 或 RunEvent 反推审批状态。
- 当前 notification wake-up truth 是 SQLite `device_push_tokens`、`notifications`、`notification_deliveries` + `internal/app` notification dispatcher ports；未配置 dispatcher 时 delivery status 必须是显式 `not_configured`，不能伪造 sent。push payload 只能是 wake-up metadata，不能带 tool output、memory、transcript、secret 或完整 run result。
- 当前长期 memory 是 `internal/memorymodule` 的 file-backed `facts/`、`skills/`、`history/`。Canonical Memory Record V2 frontmatter 必须承载 status/tags/created/updated、validity window、`source_run`、`source_refs`、`evidence_refs` 和 typed relations（`supports`、`derived_from`、`supersedes`、`contradicts`）。Search、Prepare、list 和 semantic projection 默认按 active records 工作；inactive/retired 只能通过显式 include 参数查看，不能由 client 自行推断。
- 当前 semantic retrieval 配置是必需的 `memory.semantic`：独立 OpenAI-compatible embedding base_url/model/api_key/dimensions/timeout/batch_size + Bleve path/index_name。不存在 `memory.semantic.enabled` 开关；`acorn memory semantic rebuild` 显式从 canonical Memory Record V2 records 重建 index，semantic text/hash/Bleve document 必须包含 v2 metadata；semantic `Search` / `Prepare` 失败时不能 fallback 到关键词搜索或 fake vectors。Release 打包固定包含 Bleve+FAISS；FAISS native artifact、CGO toolchain、`bleve_faiss vectors` build tags 或 packaged shared libs 缺失必须显式失败，不能回退普通 non-FAISS 包。
- 当前 compact/resume truth 是 SQLite `context_boundaries`，不是 `context.compressed` event，也不是旧 marker。

不要复活旧终端界面、legacy `/api` route group、reflection review API、memory candidate review、SQLite-backed core memory UI、codeintel/repo_map runtime surface、fixed resident shelf、sliding-window marker compression、run-wide `TokenBudget` 或旧单 agent skeleton 叙事。

## 工作方式

- 先读 live code 和 current docs，再下结论。旧 memory、旧 AGENTS、历史评审和 pasted critique 只能当线索。
- 已知文件路径直接读文件；未知位置先用语义搜索或 `rg` 定位。
- 改行为前先确认主调用链和持久化事实边界。
- 不为了“看起来更稳”添加 mock、fallback、compat alias、dual-read、dual-write、silent degradation 或吞错逻辑。
- 用户明确允许 destructive rewrite 时，默认 hard cut：新路径落地时同步删除旧路径、旧配置、旧测试和死文档。
- 不要修改或回滚用户已有的 unrelated dirty worktree 改动。

## 代码规范

- Go 1.26，模块路径 `github.com/ycvk/acorn`。
- Go 代码用 tab 缩进；前端 TS/JS 用 2 空格缩进。
- import 按 goimports 标准分组排序。
- error 返回值必须显式处理。`_ = someFunc()` 只允许用于 fmt 打印类等明确无害场景。
- SQLite 操作必须关闭 Rows/Stmt，并检查 `rows.Err()`。
- HTTP 请求必须带 context，response body 必须关闭。
- 错误命名遵循 `ErrXxx` 惯例。
- 业务逻辑不要硬 new service/infrastructure concrete implementation；通过参数、接口或 container 注入。
- 优先 immutable value 和显式返回，不要偷偷 mutate 输入参数或全局状态。

## Context Protocol 约束

- `ContextSession` 是 root-run model input owner。root mode 不允许绕过它维护第二套 message lifecycle。
- `BudgetGovernor` 是 context pressure 的唯一计算入口。不要恢复 `threshold_pct`、raw percentage trigger、字符估算或 client-local pressure 估算。
- Context assembly / memory context / rehydration packet 预算必须使用后端统一 token counter；不要恢复字符串级 `trimToBudget`、`rune/4` 估算或 silent drop active context。
- Tool output 是模型可见 tool result truth；不要恢复字符数 `toolOutputCompressor` 或在 audit wrapper 里截断真实工具输出。需要回收上下文时只用 durable `tool_result_ref` 过期替换。
- `CompactionEngine` 拥有 compact 规则：summary prompt、structured continuation validation、preserved tail、tool-call/tool-result pair preservation 和 compression metrics 不能散落回 middleware。
- contextplane post-compact rehydration helper 拥有 packet 恢复。compact 后不能只靠 summary 继续，也不能扫描 workspace 猜 recent files。
- `ContextBoundary` 是 durable compact boundary truth。`context.compressed` 只是 RunEvent projection，不能作为 loader truth。
- Reactive compact 只处理真实 provider/model context overflow，并且只允许同 provider/options 一次重试。其他 provider/runtime/tool/parser 错误必须显式失败。
- Tool lifecycle fail-loud：unknown、disabled、deferred-before-load 是模型可见 failed tool result；runtime wiring/storage/model failure 是 run failure。
- Tool result lifecycle 必须写入 durable ledger；ledger wiring/storage 失败是 run failure。workspace checkpoint / rollback side effects 只能从后端 ledger/workbench projection 消费。

## Native Skill Lifecycle 约束

- repo `./skills` 是 Acorn release seed pack 源和本地开发 builtin source；VPS release 安装后，bundled seed skills 落在 installer-owned `~/.acorn/skills`。
- 新 skill 默认由 `skill.creator` + `skill_create` 生成到 `{runtime.storage_dir}/skills/generated`，或显式写到 `./.acorn/skills/workspace`。不要把 generated skill 写回 repo root `skills/`。
- `lifecycle_status: verified` 对非 builtin skill 必须有 `evidence_refs`。没有 evidence 的结果只能是 `draft`、`unverified` 或 `needs_eval`。
- skill 质量判断默认由 LLM 基于 durable evidence refs 做，不要求用户确认“这个 skill 好不好”。用户 override 必须是显式、可审计的 lifecycle action。
- `skill_assess` 是唯一 active runtime lifecycle action；不要再恢复 `skill_eval` / `skill_curate` / judge child-run 评测平台。
- executable native skills 归 `internal/skills`；learned procedures 归 `internal/memorymodule/skills` 的 `ProcedureRecord`。两者可以互相引用，但不能混成一个 durable truth。
- skill health / routing fixture / skill pack governance 是 deterministic 检查和 operator contract，不是 lifecycle promotion。pack install/update 只面向 workspace/generated/user mutable sources，必须 dry-run 可见、dependency closure 通过、receipt/hash 写入成功；release seed updates 走 release installer。
- `skill.lifecycle` 是 RunEvent visibility truth；OpenAPI/mobile generated client 和 mobile projection 必须同步，不允许客户端从 prose 推断 skill 状态。

## Remote Client / Mobile 约束

- Remote clients 只走 `/v1`、`/healthz` 和 serve-time `/mcp` mount。
- 不新增 legacy `/api` alias、debug-only API、mobile fake type 或绕过 OpenAPI 的 wire shape。
- 修改 mobile DTO、RunEvent 类型、stream payload 或 OpenAPI schema 时，必须同步 `docs/openapi.yaml`、generated mobile client 和相关 parser/projection tests。
- Mobile 不从 local state、message length 或 token counter 猜测后端事实。context pressure、context boundary、run status、trace summary 都消费后端 projection。
- Mobile memory list/search 只消费 `/v1/memory/*` 后端 projection；`include_inactive` / `include_retired`、relations、validity、source/evidence refs 都来自 OpenAPI generated client，不允许 mobile 解析 memory markdown 或自行计算 active status。
- Mobile 是后端事实的 remote control surface，不拥有 runtime truth，不做 offline-first run execution，不维护第二套 message lifecycle。
- Self-hosted remote access 必须有显式 auth/device boundary；token 缺失、格式错误、未知或 revoked 必须显式失败，不能 fallback 到 local/dev access。
- Mobile client API/model 改动必须从 OpenAPI 重新生成 `mobile/lib/src/api/acorn_api.dart`，不得手写 parallel DTO。
- Push notification 只是 wake-up signal；client 必须回拉 `/v1/inbox`、RunDetail 或 RunEvent cursor，不得把 push payload 当事实来源。
- 涉及 mobile 视觉/交互的改动，必须在真机或模拟器里人工验证；无法连接设备时必须明确说明未验证。

## 配置和迁移

- `configs/acorn.local.yaml` 在 `.gitignore` 中，不要提交本地配置。
- `configs/acorn.example.yaml` 是可提交示例配置，修改时必须同步 config struct、defaults、validation 和 tests。
- `configs/acorn.selfhosted.example.yaml` 是可提交自托管示例配置，必须保持 `~/.acorn` storage、`/srv/acorn/workspace` tool roots 和 `127.0.0.1:8080` default listen path 可用；installer 以执行安装脚本的用户 home 作为 systemd `HOME`，root VPS install 的实际 storage 是 `/root/.acorn`。公网或私网暴露必须由 operator 显式改配置或反向代理完成。
- Provider `api_key` 支持环境变量展开；自托管 `systemd` path 通过安装用户 `~/.acorn/acorn.env` 的 `OPENAI_API_KEY` 注入，root VPS install 是 `/root/.acorn/acorn.env`。缺失密钥必须显式表现为 readiness/config error，不能 fallback 到其他 provider 或 fake success。
- `memory.semantic.embedding.api_key` 也支持环境变量展开；它是 semantic retrieval 的独立 embedding key，不能从 chat provider key 静默复用。
- Public context config 只保留 `context.window_tokens`、`context.compact_margin_tokens`、`context.preserve_recent_turns`、`context.summary_max_tokens`；reserved/static/warning/blocking/tokenizer/reduction 参数属于内部 derived policy。
- 删除配置字段时不保留兼容读取，除非用户明确要求。旧本地配置 strict-load 失败是可接受的 hard cut 结果。
- `decision.md` 是 Acorn runtime 的决策配置文件，不是协作文档。

## 文档同步

- 架构现状写入 `docs/architecture/`，不要把未来计划写成 current truth。
- 用户指南写入 `docs/user/`，开发者指南写入 `docs/dev/`；过期的内部过程记录不要提交到 public repo。
- README 面向使用者和新贡献者，必须描述当前入口、当前命令和当前架构主链。
- AGENTS 面向 AI 协作，必须描述不可破坏边界和验证要求。
- `docs/openapi.yaml` 只描述 active remote client contract；不要为内部重构或旧路径补文档。

## 验证要求

提交前必须通过：

```bash
make lint
make format-check
```

常用补充验证：

```bash
make test
python3 mobile/tool/generate_openapi_client.py --check
git diff --check
```

Mobile app checks run from `mobile/`:

```bash
flutter test
flutter analyze
flutter build apk --debug
```

Context/runtime 改动至少跑相关包测试，例如：

```bash
go test ./internal/config ./internal/contextplane ./internal/orchestration ./internal/runtime ./internal/cli
```

## 已知坑

- OpenAPI/generated mobile types 漏同步会让 mobile parser 或 analyzer 失败。
- `context.compressed` 有 `boundary_id` 不代表事件是 durable truth，真正事实在 SQLite `context_boundaries`。
- `serve` 可以在 execution-not-ready 状态启动，执行路径会显式返回 `execution_not_ready`，不要伪造可执行状态。

## Harness Orchestrate Protocol

本小节描述 AI 协作时的新对话加载协议。

### 自动加载顺序

每次新对话开始时，按以下顺序加载上下文：

1. **读取项目状态**：加载 `.acorn/harness/state/current.md`
2. **读取模块记忆**：根据当前 sprint 涉及的模块，加载 `.acorn/harness/memory/modules/*.md`
3. **读取架构决策**：加载 `.acorn/harness/memory/decisions/*.md`
4. **评估用户意图**：判断属于 `specific_task`、`status_query`、`vague` 或 `harness_meta`
5. **呈现上下文**：向用户汇报当前 sprint 状态和已知风险

### 状态更新责任

- 任何涉及 sprint 进度、blocker、风险变更的 run 结束后，必须更新 `state/current.md`
- 任何涉及架构决策、硬约束变更的讨论结束后，必须更新 `memory/decisions/` 或 `memory/modules/`
- 更新时直接改写 Markdown，不要破坏现有格式

### 跨 session 恢复

当检测到这是新 session（无历史上下文或用户输入"继续"），自动加载 `state/current.md` 并向用户呈现："上次我们做到这：..."

## Phase 2 Meta-Cognition Protocol

本小节描述 reflexion、meta-review 和 pattern-updater 的运行约束。

### Reflexion 触发条件

- run 涉及 >= 2 个模块的文件修改
- run 修改了 `internal/web/`、`docs/openapi.yaml`、`mobile/` 中任一文件
- run 执行了测试且结果非全绿
- run 结束后用户给了负面反馈
- run 识别到与已有 RISK 描述匹配的问题

### Meta-Review 自动升级边界

- 计数 >= 2 的 pattern → 自动升级为 RISK（无需确认）
- 硬约束升级 → 必须等人确认
- 新建决策 → 必须等人确认

### Pattern 版本定义

- v0: 发现（首次记录在 reflexion）
- v1: 治理（升级为 RISK 或约束）
- v2: 完善（硬约束落地）
- v3+: 迭代优化

## Phase 3 Auto-Skill-Create Protocol

本小节描述 harness 自演化能力的运行约束。

### Auto-Skill-Create 三层安全

1. AI 生成内容（基于 gap 描述 + 模板）
2. 自动验证不变量（skill-validator 检查格式/结构/安全）
3. 人确认后激活（用户说"确认激活"才正式启用）

### 不变量清单

- frontmatter 必须有 `name` + `description`（<= 200 字符）
- 正文必须有"触发时机"和"输出格式"小节
- 无循环依赖（skill 不调用自己）
- 无 destructive 命令
- 不修改 AGENTS.md 或 hooks.json

### Bootstrap Verification

- 新 skill 激活后自动触发
- 使用 replay fixture 验证历史场景覆盖率
- 覆盖率 < 50% 或验证失败 → 建议回退到草稿

### Decision Conflict Detection

- 新建决策后自动扫描
- 每 14 天全量扫描一次
- 发现直接矛盾、隐式矛盾或过期决策时生成审计报告

### Skill Governance 边界

- 自动生成的 skill 放在 `.claude/skills/{name}/SKILL.md`
- 草稿状态 skill 未激活前不进入 orchestrate 路由
- 回滚的 skill 移动到 `.claude/skills/deprecated/drafts/`
- 已激活 skill 的修改走同样的"生成-验证-确认"流程
