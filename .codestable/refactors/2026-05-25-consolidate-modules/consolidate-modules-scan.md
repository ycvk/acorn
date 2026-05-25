---
doc_type: refactor-scan
refactor: 2026-05-25-consolidate-modules
status: pending-user-selection
scope: internal/ 下 8 个微型模块（<200 行生产代码）+ 30+ 单实现接口
summary: "15 条优化点：结构 11 / 可读性 4；风险：低 7 / 中 6 / 高 2"
---

# consolidate-modules scan

## 总览

- **扫描范围**：`internal/` 下 31 个子模块，重点关注 8 个微型模块（<200 行生产代码）和 118 个接口定义
- **发现 15 条优化点**：结构 11 / 可读性 4
- **按风险**：低 7 / 中 6 / 高 2
- **建议先做**：#1 #2 #3 #4 #9 #10 #11（低风险、纯文件移动、无接口变更）
- **建议慎做 / 后做**：#5 #6 #7 #12 #13 #14（涉及接口删除、需改调用方）
- **前置检查 7 条全过**：✓

## 条目

### #1 合并 processgroup → tools

- **位置**：`internal/processgroup/` → `internal/tools/processgroup.go`
- **分类**：结构
- **现状**：90 行、3 个文件（process_group_unix.go, process_group_other.go, process_group_test.go），只提供平台相关的进程组控制（ConfigureCommand, KillCommandGroup, SignalCommandGroup, ParseSignal）
- **问题**：独立包只有纯工具函数，无状态、无接口、无复杂逻辑。被 3 个文件 import（mcp/transport.go, tools/command_tool.go, tools/workflow_tools.go），其中 2 个已经在 tools 包。
- **建议**：将 3 个文件移动到 `internal/tools/processgroup_*.go`，删除 processgroup 包
- **建议映射的方法**：M-L3-02（合并包）
- **风险**：低——纯工具函数移动，import 路径变更，无行为变化
- **验证**：AI 自证（跑 `go build ./...`，grep 确认 `internal/processgroup` 0 引用）
- **范围**：约 90 行 / 3 文件 / 3 个 import 点修改

### #2 合并 notifications → app

- **位置**：`internal/notifications/` → `internal/app/notification_*.go`
- **分类**：结构
- **现状**：89 行、1 个文件（dispatcher.go），定义 APNS/FCM push 通知路由
- **问题**：独立包只有 1 个文件，被 app 包的 3 个文件依赖。Router 是具体实现，APNSDispatcher/FCMDispatcher 接口各只有一个方法，且只有一个外部实现。
- **建议**：将 dispatcher.go 移动到 `internal/app/notification_dispatcher.go`，包名改为 app。APNSDispatcher/FCMDispatcher 接口可保留但降为 app 包内部类型（不导出），或合并为一个 PushDispatcher 接口
- **建议映射的方法**：M-L3-02（合并包）
- **风险**：低——被 3 个 app 文件依赖，移动后 import 路径统一
- **验证**：AI 自证（跑 `go build ./...` + 相关测试）
- **范围**：约 89 行 / 1 文件 / 3 个 import 点修改

### #3 合并 model → providers

- **位置**：`internal/model/` → `internal/providers/openai_model.go`
- **分类**：结构
- **现状**：34 行、1 个文件（openai.go），只有一个函数 `NewChatModel`，是对 `eino-ext/components/model/openai` 的薄包装
- **问题**："model" 包名过于宽泛且模糊，实际只做了 OpenAI 兼容 provider 的模型初始化。被 runtime 的 2 个文件依赖。
- **建议**：移动到 `internal/providers/openai_model.go`，函数改名 `NewOpenAIChatModel`（明确化）。包名统一为 providers
- **建议映射的方法**：M-L3-02（合并包）
- **风险**：低——纯工厂函数移动，2 个 import 点修改
- **验证**：AI 自证（跑 `go build ./...` + runtime 测试）
- **范围**：约 34 行 / 1 文件 / 2 个 import 点修改

### #4 合并 retrievaleval → memorymodule

