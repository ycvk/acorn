# Acorn 彻底简化重构实现计划

Date: 2026-06-25
Plan Basis: `docs/aegis/specs/2026-06-25-radical-simplification-design.md`(proposed)
Architecture: 纯化 core + 拆 runtime god-package + 精简 API + 隔离 tools/dispatch
Tech Stack: Go 1.26, modernc.org/sqlite, cloudwego/eino/adk, cobra CLI

BaselineUsageDraft:
- Required baseline refs: AGENTS.md, tests/architecture/{structural_limits,store_interface_count,dependency_direction,client_projection_boundary}_test.go
- Acknowledged before plan refs: spec §4(目标包结构), §5(7 个 Key Design Decisions), §6(三波迁移), §8(12 条验收标准)
- Missing refs: none
- Decision: continue

Requirement Ready Check:
- Requirement source refs: spec §1(问题诊断 + 证据基线), §2(Goal 6 条), §3(Non-Goals)
- Goals and scope refs: spec §4(目标包结构), §5(7 个 Key Design Decisions)
- Acceptance / verification criteria refs: spec §7(12 条 Verification Criteria)
- Open blocker questions: none
- Decision: ready

Compatibility Boundary:
- hard cutover per wave:旧路径在波次结束时删除,无 compat alias/shim
- direct_response 唯一编排模式不变
- SQLite schema 10 表不变
- OpenAPI wire contract 不变(`docs/openapi.yaml` 不改,mobile-kotlin 不动)
- hybrid context 三机制(masking + auto-compact + circuit breaker)逻辑不变,只改所属包
- file-backed memory 不变;embedding 惰性接线不变
- 零 CGO 不变;不引入新外部依赖

Verification: 每 Phase 完成后 `go build ./...` + 相关包 `go test`;全部完成后 `go test -race ./...` + `make lint && make format-check` + `make test-architecture`

Risks:
- [RISK-001] runtime 子包拆分引入环依赖 → 子包不 import runtime 根包;Executor 持有 `*runner.RunnerFactory`
- [RISK-002] 删 API 接口破坏 wire 注入 → wire 直接传 `*XxxService` 指针,编译器类型安全兜底
- [RISK-003] store.ArtifactService 签名不兼容 core.ArtifactService → Phase 1 加 compile-time assertion
- [RISK-004] tools/dispatch 子包 import 路径变更 → goimports 批量修复,编译器兜底
- [RISK-005] 5 轮重构 fatigue → 每波独立可 ship,快速反馈

Retirement: 旧接口/旧文件在每波结束时 hard cutover 删除。无 compat carrier。

Architecture Integrity Lens:
- Invariant: core 是纯 Layer 0(类型+契约,零 service);runtime 执行链在子包内闭合
- Canonical owner: core 拥有类型+契约;runtime/runner 拥有装配;runtime/context 拥有上下文管理
- Responsibility overlap: store.ArtifactStore 与 core.ArtifactStore 重叠,合并消除;11 个 ServiceAPI 与具体 service 重叠,删接口消除
- Higher-level simplification: 不是拆碎片,是按职责拆 god-package + 删假抽象
- Verdict: proceed

Complexity Budget:
- Artifact class: internal package restructure + interface purge
- Target files: ~60 non-test files across runtime/store/api/tools/core/wire
- Current pressure: runtime 5858 LOC god-package, 11 单实现接口, 2 套 store 接口
- Projected post-change pressure: runtime ~3000 LOC + 2 子包 ~2500 LOC, 0 单实现接口, 1 套 store 接口
- Budget result: within-budget (reducing complexity)

---

## Wave 1: core 纯化 + store 接口统一 + 死代码删除

**Goal:** core 只保留类型+契约+context plumbing,移出 service;store 删重复接口;删 6 处死代码。

### Phase 1: 移 SessionSummaryService 到 runtime

