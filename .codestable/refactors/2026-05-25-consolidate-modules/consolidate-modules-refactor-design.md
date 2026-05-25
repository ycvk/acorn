---
doc_type: refactor-design
refactor: 2026-05-25-consolidate-modules
status: approved
---

# consolidate-modules refactor design

## 设计原则

基于 Go 社区最佳实践和项目定位（single-user self-hosted）：

1. **YAGNI**：不为"未来可能的多实现"创建接口。当前只有一个 SQLite 存储，未来更换数据库的概率极低
2. **包按领域组织**：一个包应该可以被一句话描述。微型包（<100 行）通常意味着过度拆分
3. **接口由消费者定义**：接口应该在需要多态的地方定义，而不是由实现者预先定义
4. **隐式接口优于显式接口**：Go 的隐式接口是优势，不需要提前声明 "implements"
5. **测试用 fake 而非 mock**：用内存中的具体类型（map/slice based）替代接口 mock，减少对 interface 的滥用

## 执行顺序

按依赖关系和风险分层，分 3 个 Phase 执行：

### Phase 1：低风险、纯移动、无接口变更（7 条）

| 编号 | scan 条目 | 方法 | 估计耗时 | 前置条件 |
|------|----------|------|---------|---------|
| 1 | #1 processgroup → tools | M-L3-02 | 15min | 无 |
| 2 | #3 model → providers | M-L3-02 | 10min | 无 |
| 3 | #4 retrievaleval → memorymodule | M-L3-02 | 15min | 无 |
| 4 | #8 architecture 测试迁移 | M-L3-03 | 10min | 无 |
| 5 | #10 删除 toolfactory closer | M-L2-02 | 5min | 无 |
| 6 | #9 内联 scanner 接口 | M-L2-02 | 20min | 无 |
| 7 | #2 notifications → app（含 #11 dispatcher 合并） | M-L3-02 + M-L2-05 | 20min | 无 |

### Phase 2：中风险、接口删除（6 条）

| 编号 | scan 条目 | 方法 | 估计耗时 | 前置条件 |
|------|----------|------|---------|---------|
| 8 | #5 workingstate → contextplane（统一 CheckpointService） | M-L1-01 | 30min | Phase 1 完成 |
| 9 | #12 内联 Store 接口 | M-L2-02 | 20min | #8 完成 |
| 10 | #6 runtimehistory → contextplane（统一 SessionSummaryService） | M-L1-01 | 40min | Phase 1 完成 |
| 11 | #13 内联 SessionSummaryStore | M-L2-02 | 20min | #10 完成 |
| 12 | #15 删除重复接口定义 | M-L2-02 | 10min | #8 #10 完成 |
| 13 | #7 toolresult 拆分合并 | M-L1-02 | 45min | Phase 1-2 完成 |

### Phase 3：高风险、Ledger 内联（1 条）

| 编号 | scan 条目 | 方法 | 估计耗时 | 前置条件 |
|------|----------|------|---------|---------|
| 14 | #14 内联 Ledger 接口 | M-L2-02 | 30min | #13 完成 |

## 详细执行方案

### #1 processgroup → tools

**步骤**：
1. 复制 `internal/processgroup/*.go` 到 `internal/tools/processgroup_unix.go`、`processgroup_other.go`、`processgroup_test.go`
2. 将新文件的 `package processgroup` 改为 `package tools`
3. 修改 `internal/tools/command_tool.go` 和 `workflow_tools.go` 的 import，删除 `github.com/ycvk/acorn/internal/processgroup`，改为直接使用 `tools.ConfigureCommand` 等
4. 修改 `internal/providers/mcp/transport.go` 的 import，改为 `github.com/ycvk/acorn/internal/tools`
5. 跑 `go build ./...` 确认无编译错误
6. 跑 `go test ./internal/tools` 确认测试通过
7. 删除 `internal/processgroup/` 目录
8. 跑 `grep -r "github.com/ycvk/acorn/internal/processgroup"` 确认 0 引用

**回滚**：从 git 恢复 `internal/processgroup/` 目录，回滚 import 修改

**验证**：AI 自证（`go build ./...` + `go test ./internal/tools`）

### #2 + #11 notifications → app + dispatcher 合并

**步骤**：
1. 复制 `internal/notifications/dispatcher.go` 到 `internal/app/notification_dispatcher.go`
2. 将包名改为 `app`
3. 合并 `APNSDispatcher` + `FCMDispatcher` 为 `PushDispatcher`：
   ```go
   type PushDispatcher interface {
       Dispatch(ctx context.Context, req DispatchRequest) error
   }
   ```
