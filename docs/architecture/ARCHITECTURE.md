---
doc_type: architecture
status: current
last_reviewed: 2026-05-20
slug: architecture-index
---

# Acorn 架构总入口

Acorn 当前形态是 **single-user self-hosted agent backend + authenticated remote client API + Flutter mobile control surface**。后端以 Go/Eino 运行 agent、工具、计划、证据、trace、working checkpoint、file-backed memory 和必接入的 Bleve+FAISS semantic retrieval index，并且是 Thread、Run、RunEvent、tool result、memory、skills、context boundary 和 workspace mutation 的唯一事实源。当前可用客户端是 `mobile/` Flutter mobile app：mobile 是主产品 control surface，首屏是 thread continuation，Chat 是 thread detail surface；第一层接 `/v1` Thread/Message/Run/RunEvent、Inbox、PendingActions、RunDetail、SystemStatus、Tools、只读 Skills、Memory、Settings、Device auth 和 `/healthz`。active remote client contract 是 `/healthz`、serve-time `/mcp` mount 和 authenticated `/v1` client resources。旧 `ResidentShell`、`PersonalShelf`、fixed shelf、companion window、React/Vite frontend、legacy `/api` route group、public `/v1/codeintel/*`、Web skill mutation、reflection review API、history search API 和 SQLite-backed memory management API 已删除；client source、OpenAPI 和 generated clients 不保留兼容入口。

## 当前主链

```text
operator CLI / authenticated remote clients
  -> app Container
  -> remote client contracts (/healthz + /v1 client resources + optional /mcp mount)
  -> runtime Executor (consumer-owned store ports)
  -> RunnerFactory / runBuilder (runtime-owned store ports)
  -> run selection policy + ContextPlane + OrchestrationPlane
  -> SQLite adapter / persisted truth
  -> Flutter mobile control surface
```

主链对应的现状代码：

- `internal/app/container.go` 装配 app service、runtime executor、trace/workbench service 和 web dependencies。
- `internal/runtime/executor.go` 负责 session/run 创建、root mode routing、执行和 finalization。
- `internal/runtime/runner.go` 的 `RunnerFactory.New` 只保留 run build 入口；`internal/runtime/run.go` 的 `runBuilder.Build` 执行 per-run assembly。当前 active execution paths 都走固定主链：model -> run tool catalog -> memorymodule prepare -> ContextPlane -> OrchestrationPlane -> ActiveRunner。`direct_response` 不进入 run selection；public `plan_execute` 和 internal child `single_agent` 会先通过 `internal/decision` policy 解析 selected skill、decision record 和 context priority，再交给 ContextPlane 渲染上下文。
- `internal/contextplane/` 管 run 上下文、prepared file-backed memory、工具 lifecycle、压缩 middleware 和 deferred tool loading。
- `internal/orchestration/` 管 public root `direct_response` / `plan_execute` assembly，以及内部 child-run `single_agent` assembly。
- `internal/store/` 保存跨包 store-facing records 和 sentinel errors；这些类型不是 sqlite-owned contract。
- `internal/store/sqlite/` 是 SQLite adapter / persisted truth：sessions、runs、events、plans、checkpoints、archives、session summaries、context boundaries 和 workbench 所需事实都从这里或 workspace inspection 装配。除 app composition root 外，生产代码不直接依赖 sqlite package；runtime、app services 和 MCP provider 通过 consumer-owned ports 消费持久化能力。长期 memory 的 active truth 是 `memorymodule` Record V2 文件；旧 SQLite memory/codeintel tables/readers 和 `acorn memory migrate` CLI 已删除，schema migration 会丢弃残留旧表。
- `internal/app/client_service.go` 和 `internal/web/handlers_client.go` 暴露 `/v1` Thread/Message/Run/RunEvent/RunDetail client surface；`RunEvent` 从 existing `events` table replay/follow，不新建 event bus 或 `run_events` 表。
- `mobile/lib/main.dart` 启动当前 Flutter mobile control surface；`mobile/tool/generate_openapi_client.py` 从 `docs/openapi.yaml` 生成 `mobile/lib/src/api/acorn_api.dart`，移动端 app state 只消费 authenticated `/v1` server truth。
- `.github/workflows/ci.yml` 运行 Go gates 和 mobile Android gates；mobile job 固定 Flutter stable `3.41.6`，校验 generated OpenAPI client，跑 `flutter test` / `flutter analyze` / `flutter build apk --debug`，并上传短期 debug APK artifact。`.github/workflows/release.yml` 在 tag `v*` 或手动 dispatch 时生成并发布当前 self-hosted release tarball 和 signed Android APK；后端矩阵产物覆盖必带 Bleve+FAISS 的 `linux/amd64` 和 `linux/arm64`，mobile 产物为 `acorn_mobile_${VERSION}_android.apk`。`scripts/build-release.sh` / `make release-linux-amd64` / `make release-linux-arm64` 提供 Linux build host 上的本地等价打包，固定使用 `CGO_ENABLED=1` 和 `-tags "bleve_faiss vectors"`；`scripts/build-faiss-artifacts.sh` 构建 Bleve 兼容 FAISS fork 的 pinned checkpoint，并输出 `lib/${GOOS}_${GOARCH}/libfaiss*.so*`。后端包内包含 `acorn` binary、FAISS runtime libraries、`deploy/systemd/acorn.service`、`deploy/systemd/acorn.env.example`、`configs/acorn.selfhosted.example.yaml` 和 onboarding guide；installer 在 Debian/Ubuntu 上安装 OpenBLAS runtime 并校验 release binary/FAISS shared libraries 的动态库闭包。Android release APK 必须由 repository signing secrets 生成，缺失时 workflow 显式失败，不发布 debug-key release fallback。VPS 上的 Linux service 以执行安装脚本的用户作为 systemd `User` 和 `HOME`，使用二进制默认 `~/.acorn/acorn.yaml`；root VPS install 使用 `/root/.acorn` 保存 runtime storage，用 `/srv/acorn/workspace` 作为 operator workspace。

