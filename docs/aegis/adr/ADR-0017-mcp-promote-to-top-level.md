# ADR-0017: MCP 从 providers/mcp 提升为顶级包

Date: 2026-06-24
Status: Accepted

## Context

`internal/providers/mcp/` 是 `providers/` 下的唯一子包。`providers/` 目录只包含 `mcp/`,没有其他 provider 类型。多一层目录嵌套无实际分类意义,且 import 路径冗长。

## Decision

将 `internal/providers/mcp/` 提升为 `internal/mcp/`:
- 包名从 `mcpprovider` 改为 `mcp`
- 所有 import 路径从 `internal/providers/mcp` 改为 `internal/mcp`
- 保留 `mcpprovider` 作为 import alias(避免与 go-sdk mcp 包名冲突)
- `internal/providers/` 目录删除

## Consequences

- **正面**:import 路径更短;消除无意义的 providers 中间层;mcp 作为一等公民包
- **负面**:import alias `mcpprovider` 仍需保留(与 go-sdk `mcp` 包名冲突)
- **风险**:无——全局替换后编译通过

## Baseline Sync

- `internal/providers/mcp/` → `internal/mcp/`(commit `868093b`)
- 9 个 importer 文件 import 路径更新(commit `868093b`)
- `internal/providers/` 目录删除(commit `868093b`)
- 架构守卫 `dependency_direction_test.go` 更新(commit `3f7d76a`)