**Files:**
- Create: `internal/runtime/session_summary.go`
- Modify: `internal/core/store_types.go` — 删除 SessionSummaryService struct + NewSessionSummaryService + Get/Upsert 方法
- Modify: `internal/runtime/types.go` — `SessionSummarySvc` 字段类型改为 `*SessionSummaryService`(同包)
- Modify: `internal/wire/runtime.go` — `core.NewSessionSummaryService` → `runtime.NewSessionSummaryService`

**Why:** core 是 Layer 0,不应该有 service struct。SessionSummaryService 有状态、有依赖、有业务逻辑,属于 runtime。

**Impact/Compatibility:** 类型从 core 移到 runtime,跨包引用改 import。hard cutover。

**Steps:**
- [ ] 1. 创建 `internal/runtime/session_summary.go`,从 `internal/core/store_types.go:128-178` 复制 `SessionSummaryService` struct + `NewSessionSummaryService` + `Get` + `Upsert` 方法,改 `package core` → `package runtime`,import `core` 类型
- [ ] 2. 从 `internal/core/store_types.go` 删除 SessionSummaryService 相关代码(128-178 行),保留 `SessionSummary` struct + `SessionSummaryStore` interface
- [ ] 3. 修改 `internal/runtime/types.go:66`:`SessionSummarySvc *core.SessionSummaryService` → `SessionSummarySvc *SessionSummaryService`
- [ ] 4. 修改 `internal/wire/runtime.go:39`:`core.NewSessionSummaryService(db, 2000)` → `runtime.NewSessionSummaryService(db, 2000)`
- [ ] 5. 全局搜索 `core.SessionSummaryService` 引用,全部改为 `runtime.SessionSummaryService`
- [ ] 6. 验证编译: `go build ./...` → 零错误
- [ ] 7. 运行测试: `go test ./internal/core ./internal/runtime ./internal/wire` → 全绿
- [ ] 8. Commit: `git add -A && git commit -m "refactor(core): move SessionSummaryService to runtime — core is pure Layer 0"`

### Phase 2: 删除 store.ArtifactStore 重复接口

**Files:**
- Modify: `internal/store/artifacts.go` — 删除 `ArtifactStore` interface(33-38 行),`ArtifactService.store` 字段类型改为 `core.ArtifactService`
- Modify: `internal/store/artifacts.go` — 添加 compile-time assertion `var _ core.ArtifactService = (*Store)(nil)`

**Why:** `core.ArtifactStore`(8 方法)和 `store.ArtifactStore`(4 方法)签名不兼容。`ArtifactService` 应该依赖 core 契约,不是自己定义窄接口。

**前提验证:** 确认 `Store` struct 的 `SaveArtifact`/`LoadArtifact` 方法签名与 `core.ArtifactService` 的 `WriteArtifact`/`ReadArtifactRange` 兼容。如果不兼容(方法名不同),需要在 `Store` 上添加适配方法或调整 `core.ArtifactService` 接口签名。

**Steps:**
- [ ] 1. 检查 `Store` 的 artifact 方法签名:`grep -n 'func (s \*Store).*Artifact' internal/store/*.go`,记录每个方法签名
- [ ] 2. 对比 `core.ArtifactService` 接口(`internal/core/artifact_service.go`),确认签名兼容性
- [ ] 3. 如果签名不兼容:在 `Store` 上添加 `WriteArtifact`/`ReadArtifactRange` 方法作为适配(委托给现有 `SaveArtifact`/`LoadArtifact`),或者在 `core.ArtifactService` 接口中调整方法名以匹配 store 实现
- [ ] 4. 删除 `internal/store/artifacts.go:33-38` 的 `ArtifactStore` interface 定义
- [ ] 5. 修改 `internal/store/artifacts.go:42` 的 `ArtifactService.store` 字段类型:`ArtifactStore` → `core.ArtifactService`
- [ ] 6. 修改 `internal/store/artifacts.go:47` 的 `NewArtifactService` 参数类型:`store ArtifactStore` → `store core.ArtifactService`
- [ ] 7. 添加 compile-time assertion: `var _ core.ArtifactService = (*Store)(nil)` 在 `internal/store/artifacts.go`
- [ ] 8. 验证编译: `go build ./...` → 零错误
- [ ] 9. 运行测试: `go test ./internal/store ./internal/wire` → 全绿
- [ ] 10. Commit: `git add -A && git commit -m "refactor(store): delete duplicate ArtifactStore interface, depend on core.ArtifactService"`

