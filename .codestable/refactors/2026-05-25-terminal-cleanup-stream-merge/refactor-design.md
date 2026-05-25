# Refactor Design: Terminal Session Cleanup & Stream Package Merge

## 目标

1. **彻底删除 Terminal Session 功能**（~2,700 行代码 + SQLite schema）
2. **合并 `internal/runtime/stream/` 与 `internal/runstream/`**（消除 ~500 行重复类型定义）

---

## 问题 A：Terminal Session 彻底清理

### 背景

AGENTS.md 明确声明：**"不要恢复旧终端界面"**。但 terminal session 功能仍在代码库中残留，涉及 20+ 文件引用。这是政策与代码的不一致，必须清理。

### 影响范围分析

| 组件 | 文件 | 引用内容 | 行数 |
|------|------|---------|------|
| **核心包** | `internal/terminalsession/*.go` | Service, types, tests | ~1,075 |
| **Store** | `internal/store/sqlite/store_terminal.go` | CRUD methods | 302 |
| **Schema** | `internal/store/sqlite/store_schema.go` | terminal_sessions / terminal_session_logs 表定义 | ~30 |
| **Tools** | `internal/tools/terminal_session_tools.go` | 7 个终端工具定义 | 400 |
| **Tool Ports** | `internal/tools/ports.go` | TerminalService 接口 | ~10 |
| **Runtime Deps** | `internal/runtime/runtime_deps.go` | TerminalService 字段 | 1 |
| **Runtime Build** | `internal/runtime/runner_build.go` | buildTerminalSessionService | ~20 |
| **Runtime Toolset** | `internal/runtime/runner_toolset_build.go` | TerminalService nil check | 3 |
| **Workbench** | `internal/app/workbench_service.go` | ListTerminalSessionsByRun, buildTerminalSessionSummaries | ~40 |
| **Workbench Types** | `internal/app/workbench_types.go` | TerminalSessionSummary, TerminalSessionLogSummary | ~30 |
| **Web DTO** | `internal/web/dto_context.go` | TerminalSessionDTO, TerminalSessionLogDTO | ~40 |
| **Web DTO** | `internal/web/dto_run_detail.go` | TerminalSessions 字段 | 1 |
| **OpenAPI Test** | `internal/web/openapi_test.go` | RunTerminalSession, RunTerminalSessionLog stale guards | 2 |
| **Trace** | `internal/app/trace_service.go` | traceStore 接口含终端方法 | ~5 |
| **Tooling** | `internal/tooling/specs.go` | 可能引用终端工具类型 | ? |
| **Toolresult** | `internal/toolresult/toolresult.go` | 可能引用终端相关 | ? |

### 删除策略

采用 **Hard Cutover（硬切换）**，不保留向后兼容。因为功能已废弃，无活跃消费者。

#### Phase 1：停写（停止构建终端工具）

**操作**：
1. 删除 `internal/tools/terminal_session_tools.go`
2. 修改 `internal/tools/tools.go` — 从 BuildCatalog 中移除终端工具注册
3. 修改 `internal/tools/ports.go` — 删除 TerminalService 接口定义

**验证**：`go build ./internal/tools/...`

#### Phase 2：停读（移除 Runtime 依赖）

**操作**：
1. 修改 `internal/runtime/runtime_deps.go` — 删除 `TerminalService` 字段
2. 修改 `internal/runtime/runner_build.go` — 删除 `buildTerminalSessionService` 函数和调用
3. 修改 `internal/runtime/runner_toolset_build.go` — 删除 `TerminalService == nil` 检查
4. 检查 `internal/runtime/safe_parallel_tools_node.go` — 删除终端相关逻辑

**验证**：`go build ./internal/runtime/...`

#### Phase 3：删除 App 层终端逻辑

**操作**：
1. 修改 `internal/app/workbench_service.go`：
   - 删除 `ListTerminalSessionsByRun` 调用
   - 删除 `buildTerminalSessionSummaries` 函数
2. 修改 `internal/app/workbench_types.go`：
   - 删除 `TerminalSessions []TerminalSessionSummary` 字段
   - 删除 `TerminalSessionSummary` 类型
   - 删除 `TerminalSessionLogSummary` 类型
3. 修改 `internal/app/trace_service.go` — 删除 traceStore 接口中的终端方法

**验证**：`go build ./internal/app/...`

