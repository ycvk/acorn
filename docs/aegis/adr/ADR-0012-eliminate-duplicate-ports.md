# ADR-0012: 消除 duplicate port，推翻"不可合并"声明

Date: 2026-06-22
Status: Accepted
Supersedes: (part of INVARIANTS.md "Consumer-owned port 接口重复是故意的" claim, now removed)

## Context

`INVARIANTS.md` 曾明确声明：

> Consumer-owned port 接口重复是故意的：`tools.DelegateTaskContext`、
> `tools.OperatorQuestionContext`、`tools.ArtifactContext`、`skills.RunContextBridge`、
> `skills.LifecycleEventAppender` 与 `domain.RunContextBridge`、`domain.EventAppender`
> 结构相同但不可合并——合并会创建 import cycle（`runtime → tools/skills → runtime/api`）。

实测 import 边界后发现此声明**已过时**：

- `domain` 不依赖任何 internal 包（纯 kernel）
- `tools` 已经 import `domain`，且不依赖 `runtime`
- `runtime` 单向依赖 `tools`
- **import cycle 不存在**

`OperatorQuestionContext` 和 `ArtifactContext` 的方法签名
（`CurrentRunID` / `CurrentSessionID` / `CurrentToolCallID`）
与 `domain.ToolCallContextBridge` 完全相同。重复定义无技术理由。

## Decision

删除 `tools.OperatorQuestionContext` 和 `tools.ArtifactContext` 两个重复接口，
所有引用统一改用 `domain.ToolCallContextBridge`。

- 零新增类型——直接复用已有的 `domain.ToolCallContextBridge`
- tools 包内 8 个文件 + runtime/runner_toolset.go 的类型引用变更
- 无行为变化

## Consequences

- **正面**：消除 2 个重复接口定义，改签名只需改一处；推翻了基于错误前提的 invariant 限制
- **负面**：无
- **风险**：无——import 边界已用 `go build` 实测验证

## Baseline Sync

- `internal/tools/operator_question_tool.go`: 删除 `OperatorQuestionContext` 接口定义
- `internal/tools/artifact_tools.go`: 删除 `ArtifactContext` 接口定义
- 所有引用改 `domain.ToolCallContextBridge`（commit `d90bc75`）
- `docs/architecture/INVARIANTS.md`: 删除 "Consumer-owned port 接口重复是故意的" 条目（commit `ce2fddd`）
- `go build ./...` + `go test ./...` 通过，无 import cycle
