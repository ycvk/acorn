# DTO / Service 层重构计划

> **目标**：消除三层重复类型定义、按领域拆分 DTO、引入代码生成替代手写 mapping、将 Service 回归编排职责。
> **性质**：破坏性重构，接受硬 cut（无 backward compatibility shim）。
> **优先级**：P0 — 当前架构的核心拖累点。

---

## 一、现状诊断（精确到行号）

### 1.1 类型重复定义（三层镜像）

| 概念 | Web DTO 层 | App Model 层 | Runtime Stream 层 | 重合度 |
|------|-----------|-------------|-------------------|--------|
| `MessagePart` | `client_dto.go:57` `MessagePartDTO` | `client_models.go:35` `MessagePart` | `runtime/stream/*.go` `StreamAssistantDelta` 等 | ~90% |
| `Thread` | `client_dto.go:12` `ThreadDTO` | `client_models.go:10` `Thread` | — | ~100% |
| `Message` | `client_dto.go:40` `MessageDTO` | `client_models.go:26` `Message` | `runtime/stream/*.go` | ~90% |
| `Run` | `client_dto.go:235` `RunDTO` | `client_models.go:77` `Run` | — | ~95% |

每次修改一个概念需要同步改 2-3 个文件，极易遗漏。

### 1.2 DTO 文件大杂烩

`internal/web/client_dto.go`（663 行，59 个 type/func）混合了 8 个独立领域：

1. **Thread 管理**（12-33 行）：`ThreadDTO`, `ThreadListResponse`, `CreateThreadRequest`, `UpdateThreadRequest`
2. **Message 聊天**（34-56 行）：`MessageDTO`, `MessageContentDTO`, `MessagePartDTO`, `CreateMessageRequest`
3. **Run 执行**（235-270 行）：`RunDTO`, `RunDetailDTO`, `CreateRunRequest`；`RunEvent` wire contract 由 `internal/clientevents` 统一持有
4. **Pending Action 审批**（193-234 行）：`PendingActionDetailDTO`, `DecidePendingActionRequest`
5. **Memory 记忆**：通过 `type alias` 指向 `dto_memory.go`
6. **Skill 技能**：通过 `type alias` 指向 `dto_skills.go`
7. **Settings 设置**（336-364 行）：`ClientSettingsDTO`, `ClientProviderSettingsDTO`
8. **Inbox 聚合**（290-335 行）：`InboxResponse`, `RunSummaryDTO`, `PendingActionSummaryDTO`

这导致：任何 API 领域变更都会触发全文件 diffreview，冲突概率高；新开发者难以定位 DTO 定义。

### 1.3 手写 Mapping Boilerplate

`client_dto.go` 中定义了 20+ 个纯字段复制的 mapping 函数：

```go
func threadDTOFromDomain(thread app.Thread) ThreadDTO { ... }          // 366-376
func messageDTOFromDomain(message app.Message) MessageDTO { ... }       // 386-399
func messagePartDTOsFromDomain(parts []app.MessagePart) []MessagePartDTO { ... } // 401-450
func runDTOFromDomain(run app.Run) RunDTO { ... }                       // 542-555
// ... 共 20+ 个
```

每个函数都是纯值复制，无业务逻辑，占 `client_dto.go` 约 **40% 行数**。修改字段名时需要同时改 struct 定义和 mapping 函数。

### 1.4 Service 文件承载 Model 定义

`internal/app/runtime_workbench_service.go`（961 行）的前 213 行不是 Service 方法，而是 **内部 Model/Summary 类型定义**：

- `RuntimeWorkbench`（25-52 行）
- `WorkspaceGitStatus`（54-61 行）
- `SubagentRun`（63-82 行）
- `MutationCheckpointSummary`（84-87 行）
- `RollbackSummary`（89-99 行）
- `ContextEconomySummary`（101-124 行）
- `ProviderUsageSummary`（144-165 行）
- `ArtifactSummary`（167-178 行）
- `TerminalSessionSummary`（180-202 行）

