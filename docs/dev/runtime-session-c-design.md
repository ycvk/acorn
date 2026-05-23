# Session C: Runner + Executor 子包拆分设计

## 目标

将 `runtime/` 根的编排层拆分为三个层级：
- `runner/` — run 构建域（RunnerFactory、ActiveRunner、Builder）
- `executor/` — run 执行域（Executor、RunController、SubagentExecutor）
- `runtime/` 根 — facade + 共享基础设施（RunContext、Registry、Trace、Stream aliases）

## 当前状态

`runtime/` 根包含约 60 个文件，其中：
- 11 个 runner_factory 文件 + run_builder.go（构建逻辑）
- 5 个 executor 文件 + subagent_executor.go（执行逻辑）
- 20+ 个共享/基础设施文件

## 依赖分析

### 已完成的拆分（无循环依赖）
```
runtime/     → api/ (别名)
runtime/     → stream/ (别名)
runtime/     → graph/ (直接导入)
```

### 目标架构（Session C 后）
```
runner/      → runtime/ (共享类型: RunContext, Registry, StreamItem, EventAppender)
executor/    → runtime/ (共享类型)
executor/    → runner/  (RunnerFactory 构建 ActiveRunner)
runtime/     → (不导入 runner/ 或 executor/，避免循环)
```

## 拆分策略

### 原则 1: runtime/ 根不导入 runner/ 或 executor/

runtime/ 根当前通过 alias.go/alias_stream.go 向外提供类型，这些别名不应依赖 runner/ 或 executor/ 的具体实现。

### 原则 2: 共享基础设施留在 runtime/ 根

以下类型留在 runtime/ 根，供 runner/ 和 executor/ 共同导入：
- `RunContext`, `RunBudget`, `Registry`（原 runRegistry 导出）
- `Trace`, `TraceSummary`
- `alias.go` 和 `alias_stream.go` 中的所有别名
- `store_ports.go` 中的接口定义（EventAppender 等已通过 alias 导出）

### 原则 3: runner/ 负责 run 构建

迁移到 `internal/runtime/runner/` 的文件：
- `runner_factory.go` — RunnerFactory、RunnerBuildRequest、ActiveRunner、RunnerFactoryOptions
- `runner_factory_build.go` — build 方法
- `runner_factory_capabilities.go` — capability 方法
- `runner_factory_mcp.go` — MCP 引导
- `runner_factory_orchestration.go` — orchestration 绑定
- `runner_factory_skills.go` — skill 解析
- `runner_factory_toolset.go` — toolset 构建
- `runner_factory_provider.go` — provider 方法
- `runner_factory_sampling.go` — sampling 适配
- `runner_factory_assemblers.go` — assembler 类型
- `run_builder.go` — runBuilder 和构建流程

迁移时修改：
- package 从 `runtime` 改为 `runner`
- 导入 `github.com/ycvk/acorn/internal/runtime` 获取共享类型
- 导入 `github.com/ycvk/acorn/internal/runtime/graph` 获取 graph builder

### 原则 4: executor/ 负责 run 执行

迁移到 `internal/runtime/executor/` 的文件：
- `executor.go` — Executor、Result
- `executor_loop.go` — 执行循环
- `executor_run.go` — run 执行
- `executor_stream.go` — stream 处理
- `executor_terminal.go` — terminal 处理
- `subagent_executor.go` — SubagentExecutor
- `run_control.go` — RunController
- `context_session_bridge.go` — context session bridge

迁移时修改：
- package 从 `runtime` 改为 `executor`
- 导入 `github.com/ycvk/acorn/internal/runtime` 获取共享类型
- 导入 `github.com/ycvk/acorn/internal/runtime/runner` 获取 RunnerFactory

### 原则 5: Facade 层保持向后兼容

`runtime/` 根新增 `alias_runner.go` 和 `alias_executor.go`，重新导出 runner/ 和 executor/ 的公开类型：

```go
// alias_runner.go
package runtime

import "github.com/ycvk/acorn/internal/runtime/runner"

type RunnerFactory = runner.RunnerFactory
type RunnerBuildRequest = runner.RunnerBuildRequest
type ActiveRunner = runner.ActiveRunner
type RunnerFactoryOptions = runner.RunnerFactoryOptions
type SelectedSkill = runner.SelectedSkill
var NewRunnerFactory = runner.NewRunnerFactory
// ... 其他需要 facade 的类型
```

```go
// alias_executor.go
package runtime

import "github.com/ycvk/acorn/internal/runtime/executor"

type Executor = executor.Executor
type Result = executor.Result
type RunController = executor.RunController
type SubagentExecutor = executor.SubagentExecutor
var NewExecutorWithRunnerFactoryAndController = executor.NewExecutorWithRunnerFactoryAndController
var NewRunController = executor.NewRunController
var NewSubagentExecutor = executor.NewSubagentExecutor
```

### 原则 6: Registry 导出

当前 `runRegistry` 是未导出类型。需要导出为 `Registry`：

```go
// run_context.go
type Registry struct { ... }  // 原 runRegistry
func NewRegistry() *Registry { ... }  // 原 newRunRegistry
```

同时需要更新所有引用 `runRegistry` 和 `newRunRegistry` 的文件。

### 原则 7: Test 文件处理

Test 文件按被测代码的位置迁移：
- `runner_factory_*_test.go` → `runner/*_test.go`
- `executor_*_test.go` → `executor/*_test.go`
- `run_builder_test_helpers_test.go` → `runner/` 或删除（如果不再需要）

## 实施步骤

