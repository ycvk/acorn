# Acorn 后端彻底重构设计

Date: 2026-06-23
Status: Proposed
Scope: Go 后端(`internal/` + `cmd/` + `tests/`);mobile-kotlin 与 OpenAPI wire contract 不变

## 1. 问题诊断(证据基线)

### 1.1 runtime 是 god package(6644 LOC,18 文件)

`runner.go`(677 行)单文件混 7 职责:RunnerFactory、ActiveRunner、chat model 构造、MCP bootstrap、capability assembly、context assembly、orchestration plane 接线、inMemoryCheckpointStore、toolLifecycleStateAdapter。`runner_*` 四个碎片文件(`runner_emit`/`runner_mcp`/`runner_selection`/`runner_toolset`)是同一概念的人为拆分。

### 1.2 orchestration 是伪边界(2017 LOC,4 文件)

`DefaultPlane` 是唯一实现。8 个 strategy func type(`ToolBuilder`/`ToolNodeFactory`/`HandlersBuilder`/`InstructionBuilder`/`ToolLifecycleBinder`/`SessionContextBinder`/`ToolInvoker`/`StreamingExecutor`)全是单实现。runtime 对 orchestration 有 30+ 引用,两者紧耦合。单编排模式下 strategy pattern 无存在理由。

### 1.3 接口膨胀(46 个接口,12 个单实现)

`web/server.go` 定义 9 个 service 接口,大多 1-2 实现(主要是 test double)。`containerAppStore` 是 30 方法的 mega-interface,代码注释承认 ISP 违规("ISP regression is accepted")。`domain.RunContextBridge` 0 外部引用,`domain.SessionSummaryStore` 只被 container.go 引用。

### 1.4 app 职责过载(3412 LOC,11 文件)

Container + 8 service + 30 个 client DTO struct。`capability_service.go`(580 行)定义 13 个 struct。`client_helpers.go` 的 DTO(Thread/Message/MessagePart/DisclosureItem...)应靠近 web 层。`notification_service.go` 是 inbox 投影逻辑,命名误导。

### 1.5 toolset 22 文件按工具粒度碎片化

`browser_service*.go` 三文件(1079 行)是一个模块拆三份。`workflow_tools*.go` 三文件、`native_*_tools.go` 四文件同理。

### 1.6 store 伪分层(2994 LOC)

`internal/store/artifacts.go`(359 行)在 store 包定义 `ArtifactStore` 接口 + `ArtifactService` 服务逻辑——service 不该在 store 包。`store.go` 只有 87 行,shared 层极薄,只放 sentinel errors + 5 个 record type。

### 1.7 死代码 + 过期文档

- 8 个 0 引用导出函数(`AssembleResultToView`/`CloneMessages`/`CallSiteFromContext`/`CompactionSummaryMessage`/`DefaultConfig`/`BuildToolResultRef`/`CloneAnyMap`/`CloneContextSessionMessages`)
- `runtime-orchestration.md` 引用 3 个已删除文件
- harness state 说 "55 文件在 runtime"(实际 18)、"4 包测试 fail"(实际全绿)

### 1.8 harness 自演化系统空转

`.acorn/harness/` + `.claude/skills/`(11 skills)+ `.until-done/tasks.yaml`(927 行)。harness 自述:"21 skills 零可操作产出"。对产品零贡献的 meta-infrastructure。

## 2. 设计原则

1. **YAGNI**:单实现不抽接口;单模式不做 strategy;不预埋扩展点
2. **包边界 = 职责边界**:每个包一个清晰职责,包名即职责
3. **依赖单向**:依赖指向更稳定/更底层的包,无环
4. **Hard cutover**:不保留 compat alias/shim/旧路径
5. **wire contract 不变**:`/v1` endpoint + OpenAPI schema 不动,mobile 不受影响
6. **架构测试同步更新**:重构后守卫反映新边界

## 3. 目标包结构

### 3.1 包映射(17 → 14 个包,删 3 个)