### Phase 3: 删除 6 处死代码

**Files:**
- Modify: `internal/config/config_defaults.go` — 删除 `DefaultConfig` 函数
- Modify: `internal/core/context.go` — 删除 `GetCallSite` 函数(77 行起)
- Modify: `internal/runtime/capability_assembler.go` — 删除 `Toolset.All` 方法(355 行起)
- Modify: `internal/runtime/tool_lifecycle.go` — 删除 `OnToolResult` 函数(254 行起)
- Modify: `internal/tools/tool_lifecycle.go` — 删除 `WithToolLifecycleContext` 函数(71 行起)
- Modify: `tests/tooltest/test_helpers.go` — 删除 `MustInferTool` 函数(12 行起)

**Why:** deadcode 报告为 unreachable,无任何调用方。

**Steps:**
- [ ] 1. 逐一删除 6 个函数
- [ ] 2. 验证编译: `go build ./...` → 零错误
- [ ] 3. 运行测试: `go test ./...` → 全绿
- [ ] 4. 验证死代码清零: `deadcode ./...` → 无输出(或 install 后 `go vet -vettool=$(which deadcode) ./...`)
- [ ] 5. Commit: `git add -A && git commit -m "refactor: delete 6 unreachable functions — deadcode cleanup"`

### Phase 4: Wave 1 验证

**Steps:**
- [ ] 1. `go build ./...` → 零错误
- [ ] 2. `go test -race ./internal/core ./internal/runtime ./internal/store ./internal/wire` → 全绿
- [ ] 3. `make lint && make format-check` → 全绿
- [ ] 4. core 零 service 验证: `grep -rn 'type.*Service struct' internal/core/` → 无输出
- [ ] 5. store 无重复接口验证: `grep -rn 'type ArtifactStore interface' internal/` → 只有 `internal/core/store.go`
- [ ] 6. 死代码验证: `deadcode ./...` → 0 处
- [ ] 7. Commit: `git commit --allow-empty -m "verify: Wave 1 complete — core pure, store unified, dead code zero"`

---

## Wave 2: 拆 runtime god-package

**Goal:** runtime 5858 LOC 拆为核心 + `runner/` 装配子包 + `context/` 上下文子包。

### Phase 5: 创建 runtime/runner 子包

**Files:**
- Create: `internal/runtime/runner/` 目录
- Move: `internal/runtime/runner.go` → `internal/runtime/runner/runner.go`
- Move: `internal/runtime/runner_mcp.go` → `internal/runtime/runner/runner_mcp.go`
- Move: `internal/runtime/capability_assembler.go` → `internal/runtime/runner/capability_assembler.go`
- Move: `internal/runtime/model_builder.go` → `internal/runtime/runner/model_builder.go`
- Move: `internal/runtime/context_assembler.go` → `internal/runtime/runner/context_assembler.go`
- Move: `internal/runtime/tool_lifecycle.go` → `internal/runtime/runner/tool_lifecycle.go`
- Create: `internal/runtime/runner/types.go` — RunnerFactoryOptions + RunnerBuildRequest + assembly types(从 runtime/types.go 提取)
- Modify: `internal/runtime/types.go` — 移除装配相关类型,保留 RuntimeStore + RuntimeDeps + ElicitationInterrupt*
- Modify: `internal/runtime/executor.go` — `*RunnerFactory` → `*runner.RunnerFactory`
- Modify: `internal/wire/runtime.go` — import `runtime/runner`,适配类型引用

**Why:** runtime 根包 5858 LOC,其中装配逻辑(runner.go 578 + capability_assembler 380 + tool_lifecycle 417 + model_builder 101 + context_assembler 136 + runner_mcp 243 = ~1855 LOC)是一整块「如何装配一次 run」的职责,应独立为子包。

