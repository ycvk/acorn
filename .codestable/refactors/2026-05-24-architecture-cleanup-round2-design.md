# Acorn Architecture Cleanup Round 2 — Design

**对应扫描**: `2026-05-24-architecture-cleanup-round2-scan.md`
**日期**: 2026-05-24
**状态**: draft（待用户审批）

## 执行顺序（按依赖拓扑排序）

| 步骤 | 条目 | 方法 | 前置条件 | 退出信号 | 验证方 | 风险 |
|---|---|---|---|---|---|---|
| 1 | #1 拆分 web/dto.go | M-L3-01 | 无 | 1 文件 → 8 文件，编译通过 | AI | 低 |
| 2 | #8 提取 container.go Factory | M-L2-01 | 无 | 160 行 → 5 个 Factory 方法 | AI | 低 |
| 3 | #2 消除硬编码白名单 | M-L2-01 + M-L3-01 | Step 2 完成 | 字符串数组 → 注册表 | AI | 低 |
| 4 | #4 内联 orchestration 空壳 | M-L2-02 | 无 | 53 行删除，引用替换 | AI | 低 |
| 5 | #6 合并微型包 | M-L3-01 | Step 4 完成（orchestrationmode 依赖） | 4 包 → 0 新包 | AI | 低 |
| 6 | #5 统一 ContextPlane | M-L1-01 或 M-L2-02 | Step 4 完成 | V2 消失 | AI | 中 |
| 7 | #3 删除 store_ports 虚假接口 | M-L1-01 | Step 2 完成 | 9 接口 → 0 | AI | 中 |
| 8 | #7 拆分 runtime 巨型文件 | M-L3-01 | Step 6 完成 | 2 文件 → 6-7 文件 | AI | 中 |

## 最佳实践调研结论

### DTO 组织

**社区共识**: 按领域/边界拆分，不是一个大文件。

- **Kubernetes**: `pkg/apis/core/types.go`（7228 行，包含 Pod/Service/Node 等核心类型）说明大型项目允许单个领域内的大文件，但**不同领域必须在不同文件/包**中
- **Go 社区文章**: "Your Go Clean Architecture Is Just Java in Disguise" 主张 DTO 应该留在需要它们的边界，不要创建顶层的 `dto/` 包
- **结论**: `internal/web/dto.go` 的 1821 行包含 8 个领域，明显违反了这一原则。应该按领域拆分为 `dto_thread.go`、`dto_skill.go` 等

### 依赖注入

**大型 Go 项目的主流模式**:

- **Kubernetes、Prometheus、Caddy**: 全部使用**手动构造函数注入**，不使用 Wire/fx/dig 等框架
- **Go 谚语**: "Accept interfaces, return concrete types"
- **何时用框架**: 当依赖图超过 30-40 个节点且手动 wiring 容易出错时考虑 Wire。Acorn 当前约 20 个服务，手动 wiring 仍可接受，但需要结构化
- **结论**: `container.go` 应该使用**构造函数模式**和**领域 Factory 方法**来改善，不需要引入外部 DI 框架（避免构建复杂化和团队学习成本）

### 接口设计

**Go 社区原则**:

- **Rob Pike**: "The bigger the interface, the weaker the abstraction"
- **Dave Cheney**: "Don't use an interface if you don't have multiple implementations"
- **Jack Lindamood**: "Go interfaces generally belong in the package that uses values of the interface type, not the package that implements those values"
- **结论**: `store_ports.go` 的 9 个微型接口全部指向同一个实现，是典型的 "接口污染"。应该删除。接口应该在使用方定义（如需要的话），而不是在 app 层为 store 定义

### 包大小

**社区指导**:

- 应用代码推荐每包 < 20 个非测试文件
- 单个文件推荐 < 500 行（可放宽到 800 行对于复杂领域）
- Go 标准库中 `net/http` 有 40+ 文件，`crypto/tls` 有 30+ 文件 — 但标准库是特殊情况
- **结论**: Acorn 的 `internal/runtime/`（71 文件）、`web/dto.go`（1821 行）、`runtime/runner.go`（1967 行）都超出了健康范围