## 核心术语

| 术语 | 当前含义 |
|---|---|
| **Executor** | `internal/runtime.Executor`，接收 authenticated remote client 请求，创建 run，写入 root orchestration mode，调用 RunnerFactory，并执行 run lifecycle。 |
| **RunnerFactory** | `internal/runtime.RunnerFactory`，持有 runtime 共享依赖、registry、workspace、provider/cache 和 concrete orchestration builder；每次 run 的具体装配委托给内部 `runBuilder`。 |
| **runBuilder** | `internal/runtime/run.go` 的 per-run assembly 入口，按固定主链接 model、tool catalog、prepared memory、run selection policy、ContextPlane 和 OrchestrationPlane，返回 `ActiveRunner`。 |
| **Root orchestration mode** | 持久化到 `runs.orchestration_mode` 的 run 模式。Public root request 只接受 `direct_response` / `plan_execute`；`single_agent` 保留为内部 child-run / verifier / eval 执行模式，并可作为 persisted child run truth 投影。 |
| **ContextPlane** | `internal/contextplane` 的运行时上下文边界，负责 context assembly、budget、compression handlers、tool lifecycle 和 deferred load；初始上下文预算使用共享 token counter，不再用字符串 trim 丢 active context。 |
| **BudgetGovernor** | `internal/contextplane` 的 context pressure 计算器；用 model window、provider output cap、summary cap、static overhead 和 derived buffers 得出 `ok` / `warning` / `auto_compact` / `blocking`，替代 percentage compression trigger。 |
| **CompactionEngine** | `internal/contextplane` 的 proactive compact 执行所有者；生成 no-tool structured continuation summary、保护 preserved tail/tool pairs，并产出 `context.compressed` projection 所需的 token/summary metrics。 |
| **RehydrationPlanner** | `internal/contextplane` 的 post-compact packet 计划器；从 compact 前 context envelopes、tool lifecycle state 和显式 plan/recent-file 输入恢复 working checkpoint、selected skill、skill catalog、memory、tool state 等 reference packets，packet 超过 token limit 时直接失败。 |
| **ContextBoundary** | `internal/runtimehistory.ContextBoundary` 的 persisted compact boundary fact；SQLite `context_boundaries` 保存 root mode、trigger、covered/preserved refs、transcript ref、summary 和 token metrics，`context.compressed` 只投影 `boundary_id` 与展示字段。 |
| **Tool lifecycle state** | run-scoped 工具可见性状态，记录 loaded tools、deferred tools 和 tool result references；执行前由 `SafeParallelToolsNode` 调 `OnToolCall` 校验，执行后写入 durable `ToolResultLedger`。 |
| **ToolResultLedger** | `internal/toolresult` / SQLite `tool_results` 的工具结果事实层；保存 result ref、arguments、status、preview/full text、side-effect refs 和 evidence refs，是 context rehydration、plan evidence backlink、workbench checkpoint/rollback projection 的共同来源。 |
| **Device Auth** | Single-owner self-hosted auth boundary：`acorn pair` 通过本地 store 写入一次性 pairing code hash，`POST /v1/devices:pair` 换取一次性展示的 bearer token；SQLite 只保存 token hash，protected `/v1` 请求由 `internal/web` middleware 调 `internal/app.DeviceAuthService` 验证。 |
| **Self-hosted Onboarding** | GitHub Release 预构建 tarball + VPS/Linux binary + signed Android APK + `systemd` 主路径；tag `v*` 自动生成 `linux/amd64` 和 `linux/arm64` 的 `acorn`、`acorn.service`、`acorn.env.example`、`acorn.yaml.example`、checksum 和 guide，并生成 `acorn_mobile_${VERSION}_android.apk`，本地 `make release-linux-*` 作为后端等价 fallback。VPS 使用安装用户的 `~/.acorn` runtime storage、`/srv/acorn/workspace` operator workspace、`/healthz` process health 和 `acorn pair --qr` pairing payload；`/usr/local/bin/acorn` wrapper 默认让 service-backed operator commands 消费同一个安装用户的默认 `~/.acorn/acorn.yaml`。 |
| **ToolExecutionScheduler** | `internal/runtime/tool.go` 的共享调度核；direct_response 和 graph tool execution 都通过它消费 `ToolContract.Execution`，处理 read-only 并发、write-scoped path conflict 和 exclusive sequencing。 |
| **Deferred tool** | enabled 但首轮未暴露给模型的工具；必须通过 `load_tools` / `DeferredLoad` 加载后才能调用。 |
| **Run selection policy** | `internal/decision` 的小型选择策略，由 runtime 在 tool-enabled mode 中调用；消费 explicit skill、skill candidates 和 working context，持久化 decision record，返回 selected skill 和 context priority。它不是独立 Plane，也不负责 root mode routing。 |
| **MemoryModule** | `internal/memorymodule` 的 file-backed memory 模块；管理 facts、skills、history、`.index/insights`、search、prepare、history append 和 memory file tools。Canonical Memory Record V2 metadata 包含 validity、provenance、typed relations、lifecycle timestamps 和 active selection。Insights 是 L1 retrieval routing，最终 truth 仍回到 facts/skills/history。 |
| **Bleve+FAISS semantic index** | 必接入的 rebuildable retrieval index，由 `memory.semantic` 配置、`acorn memory semantic rebuild` 重建，并在 runtime 中为 `memorymodule.Search` / `Prepare` 提供 hybrid text/vector semantic candidates。它投影 Memory Record V2 metadata，但不是 SQLite persisted truth，也不是 file-backed L0 memory truth。 |
| **OrchestrationPlane** | `internal/orchestration` 的编排入口；RunnerFactory 委托它构建 public direct/plan-execute assembly 和内部 single-agent child assembly。 |
| **Store ports** | app/runtime/provider 包内定义的 consumer-owned persistence ports；`internal/app/container*.go` 是当前唯一允许直接打开/持有 sqlite adapter 的 production composition root。 |
| **PlanStore** | runtime graph 使用的计划持久化接口，当前由 `internal/runtime/plan_store.go` 从 `internal/store.PlanRecord` 适配为 runtime-owned `Plan`。 |
| **ChildAgent contract** | `internal/orchestration/child_agent.go` 的 `ChildAgentRequest` / `ChildAgentResult` / `ChildAgentExecutor`，被 `delegate_task`、plan_execute 和 verifier 共用；`ChildAgentOriginVerifier` 与 `VerificationRequest` / `VerificationResult` 是只读 verifier 子 run 合同。`plan_execute` 只在 step 显式声明 `verification_intent.kind=verifier` 时运行 verifier，并把 verdict 回填为 `EvidenceKindVerifier` plan evidence。 |
| **Trace** | run events 的 persisted projection；`internal/app.TraceService` 从 SQLite runs/events 构建 trace 和 resume status。RunDetail 的 trace summary 由已加载的 run event records 原地投影。 |
| **RuntimeWorkbench** | 当前 active session 的聚合 overview；`internal/app.RuntimeWorkbenchService` 从 session/run/plan/events/tool-results/workspace git truth 装配，并投影 mutation checkpoints / rollback results，不从前端本地猜测。 |
| **Mobile Control Surface** | `mobile/` Flutter app，当前包含 connect、threads/chat detail、pending approval decision 和 settings surfaces。它通过 generated Dart client 消费 `/v1`；连接/API/stream clients 由 `ConnectionController` 承载，shell tab、inbox、threads、approvals、chat foreground streaming 和 run detail 分别由 feature controllers 隔离；mobile 不执行 runtime、不维护第二套 message lifecycle、不做 offline-first truth。 |
| **Run detail surface** | 当前 run deep dive 入口，只消费 `GET /v1/runs/{run_id}/detail` aggregate；Workbench、trace、plan 和 unsupported raw events 都作为 detail 投影，不由 client 散读 legacy endpoints。 |
| **SQLite persisted truth** | 后端 runtime 事实来源；events、runs、plans、checkpoints、tool results、archives、session summaries、context boundaries 等事实由 SQLite 持久化和迁移维护。长期 memory 的 active truth 是 `memorymodule` 文件。 |
| **SSE StreamItem** | runtime internal / legacy trace JSON 事件形态；不是 remote client 的 live stream contract。 |
| **Client RunEvent** | `/v1` 的 client-facing live event envelope；mobile client 的 send/live stream path 消费它。它由 SQLite `events` table 投影，SSE `id` 是 `event_id`，`event` 是 `type`，`data` 是完整 `RunEvent` JSON。 |
| **Mobile Inbox** | `GET /v1/inbox` 的 authenticated aggregate；由 `internal/app.InboxService` 从 pending actions、active runs、recent terminal runs、backend-projected run summary fields 和 `CapabilitiesService.Snapshot` 装配，给 mobile 一个 reconnect/attention surface，不替代 source endpoints。 |
| **Pending Action Control Surface** | `GET /v1/pending-actions`、`GET /v1/pending-actions/{action_id}` 和 `POST /v1/pending-actions/{action_id}:decide`；由 `internal/app.PendingActionService` 直接消费 SQLite `pending_actions` 和 owning run，不从自然语言或 RunEvent 猜审批状态。 |