- **位置**：`internal/retrievaleval/` → `internal/memorymodule/eval_*.go`
- **分类**：结构
- **现状**：140 行、1 个生产文件（sample.go），定义 retrieval evaluation 的 Sample 结构体和 Sink/FileSink 捕获机制
- **问题**：被 memorymodule（2 个文件）和 skills（2 个文件）依赖，职责是 memory search/prepare 的评估采样，与 memorymodule 紧密耦合。
- **建议**：移动到 `internal/memorymodule/eval_sink.go`（保留 Sink 接口，因为消费者需要）和 `internal/memorymodule/eval_sample.go`（Sample 类型）。skills 包中如有使用改为 import memorymodule
- **建议映射的方法**：M-L3-02（合并包）
- **风险**：低——4 个 import 点修改，Sink 接口保留但移动
- **验证**：AI 自证（跑 `go test ./internal/memorymodule ./internal/skills`）
- **范围**：约 140 行 / 1 文件 / 4 个 import 点修改

### #5 合并 workingstate → contextplane（统一 CheckpointService 接口）

- **位置**：`internal/workingstate/` → `internal/contextplane/working_*.go`
- **分类**：结构
- **现状**：158 行、3 个文件（model.go, service.go, tools.go）。定义 Checkpoint 数据模型、Store 接口、Service 实现、CheckpointService 接口、BuildWorkingCheckpointTools
- **问题**：
  1. `workingstate.CheckpointService` 和 `contextplane.CheckpointService` 是**同一接口重复定义**（对比 types.go:91 和 workingstate/tools.go:18）
  2. `Store` 接口只有 SQLite 实现（store_work.go），是单实现接口
  3. 被 10 个文件依赖，其中 contextplane 和 runtime 是核心消费者
- **建议**：
  1. 删除 `workingstate.Store` 接口，将 `Service.store` 字段改为具体类型 `*sqlite.Store`（或保留接口但移到 contextplane 包）
  2. 删除 `workingstate.CheckpointService`，统一使用 `contextplane.CheckpointService`
  3. 将 Service 实现和 Checkpoint 模型移到 `internal/contextplane/working_checkpoint.go`
  4. `BuildWorkingCheckpointTools` 移到 `internal/contextplane/working_tools.go`
- **建议映射的方法**：M-L1-01（Parallel Change：先在新位置建副本，逐步切换调用方，最后删除旧包）
- **风险**：中——涉及接口删除和统一，需要改 10 个 import 点和接口使用
- **验证**：AI 自证（跑 `go test ./internal/contextplane ./internal/runtime ./internal/app`）+ HUMAN（review CheckpointService 统一后的行为）
- **范围**：约 158 行 / 3 文件 / 10 个 import 点修改

### #6 合并 runtimehistory → contextplane（统一 SessionSummaryService 接口）

- **位置**：`internal/runtimehistory/` → `internal/contextplane/session_*.go`
- **分类**：结构
- **现状**：155 行、2 个生产文件（model.go, session_summary_service.go）。定义 SessionSummaryService 接口和实现、SessionSummary/RunArchive/HistoryHit/ContextBoundary 数据模型
- **问题**：
  1. `runtimehistory.SessionSummaryService` 和 `contextplane.SessionSummaryService` 是**同一接口重复定义**（对比 types.go:95 和 session_summary_service.go:10）
  2. `SessionSummaryStore` 只有 SQLite 实现，是单实现接口
  3. 被 20+ 个文件依赖，但核心消费者是 contextplane 和 runtime
  4. `ContextBoundary` 是 context plane 的核心概念，却放在 runtimehistory 包
- **建议**：
  1. 删除 `runtimehistory.SessionSummaryStore`，`Service.store` 改为具体类型
  2. 删除 `runtimehistory.SessionSummaryService`，统一使用 `contextplane.SessionSummaryService`
  3. 将 Service 实现、SessionSummary/ContextBoundary 模型移到 `internal/contextplane/session_summary.go`
  4. RunArchive/HistoryHit 移到 `internal/events/`（它们是事件/历史记录相关的数据模型）
- **建议映射的方法**：M-L1-01（Parallel Change）
- **风险**：中——20+ import 点修改，ContextBoundary 移动影响 context plane 核心逻辑
- **验证**：AI 自证（跑全量测试）+ HUMAN（review session summary 格式化后的 prompt 输出）
- **范围**：约 155 行 / 2 文件 / 20+ import 点修改

### #7 合并 toolresult → runtime（拆分 Ledger 接口和数据模型）