### Phase 1: 准备工作（不改变行为）
1. 导出 Registry（runRegistry → Registry，newRunRegistry → NewRegistry）
2. 验证编译和测试

### Phase 2: 创建 runner/ 子包
1. 创建 `internal/runtime/runner/` 目录
2. 复制并修改 runner factory 文件（package runner + 添加 runtime import）
3. 复制并修改 run_builder.go
4. 验证 runner/ 包独立编译

### Phase 3: 创建 executor/ 子包
1. 创建 `internal/runtime/executor/` 目录
2. 复制并修改 executor 文件（package executor + 添加 runtime + runner import）
3. 验证 executor/ 包独立编译

### Phase 4: Facade 层和清理
1. 创建 `alias_runner.go` 和 `alias_executor.go`
2. 删除 runtime/ 根的原文件（保留 facade 和共享文件）
3. 更新外部包的 import（app/, web/ 等）
4. 验证完整编译和测试

### Phase 5: 验证
1. `go build ./...`
2. `go test ./...`
3. `make lint`
4. `make format-check`
5. `python3 mobile/tool/generate_openapi_client.py --check`

## 已知风险

1. **app/ 层 import 变化**: app/ 层直接引用 `runtime.RunnerFactory`、`runtime.Executor` 等，alias 文件需要完整覆盖这些类型。
2. **测试辅助函数**: `newRunnerFactory()` 等测试辅助函数在 test 文件中定义，需要随被测文件一起迁移。
3. **SubagentExecutor 双向依赖**: SubagentExecutor 使用 RunnerFactory，RunnerFactory 创建 SubagentExecutor。拆分后 SubagentExecutor 在 executor/，RunnerFactory 在 runner/。RunnerFactory 通过 `newChildAgentExecutor()` 创建 SubagentExecutor，需要改为从外部注入或通过接口解耦。

## SubagentExecutor 解耦方案

当前 `RunnerFactory.newChildAgentExecutor()` 直接创建 `SubagentExecutor`：

```go
func (f *RunnerFactory) newChildAgentExecutor() *SubagentExecutor {
    return NewSubagentExecutor(f.cfg, f.store, f, nil)
}
```

拆分后，RunnerFactory 在 runner/，SubagentExecutor 在 executor/。如果 runner/ 导入 executor/，则形成循环：
- runner/ → executor/ (RunnerFactory 需要创建 SubagentExecutor)
- executor/ → runner/ (SubagentExecutor 需要 RunnerFactory)

**解决方案**: 将 `newChildAgentExecutor()` 从 RunnerFactory 中移除，改为在 orchestration 层或 runtime 根注入：

```go
// runner/ RunnerFactory 中不再包含 child agent executor 创建逻辑
// 改为接受外部注入的 orchestration.ChildAgentExecutor

// executor/ 中新增函数：
func NewChildAgentExecutor(cfg *config.Config, store executorStore, rf *runner.RunnerFactory) *SubagentExecutor {
    return NewSubagentExecutor(cfg, store, rf, nil)
}

// runtime/ 根或 app/ 层负责连接：
rf := runner.NewRunnerFactory(cfg, store, opts)
childExec := executor.NewChildAgentExecutor(cfg, store, rf)
rf.SetChildAgentExecutor(childExec) // 或通过 Options 传入
```

但这需要修改 RunnerFactory 的初始化流程。更简单的方案：**将 SubagentExecutor 留在 runner/ 包**，因为它本质上是 RunnerFactory 的附属工具（child run creation），不是真正的"执行器"。

**最终决定**: SubagentExecutor 留在 runner/ 包，因为它是 RunnerFactory 的工具，不是独立的执行域组件。

## 文件迁移清单

### 留在 runtime/ 根的文件
- `alias.go`
- `alias_stream.go`
- `run_context.go` (导出 Registry)
- `trace_types.go`
- `trace_projector.go`
- `streaming_assistant_stream.go`
- `streaming_tool_executor.go`
- `checkpoint_json.go`
- `context.go`
- `contextplane_bridge.go`
- `helpers.go`
- `plan_stream.go`
- `store_ports.go`
- `tool_audit.go`
- `act_node.go`
- `agent_graph.go`
- `plan_node.go`
- `plan_execute_graph.go`
- `plan_runtime.go`
- `plan_evidence.go`
- `safe_parallel_tools_node.go`

### 迁移到 runner/ 的文件
- `runner_factory.go`
- `runner_factory_build.go`
- `runner_factory_capabilities.go`
- `runner_factory_mcp.go`
- `runner_factory_orchestration.go`
- `runner_factory_skills.go`
- `runner_factory_toolset.go`
- `runner_factory_provider.go`
- `runner_factory_sampling.go`
- `runner_factory_assemblers.go`
- `run_builder.go`
- `runner_factory_test_helpers_test.go`
- `runner_factory_capabilities_test.go`
- `runner_factory_compression_test.go`
- `runner_factory_mcp_test.go`
- `runner_factory_provider_test.go`
- `runner_factory_toolset_test.go`
- `run_builder_test_helpers_test.go`

### 迁移到 executor/ 的文件
- `executor.go`
- `executor_loop.go`
- `executor_run.go`
- `executor_stream.go`
- `executor_terminal.go`
- `run_control.go`
- `context_session_bridge.go`
- `executor_crystallization_test.go`
- `executor_finalization_test.go`
- `executor_lifecycle_test.go`
- `run_control_test.go`

### 特殊处理
- `subagent_executor.go` → 留在 runner/ (因为它是 RunnerFactory 的子组件)
- `subagent_executor_test.go` → 留在 runner/ 或跟随 SubagentExecutor