**环依赖处理:**
- `runtime/runner` import `core` + `tools` + `mcp` + `memory` + `skills` + `workspace` + `config`
- `runtime/runner` **不 import** `runtime` 根包
- `runtime` 根包 import `runtime/runner`
- `Executor`(runtime 根包)持有 `*runner.RunnerFactory`
- `Session` 接口留在 runtime 根包(Executor 和 direct_response 直接依赖)
- `Plane` 接口留在 runtime 根包(runner 子包的 `context_assembler.go` 需要它——通过参数传入,不直接引用)

**Steps:**
- [ ] 1. 创建 `internal/runtime/runner/` 目录
- [ ] 2. `git mv internal/runtime/runner.go internal/runtime/runner/runner.go`,改 `package runtime` → `package runner`
- [ ] 3. `git mv internal/runtime/runner_mcp.go internal/runtime/runner/`,改 package
- [ ] 4. `git mv internal/runtime/capability_assembler.go internal/runtime/runner/`,改 package
- [ ] 5. `git mv internal/runtime/model_builder.go internal/runtime/runner/`,改 package
- [ ] 6. `git mv internal/runtime/context_assembler.go internal/runtime/runner/`,改 package
- [ ] 7. `git mv internal/runtime/tool_lifecycle.go internal/runtime/runner/`,改 package
- [ ] 8. 创建 `internal/runtime/runner/types.go`,从 `internal/runtime/types.go` 提取 `RunnerFactoryOptions` + `RunnerBuildRequest` + `ActiveRunner` + `RunAssembly` + `capabilityAssembly` + `runCapabilities` + `inMemoryCheckpointStore` 类型到 runner 子包
- [ ] 9. 修改 `internal/runtime/types.go`:删除已移到 runner 的类型,保留 `RuntimeStore` + `RuntimeDeps` + `ElicitationInterrupt*` + `DirectResponseRequest` + `AssembleResultView` + `ToolLifecycleStateView`
- [ ] 10. 修改 `RuntimeDeps`:`SessionSummarySvc *SessionSummaryService` → `*SessionSummaryService`(已在 Phase 1 移到 runtime),其他字段保持。注意 `RuntimeDeps` 留在 runtime 根包,runner 子包通过参数接收它
- [ ] 11. 修改 `internal/runtime/executor.go`:`runRuntime *RunnerFactory` → `runRuntime *runner.RunnerFactory`。添加 `import "github.com/ycvk/acorn/internal/runtime/runner"`
- [ ] 12. 修改 runner 子包内所有文件:把对 `runtime` 根包类型的引用改为通过参数传入或移到 runner 包。具体:
  - `Session` interface → runner 子包不直接引用,通过 `runtime.Session` 参数传入(如果需要)
  - `Plane` interface → 同上,通过参数传入
  - `AssembleResult` → 移到 runner 包(它是装配产物)
- [ ] 13. 修改 `internal/wire/runtime.go`:import `runtime/runner`,`runtime.NewRunnerFactory` → `runner.NewRunnerFactory`
- [ ] 14. 验证编译: `go build ./internal/runtime/runner/` → 零错误
- [ ] 15. 验证无环: `go build ./internal/runtime/` → 零错误
- [ ] 16. 运行测试: `go test ./internal/runtime/...` → 全绿
- [ ] 17. Commit: `git add -A && git commit -m "refactor(runtime): extract runner subpackage — assembly logic isolated"`

### Phase 6: 创建 runtime/context 子包

**Files:**
- Create: `internal/runtime/context/` 目录
- Move: `internal/runtime/masking.go` → `internal/runtime/context/masking.go`
- Move: `internal/runtime/auto_compact.go` → `internal/runtime/context/auto_compact.go`
- Move: `internal/runtime/memory_context.go` → `internal/runtime/context/memory_context.go`
- Move: `internal/runtime/context_helpers.go` → `internal/runtime/context/context_helpers.go`
- Modify: `internal/runtime/session.go` — `defaultContextSession` 引用 context 子包的 masking/compact 函数
- Modify: `internal/runtime/direct_response.go` — 适配 context 子包引用