- **位置**：`internal/toolresult/` → `internal/runtime/toolresult_*.go` + `internal/store/toolresult_*.go`
- **分类**：结构
- **现状**：179 行、1 个生产文件（toolresult.go）。定义 Ledger 接口、AppendRequest/Record 数据模型、SideEffectRef/EvidenceRef 类型、BuildRef/Preview/NormalizeAppendRequest 辅助函数
- **问题**：
  1. `Ledger` 接口只有 SQLite 实现（store_tool_results.go），是单实现接口
  2. 被 25+ 个文件依赖，分布在 app, contextplane, orchestration, runtime, store, tools 多个包
  3. 跨模块契约放在独立微型包中，增加了依赖复杂度
- **建议**：
  1. **数据模型**（Record, AppendRequest, SideEffectRef, EvidenceRef）→ `internal/store/toolresult_types.go`（存储契约）
  2. **Ledger 接口** → `internal/store/toolresult_ledger.go`（与实现放在一起）
  3. **辅助函数**（BuildRef, Preview, NormalizeAppendRequest）→ `internal/runtime/toolresult_helpers.go`（runtime 是主要消费者）
  4. 这样 store 包包含接口+实现，runtime 包包含辅助函数，contextplane 等包从 store import 数据模型
- **建议映射的方法**：M-L1-02（Strangler Fig：分步迁移，先建新文件，逐步切 import，最后删旧包）
- **风险**：高——25+ import 点分散在 6 个包中，任何遗漏都是编译错误
- **验证**：AI 自证（`go build ./...` 全量编译）+ HUMAN（review tool result 相关测试）
- **范围**：约 179 行 / 1 文件 / 25+ import 点修改

### #8 迁移 architecture 测试到 tests/architecture

- **位置**：`internal/architecture/` → `tests/architecture/`
- **分类**：结构
- **现状**：0 行生产代码、2 个测试文件（bleve_faiss_release_guard_test.go, store_boundary_test.go），验证架构约束（Bleve import 限制、SQLite import 限制）
- **问题**：`internal/architecture` 是一个没有生产代码的包，纯测试包放在 `internal/` 下不符合 Go 惯例。测试使用 `go/parser` 扫描整个项目源码。
- **建议**：移动到 `tests/architecture/` 或 `test/architecture/`，包名保持 `architecture_test`。在 Makefile 中增加 `make test-architecture` 目标
- **建议映射的方法**：M-L3-03（重定位包）
- **风险**：低——纯测试移动，无生产代码影响
- **验证**：AI 自证（跑 `go test ./tests/architecture`，确认 Makefile 目标）
- **范围**：约 137 行 / 2 文件 / Makefile 修改

### #9 内联 store/sqlite scanner 接口

- **位置**：`internal/store/sqlite/store_scan_helpers.go:11`, `store_work.go:130`, `store_work.go:287`, `store_device.go:217`, `store_oauth.go:664`
- **分类**：可读性
- **现状**：5 个 scanner 接口（runRecordScanner, artifactScanner, toolResultScanner, rowScanner, providerUsageScanner），各只有一个闭包/函数实现
- **问题**：接口定义行数（3-5 行）比实际使用价值小。它们只是为了在 scan 函数签名中统一参数类型，但每个 scanner 只在 1-2 个函数中使用。
- **建议**：将每个 scanner 改为具体函数签名。例如 `type runRecordScanner interface { ScanRunRecord(...) error }` 改为直接传递 `func(...) error`。或直接内联 scanner 逻辑到调用函数中
- **建议映射的方法**：M-L2-02（Inline Function/Type）
- **风险**：低——只在 store/sqlite 包内部使用，不跨包
- **验证**：AI 自证（跑 `go test ./internal/store/sqlite`）
- **范围**：约 25 行 / 5 个接口 / 5-10 个调用点

### #10 删除 toolfactory closer 接口

- **位置**：`internal/toolfactory/toolset.go:29`
- **分类**：可读性
- **现状**：`type closer interface { Close() error }`，与 `io.Closer` 完全重复
- **问题**：无意义重复定义，增加认知负担
- **建议**：将 `[]closer` 改为 `[]io.Closer`，删除自定义接口
- **建议映射的方法**：M-L2-02（Inline Function/Type）
- **风险**：低——包内部使用，只影响 toolset.go
- **验证**：AI 自证（跑 `go build ./internal/toolfactory`）
- **范围**：约 3 行 / 1 个接口

### #11 合并 notifications 的 APNSDispatcher + FCMDispatcher

