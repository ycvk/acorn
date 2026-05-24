# Web 层 Handler 拆分计划 v3.0

> 基于对 Chi 路由框架社区最佳实践（go-chi/chi _examples、backend-go/go-chi-project-starter、ecommerce-golang、go-backend-clean-architecture-chi）的调研。

---

## 一、调研结论

### 1.1 Go + Chi 社区标准

| 项目 | Handler 组织方式 | 文件命名 |
|------|-----------------|----------|
| **go-chi/chi _examples** | 每个资源一个文件 | `users.go`, `todos.go` |
| **go-chi-project-starter** | `internal/handler/{domain}.go` | `user.go`, `auth.go`, `health.go` |
| **ecommerce-golang** | 垂直切片 | `internal/domain/{x}/handler.go` |
| **go-backend-clean-architecture-chi** | `api/controller/{domain}_controller.go` | `task_controller.go` |

**共识**：每个领域/资源一个 handler 文件，文件名反映领域，不是 `handlers.go` 大杂烩。

### 1.2 Acorn 当前反模式

`handlers_client.go`（381 行）混合了 6 个领域的 21 个 handler + 3 个辅助函数：

| 领域 | Handler 数量 | 行数估计 |
|------|-------------|---------|
| Thread | 5 | ~70 |
| Message | 2 | ~30 |
| Run | 6 | ~120 |
| PendingAction | 3 | ~50 |
| Inbox/System/Settings | 5 | ~80 |

**命名问题**：“client”不是领域名，是 API 消费者角色。Thread/Message/Run 才是领域。

---

## 二、拆分方案

### 2.1 目标文件结构

```
internal/web/
├── handlers_thread.go          # Thread CRUD
├── handlers_message.go         # Message list/create
├── handlers_run.go             # Run + events + SSE
├── handlers_pending_action.go  # Pending action
├── handlers_inbox.go           # Inbox + system + tools + settings
├── handler_helpers.go          # 跨领域通用辅助函数
└── (delete) handlers_client.go # 381 行大杂烩
```

### 2.2 每个文件的内容

**`handlers_thread.go`**（~75 行）：
- `handleClientListThreads`
- `handleClientCreateThread`
- `handleClientGetThread`
- `handleClientUpdateThread`
- `handleClientDeleteThread`

**`handlers_message.go`**（~35 行）：
- `handleClientListMessages`
- `handleClientCreateMessage`

**`handlers_run.go`**（~125 行）：
- `handleClientCreateRun`
- `handleClientGetRun`
- `handleClientInterruptRun`
- `handleClientResumeRun`
- `handleClientRunDetail`
- `handleRunEvents`
- `followRunEvents`（私有辅助，只被 handleRunEvents 使用）

**`handlers_pending_action.go`**（~55 行）：
- `handleDecidePendingAction`
- `handleListPendingActions`
- `handleGetPendingAction`

**`handlers_inbox.go`**（~85 行）：
- `handleClientInbox`
- `handleClientSystemStatus`
- `handleClientTools`
- `handleClientSettings`
- `handlePatchClientSettings`

**`handler_helpers.go`**（~35 行）：
- `respondClientKnownError`（被所有 client handler 使用）
- `runtimeWorkbenchDTOPointer`（通用 DTO 辅助）
- `clientWorkspaceRoot`（通用配置辅助）

---

## 三、关键约束

1. **Go 方法可以跨文件定义**：`*Server` 的方法分散到多个文件，只要在同一 `package web` 包内，完全合法。
2. **routes.go 无需修改**：它引用 `s.handleXXX`，方法位置改变不影响调用。
3. **request_decode.go 无需修改**：decode 函数已独立。
4. **测试文件无需修改**：`client_handlers_test.go` 测试的是 routes + handler 集成，不是单个 handler。

---

## 四、关于 runtime_workbench_dto.go 的决定

**不拆分**（497 行，15 个类型）。

原因：
- 职责已单一（Workbench DTO）
- Go 社区 200-500 行是健康范围
- 过度拆分成 7 个 <100 行小文件会增加导航成本
- 已有 `runtime_workbench_dto_test.go` 对应

---

## 五、验收标准

- [ ] `handlers_client.go` 已删除
- [ ] 6 个新 handler 文件编译通过
- [ ] `handler_helpers.go` 包含所有跨领域辅助函数
- [ ] `routes.go` 无需修改（验证引用仍然有效）
- [ ] `go build ./...` 通过
- [ ] `go test ./internal/web/...` 通过
- [ ] `make lint` 无新增问题
- [ ] `python3 mobile/tool/generate_openapi_client.py --check` 通过

---

*文档版本：v3.0*
*调研日期：2026-05-24*
