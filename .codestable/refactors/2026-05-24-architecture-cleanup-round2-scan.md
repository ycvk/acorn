---
doc_type: refactor-scan
refactor: 2026-05-24-architecture-cleanup-round2
status: user-reviewed
scope: internal/web/dto.go, internal/app/container.go, internal/app/store_ports.go, internal/orchestration/ports.go, internal/contextplane/ports.go, internal/runtime/runner.go, internal/runtime/plan.go, internal/notifications/, internal/store/
summary: 发现 8 条优化点：结构拆分 5 条 / 行为等价迁移 2 条 / 代码级重构 1 条
---

# Architecture Cleanup Round 2 Scan

## 总览

- **扫描范围**: `internal/web/dto.go`, `internal/app/container.go`, `internal/app/store_ports.go`, `internal/orchestration/ports.go`, `internal/contextplane/ports.go`, `internal/runtime/runner.go`, `internal/runtime/plan.go`, `internal/notifications/`, `internal/store/`
- **发现 8 条优化点**: 结构拆分 5 条 / 行为等价迁移 2 条 / 代码级重构 1 条
- **按风险**: 低 4 条 / 中 3 条 / 高 1 条
- **建议先做**: #1 #2 #5（低风险、独立、AI 可自证）
- **建议慎做 / 后做**: #3 #7（涉及测试回退风险、公开 API 契约）
- **前置检查 7 条全过**: ✓

## 条目

### #1 拆分 web/dto.go 为按领域分组的 DTO 文件 ✓

- **位置**: `internal/web/dto.go`（1821 行）
- **分类**: 结构
- **现状**: 单个文件包含 Thread、Message、Run、Skill、Memory、Device、Settings、Provider 等 8 个领域的所有 DTO 类型定义和转换函数
- **问题**: 文件 1821 行，修改任何领域的 API 格式都需要在千行文件中定位；违反 Go 单一职责原则；新开发者难以快速找到特定领域的 DTO
- **建议**: 按领域拆分为 `dto_thread.go`、`dto_message.go`、`dto_run.go`、`dto_skill.go`、`dto_memory.go`、`dto_device.go`、`dto_settings.go`，每个文件只包含一个领域的 DTO 和转换函数
- **建议映射的方法**: M-L3-01 包内文件拆分
- **风险**: 低（纯类型定义和转换函数，无行为变化，编译失败可立即发现）
- **验证**: AI 自证 — `go build ./...` 通过 + `go test ./internal/web/...` 通过
- **范围**: 约 1821 行 / 1 文件 → 8 文件

### #2 消除 container.go 硬编码工具白名单 ✓

- **位置**: `internal/app/container.go:99-109`
- **分类**: 结构
- **现状**: 9 个工具名字符串硬编码在容器初始化中：`delegate_task`、`memory_search`、`memory_read_file`、`memory_list_files`、`memory_create_file`、`memory_replace_span`、`skill_list`、`skill_view`、`load_tools`
- **问题**: 新增内置工具时容易遗漏此处；工具定义在 `internal/tools/` 但资格判断在 `internal/app/`；同一数组在不同地方可能被重复定义
- **建议**: 在 `internal/tooling/` 引入内置工具注册表（`BuiltinToolRegistry`），所有内置工具在注册表中自注册，容器从注册表读取而非硬编码列表
- **建议映射的方法**: M-L2-01 Extract Function + M-L3-01 引入注册表模式
- **风险**: 低（替换字符串数组为函数调用，行为不变）
- **验证**: AI 自证 — `go build ./...` + `go test ./internal/app/...` + `go test ./internal/tooling/...`
- **范围**: 约 20 行改动 / 2-3 文件

### #3 删除 app/store_ports.go 虚假接口层 ✓