4. 修改 Router：
   ```go
   type Router struct {
       dispatchers map[string]PushDispatcher
   }
   ```
5. 修改 `internal/app/notification_service.go`、`container.go` 中的 import，删除 notifications import
6. 跑 `go build ./internal/app` + `go test ./internal/app`
7. 删除 `internal/notifications/`

**回滚**：从 git 恢复目录

**验证**：AI 自证（`go test ./internal/app`）

### #3 model → providers

**步骤**：
1. 复制 `internal/model/openai.go` 到 `internal/providers/openai_model.go`
2. 包名改为 `providers`
3. 函数改名 `NewChatModel` → `NewOpenAIChatModel`（明确化）
4. 修改 `internal/runtime/runner_toolset_build.go` 和 `runner_factory_compression_test.go` 的 import 和调用
5. 跑 `go build ./internal/runtime` + 相关测试
6. 删除 `internal/model/`

**回滚**：从 git 恢复目录

**验证**：AI 自证

### #4 retrievaleval → memorymodule

**步骤**：
1. 复制 `sample.go` 到 `internal/memorymodule/eval_sink.go`（Sink + FileSink）和 `eval_sample.go`（Sample 类型）
2. 包名改为 `memorymodule`
3. 修改 `internal/memorymodule/capture.go` 和 `internal/skills/capture.go` 的 import
4. 跑 `go test ./internal/memorymodule ./internal/skills`
5. 删除 `internal/retrievaleval/`

**回滚**：从 git 恢复

**验证**：AI 自证

### #5 + #12 workingstate → contextplane + Store 内联

**步骤**：
1. 复制 `workingstate/model.go` 到 `contextplane/working_checkpoint.go`（Checkpoint 类型）
2. 复制 `workingstate/service.go` 到 `contextplane/working_service.go`，删除 `Store` 接口，`Service.store` 改为 `*sqlite.Store`
3. 复制 `workingstate/tools.go` 到 `contextplane/working_tools.go`，删除 `CheckpointService` 接口（使用 contextplane 中已有的）
4. 修改所有 import `github.com/ycvk/acorn/internal/workingstate` → `contextplane`（或直接删除 import，因为同包）
5. 修改 `internal/store/sqlite/store_work.go` 中的实现，改为直接实现 `*sqlite.Store` 的方法（而非实现 `workingstate.Store` 接口）
6. 在测试中创建内存 fake Store（基于 map[string]Checkpoint）：
   ```go
   type fakeCheckpointStore struct {
       data map[string]Checkpoint
   }
   ```
7. 跑 `go test ./internal/contextplane ./internal/runtime ./internal/app`
8. 删除 `internal/workingstate/`

**回滚**：从 git 恢复目录，回滚所有 import 修改

**验证**：AI 自证（全量编译 + 核心测试）+ HUMAN（review CheckpointService 行为）

### #6 + #13 runtimehistory → contextplane + SessionSummaryStore 内联

**步骤**：
1. 复制 `runtimehistory/session_summary_service.go` 到 `contextplane/session_summary.go`，删除 `SessionSummaryStore` 接口
2. 复制 `runtimehistory/model.go` 中的 `SessionSummary`、`ContextBoundary` 到 `contextplane/session_summary.go`
3. 复制 `RunArchive`、`HistoryHit` 到 `internal/events/run_history.go`（新文件）
4. 修改所有 import，将 `runtimehistory.SessionSummaryService` 改为 `contextplane.SessionSummaryService`
5. 修改 `store/sqlite/store_session_summary.go`，直接实现具体方法而非接口
6. 在测试中创建内存 fake Store
7. 跑 `go build ./...` + `go test ./internal/contextplane ./internal/runtime ./internal/app ./internal/store/sqlite`
8. 删除 `internal/runtimehistory/`

**回滚**：从 git 恢复

**验证**：AI 自证（全量编译 + 核心测试）+ HUMAN（review session summary prompt 输出）

### #7 + #14 toolresult 拆分 + Ledger 内联