### 文件命名

- 不要在文件名中重复包名（如 `web/dto.go` 在 `web` 包中就是 `dto.go`，不要叫 `web_dto.go`）
- 测试文件：`thing_test.go`
- 按职责命名：`thing.go` 包含类型定义和核心逻辑，`thing_helpers.go` 包含辅助函数

---

## 详细执行方案

### Step 1: 拆分 web/dto.go（#1）

**目标**: 将 1821 行的 DTO 单文件按领域拆分为 8 个文件

**拆分策略**:

```
dto.go                  → 保留通用/基础类型（Time格式化辅助、optional辅助等）
                        → 删除所有领域特定 DTO
dto_thread.go           → ThreadDTO, ThreadListResponse, threadDTOsFromDomain 等
dto_message.go          → MessageDTO, MessagePartDTO, messagePartDTOFromDomain 等
dto_run.go              → RunDTO, RunEventDTO, RunEventDetail, runEventDTOFromDomain 等
dto_skill.go            → SkillDTO, SkillListResponse, SkillMatchDTO 等
dto_memory.go           → MemoryRecordDTO, MemorySearchResponse 等
dto_device.go           → DeviceDTO, DeviceListResponse, PairDeviceRequest/Response 等
dto_settings.go         → ClientSettingsDTO, ClientProviderSettingsDTO 等
```

**注意事项**:
- 不要在文件名中重复 `web` 包名（保持 `dto_xxx.go` 而非 `web_dto_xxx.go`）
- 测试文件如果对应特定领域 DTO（如 `message_dto_test.go`），保持不动
- 所有转换函数（`xxxDTOFromDomain`）随 DTO 类型一起移动
- 保持 `dto.go` 中的通用辅助函数（如 `optionalDeviceTime`、`formatTimestamp` 等）

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/web/...` 全部通过
- `internal/web/` 下不再有 > 500 行的单个 DTO 文件

---

### Step 2: 提取 container.go 领域 Factory（#8）

**目标**: 将 `buildContainer()` 的 160 行初始化逻辑按领域提取为 Factory 方法

**提取策略**:

```go
// buildContainer 变成协调者
func buildContainer(ctx context.Context, cfg *config.Config) (*Container, error) {
    store, err := storesqlite.Open(cfg.Runtime.StorageDir)
    if err != nil { return nil, err }
    defer cleanupUnlessCommitted(store)

    ws, err := cfg.Workspace()
    if err != nil { return nil, err }

    // 领域 Factory 调用
    memoryServices, err := buildMemoryServices(ctx, cfg, store)
    if err != nil { return nil, err }

    contextPlane, err := buildContextPlane(cfg, store, memoryServices)
    if err != nil { return nil, err }

    runtimeDeps, err := buildRuntimeDeps(cfg, store, ws, memoryServices, contextPlane)
    if err != nil { return nil, err }

    // ... 用 runtimeDeps 组装 services
    container := assembleServices(cfg, store, runtimeDeps)

    mcpServer, err := buildMCPServer(cfg, runtimeDeps.RunnerFactory)
    if err != nil { return nil, err }
    container.mcpServer = mcpServer

    committed = true
    return container, nil
}
```

**注意事项**:
- 每个 Factory 方法接收其需要的依赖参数，返回领域服务对象和 error
- `committed` 和 `defer` 回滚逻辑保留在 `buildContainer` 中
- 如果领域初始化失败，由调用方处理 error（不需要每个 Factory 都处理 committed 逻辑）

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/app/...` 通过
- `buildContainer` 函数 < 80 行

---

### Step 3: 消除硬编码工具白名单（#2）

**目标**: 用内置工具注册表替换 `container.go:99-109` 的硬编码字符串数组

**实现策略**:

在 `internal/tooling/` 引入:

```go
// builtin_registry.go
package tooling

var builtinToolRegistry = []string{
    "delegate_task",
    "memory_search",
    // ... 所有内置工具
}

func BuiltinToolNames() []string {
    return append([]string(nil), builtinToolRegistry...)
}
```

然后 `container.go` 中:
```go
// 替换
for _, name := range []string{"delegate_task", ...} { ... }
// 为
for _, name := range tooling.BuiltinToolNames() { ... }
```

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/app/...` + `go test ./internal/tooling/...` 通过
- `container.go` 中无硬编码工具名字符串

---

### Step 4: 内联 orchestration 空壳（#4）

**目标**: 删除 `internal/orchestration/ports.go`，让 orchestration 直接使用 contextplane 类型

**执行步骤**:

1. 在 `orchestration/agent_loop.go` 中，将 `SessionOwner` 引用替换为 `contextplane.ContextSession`
2. 将 `ModelCallRequest` 替换为 `contextplane.ModelCallRequest`
3. 将 `ModelInput` 替换为 `contextplane.ModelInput`
4. 将 `DefaultOverflowChecker` 和 `IsContextOverflowError` 移动到 `contextplane/` 或删除（如果 contextplane 已有等效检查）
5. 删除 `orchestration/ports.go`

**注意事项**:
- `orchestration` 包导入 `internal/contextplane` 是已有依赖，无需新增 import
- 检查 `orchestration/agent_loop.go` 是否使用了 `contextplane` 中不存在的方法（如 `SessionOwner` 的字段比 `ContextSession` 少）

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/orchestration/...` 通过
- `orchestration/ports.go` 文件不存在

---

### Step 5: 合并微型包（#6）

**目标**: 将 4 个微型包合并到相关父包

**合并策略**:

```
internal/notifications/          → 合并到 internal/app/
  - notification_router.go       → internal/app/notification_router.go

internal/store/                  → 评估后决定
  - 如果 store/ 只是 sqlite.Store 的 alias → 删除，全局改为 storesqlite
  - 如果 store/ 包含通用接口 → 保留但精简

internal/orchestrationmode/      → 合并到 internal/orchestration/ 或 internal/runtime/
  - mode.go (2 个字符串常量)  → 移动到 orchestration/types.go

internal/providerusage/          → 合并到 internal/runtime/ 或 internal/app/
  - usage.go                    → 移动到 runtime/provider_usage.go
```

**注意事项**:
- 合并后更新所有 import 路径
- `goimports` 会自动处理大部分 import 变更
- 确保没有循环依赖引入

**退出信号**:
- `go build ./...` 通过
- `go test ./...` 通过
- `internal/` 下 1-2 文件的包数量减少 50%

---

### Step 6: 统一 ContextPlane 版本（#5）

**目标**: 消除 `ContextPlaneV2`，统一为单一接口

**决策树**:

1. **先评估**: `ContextPlaneV2` 是否确实比 `ContextPlane` 设计更好？
   - 读取 `contextplane/ports.go` 中 `ContextPlaneV2` 的方法列表
   - 对比 `ContextSession` 和旧版 `ContextPlane` 的方法列表
   - 如果 V2 更简洁/更聚焦 → 迁移到 V2
   - 如果 V2 是实验性的/未完成的 → 删除 V2

2. **当前证据**: `RuntimeDeps` 使用的是 `ContextPlane`（不是 V2），说明 V2 未被采用