- **位置**: `internal/app/store_ports.go`（91 行，9 个接口）
- **分类**: 行为等价迁移
- **现状**: 定义了 `sessionStore`、`traceStore`、`decisionStore`、`sessionStateStore`、`clientStore`、`inboxStore`、`deviceAuthStore`、`notificationStore`、`pendingActionDecisionStore`，全部 9 个接口的实现者都是同一个 `*sqlite.Store`
- **问题**: 2026-05-24 重构曾删除此文件，因架构测试失败而恢复；接口没有带来抽象价值，反而增加改动成本（给 Store 新增方法需同步修改多个接口）；构成 Go 反模式 "接口爆炸"
- **建议**: 使用 M-L1-01 Parallel Change：1) 保留文件但将接口内联为 `*sqlite.Store` 的具体类型引用；2) 逐服务替换接口依赖为 `*sqlite.Store`；3) 确认全局无引用后删除文件
- **建议映射的方法**: M-L1-01 Parallel Change
- **风险**: 中（删除接口后编译失败，修复方向明确但可能涉及多处改动；上次尝试已失败过一次）
- **验证**: AI 自证 — `go build ./...` + `go test ./internal/app/...` + `go test ./internal/web/...`
- **范围**: 约 91 行删除 + 20+ 处引用替换 / 8-10 文件

### #4 内联 orchestration 空壳抽象到 contextplane ✓

- **位置**: `internal/orchestration/ports.go`（53 行）
- **分类**: 结构
- **现状**: 定义了 `SessionOwner`、`ModelCallRequest`、`ModelInput`、`ToolLifecycleStateView`、`AssembleResultView` 等类型，全部是从 `internal/contextplane/` 复制/子集化的类型；`agent_loop.go` 几乎只是转发调用到 contextplane
- **问题**: 典型的 Middle Man 反模式 — orchestration 包没有引入新的抽象价值，只是从 contextplane "拷贝"类型并做转发；增加包的认知负担和 import 路径噪音
- **建议**: 删除 `orchestration/ports.go`，将 orchestration 包中使用的类型直接引用 `internal/contextplane` 的对应类型；将 `DefaultOverflowChecker` 移动到 contextplane 包或删除（如已有等效检查）
- **建议映射的方法**: M-L2-02 Inline Function（包级别）
- **风险**: 低（类型别名替换，编译器保护，无行为变化）
- **验证**: AI 自证 — `go build ./...` + `go test ./internal/orchestration/...` + `go test ./internal/contextplane/...`
- **范围**: 约 53 行删除 + 引用替换 / 3-5 文件

### #5 统一 ContextPlane 版本消除 V2 撕裂 ✓

- **位置**: `internal/contextplane/ports.go`（42 行，定义 `ContextPlaneV2`）
- **分类**: 结构
- **现状**: `contextplane/context_session.go` 定义 `ContextSession`，`contextplane/ports.go` 定义 `ContextPlaneV2`，但 `RuntimeDeps` 实际使用的是旧版 `ContextPlane` 接口；V2 是 2026-05-24 重构的"半拉子"产物，占着位置但未被采用
- **问题**: 新开发者困惑"应该实现 ContextPlane 还是 ContextPlaneV2"；V2 接口的存在暗示"旧版即将被替换"，但实际情况并非如此；增加维护成本
- **建议**: 评估 `ContextPlaneV2` 的设计是否确实优于 `ContextPlane`：如果是，用 M-L1-01 Parallel Change 全面迁移到 V2 后删除旧版；如果不是，直接删除 V2 接口和相关代码
- **建议映射的方法**: M-L1-01 Parallel Change 或 M-L2-02 Inline Function
- **风险**: 中（涉及运行时核心接口，需确认 V2 的设计意图和当前使用状态）
- **验证**: AI 自证 — `go build ./...` + `go test ./internal/contextplane/...` + `go test ./internal/runtime/...`
- **范围**: 约 42 行 + 相关引用 / 3-5 文件

### #6 合并微型包减少包数量噪音 ✓

