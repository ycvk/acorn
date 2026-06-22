# ADR-0004: 砍掉 MCP server mode

Date: 2026-06-21
Status: Accepted
Supersedes: (none)

## Context

Acorn 原有 MCP server mode：通过 `ServeConfig` / `ServeToolsConfig` 将工具暴露给外部 MCP 客户端。单用户自托管系统不需要把工具暴露给外部——MCP client（连外部 server）保留，server mode 删除。

## Decision

- 删除 `Container.mcpServer` / `serveToolset` 字段
- 删除 `internal/app/mcp_wiring.go` 的 server mode 部分
- 删除 `internal/web/routes.go` 的 `/mcp` route group
- 删除 `internal/cli/serve.go` 的 MCP server 挂载
- 删除 `config.ServeConfig` / `ServeToolsConfig`
- 保留 MCP client（连外部 MCP server 的工具）

## Consequences

- **正面**：减少 server mode 装配复杂度，消除 serve profile 概念
- **负面**：无法将 Acorn 工具暴露给其他 MCP 客户端（如 Claude Desktop）
- **风险**：无——单用户不需要外部工具消费

## Baseline Sync

- `Container` struct 已删除 `mcpServer` / `serveToolset`
- `serve.go` 已删除 `/mcp` 挂载
- OpenAPI schema 已删除 `/mcp` 路由