```
cmd/acorn/              入口(main.go)
internal/
  config/               配置加载+校验(不变)
  domain/               核心 domain 类型 + sentinel errors + context plumbing
  store/                SQLite adapter + record types + sentinel errors(合并 store + store/sqlite)
  workspace/            workspace + mutation checkpoint + worktree(不变)
  webaccess/            web_search/web_fetch/browser 共享 URL policy + fetcher/search/extractor(不变)
  memory/               file-backed memory facts/history + embedding + vector search(rename from memorymodule)
  skills/               skill file loader + model + scan(不变)
  toolkit/              ToolContract/Catalog/ToolSpec 契约(不变)
  toolset/              工具实现(file/git/browser/web/command/artifact)(按 domain 合并碎片文件)
  runtime/              Executor + Runner + tool execution + eventstream(合并 orchestration + eventstream,消除伪边界)
  context/              ContextSession + masking + auto-compact + tool lifecycle(rename from contextplane)
  api/                  HTTP /v1 server + handlers + DTO + device auth(rename from web,合并 clientevents)
  app/                  Container 组合根 + service 层(精简,DTO 移到 api)
  cli/                   CLI 命令(不变)
```

**删除的包**:
- `internal/runtime/orchestration` → 合并到 `internal/runtime`(单模式,strategy 全内联)
- `internal/runtime/eventstream` → 合并到 `internal/runtime`(eventstream 是 runtime 执行产物)
- `internal/clientevents` → 合并到 `internal/api`(投影逻辑属于 API 层)

**rename 的包**:
- `memorymodule` → `memory`(更短,语义更清晰)
- `contextplane` → `context`(更短)
- `web` → `api`(反映职责:这是 API 层,不只是 web)

### 3.2 包职责与依赖方向

```
config          → (无 internal dep)
domain          → (无 internal dep,纯 kernel)
store           → domain
workspace       → (无 internal dep)
webaccess       → (无 internal dep)
memory          → domain
skills          → config, domain
toolkit         → config, skills, domain
toolset         → domain, store, toolkit, webaccess, workspace
context         → domain, memory, skills, toolkit
runtime         → config, domain, memory, skills, toolkit, toolset, context, webaccess, workspace, store
api             → app, domain, memory, skills, config, store
app             → config, domain, memory, skills, runtime, context, store, workspace
cli             → app, config, skills, api
```

### 3.3 runtime 内部结构(合并后)

```
internal/runtime/
  executor.go              Run lifecycle + ExecuteMessages + consume + finalize
  runner.go                RunnerFactory + ActiveRunner + buildRun 主链
  runner_chat_model.go     chat model 构造(从 runner.go 拆出,单一职责)
  runner_capabilities.go   capability assembly + MCP bootstrap + catalog(合并 runner_mcp + runner_toolset + runner_selection)
  runner_context.go        context assembly + memory prepare + skill selection(合并 runner_selection 相关)
  runner_emit.go           run event emission(保留)
  safe_parallel_tools.go   tool dispatch adapter(从 safe_parallel_tools_node.go rename)
  assistant_stream.go      assistant streaming + delta persistence(保留)
  streaming_assistant.go   streaming assistant stream(保留,rename)
  side_effects.go          run side effects(保留)
  audit.go                 tool audit(保留)
  validator.go             tool validation(保留)
  memory_tools.go          remember/memory tools(保留)
  memory_tools_search.go   memory search tool(保留)
  fact_extractor.go        fact extraction(保留)
  catalog.go               catalog spec helpers + load_tools tool(保留)
  types.go                 types + ExecutorStore + RunnerFactoryStore + RuntimeDeps(保留)
  run.go                   RunRecord helpers(保留)
  eventstream.go           StreamItem + projection + accessors(从 eventstream/ 合并)
  eventstream_types.go     StreamItem types + payloads
  direct_response.go       direct_response agent assembly + ExecuteRound(从 orchestration/ 合并)
  interrupt.go             interrupt signal(从 orchestration/ 合并)
```

