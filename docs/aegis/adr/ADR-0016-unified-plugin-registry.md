# ADR-0016: 统一插件注册中心 (ToolRegistry + ProviderRegistry)

Date: 2026-06-24
Status: Accepted

## Context

工具注册埋在 `tools.Catalog` + `tools.configuredLocalSpec` + MCP manager 的 `buildCapabilitySpecs` 里,没有统一入口。MCP reconcile 通过 `RunnerFactory.ReconcileMCPProviders` 间接调用,绕了多层。新增工具需改多处代码。

## Decision

在 `core` 包定义两个一等公民接口:
- `core.ToolRegistry` — 统一注册原生工具和 MCP 工具,含 Register/Unregister/Resolve
- `core.ProviderRegistry` — 统一管理 MCP provider 生命周期,含 RegisterProvider/UnregisterProvider/Reconcile

`tools` 包实现 `ToolRegistry`,`mcp.Manager` 实现 `ProviderRegistry`。wire 构造 registry,注册原生工具,MCP 连接后注册 MCP 工具到同一 registry。runtime 通过 `registry.Resolve()` 获取工具实例。

## Consequences

- **正面**:新增工具只需 `registry.Register(spec, factory)`,不改 runtime;MCP 热更新通过 registry.Unregister + Register;统一入口替代分散的 Catalog + reconcile
- **负面**:ToolSpec 新增 Factory 字段;port.ToolSpec 与 core.ToolSpec 类型不同,需转换(Phase 7 已处理)
- **风险**:MCP 工具注册时机依赖连接生命周期——已通过 registerProviderTools/unregisterProviderTools 在 connect/disconnect 时处理

## Baseline Sync

- `core/registry.go` 定义接口(commit `e4d990a`)
- `tools/registry.go` 实现 ToolRegistry(commit `0bf00e2`)
- wire 构造 registry 并注入(commit `3a5ce5f`)
- MCP 工具注册到 registry(commit `6efdb2e`)
- `mcp/registry_adapter.go` 实现 ProviderRegistry(commit `868093b`)
