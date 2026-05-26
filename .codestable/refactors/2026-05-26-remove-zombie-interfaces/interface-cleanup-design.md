# 重构设计方案：消除僵尸接口与类型重复

## 状态

status: approved

## 执行顺序

| 阶段 | 目标 | 验证方式 |
|---|---|---|
| Phase 1 | 删除 `internal/runtime/stream/` 副本 | `go build ./...` |
| Phase 2 | 统一核心类型定义（Result/TraceSummary/SessionState） | `go build ./...` + 相关测试 |
| Phase 3 | 删除 `internal/app/` 中的僵尸接口 | `go build ./...` + 全部测试 |
| Phase 4 | 合并 `internal/decision/` 到 `internal/runtime/` | `go build ./...` + 全部测试 |

---

## Phase 1: 删除 `internal/runtime/stream/` 副本

### 变更范围

- **删除**：`internal/runtime/stream/` 整个目录（14 个文件）
- **删除**：`internal/runtime/alias_stream.go`（它的唯一目的是 re-export 副本中的类型）
- **修改**：`internal/runtime/plan.go` — 将 `import "github.com/ycvk/acorn/internal/runtime/stream"` 改为 `import "github.com/ycvk/acorn/internal/stream"`
- **验证**：`internal/providers/mcp/` 已经使用 `internal/stream`，确认 `internal/runtime/` 中的 StreamItem 等类型使用方也能直接导入 `internal/stream`

### 退出信号

- `go build ./...` 零错误
- 测试通过：`go test ./internal/runtime/... ./internal/providers/mcp/...`

---

## Phase 2: 统一核心类型定义

### 问题

`Result`、`TraceSummary`、`SessionState` 在 4+ 个位置重复定义。

### 策略

**目标 canonical source**：
- `Result` → `internal/runbase/` 保留，其他包使用 `runbase.Result`
- `TraceSummary` → `internal/runbase/` 保留，其他包使用 `runbase.TraceSummary`
- `SessionState` → `internal/runbase/` 保留，其他包使用 `runbase.SessionState`
- `StreamItemKind` 等流类型 → 已归 `internal/stream/` 所有

**具体步骤**：

1. `internal/runtime/executor.go`：删除本地 `Result` 定义，改为 `runbase.Result`
2. `internal/runtime/trace.go`：删除本地 `TraceSummary` 定义，改为 `runbase.TraceSummary`
3. `internal/stream/api.go`：删除本地 `Result`、`TraceSummary`、`SessionState` 定义，改为使用 `runbase` 类型
4. `internal/runtime/api/api.go`：删除本地 `SessionState` 定义，改为 `runbase.SessionState`
5. `internal/runtime/stream/api.go`（将在 Phase 1 中删除，所以不需要修改）

### 退出信号

- `go build ./...` 零错误
- 相关测试通过

---

## Phase 3: 删除 `internal/app/` 中的僵尸接口

### 策略

Go 最佳实践："Accept interfaces, return structs"。接口应该由**消费者**定义，且只在**有多个实现**或**测试需要 mock** 时才存在。

### 具体接口处理

#### 3.1 `store_ports.go` — 9 个接口 → 全部删除

```go
// BEFORE: store_ports.go 定义 9 个接口

type sessionStore interface { ... }
type pendingActionDecisionStore interface { ... }
// ... 等等

// AFTER: 直接依赖 sqlite.Store 的 concrete type
```

**修改方式**：
1. 删除 `store_ports.go` 文件
2. 在所有引用这些接口的 service 中，将字段类型从接口改为 `*sqlite.Store`
3. 如果 service 只需要 store 的部分方法，保持这样也可以——`*sqlite.Store` 仍然只会使用它需要的那些方法

#### 3.2 `capability_service.go` — 3 个接口 → 全部删除

- `skillSnapshotStore` → 使用 `*skills.Loader` 或 `skills.Scanner`
- `liveMCPManager` → 使用 `*mcpprovider.Manager`
- `toolCatalogBuilder` → 使用 `*tooling.CatalogBuilder`（或内联逻辑）

#### 3.3 `trace_service.go` — 2 个接口 → 删除

- `traceStore` → 使用 `*sqlite.Store`
- `runtimeWorkbenchPlanStore` → 这是一个已存在的接口，可能用于适配器模式。检查它是否只有一个实现，如果是则删除。

#### 3.4 其他单文件接口

- `ChatStore` → 检查测试后决定
- `stableSkillScanner` → 使用 `skills.Loader` 或 `skills.Scanner` concrete type
- `runtimePlanRecordStore` → 使用 `*sqlite.Store`
- `executorHandle` → 使用 `*runtime.Executor`
- `PendingActionCreateStore` → 使用 `*sqlite.Store`
- `inboxCapabilityService` → 使用 `*CapabilityService`

### 退出信号

- `go build ./...` 零错误
- `go test ./internal/app/...` 全部通过

---

## Phase 4: 合并 `internal/decision/` 到 `internal/runtime/`

### 策略

将 `internal/decision/` 的核心类型和逻辑移动到 `internal/runtime/decision.go`，然后删除原包。

### 需要移动的内容

**类型**（移动到 `internal/runtime/decision_types.go`）：
- `decision.Record`
- `decision.DecideInput`
- `decision.Action` 及常量
- `decision.Profile` 及常量
- `decision.Route`
- `decision.RecommendedSkill`
- `decision.Engine`

**逻辑**（移动到 `internal/runtime/decision.go`）：
- `decision.NewEngine()`
- `decision.Engine.Decide()`
- `decision.NewProfileService()`
- `decision.ProfileService.Get()`

### 引用更新

1. `internal/runtime/run.go` — 引用 `decision.ProfileService` 等，改为本地引用
2. `internal/runtime/runtime_deps.go` — 引用 `decision.ProfileService`，改为本地引用
3. `internal/runtime/store_ports.go` — 引用 `decision.Record`，改为本地引用
4. `internal/runtime/runner_factory_skills_test.go` — 引用 `decision` 包，改为本地引用
5. `internal/app/trace_service.go` — 引用 `decision.Record`，改为 `runtime.Record` 或本地类型

### 退出信号

- `go build ./...` 零错误
- `go test ./internal/runtime/...` 全部通过
- `go test ./internal/decision/...` 不再存在（包已删除）

---

## 回滚策略

每个 Phase 独立提交，如果某个 Phase 出现问题：
1. 回滚到该 Phase 开始前的 commit
2. 记录失败原因
3. 调整方案后重试

---

## 验证清单

- [ ] Phase 1: `go build ./...` 通过
- [ ] Phase 2: `go build ./...` 通过
- [ ] Phase 2: `go test ./internal/runtime/... ./internal/stream/...` 通过
- [ ] Phase 3: `go build ./...` 通过
- [ ] Phase 3: `go test ./internal/app/...` 通过
- [ ] Phase 4: `go build ./...` 通过
- [ ] Phase 4: `go test ./internal/runtime/...` 通过
- [ ] 全量: `go test ./...` 通过
- [ ] 格式: `make format-check` 通过
- [ ] Lint: `make lint` 通过