这些类型被 `internal/web/runtime_workbench_dto.go` 消费（通过 `runtimeWorkbenchDTOFromDomain`），但定义在 Service 文件里，违反了**关注点分离**。

### 1.5 死代码：`alias_stream.go`

`internal/runtime/alias_stream.go` 定义了 67 个类型别名：

```go
type StreamItem = stream.StreamItem
type StreamMessage = stream.StreamMessage
// ... 共 67 个
```

注释声称 "for backward compatibility"。但 grep 统计：
- **0 个文件** import `alias_stream`
- **67 个文件** 直接从 `internal/runtime/stream` 子包导入

自重构完成后即无消费者，属纯死代码。

---

## 二、重构目标

1. **消除三层重复**：Web DTO 与 App Model 同构时，让 App Model 直接充当 DTO（或代码生成映射）
2. **按领域拆分 DTO**：每个领域独立文件，单文件职责 ≤2 个领域
3. **消灭手写 mapping boilerplate**：引入代码生成工具替代纯字段复制
4. **Service 回归编排**：把内部 Model 类型从 Service 文件迁移到 Model 层
5. **删除死代码**：移除 `alias_stream.go`

---

## 三、重构方案（分 4 个阶段）

### 阶段 0：死代码清理（快速胜利，1-2 小时）

**目标**：删除 `alias_stream.go`，验证无破坏。

**变更**：
- `git rm internal/runtime/alias_stream.go`
- 运行 `go build ./...` 确认 0 个引用
- 运行 `go test ./internal/runtime/...` 确认通过
- 运行 `make lint` + `make test`

**回滚**：`git revert` 即可。

---

### 阶段 1：按领域拆分 `client_dto.go`（核心，4-6 小时）

**原则**：
- `client_dto.go` 不再保留任何具体定义，只作为 **re-export 入口文件**（可选，如果 Go 1.26 的 re-export 风格不需要）
- 每个领域一个文件，命名规范：`{domain}_dto.go`
- 原 `client_dto.go` 中的 mapping 函数随对应领域一起迁移

**拆分方案**：

| 新文件 | 从 `client_dto.go` 迁移的内容 | 行数估计 |
|--------|---------------------------|---------|
| `thread_dto.go` | `ThreadDTO`, `ThreadListResponse`, `CreateThreadRequest`, `UpdateThreadRequest` + `threadDTOFromDomain`, `threadDTOsFromDomain` | ~30 |
| `message_dto.go` | `MessageDTO`, `MessageContentDTO`, `MessagePartDTO`, `CreateMessageRequest`, `MessageListResponse` + `messageDTOFromDomain`, `messagePartDTOsFromDomain`, `disclosureItemDTOsFromDomain`, `messageActionDTOFromDomain`, `messageDTOsFromDomain` | ~120 |
| `run_dto.go` | `RunDTO`, `RunDetailDTO`, `RunDetailRawDTO`, `CreateRunRequest`, `ResumeRunRequest`, `InterruptRunResponse` + `runDTOFromDomain`；`RunEvent`/unsupported-event types come from `internal/clientevents` | ~60 |
| `pending_action_dto.go` | `PendingActionDetailDTO`, `PendingActionListResponse`, `PendingActionDecisionDTO`, `DecidePendingActionRequest`, `DecisionOptionDTO` + `pendingActionDetailDTOFromDomain`, `pendingActionDecisionDTOFromDomain`, `decisionOptionDTOsFromDomain`, `pendingActionSummaryDTOsFromDomain` | ~70 |
| `settings_dto.go` | `ClientSettingsDTO`, `ClientProviderSettingsDTO`, `ClientRuntimeSettingsDTO`, `ClientWebSettingsDTO` + `clientSettingsDTOFromConfig` | ~40 |
| `inbox_dto.go` | `InboxResponse`, `SystemStatusDTO`, `RunSummaryDTO`, `PendingActionSummaryDTO`, `PendingActionOptionDTO`, `ToolSummaryDTO` + `inboxDTOFromDomain` | ~60 |