**Why:** 上下文管理(masking 60 + auto_compact 100 + memory_context 250 + context_helpers 193 = ~603 LOC)是一整块「如何管理上下文窗口」的职责。

**环依赖处理:**
- `runtime/context` import `core` + `memory` + `skills` + `config`
- `runtime/context` **不 import** `runtime` 根包
- `runtime` 根包 import `runtime/context`
- `defaultContextSession`(runtime 根包)调用 context 子包函数做 masking/compact

**Steps:**
- [ ] 1. 创建 `internal/runtime/context/` 目录
- [ ] 2. `git mv internal/runtime/masking.go internal/runtime/context/`,改 `package runtime` → `package context`
- [ ] 3. `git mv internal/runtime/auto_compact.go internal/runtime/context/`,改 package
- [ ] 4. `git mv internal/runtime/memory_context.go internal/runtime/context/`,改 package
- [ ] 5. `git mv internal/runtime/context_helpers.go internal/runtime/context/`,改 package
- [ ] 6. 修改 context 子包内文件:对 `Session`/`defaultContextSession` 的引用改为通过函数参数传入。masking 和 auto_compact 函数接收 `[]adk.Message` 并返回 `[]adk.Message`,不依赖 Session struct
- [ ] 7. 修改 `internal/runtime/session.go`:`defaultContextSession` 的 `BeforeModelCall` 调用 `context.ApplyMasking(...)` / `context.AutoCompact(...)`。添加 import
- [ ] 8. 修改 `internal/runtime/direct_response.go`:如果引用了 masking/compact 函数,改为 `context.XXX`
- [ ] 9. 验证编译: `go build ./internal/runtime/context/` → 零错误
- [ ] 10. 验证无环: `go build ./internal/runtime/` → 零错误
- [ ] 11. 运行测试: `go test ./internal/runtime/...` → 全绿
- [ ] 12. Commit: `git add -A && git commit -m "refactor(runtime): extract context subpackage — masking + compact + memory context isolated"`

### Phase 7: Wave 2 验证

**Steps:**
- [ ] 1. `go build ./...` → 零错误
- [ ] 2. `go test -race ./internal/runtime/... ./internal/wire ./internal/api` → 全绿
- [ ] 3. `make lint && make format-check` → 全绿
- [ ] 4. runtime 根包 LOC 验证: `find internal/runtime -maxdepth 1 -name '*.go' -not -name '*_test.go' -exec cat {} + | wc -l` → <3500
- [ ] 5. 子包独立编译验证: `go build ./internal/runtime/runner/ && go build ./internal/runtime/context/` → 零错误
- [ ] 6. Commit: `git commit --allow-empty -m "verify: Wave 2 complete — runtime split into root + runner + context"`

---

## Wave 3: 精简 API + 隔离 tools/dispatch

**Goal:** 删 11 个单实现 ServiceAPI 接口;提取 tools/dispatch 子包;评估 StoreView。

### Phase 8: 删除 api 单实现 ServiceAPI 接口

**Files:**
- Modify: `internal/api/server.go` — 删除 11 个 `XxxServiceAPI` interface 定义(14-71 行),`Dependencies` 和 `Server` struct 改为具体类型指针
- Modify: `internal/wire/container.go` — `Dependencies` 字段从接口改为具体 `*XxxService` 指针
- Modify: `internal/api/pending_action_service_decision.go` — 删除 `inboxCapabilityService` interface(150 行)

**Why:** 11 个 ServiceAPI 接口每个只有 1 个实现(wire 注入的具体 service)。测试覆盖率 27% 证明接口未用于测试替身。单实现接口是假解耦。

**保留的接口**:
- `memory.Service`:memory 包定义,多消费者
- `api.ExecutorHandle` + `RunStartObserver`:wire 定义的执行回调
- `api.StoreView`:Phase 10 评估