- **位置**: `internal/notifications/`（1 文件）、`internal/store/`（1 文件）、`internal/orchestrationmode/`（2 文件）、`internal/providerusage/`（2 文件）
- **分类**: 结构
- **现状**: 45 个 internal 子目录中，约 10 个目录只包含 1-2 个文件；`internal/notifications/` 只有一个路由器定义；`internal/store/` 只是一个 alias/proxy；`internal/orchestrationmode/` 只有两个字符串常量
- **问题**: 包数量噪音增加认知负担；import 路径碎片化；每个包都需要独立维护 go.mod 依赖关系
- **建议**: `internal/notifications/` → 合并到 `internal/app/`；`internal/store/` → 评估是否可以直接使用 `internal/store/sqlite/` 导出；`internal/orchestrationmode/` → 合并到 `internal/orchestration/` 或 `internal/runtime/`；`internal/providerusage/` → 合并到 `internal/runtime/` 或 `internal/app/`
- **建议映射的方法**: M-L3-01 包合并
- **风险**: 低（文件移动，import 路径变更，编译器保护）
- **验证**: AI 自证 — `go build ./...` + `go test ./...`
- **范围**: 约 4 包 / 8-10 文件移动

### #7 拆分 runtime 巨型文件（runner.go + plan.go）✓

- **位置**: `internal/runtime/runner.go`（1967 行）、`internal/runtime/plan.go`（2225 行）
- **分类**: 结构
- **现状**: `runner.go` 包含 RunnerFactory 结构体、New 构造函数、BuildCapabilityCatalog、BuildServeToolset、工具集组装、MCP 管理、能力目录等；`plan.go` 包含 PlanNode、ActNode、ObserveNode、PlanEvidence、PlanGate 等 Plan/Act/Observe 全周期逻辑
- **问题**: 文件超过 1500 行后导航困难、代码审查效率低、同一文件承担多个职责；上次 2026-05-24 重构已合并大量文件但仍残留这两个最大文件
- **建议**: `runner.go` → 拆分为 `runner_factory.go`（构建相关）、`runner_toolset.go`（工具集管理）、`runner_catalog.go`（能力目录）；`plan.go` → 拆分为 `plan_node.go`（计划节点）、`plan_act.go`（执行节点）、`plan_observe.go`（观察节点）、`plan_evidence.go`（证据管理）
- **建议映射的方法**: M-L3-01 包内文件拆分
- **风险**: 中（涉及核心运行时逻辑，文件拆分需确保类型可见性和循环依赖不引入；上次重构已证明此风险可控）
- **验证**: AI 自证 — `go test ./internal/runtime/...` 全部通过 + `go build ./...`
- **范围**: 约 4192 行 / 2 文件 → 6-7 文件

### #8 提取 container.go 初始化逻辑为领域 Factory 方法 ✓

- **位置**: `internal/app/container.go:272-432`（`buildContainer` 函数 160 行）
- **分类**: 代码级重构
- **现状**: `buildContainer()` 按硬编码顺序初始化 20+ 个服务，包含 `memoryModule.BuildIndex`、`semanticIndex` 构建、`contextPlane` 组装、`runnerFactory` 创建、`executors` 工厂、`mcpServer` 构建等；使用 `committed` 布尔标志 + `defer` 做回滚保护
- **问题**: 函数 160 行，圈复杂度高；新增服务必须修改此文件；没有编译器保护的初始化顺序依赖；不同领域（memory、context、runtime、mcp）的初始化逻辑混在一个函数中
- **建议**: 按领域提取 Factory 方法：`buildMemoryServices()`、`buildContextPlane()`、`buildRuntimeDeps()`、`buildMCPServer()`；每个 Factory 返回其领域需要的结构体和 error；`buildContainer` 变成协调者而非实现者
- **建议映射的方法**: M-L2-01 Extract Function
- **风险**: 低（纯提取函数，无行为变化，编译器保护）
- **验证**: AI 自证 — `go build ./...` + `go test ./internal/app/...`
- **范围**: 约 160 行重构 / 1 文件