从 22 文件(18 runtime + 4 orchestration)降到 ~20 文件,但每个文件职责清晰,无碎片。

## 4. 重构动作

### Phase 1:删除 harness 自演化系统(无依赖,最安全)

**删除**(项目自造的空转 meta-infrastructure):
- `.acorn/harness/`(state + memory + reflexions + skill_templates + fixtures)
- `.until-done/tasks.yaml`

**不删**(外部运行时配置,非项目资产):
- `.claude/`(Claude Code 运行时配置 + skills,由 Claude Code 管理)
- `.serena/`(Serena MCP 运行时配置,由 Serena 管理)

**保留**:
- `skills/`(repo seed pack,产品功能)
- `internal/skills/`(skill loader,产品代码)

**同步**:AGENTS.md 删除 "Harness 自演化系统" 章节。

### Phase 2:合并伪边界包

#### 2a. orchestration → runtime

- `internal/runtime/orchestration/` 4 文件合并到 `internal/runtime/`
- `DefaultPlane` struct → `directResponseRunner`(去掉 Plane 抽象)
- 8 个 strategy func type 全部内联为具体函数调用
- `direct_response_builder.go` → `direct_response.go`
- `agent_loop.go` → `agent_loop.go`(保留,ExecuteRound 逻辑)
- `types.go` → 合并到 `runtime/types.go`
- `interrupt_signal.go` → `interrupt.go`

#### 2b. eventstream → runtime

- `internal/runtime/eventstream/` 6 文件合并到 `internal/runtime/`
- `types.go` → `eventstream_types.go`
- `accessors.go`/`payloads.go`/`projection.go`/`agent.go`/`item_json.go` → `eventstream.go` 或保持独立文件

#### 2c. clientevents → api(与 web rename 同步)

- `internal/clientevents/` 3 文件合并到 `internal/api/`
- projector 逻辑属于 API 投影层

### Phase 3:rename 包

- `internal/memorymodule` → `internal/memory`
- `internal/contextplane` → `internal/context`
- `internal/web` → `internal/api`

全量更新 import path(golangci-lint + goimports 辅助)。

### Phase 4:store 合并

- `internal/store/sqlite/` 合并到 `internal/store/`
- `Store` struct + 所有 `store_*.go` 方法文件提升到 `internal/store/`
- `store.go` 的 sentinel errors + record types 保留
- `artifacts.go` 的 `ArtifactService` 移到 `internal/store/artifact_service.go`(仍在 store 包,因为它是 store 的领域服务)
- 更新 `store_boundary_test.go`:composition root 仍只允许 `internal/app/container.go` 直接 import store

### Phase 5:消除单实现接口

- `web/server.go`(→ `api/server.go`)的 9 个 service 接口:保留 `ClientService`/`PendingActionService`/`DeviceAuthService`(多实现或 test double 需要),内联其余单实现接口为直接 struct 依赖
- `domain.RunContextBridge`:0 外部引用,删除
- `domain.SessionSummaryStore`:只被 container.go 引用,内联到 `SessionSummaryService`
- `orchestration.ToolLifecycleStateView`:单实现,内联
- `containerAppStore`:拆分为按消费者需要的 narrow 接口(恢复 ISP),或接受 mega-interface 但去掉"ISP 违规"的注释负担

### Phase 6:toolset 文件合并

- `browser_service.go` + `browser_service_navigate.go` + `browser_service_events.go` → `browser_service.go`(按 domain 聚合)
- `workflow_tools.go` + `workflow_tools_multi_edit.go` + `workflow_tools_verification.go` → `workflow_tools.go`(按 domain 聚合)
- `native_read_tools.go` + `native_mutation_tools.go` + `native_git_tools.go` + `native_search_tools.go` → 按 domain 合并(file/git/search/mutation 各一个文件)
- 目标:22 文件 → ~14 文件