**Steps:**
- [ ] 1. 修改 `internal/api/server.go`:
  - 删除 `RunServiceAPI`/`EventServiceAPI`/`ThreadServiceAPI`/`PendingActionServiceAPI`/`RunResumeServiceAPI`/`CapabilityServiceAPI`/`InboxServiceAPI`/`MemoryServiceAPI`/`SkillServiceAPI`/`DeviceAuthServiceAPI` interface 定义
  - `Dependencies` struct 字段类型:`ThreadServiceAPI` → `*ThreadService`,`RunServiceAPI` → `*RunService`,以此类推。`MemoryServiceAPI` → `memory.Service`(保留,因为是 memory 包接口)
  - `Server` struct 同上
- [ ] 2. 修改 `internal/api/pending_action_service_decision.go`:删除 `inboxCapabilityService` interface,改为直接持有 `*CapabilitiesService`
- [ ] 3. 修改 `internal/wire/container.go`:`buildContainerAppServices` 构造 `api.Dependencies` 时,字段值已经是 `*XxxService` 指针,无需适配。但如果之前用接口包裹,去掉接口
- [ ] 4. 验证编译: `go build ./...` → 零错误(编译器验证类型安全)
- [ ] 5. 运行测试: `go test ./internal/api ./internal/wire` → 全绿
- [ ] 6. 验证接口清零: `grep -rn 'type.*ServiceAPI interface' internal/api/` → 无输出
- [ ] 7. Commit: `git add -A && git commit -m "refactor(api): delete 11 single-impl ServiceAPI interfaces — concrete types replace fake decoupling"`

### Phase 9: 提取 tools/dispatch 子包

**Files:**
- Create: `internal/tools/dispatch/` 目录
- Move: `internal/tools/dispatch_scheduler.go` → `internal/tools/dispatch/scheduler.go`
- Move: `internal/tools/dispatch_node.go` → `internal/tools/dispatch/node.go`
- Move: `internal/tools/dispatch_streaming.go` → `internal/tools/dispatch/streaming.go`
- Move: `internal/tools/dispatch_side_effects.go` → `internal/tools/dispatch/side_effects.go`
- Move: `internal/tools/dispatch_types.go` → `internal/tools/dispatch/types.go`
- Modify: `internal/runtime/runner/runner.go` 或 `internal/runtime/runner/capability_assembler.go` — import `tools/dispatch`
- Modify: 所有引用 `tools.dispatch*` 类型/函数的文件

**Why:** tools 包 6418 LOC,其中 dispatch 调度逻辑(894 LOC)是独立职责。

**Steps:**
- [ ] 1. 创建 `internal/tools/dispatch/` 目录
- [ ] 2. `git mv internal/tools/dispatch_scheduler.go internal/tools/dispatch/scheduler.go`,改 `package tools` → `package dispatch`
- [ ] 3. `git mv internal/tools/dispatch_node.go internal/tools/dispatch/node.go`,改 package
- [ ] 4. `git mv internal/tools/dispatch_streaming.go internal/tools/dispatch/streaming.go`,改 package
- [ ] 5. `git mv internal/tools/dispatch_side_effects.go internal/tools/dispatch/side_effects.go`,改 package
- [ ] 6. `git mv internal/tools/dispatch_types.go internal/tools/dispatch/types.go`,改 package
- [ ] 7. 修改 dispatch 子包内文件:对 `tools` 包类型的引用改为通过参数传入或 import `tools` 包(如果 dispatch 需要 tools 的类型,import tools 是合法的——dispatch 是 tools 的子包)
- [ ] 8. 全局搜索 dispatch 类型引用: `grep -rn 'tools\.\(ToolInvoker\|StreamingExecutor\|SafeParallel\|dispatch\)' internal/ --include='*.go' | grep -v _test`,全部改为 `dispatch.XXX`
- [ ] 9. 修改 `internal/runtime/runner/capability_assembler.go` 或引用 dispatch 的文件:import `tools/dispatch`
- [ ] 10. 验证编译: `go build ./internal/tools/dispatch/` → 零错误
- [ ] 11. 运行测试: `go test ./internal/tools/...` → 全绿
- [ ] 12. Commit: `git add -A && git commit -m "refactor(tools): extract dispatch subpackage — scheduling logic isolated from tool implementations"`

