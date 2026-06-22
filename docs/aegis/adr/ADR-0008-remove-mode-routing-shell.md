# ADR-0008: 删 mode 路由壳 (OrchestrationMode)

Date: 2026-06-22
Status: Accepted
Supersedes: (none)

## Context

Acorn 的运行时只剩 `direct_response` 一种 root mode（ADR-0001 已删除 plan_execute/single_agent/child_agent/verifier）。但 API 层仍保留着多模式路由的空壳：

- `OrchestrationMode` 类型 + `parseClientRunMode` 函数
- `ExecuteRequest.OrchestrationMode` / `ParentRunID` / `Depth` 字段
- `assembleRunnerByMode` 路由函数
- smoke 测试的 `--mode` flag
- `ErrClientInvalidRunMode` sentinel error

这些代码在 `direct_response` only 的世界里永远只走一个分支，是死路径。

## Decision

硬删除整个 mode 路由壳：

- 删 `OrchestrationMode` 类型 + `parseClientRunMode`
- 删 `ExecuteRequest.OrchestrationMode` / `ParentRunID` / `Depth` 字段
- 删 `assembleRunnerByMode`，直接调用 `assembleRunner`（direct_response only）
- 删 smoke `--mode` flag
- 删 `ErrClientInvalidRunMode` sentinel error
- `ExecuteRequest.Mode` DTO 字段保留注释标记兼容（OpenAPI schema 未改动）

## Consequences

- **正面**：消除永远只走一个分支的死路由代码，API 层直接映射到唯一编排模式
- **负面**：无——如果未来需要多模式（如 plan_execute），重新引入即可，当前不需要
- **风险**：无——OpenAPI `docs/openapi.yaml` 未改动，wire contract 保持不变

## Baseline Sync

- `internal/app/run_once.go` 已删 `assembleRunnerByMode`，直接 `assembleRunner`
- `internal/web/dto.go` 已删 `ExecuteRequest.OrchestrationMode` / `ParentRunID` / `Depth`
- `internal/cli/smoke.go` 已删 `--mode` flag
- `internal/app/errors.go` 已删 `ErrClientInvalidRunMode`
- `go build ./...` + `go test ./...` 通过