**处理已有的部分拆分**：
当前已经有：
- `runtime_workbench_dto.go` — 保持不变（已是单一职责）
- `session_state_dto.go` — 保持不变
- `dto_skills.go` — 保持不变
- `dto_memory.go` — 保持不变

**`client_dto.go` 的命运**：

选项 A：**保留 `client_dto.go` 作为聚合入口文件**，只放临时 `type alias` 和 `re-export`；当前硬 cut 规则下不采用这个选项：
```go
package web

// Historical sketch only. Current code should import specific domain files directly.
type ThreadDTO = thread.ThreadDTO
```

选项 B（当前规则）：**彻底删除 `client_dto.go`**，让所有 import 方改引用新文件。更符合 hard-cut migration。

**建议**：选 A，保留一个版本的 re-export，给迁移缓冲期，之后下一个 release 删除。

---

### 阶段 2：引入代码生成消灭手写 Mapping（2-3 小时）

**工具选择**：`github.com/jmattheis/goverter`

**原因**：
- 专门解决 Go 的 DTO↔Model 类型转换代码生成
- 基于 `//go:generate`，零运行时依赖
- 支持自定义转换规则（如 `string → time.Time`）
- 已经有 1.5k+ stars，维护活跃

**实施方案**：

1. **安装**：
   ```bash
   go install github.com/jmattheis/goverter/cmd/goverter@latest
   ```

2. **在 `internal/web` 下创建 `converter.go`**：
   ```go
   package web

   //go:generate goverter gen .

   import "github.com/ycvk/acorn/internal/app"

   // Converter 定义 DTO 和 App Model 之间的转换规则。
   // goverter 会基于此接口生成实现。
   //go:generate goverter gen --output converter_gen.go .
   type Converter interface {
       // Thread
       ThreadDTOFromDomain(source app.Thread) ThreadDTO
       ThreadDTOsFromDomain(source []app.Thread) []ThreadDTO

       // Message
       MessageDTOFromDomain(source app.Message) MessageDTO
       MessageDTOsFromDomain(source []app.Message) []MessageDTO
       MessagePartDTOsFromDomain(source []app.MessagePart) []MessagePartDTO

       // Run
       RunDTOFromDomain(source app.Run) RunDTO
       // RunEvent wire envelopes are projected by internal/clientevents, not by web DTO converters.

       // PendingAction
       PendingActionDetailDTOFromDomain(source app.PendingActionDetail) PendingActionDetailDTO
       PendingActionSummaryDTOsFromDomain(source []app.PendingActionSummary) []PendingActionSummaryDTO
   }
   ```

3. **运行代码生成**：
   ```bash
   go generate ./internal/web/...
   ```
   这会生成 `converter_gen.go`，包含所有转换函数的实现。

4. **替换手写 mapping**：
   删除 `thread_dto.go`、`message_dto.go`、`run_dto.go`、`pending_action_dto.go` 中的手写 `xxxFromDomain` 函数，改为调用 `Converter`。

5. **CI 集成**：
   在 `make format-check` 或 `make test` 之前增加 `go generate ./...` 检查，确保生成的代码是最新的。

**处理特殊转换**：

有些字段需要手动规则（goverter 支持 `//goverter:map` 注释）：
- `RunStatus`（`events.RunStatus`）→ `string`：`//goverter:map Status string(source.Status)`
- `SessionSummary` 的字段提取（已在 `runtime_workbench_dto.go` 中通过 `summaryText()` 等辅助函数处理）

对于 `runtime_workbench_dto.go` 中复杂的 `runtimeWorkbenchDTOFromDomain`，由于其包含大量的嵌套转换和 nil 检查，可以：
- 将内部的子转换交给 goverter（`terminalSessionDTOsFromDomain`, `artifactSummaryDTOsFromDomain` 等）
- 保留主函数的手写结构，但调用 goverter 生成的方法做子对象转换