#### Phase 4：删除 Web DTO

**操作**：
1. 修改 `internal/web/dto_context.go`：
   - 删除 `TerminalSessionDTO` 类型
   - 删除 `TerminalSessionLogDTO` 类型
   - 删除 `terminalSessionDTOsFromDomain` 函数
   - 删除 `terminalSessionLogDTOsFromDomain` 函数
2. 修改 `internal/web/dto_run_detail.go` — 删除 `TerminalSessions` 字段
3. 检查 `internal/web/openapi_test.go` — 从 stale guards 中移除 RunTerminalSession / RunTerminalSessionLog

**验证**：`go build ./internal/web/...`

#### Phase 5：删除 SQLite Store 和 Schema

**操作**：
1. 删除 `internal/store/sqlite/store_terminal.go`
2. 修改 `internal/store/sqlite/store_schema.go`：
   - 删除 `terminal_sessions` 表定义
   - 删除 `terminal_session_logs` 表定义
   - 删除相关 index 定义
   - 从 schema validation map 中移除这两个表
3. 在 `migrateV2()` 或新增 migration 中添加 `DROP TABLE IF EXISTS terminal_sessions` 和 `DROP TABLE IF EXISTS terminal_session_logs`

**验证**：`go build ./internal/store/...`

#### Phase 6：删除核心包

**操作**：
1. 删除 `internal/terminalsession/` 整个目录

**验证**：`go build ./...`

#### Phase 7：全量验证

```bash
go test ./...
make lint
```

### 风险评估

| 风险 | 等级 | 缓解措施 |
|------|------|----------|
| 误删仍有间接引用的代码 | 低 | 已用 `grep -r "terminalsession\|TerminalService\|TerminalSession"` 全面扫描 |
| 删除后编译失败 | 低 | 每阶段后都编译验证 |
| 删除 schema 后现有数据库无法启动 | 极低 | 添加 DROP TABLE migration；SQLite 是 single-user，无并发问题 |
| 删除 Web DTO 后 OpenAPI 测试失败 | 极低 | 同步更新 openapi_test.go stale guards |
| 删除 store_terminal 后其他 store 方法引用 | 极低 | store_terminal.go 的方法只被 terminalsession 包使用 |

### 回滚策略

- 使用 git branch：`git checkout -b refactor/remove-terminal-session`
- 每 Phase 独立 commit
- 回滚：可逐 commit `git revert` 或整 branch 丢弃

---

## 问题 B：合并 `runtime/stream` 与 `runstream`

### 现状分析

| 维度 | `runtime/stream/` | `internal/runstream/` |
|------|-------------------|---------------------|
| 非测试文件 | 9 个 | 12 个 |
| 核心文件 | `payloads.go` (565行) | `payloads.go` (548行) |
| 外部消费者 | 6 个 | 3 个 |
| 是否定义 StreamItemKind | 是 | 是 |
| 是否定义 payload 类型 | 是 | 是 |

**重复类型映射**：

| `runtime/stream/` 类型 | `runstream/` 类型 | 差异 |
|------------------------|-------------------|------|
| `RunStartedPayload` | `RunStartedPayload` | 完全一致 |
| `RunCompletedPayload` | `RunCompletedPayload` | 完全一致 |
| `RunFailedPayload` | `RunFailedPayload` | 完全一致 |
| `RunInterruptedPayload` | `RunInterruptedPayload` | 完全一致 |
| `RunResumeRequestedPayload` | `RunResumeRequestedPayload` | 完全一致 |
| `RunArchivedPayload` | `RunArchivedPayload` | 完全一致 |
| `DecisionSelectedPayload` | `DecisionSelectedPayload` | 完全一致 |
| `DecisionBlockedPayload` | `DecisionBlockedPayload` | 完全一致 |
| `SkillDiscoveredPayload` | `SkillDiscoveredPayload` | 完全一致 |
| `SkillSelectedPayload` | `SkillSelectedPayload` | 完全一致 |
| `StreamAssistantDelta` | `StreamAssistantDelta` | 字段完全一致，但顺序不同 |

**关键发现**：两个包中几乎所有 payload 类型定义都是**完全一致的副本**。唯一的区别是 `runtime/stream/payloads.go` import 了 `runtime/api`，而 `runstream/payloads.go` 没有。

### 合并策略

