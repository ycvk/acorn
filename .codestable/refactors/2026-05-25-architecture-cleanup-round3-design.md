# Round 3 架构重构设计：深层债务清理

基于 `scan.md` 中识别的 3 个问题，制定执行方案。

---

## 问题 1：contextplane 测试耦合 SQLite 实现

### 根因
`contextplane_test.go` 和 `contextplane_graph_test.go` 直接使用 `store/sqlite.Store` 来创建测试环境，而不是定义自己的接口并使用 fake 实现。

### 方案：引入 `contextplane_test` 专用 Store 接口

1. **在 `internal/contextplane` 包内定义最小测试接口**：
   ```go
   type testStore interface {
       CreateSession(ctx context.Context, record events.SessionRecord) error
       LoadSession(ctx context.Context, sessionID string) (events.SessionRecord, error)
       AppendEvent(ctx context.Context, sessionID string, event events.EventRecord) error
       LoadEvents(ctx context.Context, sessionID string) ([]events.EventRecord, error)
       // ... 仅包含 ContextPlane 测试所需的方法
   }
   ```

2. **创建内存 fake 实现**：
   - 新建 `internal/contextplane/testdata/fake_store.go`
   - 使用 `map[string]` 和 `sync.RWMutex` 实现轻量级内存 store
   - 实现 `testStore` 接口的所有方法
   - 不需要持久化、索引、Bleve — 只需要能存取 events

3. **修改测试**：
   - 所有 `store/sqlite.NewStore(...)` 调用改为 `fake_store.New()`
   - 删除 `import "github.com/ycvk/acorn/internal/store/sqlite"`

### 为什么这是最佳实践
- **Go 测试哲学**：测试应该依赖接口，不依赖具体实现
- **Dave Cheney**："Interfaces belong to the package that uses values of the interface type, not the package that implements those values." 测试作为接口的消费者，应该自己定义接口
- **分层清晰**：`contextplane`（业务逻辑层）测试不依赖 `store/sqlite`（持久化层）
- **编译速度**：测试不再编译 SQLite + Bleve + FAISS 的庞大依赖链

### 依赖关系解除后
```
contextplane_test → fake_store (接口实现)
store/sqlite → orchestrationmode (可移除)
orchestration → contextplane (正常)
```
循环依赖被打破。

---

## 问题 4 + 2：orchestrationmode 归并到 events

### 根因
`orchestrationmode` 是一个 18 行的微型包，但 `store/sqlite`（底层持久化）依赖它违反分层。`Mode` 类型本质上是 orchestration 运行模式枚举，与 `events` 包的 `RunStatus`、`EventType` 等枚举是同类的元数据概念。

### 方案：将 `orchestrationmode.Mode` 移动到 `internal/events`

1. **在 `internal/events` 中定义新类型**：
   ```go
   package events
   
   type OrchestrationMode string
   
   const (
       ModeDirectResponse OrchestrationMode = "direct_response"
       ModePlanExecute    OrchestrationMode = "plan_execute"
       ModeSingleAgent    OrchestrationMode = "single_agent"
   )
   
   func (m OrchestrationMode) Normalize() OrchestrationMode { ... }
   ```

2. **更新所有导入者**：
   - 将 `orchestrationmode.Mode` 改为 `events.OrchestrationMode`
   - 将 `orchestrationmode.ModeDirectResponse` 改为 `events.ModeDirectResponse`
   - 等

3. **删除 `internal/orchestrationmode/` 目录**

4. **在 `store/sqlite/mode.go` 中用本地函数替代 `orchestrationmode.Normalize()`**：
   ```go
   func normalizeMode(m string) string {
       switch m {
       case string(events.ModeDirectResponse), string(events.ModePlanExecute), string(events.ModeSingleAgent):
           return m
       default:
           return string(events.ModeDirectResponse)
       }
   }
   ```

### 为什么 `events` 是正确位置
- `events` 包已经被几乎所有包导入（包括 `store/sqlite`、`runtime`、`orchestration`、`app`）
- `Mode` 是事件/运行元数据的一部分，与 `RunStatus`、`SessionStatus` 同类
- 不会引入新的依赖关系
- 消除了微型包，无需额外 import 路径

### 为什么不是 `orchestration`
- `orchestration` 导入 `contextplane`，如果 `store/sqlite` 也导入 `orchestration`，会形成 `store/sqlite` → `orchestration` → `contextplane` → `contextplane_test` → `store/sqlite` 的循环
- `events` 是最底层的枚举/类型包，没有这种循环风险

---

## 执行顺序

1. **Step 1**: 移动 `orchestrationmode` 到 `events`（因为 Step 2 的 fake store 需要用到 `Mode` 类型）
2. **Step 2**: 创建 `contextplane` 的 fake store 并修改测试

每步都需验证：`go build ./...` + `go test ./...`

---

## 不处理的问题（本次明确排除）

- **runtime 拆分子包**：文件级拆分已完成，所有文件 < 1000 行。进一步拆分为子包会引入跨包循环依赖和接口膨胀，收益不足。
- **oauth_handler_test.go 的 store 导入错误**：这是既有问题，非本次重构引入。