### Phase 7:app 精简

- `client_helpers.go` 的 30 个 DTO struct(Thread/Message/MessagePart/DisclosureItem/Run/ArtifactSummary...)移到 `api/dto.go`
- `notification_service.go` rename 为 `inbox_projection.go`(反映真实职责)
- `capability_service.go` 拆分:DTO struct 移到 `api/dto_system.go`,service 逻辑留在 `app/capability_service.go`
- `container.go` 的 `containerAppStore`/`containerRuntimeStore` 精简或拆分

### Phase 8:删除死代码 + 修文档

- 删除 8 个 0 引用导出函数
- 修 `runtime-orchestration.md` 引用的已删除文件
- 修 harness state(或随 Phase 1 一起删除)
- 更新 `AGENTS.md`、`ARCHITECTURE.md`、`INVARIANTS.md` 反映新包结构

### Phase 9:架构测试更新

- `structural_limits_test.go`:更新 `refactorOwnedDirs`(删 orchestration/eventstream/clientevents,加 context/api/memory)
- `store_boundary_test.go`:更新 allowlist(store 合并后 import path 变化)
- `store_interface_count_test.go`:接口数可能变化,更新阈值
- `client_projection_boundary_test.go`:更新 `clientProjectionBoundaryFiles` 路径
- `runtime_split_test.go`:删除或重写(orchestration 合并后 "runtime must split" 断言不再适用)

## 5. 不变量(wire contract 不变)

以下在重构中**必须保持不变**:

- `docs/openapi.yaml` 不改一个字
- `/v1` endpoint 路径、request/response shape 不变
- `mobile-kotlin/` 不改一行
- SQLite schema(12 张表)不变
- CLI 命令(`serve`/`run`/`smoke`/`init`/`pair`/`devices`/`token`/`skills`/`memory`/`doctor`)不变
- 配置文件格式(`configs/*.yaml`)不变
- `make build`/`make serve`/`make test`/`make lint`/`make format-check` 命令不变

## 6. 验证计划

每个 Phase 完成后:
- `go build ./...`
- `go test ./...`
- `make format-check && make lint`
- `make test-architecture`(Phase 9 后)

全部完成后:
- `go test -race ./...`
- 确认 `docs/openapi.yaml` git diff 为空
- 确认 `mobile-kotlin/` git diff 为空

## 7. 风险

- [RISK-001] store 合并可能触发 `store_boundary_test.go` allowlist 路径失效 → Phase 4 同步更新测试
- [RISK-002] rename 包可能导致 generated code(`converter_gen.go`)import path 失效 → 需 `make generate` 重新生成
- [RISK-003] toolset 文件合并可能超 800 行守卫 → 合并后检查,必要时按 sub-domain 再拆
- [RISK-004] interface 内联可能破坏 test double → 保留有多实现的接口,只内联确定单实现的

## 8. 非 goals

- 不改 OpenAPI wire contract
- 不改 mobile-kotlin
- 不改 SQLite schema
- 不改 CLI 命令接口
- 不改配置文件格式
- 不重写工具实现逻辑(只移动文件边界)
- 不改 runtime 执行流程(只合并包,不改 direct_response 逻辑)

## 9. 执行方式建议

分阶段渐进,每阶段可编译可测试。建议顺序:

1. Phase 1(删 harness)— 无代码依赖,最安全,先清场
2. Phase 2(合并伪边界包)— 结构性大改,但无逻辑变化
3. Phase 3(rename 包)— 机械操作,goimports 辅助
4. Phase 4(store 合并)— 触及边界守卫
5. Phase 5(消除接口)— 逐个内联,需验证 test double
6. Phase 6(toolset 合并)— 文件级操作
7. Phase 7(app 精简)— DTO 移动 + rename
8. Phase 8(死代码 + 文档)— 收尾
9. Phase 9(架构测试)— 同步守卫

每个 Phase 是一个独立 commit,可单独验证。