### Phase 10: 评估 api.StoreView

**Files:**
- Modify: `internal/api/store_view.go`(可能删除或移动)
- Modify: `internal/api/pending_action_service.go`
- Modify: `internal/wire/runtime.go`

**Why:** StoreView 定义在 api 包(Layer 4),是 store 的消费视图,语义应由 core 定义或直接用 `core.SessionStore`。

**Steps:**
- [ ] 1. 检查 `api.PendingActionService` 的 store 依赖: `grep -n 'StoreView\|store ' internal/api/pending_action_service.go`,确认它用了哪些方法
- [ ] 2. 如果 PendingActionService 只用 `core.SessionStore` 的 pending action 子集方法 → 删 `StoreView`,改用 `core.SessionStore`
- [ ] 3. 如果 PendingActionService 需要更窄的视图 → 把 `StoreView` 移到 core 作为 `PendingActionStore` facet,或保留在 api 但确认方向合法
- [ ] 4. 修改 `internal/wire/runtime.go:49`:`api.StoreView(db)` → 相应类型
- [ ] 5. 验证编译: `go build ./...` → 零错误
- [ ] 6. 运行测试: `go test ./internal/api ./internal/wire` → 全绿
- [ ] 7. Commit: `git add -A && git commit -m "refactor(api): resolve StoreView ownership — narrow store facet"`

### Phase 11: 更新架构守卫

**Files:**
- Modify: `tests/architecture/structural_limits_test.go` — 更新 `refactorOwnedDirs` 添加 `internal/runtime/runner`、`internal/runtime/context`、`internal/tools/dispatch`
- Modify: `tests/architecture/store_interface_count_test.go` — 更新 `consumerOwnedDirs`
- Modify: `tests/architecture/dependency_direction_test.go` — 更新 `layerRank` 添加子包层级
- Modify: `tests/architecture/client_projection_boundary_test.go` — 更新文件列表

**Why:** 守卫必须反映新边界。

**Steps:**
- [ ] 1. 修改 `structural_limits_test.go` 的 `refactorOwnedDirs`:添加 `"internal/runtime/runner"`、`"internal/runtime/context"`、`"internal/tools/dispatch"`
- [ ] 2. 修改 `dependency_direction_test.go` 的 `layerRank`:添加 `"runner": 2`、`"context": 2`、`"dispatch": 2`(子包继承父包层级)。注意 `runtime/runner` 和 `runtime/context` 的层级处理:`layerForPkg` 需要正确解析子包路径
- [ ] 3. 修改 `store_interface_count_test.go` 的 `consumerOwnedDirs`:如果 runtime 子包有 store 接口,添加到检查范围
- [ ] 4. 修改 `client_projection_boundary_test.go`:更新文件列表反映 api 包变更
- [ ] 5. 运行守卫: `make test-architecture` → 全绿
- [ ] 6. Commit: `git add tests/architecture/ && git commit -m "test(architecture): update guards for runtime subpackages + tools/dispatch"`

### Phase 12: Wave 3 验证 + 文档同步

**Steps:**
- [ ] 1. `go build ./...` → 零错误
- [ ] 2. `go test -race ./...` → 全绿
- [ ] 3. `make lint && make format-check` → 全绿
- [ ] 4. `make test-architecture` → 全绿
- [ ] 5. api 接口验证: `grep -rn 'type.*ServiceAPI interface' internal/api/` → 无输出
- [ ] 6. tools/dispatch 独立编译: `go build ./internal/tools/dispatch/` → 零错误
- [ ] 7. runtime 子包 LOC 验证:
  - `find internal/runtime -maxdepth 1 -name '*.go' -not -name '*_test.go' -exec cat {} + | wc -l` → <3500
  - `find internal/runtime/runner -name '*.go' -not -name '*_test.go' -exec cat {} + | wc -l` → <2000
  - `find internal/runtime/context -name '*.go' -not -name '*_test.go' -exec cat {} + | wc -l` → <1000