---

### 阶段 3：Service 内部 Model 迁移（3-4 小时）

**目标**：将 `runtime_workbench_service.go` 中前 213 行的类型定义迁移到独立的 model 文件。

**方案**：

1. **创建 `internal/app/workbench_models.go`**：
   迁移以下类型（保持字段不变）：
   - `RuntimeWorkbench`
   - `WorkspaceGitStatus`
   - `SubagentRun`
   - `MutationCheckpointSummary`
   - `RollbackSummary`
   - `ContextEconomySummary` + 子类型
   - `ProviderUsageSummary` + `ProviderUsageCallSummary`
   - `ArtifactSummary`
   - `TerminalSessionSummary` + `TerminalSessionLogSummary`

2. **修改 `runtime_workbench_service.go`**：
   - 删除类型定义，保留 Service struct 和接口
   - 添加 import：`"github.com/ycvk/acorn/internal/app"`（如果已经在这个包里则不需要）
   - 注意：这些类型本来就是 `package app`，所以只需要移到新文件即可

3. **修改 `runtime_workbench_dto.go`**：
   - 当前它 import `"github.com/ycvk/acorn/internal/app"`，引用 `app.RuntimeWorkbench`
   - 类型迁移后引用不变，无需修改

**验证**：
- 确保 `go build ./...` 通过
- 确保 `go test ./internal/app/...` 通过
- 确保 `go test ./internal/web/...` 通过

---

## 四、文件变更汇总

### 新增文件

| 文件 | 来源 | 说明 |
|------|------|------|
| `internal/web/thread_dto.go` | 从 `client_dto.go` 拆分 | Thread 领域 DTO |
| `internal/web/message_dto.go` | 从 `client_dto.go` 拆分 | Message 领域 DTO + mapping |
| `internal/web/run_dto.go` | 从 `client_dto.go` 拆分 | Run 领域 DTO + mapping |
| `internal/web/pending_action_dto.go` | 从 `client_dto.go` 拆分 | PendingAction DTO |
| `internal/web/settings_dto.go` | 从 `client_dto.go` 拆分 | Settings DTO |
| `internal/web/inbox_dto.go` | 从 `client_dto.go` 拆分 | Inbox/System DTO |
| `internal/web/converter.go` | 新建 | goverter 接口定义 |
| `internal/web/converter_gen.go` | 代码生成 | goverter 生成的转换函数 |
| `internal/app/workbench_models.go` | 从 `runtime_workbench_service.go` 拆分 | Workbench 内部模型 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `internal/web/client_dto.go` | 删除具体定义，改为 re-export 入口（阶段1后）或彻底删除（阶段2后） |
| `internal/web/runtime_workbench_dto.go` | 子转换函数替换为 goverter 生成的方法 |
| `internal/app/runtime_workbench_service.go` | 删除前 213 行的类型定义，改为 import model |
| `Makefile` | 增加 `go generate` 检查 |
| `go.mod` | 增加 `goverter` build-time 依赖（作为 tool） |

### 删除文件

| 文件 | 说明 |
|------|------|
| `internal/runtime/alias_stream.go` | 死代码，67 个无引用别名 |

---

## 五、引入的新依赖

**build-time 依赖**（不影响运行时）：

```bash
go install github.com/jmattheis/goverter/cmd/goverter@latest
```

在 `go.mod` 中不需要增加 require，因为 goverter 是代码生成工具，不是库依赖。可以在 `Makefile` 中：

```makefile
generate:
	go generate ./...

generate-check:
	go generate ./...
	git diff --exit-code || (echo "Generated code is out of date. Run 'make generate'." && exit 1)
```

**CI 变更**：
在 `.github/workflows/ci.yml` 的 `lint` job 中增加：
```yaml
- run: go generate ./...
- run: git diff --exit-code || (echo "Generated code out of date" && exit 1)
```

---

## 六、验证策略

每个阶段完成后必须验证：