3. **执行**: 删除 `ContextPlaneV2` 接口和 `BuildHandlersRequest` 中 V2 相关字段；如果 `ContextPlaneV2` 有独特的方法被其他地方引用，将其方法合并到 `ContextPlane`

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/contextplane/...` 通过
- `contextplane/ports.go` 中无 `V2` 字样

---

### Step 7: 删除 store_ports.go 虚假接口（#3）

**目标**: 删除 9 个微型接口，让 service 直接依赖 `*sqlite.Store`

**执行步骤**（M-L1-01 Parallel Change）:

1. **阶段 A**: 保留 `store_ports.go`，但在每个 service 构造函数中新增一个接受 `*sqlite.Store` 的重载版本（或修改现有版本）
2. **阶段 B**: 逐个修改 service 文件，将接口字段改为 `*sqlite.Store` 字段
   - `SessionService` 的 `store sessionStore` → `store *storesqlite.Store`
   - `TraceService` 的 `store traceStore` → `store *storesqlite.Store`
   - ... 依此类推
3. **阶段 C**: 确认 `store_ports.go` 中所有接口全局无引用（用 grep 验证）
4. **阶段 D**: 删除 `store_ports.go`

**为什么上次失败**:
- 上次直接删除后编译失败，说明有 service 或 web handler 依赖于这些接口的特定方法子集
- 解决方案：不直接删除，而是**逐步替换每个 service 的字段类型**，每次替换后编译验证

**退出信号**:
- `go build ./...` 通过
- `go test ./internal/app/...` + `go test ./internal/web/...` 通过
- `grep -r "sessionStore\|traceStore\|decisionStore" internal/` 返回 0 结果

---

### Step 8: 拆分 runtime 巨型文件（#7）

**目标**: 将 `runner.go`（1967 行）和 `plan.go`（2225 行）按职责拆分

**runner.go 拆分策略**:

```
runner.go                  → 保留 RunnerFactory 结构体定义和 New 构造函数
                        → 删除工具集/目录/MCP 相关方法
runner_toolset.go          → BuildCapabilityCatalog、BuildServeToolset、buildToolset 等
runner_catalog.go          → Registry 相关、工具目录管理
runner_mcp.go              → MCP 管理、BuildServeToolset 中 MCP 部分
runner_helpers.go          → 工具名处理、eligibility 辅助函数
```

**plan.go 拆分策略**:

```
plan.go                    → 保留 PlanNode、PlanState 核心类型
                        → 删除 Act/Observe/Evidence 相关
plan_act.go                → ActNode、Act 执行逻辑
plan_observe.go            → ObserveNode、Observe 执行逻辑
plan_evidence.go           → PlanEvidence、证据收集/验证
plan_gate.go               → PlanGate、Gate 逻辑
plan_helpers.go            → Plan 相关辅助函数
```

**注意事项**:
- 上次 2026-05-24 重构已合并了大量文件，但仍残留这两个最大文件
- 拆分后确保没有引入包内循环依赖（Go 允许包内循环，但逻辑上应避免）
- 测试文件跟随被测试的文件一起移动

**退出信号**:
- `go test ./internal/runtime/...` 全部通过
- `go build ./...` 通过
- 单个文件 < 1000 行

---

## 回滚策略

| 步骤 | 回滚方式 | 触发条件 |
|---|---|---|
| 1-5 | `git checkout -- <files>` | 编译失败且 10 分钟内无法修复 |
| 6 | `git revert` + 保留 V2 | 删除 V2 后发现有未发现的依赖 |
| 7 | `git checkout -- internal/app/store_ports.go` + 恢复字段类型 | 替换 Store 类型后编译失败 |
| 8 | `git checkout -- internal/runtime/` | 拆分后测试失败 |

**全局策略**: 每个步骤独立提交，确保可以随时回退到上一步的已知良好状态。

---

## 验证矩阵

| 步骤 | 编译 | 单元测试 | 集成测试 | 人工检查 |
|---|---|---|---|---|
| 1 | ✓ | ✓ | - | - |
| 2 | ✓ | ✓ | - | - |
| 3 | ✓ | ✓ | - | - |
| 4 | ✓ | ✓ | - | - |
| 5 | ✓ | ✓ | - | - |
| 6 | ✓ | ✓ | ✓ | - |
| 7 | ✓ | ✓ | ✓ | - |
| 8 | ✓ | ✓ | ✓ | - |