- [ ] 8. 更新 `docs/architecture/ARCHITECTURE.md`:包结构图 + 依赖图 + 子包职责
- [ ] 9. 更新 `AGENTS.md`:internal 包描述(新增 runtime/runner、runtime/context、tools/dispatch)
- [ ] 10. 更新 `docs/aegis/INDEX.md`:添加新 spec + plan 条目
- [ ] 11. Commit: `git add -A && git commit -m "verify+docs: Wave 3 complete — API simplified, tools/dispatch isolated, docs synced"`

---

## Self-Review

### 1. Spec Coverage
- [x] core 纯化(移 SessionSummaryService) → Phase 1
- [x] 统一 store 接口(删 store.ArtifactStore) → Phase 2
- [x] 删死代码(6 处) → Phase 3
- [x] 拆 runtime god-package(runner + context 子包) → Phase 5 + Phase 6
- [x] 精简 API(删 11 个 ServiceAPI) → Phase 8
- [x] 隔离 tools/dispatch → Phase 9
- [x] 评估 StoreView → Phase 10
- [x] 架构守卫更新 → Phase 11
- [x] 文档同步 → Phase 12
- [x] 最终验证 → Phase 12

### 2. Placeholder Scan
- [x] 无 TBD/TODO
- [x] Phase 2 的签名兼容性检查是「前提验证」步骤,不是 placeholder——有明确的验证命令
- [x] Phase 10 的 StoreView 评估有两个分支(删除/移动),都有具体步骤

### 3. Type Consistency
- [x] SessionSummaryService 从 core 移到 runtime,wire import 从 `core.NewSessionSummaryService` → `runtime.NewSessionSummaryService`
- [x] store.ArtifactService 的 store 字段从 `store.ArtifactStore`(4 方法) → `core.ArtifactService`(4 方法,签名需验证)
- [x] Executor 的 `runRuntime` 从 `*RunnerFactory` → `*runner.RunnerFactory`
- [x] api Dependencies 的字段从 `XxxServiceAPI` 接口 → `*XxxService` 具体指针

### 4. Compatibility
- [x] direct_response 不变
- [x] SQLite schema 不变
- [x] OpenAPI 不变
- [x] mobile-kotlin 不变
- [x] hybrid context 三机制不变(只改所属包)
- [x] file-backed memory 不变
- [x] embedding 惰性接线不变

### 5. Plan-Time Complexity
- [x] Wave 1: 3 文件创建 + 8 文件修改,低风险
- [x] Wave 2: 6 文件移动 + 4 文件创建 + 6 文件修改,中风险(环依赖)
- [x] Wave 3: 5 文件移动 + 8 文件修改,中风险(import 路径变更)
- [x] 每文件 ≤ 800 行限制:runner.go 578 行最大,在限制内

### 6. Architecture Integrity
- [x] core 是纯 Layer 0(移出 service 后只有类型+契约)
- [x] runtime/runner 是装配 owner(RunnerFactory + assembly)
- [x] runtime/context 是上下文管理 owner(masking + compact + memory context)
- [x] runtime 根包是执行引擎 owner(Executor + direct_response + Session)
- [x] tools/dispatch 是调度 owner(scheduler + node + streaming)
- [x] 无 compat carrier,hard cutover

### 7. Verification
- [x] 每 Phase 有 `go build` 验证
- [x] 每 Phase 有 `go test` 验证
- [x] 每 Wave 有独立验证 Phase(Phase 4/7/12)
- [x] 最终有全量 `go test -race ./...` + lint + architecture
- [x] 验证命令精确可执行

### 8. ADR/Baseline-Sync Signals
- [x] spec §8 标注了 5 个 ADR-worthy 决策
- [x] 计划不创建 ADR(spec 已说明"实现完成后创建")
- [x] ADR 在实现完成后创建