```bash
# 1. 编译
make build

# 2. 全量测试
make test

# 3. 格式化
make format-check

# 4. Lint
make lint

# 5. Mobile 检查（如涉及 DTO 变更）
python3 mobile/tool/generate_openapi_client.py --check
```

**阶段 1 特殊验证**：
- 确认所有 handler 仍能正确引用 DTO 类型
- 确认 `client_dto_test.go` 中的测试仍通过（可能需要按新文件拆分测试）

**阶段 2 特殊验证**：
- 对比 goverter 生成的代码与手写 mapping 的输出一致性（写临时对比测试）
- 确认无性能退化（纯值复制，理论上相同）

---

## 七、风险评估

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|----------|
| 拆分 DTO 后 import 路径遗漏 | 中 | 编译失败 | `go build ./...` 全量编译即可发现 |
| goverter 生成代码与手写不一致 | 低 | 运行时 bug | 阶段 2 先并行保留两套，写对比测试验证 |
| OpenAPI/mobile 客户端不同步 | 中 | mobile CI 失败 | 每阶段跑 `generate_openapi_client.py --check` |
| 测试文件未同步拆分 | 中 | 测试找不到类型 | 同步拆分 `client_dto_test.go` |
| 未提交的 runtime 重构冲突 | 高 | merge conflict | 先提交/清理当前工作树，再开始重构 |

**当前未提交变更的冲突风险**：
当前工作树有 26 个文件的未提交变更（`git diff --stat` 显示）。这些变更涉及 `internal/app/*.go` 和 `internal/runtime/*.go`，正是我们要改的文件。

**建议**：在开始重构前，先完成并提交当前的 runtime 重构，或者确认当前 diff 的方向与我们的重构不冲突。

---

## 八、执行顺序建议

```
Day 1 (2-4 小时):
  ├─ 0.1 提交/清理当前工作树中的 runtime 重构
  ├─ 0.2 删除 alias_stream.go → make build + make test ✅
  └─ 1.0 拆分 client_dto.go 为 6 个领域文件
      ├─ 每拆一个文件 → go build ✅
      ├─ 全部拆完 → make test ✅
      └─ 保留 client_dto.go 作为 re-export 入口

Day 2 (2-4 小时):
  ├─ 2.0 安装 goverter，创建 converter.go
  ├─ 2.1 生成 converter_gen.go
  ├─ 2.2 替换一个领域的 mapping（如 thread）→ 对比测试
  ├─ 2.3 全部替换 → make test ✅
  └─ 2.4 删除 client_dto.go 中的手写 mapping

Day 3 (2-3 小时):
  ├─ 3.0 创建 workbench_models.go
  ├─ 3.1 迁移 RuntimeWorkbench 相关类型
  ├─ 3.2 修改 runtime_workbench_service.go 删除类型定义
  └─ 3.3 make test + make lint ✅

Day 4 (1-2 小时):
  ├─ 4.0 清理 client_dto.go re-export（可选，可延后）
  ├─ 4.1 更新 Makefile + CI
  ├─ 4.2 完整验证
  └─ 4.3 文档更新（AGENTS.md / architecture docs）
```

---

## 九、验收标准

- [ ] `internal/runtime/alias_stream.go` 已删除，编译通过
- [ ] `client_dto.go` 不再定义具体 struct（或已删除）
- [ ] 每个领域 DTO 独立文件，单文件 < 200 行
- [ ] goverter 生成的转换函数覆盖 ≥80% 的纯字段复制 mapping
- [ ] `runtime_workbench_service.go` 不再包含 model 类型定义
- [ ] 所有测试通过（`make test`）
- [ ] Lint 通过（`make lint`）
- [ ] Mobile OpenAPI 检查通过（`python3 mobile/tool/generate_openapi_client.py --check`）
- [ ] 编译通过（`make build`）

---

*文档版本：v1.0*
*制定日期：2026-05-24*
*下一动作：用户确认计划后，开始阶段 0*