- **位置**：`internal/notifications/dispatcher.go:37-43`
- **分类**：可读性
- **现状**：两个单方法接口 `APNSDispatcher` 和 `FCMDispatcher`，分别只有一个外部实现
- **问题**：可以合并为一个 `PushDispatcher` 接口，或改为函数类型。当前 Router 需要同时持有两个字段，增加了不必要的复杂度。
- **建议**：合并为 `type PushDispatcher interface { Dispatch(ctx context.Context, req DispatchRequest) error }`，Router 改为 `map[string]PushDispatcher`。与 #2 一起执行
- **建议映射的方法**：M-L2-05（合并接口）
- **风险**：低——与 #2 一起，只影响 app 包
- **验证**：AI 自证（跑 `go test ./internal/app`）
- **范围**：约 10 行 / 2 个接口合并

### #12 内联 workingstate Store 接口

- **位置**：`internal/workingstate/service.go:10-14`
- **分类**：结构
- **现状**：`Store` 接口有 3 个方法（GetWorkingCheckpoint, UpsertWorkingCheckpoint, DeleteWorkingCheckpoint），只有 SQLite 实现
- **问题**：单实现接口，为测试 mock 而存在。但测试可以用内存 fake（基于 map）替代
- **建议**：删除 Store 接口，`Service.store` 改为 `*sqlite.Store` 具体类型。在测试中创建内存 fake Store（基于 map 的具体类型）替代 mock。与 #5 一起执行
- **建议映射的方法**：M-L2-02（Inline Type）
- **风险**：中——需要改测试中的 mock 实现，改为内存 fake
- **验证**：AI 自证（跑 `go test ./internal/workingstate ./internal/contextplane`）+ HUMAN（确认测试仍覆盖核心路径）
- **范围**：约 5 行 / 1 个接口 / 2-3 个测试文件修改

### #13 内联 runtimehistory SessionSummaryStore 接口

- **位置**：`internal/runtimehistory/session_summary_service.go:10-13`
- **分类**：结构
- **现状**：`SessionSummaryStore` 接口有 2 个方法，只有 SQLite 实现
- **问题**：单实现接口，为测试 mock 存在。与 #12 同理
- **建议**：删除接口，`Service.store` 改为具体类型。测试中改为内存 fake。与 #6 一起执行
- **建议映射的方法**：M-L2-02（Inline Type）
- **风险**：中——与 #6 一起，改 20+ import 点和测试
- **验证**：AI 自证（跑 `go test ./internal/runtimehistory ./internal/contextplane`）
- **范围**：约 4 行 / 1 个接口

### #14 内联 toolresult Ledger 接口

- **位置**：`internal/toolresult/toolresult.go:70-75`
- **分类**：结构
- **现状**：`Ledger` 接口有 4 个方法，只有 SQLite 实现（store/sqlite/store_tool_results.go）。orchestration 测试中有 mock 实现
- **问题**：单实现接口，为测试 mock 存在。跨 25+ 文件依赖，影响面最大
- **建议**：删除 Ledger 接口，`store/sqlite` 中改为具体方法签名。orchestration 测试中改为内存 fake Ledger。与 #7 一起执行
- **建议映射的方法**：M-L2-02（Inline Type）
- **风险**：高——25+ 文件受影响，orchestration 测试需要改 mock
- **验证**：AI 自证（`go test ./internal/orchestration ./internal/store/sqlite ./internal/runtime`）
- **范围**：约 6 行 / 1 个接口 / 3-4 个测试文件修改

### #15 删除 contextplane 的接口重复定义

- **位置**：`internal/contextplane/types.go:91-106`
- **分类**：可读性
- **现状**：`CheckpointService` 和 `SessionSummaryService` 在 contextplane 中定义，但实现分别在 workingstate 和 runtimehistory 包中
- **问题**：接口和实现在不同包，违反了"接口由消费者定义"的 Go 原则（实际上这里接口在消费者包定义，但实现也在不同包）。更关键的是，与 #5 #6 一起，这些接口重复了
- **建议**：在 #5 #6 完成后，contextplane 中保留唯一的 `CheckpointService` 和 `SessionSummaryService` 接口定义，workingstate 和 runtimehistory 中删除重复定义
- **建议映射的方法**：M-L2-02（Inline Type——删除重复）
- **风险**：中——与 #5 #6 绑定，需要协调执行顺序
- **验证**：AI 自证（确认全局 grep 无重复接口定义）
- **范围**：约 10 行 / 2 个重复接口
