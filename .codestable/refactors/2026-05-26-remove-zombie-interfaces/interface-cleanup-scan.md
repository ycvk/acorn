# 重构扫描报告：消除僵尸接口与类型重复

## 扫描范围

- `internal/app/` — 21 个接口定义，检查单实现/多实现情况
- `internal/decision/` — 决策引擎，检查合并到 runtime 的可行性
- `internal/runbase/`, `internal/stream/`, `internal/runtime/stream/`, `internal/runtime/api/`, `internal/runtime/trace.go` — 类型重复检查

## 发现清单

### L1 行为等价迁移（结构性问题）

#### 1. `internal/app/store_ports.go` — 9 个单实现接口 [高风险/高回报]

**现状**：
```go
type sessionStore interface { ... }
type pendingActionDecisionStore interface { ... }
type traceStore interface { ... }
type decisionStore interface { ... }
type sessionStateStore interface { ... }
type clientStore interface { ... }
type inboxStore interface { ... }
type notificationStore interface { ... }
type deviceAuthStore interface { ... }
```

**问题**：这 9 个接口全部只有 **1 个实现**：`internal/store/sqlite.Store`。它们纯粹是为了将一个大结构体的不同方法子集暴露给不同的 service，但 Go 不需要这种 Java 式的切割。

**收益**：删除 9 个接口 + 消除 9 个适配层 → 直接理解数据流。

**方法**：M-L1-03 Parallel Change（先将接口替换为 concrete type，再删除接口定义）

---

#### 2. `internal/app/` 其他单实现接口 [高风险/高回报]

**现状**：
- `capability_service.go`: `skillSnapshotStore`, `liveMCPManager`, `toolCatalogBuilder` — 各 1 实现
- `trace_service.go`: `traceStore`, `runtimeWorkbenchPlanStore` — 各 1 实现
- `skill_service.go`: `stableSkillScanner` — 1 实现
- `workbench_plan_adapter.go`: `runtimePlanRecordStore` — 1 实现
- `container.go`: `executorHandle` — 1 实现
- `device_auth_service.go`: `PendingActionCreateStore` — 1 实现
- `pending_action_service.go`: `inboxCapabilityService` — 1 实现

**总计**：`internal/app/` 中 **21 个接口**，预计 **19-20 个可以删除**。

**需要保留的**：
- `PushDispatcher`（`internal/app/notification_dispatcher.go`）— 可能有多个推送后端实现
- 可能 `ChatStore`（需验证测试是否 mock）

---

#### 3. `internal/decision/` — 过度抽象的决策引擎 [中风险/中回报]

**现状**：9 个文件构成一个完整的决策引擎包，核心逻辑在 `engine.go` 中是一个 if-else 链：
```go
if hasCapabilityFailure(...) { ... }
else if explicitID := ...; explicitID != "" { ... }
else if route := routeForIntent(...); route != nil { ... }
else if top, ok := topRecommendedSkill(...); ok { ... }
else if !input.HasWorkingContext { ... }
```

**问题**：
- 这种简单路由逻辑不值得一个独立包 + Profile/Route/Action 抽象
- `internal/runtime/` 已经深度依赖 `decision` 包（`run.go`, `runtime_deps.go`, `store_ports.go`, `runner_factory_skills_test.go`）
- `ProfileService` 只是读取 YAML 文件，逻辑极简

**合并策略**：将 `decision.DecideInput`, `decision.Record`, `decision.Action`, `decision.Profile`, `decision.ProfileService`, `decision.Engine` 内联到 `internal/runtime/decision.go`，删除 `internal/decision/` 包。

---

#### 4. `internal/runtime/stream/` 是 `internal/stream/` 的完整副本 [低风险/高回报]

**验证**：`diff internal/stream/api.go internal/runtime/stream/api.go` — 零差异。

**影响**：
- `internal/runtime/alias_stream.go` 提供 re-export "for backward compatibility"
- `internal/runtime/plan.go` 导入 `internal/runtime/stream`
- `internal/providers/mcp/` 导入 `internal/stream`

**策略**：删除 `internal/runtime/stream/`，让 `internal/runtime/` 的代码直接导入 `internal/stream/`。删除 `alias_stream.go`。

---

#### 5. `Result`, `TraceSummary`, `SessionState` 在 4+ 个地方重复定义 [中风险/高回报]

**映射**：
| 类型 | 定义位置 | 使用位置 |
|---|---|---|
| Result | `internal/runtime/executor.go` | runtime 内部 |
| Result | `internal/runbase/types.go` | 声称是"canonical source" |
| Result | `internal/stream/api.go` | stream 包 |
| Result | `internal/runtime/stream/api.go` | stream 副本 |
| TraceSummary | `internal/runbase/types.go` | 声称是"canonical source" |
| TraceSummary | `internal/stream/api.go` | stream 包 |
| TraceSummary | `internal/runtime/stream/api.go` | stream 副本 |
| TraceSummary | `internal/runtime/trace.go` | runtime 内部 |
| SessionState | `internal/stream/api.go` | stream 包 |
| SessionState | `internal/runbase/types.go` | 声称是"canonical source" |
| SessionState | `internal/runtime/stream/api.go` | stream 副本 |
| SessionState | `internal/runtime/api/api.go` | runtime/api 包 |

**策略**：
1. 删除 `internal/runbase/` 包（它的目标"成为 single source of truth"失败了）
2. 删除 `internal/runtime/stream/`（副本）
3. 将 `Result`, `TraceSummary` 统一归到 `internal/events/`（它们已经是 `events` 的紧密相关概念）
4. 将 `SessionState` 统一到 `internal/events/`
5. `internal/runtime/executor.go` 和 `internal/runtime/trace.go` 使用 `events.Result` 和 `events.TraceSummary`
6. `internal/stream/api.go` 继续使用自己的类型或也统一

---

## 方法映射

| 条目 | 方法号 | 方法名 |
|---|---|---|
| 1 | M-L1-03 | Parallel Change |
| 2 | M-L1-03 | Parallel Change |
| 3 | M-L1-01 | Strangler Fig |
| 4 | M-L1-01 | Strangler Fig |
| 5 | M-L1-03 | Parallel Change |

---

## 执行顺序建议

1. **先做条目 4**（删除 runtime/stream 副本）— 零行为变更，纯粹删除死代码
2. **再做条目 5**（统一类型定义）— 配合条目 1 减少编译错误
3. **然后做条目 1+2**（删除僵尸接口）— 大规模但机械化的替换
4. **最后做条目 3**（合并 decision 引擎）— 涉及逻辑移动，需要最小心

---

## 风险矩阵

| 条目 | 编译风险 | 测试风险 | 行为变更风险 | 回滚难度 |
|---|---|---|---|---|
| 1 | 低 | 低 | 零 | 低 |
| 2 | 低 | 低 | 零 | 低 |
| 3 | 中 | 中 | 零 | 中 |
| 4 | 低 | 低 | 零 | 低 |
| 5 | 中 | 低 | 零 | 中 |