## 子架构文档

- [runtime-execution.md](runtime-execution.md)：Executor、run lifecycle、root mode routing、stream/finalization。
- [runtime-orchestration.md](runtime-orchestration.md)：OrchestrationPlane、public direct_response/plan_execute、internal single_agent、child-agent contract、tool boundary validation。
- [runtime-context-memory-decision.md](runtime-context-memory-decision.md)：ContextPlane、MemoryModule、run selection policy、skill retrieval、tool lifecycle。
- [data-web-store.md](data-web-store.md)：SQLite truth、events/runs/plans/session/workbench、remote client DTO/API responsibility、`/v1` RunEvent replay/follow。
- [mobile-control-surface.md](mobile-control-surface.md)：Flutter mobile app、generated Dart client、secure connection profile 和移动端事实边界。
- [self-hosted onboarding guide](../../docs/user/self-hosted-onboarding.md)：VPS binary service、first-run、pairing、remote access、storage 和 backup。

## 关键边界

- **Runtime 只从持久化事实恢复状态**：run mode、lineage、plans、events、trace summary、resume status 都从 store ports 或 workspace inspection 来；不能从 assistant 自然语言或前端 local state 反推。
- **SQLite adapter 不跨层泄漏**：production direct import `internal/store/sqlite` 只允许在 `internal/app/container.go`，由 `internal/architecture/store_boundary_test.go` enforce；其他 production packages 只能依赖 consumer-owned ports 或 `internal/store` shared records/errors。
- **Context boundary 是 compact/resume 事实**：compact boundary chain、summary、transcript reference、preserved segment references 和 token metrics 以 SQLite `context_boundaries` 为准；`context.compressed` RunEvent 是 projection，不能作为 loader truth。
- **Context pressure 由 BudgetGovernor 计算**：compact trigger 和 ContextSession blocking 都基于 effective input window 与内部派生 policy；public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.preserve_recent_turns`、`context.summary_max_tokens`，不暴露 reserved/static/warning/blocking/tokenizer/reduction 细节，也不使用 `threshold_pct`、raw window percentage 或 client-local 估算。
- **没有旧压缩兜底**：runtime 不再有 sliding-window marker、`max_history_turns`、`hard_token_cap_pct`、run-wide `TokenBudget` 或 `token_budget.exceeded` 事件；压力和恢复只走 context protocol。
- **CompactionEngine 拥有 proactive compact 规则**：ADK middleware 只作为 adapter 调用 engine；summary prompt、structured continuation validation、preserved tail/tool-pair boundary 和 compression outcome 不能回散到 middleware callback。
- **Post-compact context 由 RehydrationPlanner 恢复**：compact 后第一轮模型输入必须显式注入 rehydrated packets；packet 有 kind/source/token limit，超限直接失败，不从 workspace 扫描 recent files。
- **Tool 调用合同 fail-loud**：loaded tool 允许调用；unknown / disabled / deferred-before-load 是 typed lifecycle rejection，作为 failed tool result 回给模型；空 tool name、缺 lifecycle context/state/plane 和 tool result lifecycle 写入失败是 runtime error。
- **Tool result ledger 是工具结果事实**：tool result refs、arguments、side effects 和 evidence backlinks 以 SQLite `tool_results` 为准；当前 tool output 不经过字符数 compressor，过期 tool message 只替换为 durable `tool_result_ref`；workspace checkpoint / rollback、artifact、operator question、verification artifact 和 git diff artifact projection 只从 ledger/workbench 读取，不能从 assistant prose 或 client local state 推断。
- **Memory Record V2 是长期记忆事实**：facts、procedures 和 history projection 的 validity、source/evidence refs、typed relations、active/retired 状态都由 `internal/memorymodule` 解析和投影；client、ContextPlane、run selection policy 和 semantic index 不能解析 markdown 或自行推断 active status。
- **Workbench 装配 fail-loud**：session summary、trace projection、resume status probe、plan、git inspection 等真实装配失败时 endpoint 返回错误，不伪造 clean、ready 或 resumable。
- **Client shell 单一路径**：Flutter mobile app 是当前唯一产品 control surface；旧 React/Vite frontend、`ResidentShell`、`PersonalShelf`、`ClientShell -> FoundationWorkspace`、旧 chat/composer/runtime-controller 不作为兼容路径存在。
- **OrchestrationPlane 单一编排入口**：runtime 不新增第二套 plane；RunnerFactory 持有 concrete `orchestration.DefaultPlane`，runBuilder 直接按 mode 委托给 factory 的 direct / single-agent / plan-execute assembly 方法。跨包 `Plane` interface 已删除，runtime 本地 seam 只用于测试注入。
- **OpenAPI 是 wire contract**：remote client DTO 只投影 app/runtime domain；不为内部重构新增 endpoint、改 wire shape 或生成 mobile 假类型。
- **Client v1 不复用 legacy StreamItem**：remote client 的 live stream fact 是 `/v1` `RunEvent`；runtime internal 可以继续使用 `runtime.StreamItem` 作为执行内部事件形态，但它不是 OpenAPI/generated 或 mobile app source contract。
- **Remote client 必须设备认证**：除 `/healthz` 和 `POST /v1/devices:pair` 外，`/v1` 只接受 valid device bearer token；missing/malformed/unknown token 返回 `unauthenticated`，revoked token 返回 `device_revoked`。不存在 local/dev fallback。
- **Self-hosted deployment 不创建第二套运行时**：`systemd` 只启动同一个 `acorn serve`，配置通过 `/etc/acorn/acorn.yaml` 进入同一 app composition root；部署路径不新增 debug-only API、auth bypass、mock provider 或独立 mobile backend。
- **Pairing QR 是 payload，不是事实通道**：`acorn pair --qr` 只编码 `server_url`、`pairing_code`、`expires_at`；设备 token 仍只能由 `POST /v1/devices:pair` 一次性交换得到。
- **Codeintel 已删除**：active backend 没有 `internal/codeintel`、repo-map/symbol 模型工具、codeintel SQLite index 或 `/v1/codeintel/*` client resource。需要代码定位时应使用普通文件/搜索工具或外部 LSP/MCP。
- **Skills client 面只读**：OpenAPI 只保留 list/get/read-file；create/patch/delete 通过显式 CLI/operator path 处理，不作为 remote client mutation surface。
- **Legacy `/api` 已删除**：backend 不再注册 `/api` route group；OpenAPI path map 和 generated mobile API 不包含 legacy `/api` paths、operationId 或 legacy-only schemas。不要恢复 `/api -> /v1` alias、compat handler 或 debug-only `/api` surface。

## 重要决定与历史来源

- [2026-05-02 Acorn-native Frontend Hard Cut](../compound/2026-05-02-decision-acorn-native-frontend-hard-cut.md)：决定前端按 Acorn-native resident client 硬切，不保留旧 UI 兼容层。
- [2026-05-03 Standard Client Rebuild Execution Locks](../compound/2026-05-03-decision-standard-client-rebuild-execution-locks.md)：决定 Acorn Web 后续以 standard app client 为唯一主线，不保留 resident/fixed shelf 兼容壳。
- [2026-05-16 Web Client Deletion Cutover](../compound/2026-05-16-decision-web-client-deletion-cutover.md)：决定删除 React/Vite frontend 和 root Node workspace，Flutter mobile 成为唯一产品 control surface。
- [2026-05-16 VPS Binary Self-hosted Onboarding](../compound/2026-05-16-decision-vps-binary-self-hosted-onboarding.md)：决定 self-hosted onboarding 硬切为 Linux binary + `systemd`，删除容器化发行入口，后续不把容器镜像作为当前或未来部署路径。
- [2026-05-03 Legacy API Cutover Contract](../compound/2026-05-03-decision-legacy-api-cutover-contract.md)：决定 legacy `/api` 的 route-by-route 删除/迁移路线和执行顺序。
- [2026-05-04 Resident Companion Home Shell Contract](../compound/2026-05-04-decision-resident-companion-home-shell-contract.md)：决定首页默认形态收束为 companion-first，聊天入口优先，不默认展示 start prompts、常驻 sidebar、右侧 context panel 或能力目录。
- [runtime-evolution roadmap](../roadmap/runtime-evolution/runtime-evolution-roadmap.md)：历史 runtime evolution 规划来源；同名条目中已迁移的内容以当前 live code 和更新后的 roadmap 为准。
- [repo-aware-controlled-execution roadmap](../roadmap/repo-aware-controlled-execution/repo-aware-controlled-execution-roadmap.md)：当前 repo-aware planning、evidence、delegation、workbench visibility 和 context tool lifecycle 的有效规划归属。
- [acorn-native-client-rebuild roadmap](../roadmap/acorn-native-client-rebuild/acorn-native-client-rebuild-roadmap.md)：当前 resident client、visual system、conversation stream、workbench/trace/system surfaces 的来源记录。
- [self-hosted-mobile-client roadmap](../roadmap/self-hosted-mobile-client/self-hosted-mobile-client-roadmap.md)：新的产品方向入口，规划 single-user self-hosted backend、remote client contract 和 mobile control surface；已落地 device auth、`/v1/inbox`、pending approval source endpoints、Flutter mobile MVP、notification wake-up backend contract 和 VPS binary onboarding path 是 current remote boundary。
