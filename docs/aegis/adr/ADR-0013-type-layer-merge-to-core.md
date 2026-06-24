# ADR-0013: 类型层 4→1 合并 (domain/port/contract/clientevents → core)

Date: 2026-06-24
Status: Accepted
Supersedes: ADR-0010 (toolkit+toolset merge, now fully absorbed into core)

## Context

前 4 轮重构保留了 domain/port/contract/clientevents 四个类型定义包,造成类型间接:追一个 `RunRecord` 需跨 4 包跳转,21 个 store-like 接口散在 6 个包里。greenfield refactor 新增 port 包的 9 个 Repo 接口后,类型间接不减反增。

## Decision

将 domain/port/contract/clientevents 全部合并为 `internal/core/` 单一包:
- 核心类型(RunRecord/EventRecord/SessionRecord 等)从 domain 移入
- Store 接口从 port 的 9 个 Repo 收敛为 3 个能力接口(SessionStore/IdentityStore/ArtifactStore)
- Tool 契约(ToolSpec/ToolContract/Catalog)从 port 移入,ToolSpec 新增 Factory 字段
- StoreView 从 contract 删除,ExecutorHandle 移入 api
- 投影类型从 clientevents 移入 core
- 新增 ToolRegistry + ProviderRegistry 注册中心接口

core 零 internal 依赖,由 `go list` 验证。

## Consequences

- **正面**:类型定义集中在一处;store 接口从 21→3;追代码不再跨包跳转;新增工具只需实现 ToolSpec + 注册到 registry
- **负面**:core 包 ~1573 行,需用多文件组织(13 文件,最大 273 行)
- **风险**:无——编译器 + 架构守卫验证通过

## Baseline Sync

- `internal/core/` 创建(commit `e4d990a`)
- `internal/domain/` 类型通过 alias 过渡后删除(commit `6453d5c`)
- `internal/port/`、`internal/contract/`、`internal/clientevents/` 删除(commit `6453d5c`)
- `docs/architecture/ARCHITECTURE.md` + `INVARIANTS.md` 更新(commit `3f7d76a`)