采用 **Canonical Consolidation + Type Alias Deprecation（规范包合并 + 类型别名废弃）**。

#### 决策：哪个包成为 Canonical？

选择 **`internal/runtime/stream/`** 作为 canonical 包：
- 更多外部消费者（6 vs 3）
- 名称语义更清晰（`runtime/stream` 明确表示运行时流数据）
- 已作为 `runtime` 子包存在，与 `runtime` 其他子包（`api`, `graph`）组织结构一致

#### 合并步骤

**Step 1：确认 `runtime/stream/` 已包含所有必需类型**

检查 `runstream/` 中是否有 `runtime/stream/` 没有的类型定义。

**Step 2：在 `runstream/` 中创建 deprecated aliases**

创建 `runstream/deprecated.go`：
```go
package runstream

import "github.com/ycvk/acorn/internal/runtime/stream"

// Deprecated: use runtime/stream instead.
type RunStartedPayload = stream.RunStartedPayload
// ... 所有重复类型
```

**Step 3：删除 `runstream/` 中已迁移到 alias 的原始类型定义**

从 `runstream/payloads.go` 中删除已 alias 的类型。

**Step 4：迁移 `runstream/` 的消费者到 `runtime/stream/`**

修改 3 个消费者的 import：
- 找到所有 `import "github.com/ycvk/acorn/internal/runstream"`
- 改为 `import "github.com/ycvk/acorn/internal/runtime/stream"`
- 更新类型引用（如 `runstream.RunStartedPayload` → `stream.RunStartedPayload`）

**Step 5：检查并清理 `runstream/` 剩余代码**

如果 `runstream/` 中还有非重复代码，评估是否：
- 迁移到 `runtime/stream/`
- 或保留在 `runstream/` 中（如果语义不同）

**Step 6：删除空的 `runstream/` 包**

如果所有类型和函数都已迁移，删除整个 `internal/runstream/` 目录。

### 风险评估

| 风险 | 等级 | 缓解措施 |
|------|------|----------|
| 消费者迁移后编译失败 | 低 | 使用 type alias 过渡，编译器确保等价 |
| 遗漏非重复代码 | 低 | Step 5 专门检查剩余代码 |
| 循环依赖 | 极低 | `runstream` → `runtime/stream` 单向依赖 |

### 回滚策略

- 使用 git branch：`git checkout -b refactor/merge-stream-packages`
- 每个 Step 独立 commit
- 回滚：逐 commit `git revert` 或整 branch 丢弃

---

## 执行顺序

```
Phase A: Terminal Session 清理
  ├─ Phase 1: 停写（tools 层）
  ├─ Phase 2: 停读（runtime 层）
  ├─ Phase 3: 删除 app 层
  ├─ Phase 4: 删除 web DTO
  ├─ Phase 5: 删除 store/schema
  ├─ Phase 6: 删除 terminalsession 包
  └─ Phase 7: 全量验证

Phase B: Stream 包合并
  ├─ Step 1: 确认 canonical 包完整性
  ├─ Step 2: 创建 type aliases
  ├─ Step 3: 删除重复定义
  ├─ Step 4: 迁移消费者
  ├─ Step 5: 清理剩余代码
  └─ Step 6: 删除 runstream 包
```

**为什么先 A 后 B？**
- A 是政策驱动的清理，优先级更高
- A 完成后代码库更小，B 的合并工作量减少

---

## 总体风险评估

| 维度 | 评估 |
|------|------|
| 行为改变风险 | **零** — 两个都是废弃/重复代码，无活跃消费者 |
| 编译风险 | **低** — 每阶段后编译验证 |
| 测试覆盖 | **高** — 全量测试可自证 |
| 回滚难度 | **低** — git branch + 独立 commit |
| 预估工作量 | A: 1-2 小时；B: 1 小时 |

---

## 批准前确认

**请用户确认以下决策：**

1. ✅ 是否接受 Phase A（Terminal Session 清理）的执行策略（硬切换，不保留兼容）？
2. ✅ 是否接受 Phase B（Stream 合并）的 canonical 包选择（`runtime/stream/`）？
3. ✅ 是否接受使用 type alias 过渡策略迁移 `runstream` 消费者？
4. ✅ 是否需要我在执行每个 Phase/Step 后立即汇报并等待下一步确认？

**批准后开始执行。**