**步骤**：
1. 在 `internal/store/toolresult_types.go` 创建数据模型（Record, AppendRequest, SideEffectRef, EvidenceRef）
2. 在 `internal/store/toolresult_ledger.go` 创建 Ledger 接口（暂时保留，等 #14）
3. 在 `internal/runtime/toolresult_helpers.go` 创建辅助函数（BuildRef, Preview, NormalizeAppendRequest）
4. 逐步修改 import：
   - `contextplane` → 从 `store` import 数据模型
   - `runtime` → 从 `runtime` import 辅助函数
   - `tools` → 从 `store` import 数据模型
   - `orchestration` → 从 `store` import 接口和数据模型
   - `app` → 从 `store` import 数据模型
5. 跑 `go build ./...` 确认编译通过
6. 执行 #14：删除 Ledger 接口，`store/sqlite/store_tool_results.go` 改为直接导出方法。orchestration 测试中改为内存 fake
7. 跑 `go test ./internal/orchestration ./internal/store/sqlite ./internal/runtime`
8. 删除 `internal/toolresult/`

**回滚**：从 git 恢复目录

**验证**：AI 自证（全量编译 + 核心测试）+ HUMAN（review tool result 相关功能）

### #8 architecture 测试迁移

**步骤**：
1. 创建 `tests/architecture/` 目录
2. 复制 2 个测试文件，包名改为 `architecture_test`
3. 修改文件中的相对路径（`filepath.Join("..", "..")` → 新路径）
4. 在 Makefile 中增加 `test-architecture` 目标
5. 跑 `go test ./tests/architecture`
6. 删除 `internal/architecture/`

**回滚**：从 git 恢复

**验证**：AI 自证

### #9 scanner 接口内联

**步骤**：
1. 逐个处理 5 个 scanner 接口：
   - `runRecordScanner` → 改为函数签名中的具体函数类型或直接内联
   - `artifactScanner` → 同上
   - `toolResultScanner` → 同上
   - `rowScanner` → 同上
   - `providerUsageScanner` → 同上
2. 跑 `go test ./internal/store/sqlite`

**回滚**：从 git 恢复单个文件

**验证**：AI 自证

### #10 删除 toolfactory closer

**步骤**：
1. 删除 `type closer interface { Close() error }`
2. 将 `[]closer` 改为 `[]io.Closer`
3. 跑 `go build ./internal/toolfactory`

**回滚**：从 git 恢复

**验证**：AI 自证

### #15 删除重复接口定义

**步骤**：
1. 在 #5 #6 完成后，grep 全局 `CheckpointService` 和 `SessionSummaryService` 的定义位置
2. 确认 contextplane 中有且仅有一个定义
3. 删除其他包中的重复定义
4. 跑 `go build ./...`

**回滚**：从 git 恢复

**验证**：AI 自证

## 回滚策略（全局）

1. **每步完成后立即 commit**：不要等 Phase 结束才 commit。每步一个 commit，commit message 格式：`refactor(consolidate): #N {简短描述}`
2. **单步回滚**：如果某步失败，只 revert 该 commit，不影响已完成步骤
3. **全 Phase 回滚**：如果 Phase 中多步有问题，revert 整个 Phase 的 commits
4. **分支保护**：在 feature branch 上工作，不直接改 main：`git checkout -b refactor/consolidate-modules`

## 预期成果

| 指标 | 当前 | 目标 | 变化 |
|------|------|------|------|
| 模块数 | 31 | 23 | -8 |
| 接口数 | 118 | ~75 | -43 |
| 生产代码行数 | 111,026 | ~110,500 | -526 |
| 单实现接口 | 30+ | <10 | -20+ |
| 循环依赖 | 1 (config→workspace) | 0 | -1 |
| 微型模块（<200行）| 8 | 0 | -8 |

## 风险缓解

1. **编译错误**：每步后 `go build ./...` 必须通过，不通过不推进
2. **测试失败**：每步后跑相关测试，失败立即回滚
3. **import 遗漏**：使用 `grep -r "github.com/ycvk/acorn/internal/{旧包名}"` 确认 0 引用后再删除旧目录
4. **mock 测试断裂**：提前准备内存 fake 实现替代 mock
5. **mobile 同步**：本 refactor 不涉及 mobile/，但需确认无影响

## 未覆盖项（明确排除）

以下问题在本次 refactor 中**不处理**：
- `contextplane` 内部的 7 个压缩组件（CompactionEngine, MicrocompactEngine 等）——建议单独 refactor
- `CapabilityService` 的过度报告——建议单独简化
- `Browser` 服务的可选化——建议单独处理
- 配置系统 98 个字段的精简——建议单独处理
- `memorymodule` 的 SkillTree/Relation 系统——建议单独评估
