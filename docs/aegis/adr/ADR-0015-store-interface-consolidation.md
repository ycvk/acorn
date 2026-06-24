# ADR-0015: Store 接口 21→3 收敛

Date: 2026-06-24
Status: Accepted
Supersedes: ADR-0012 (duplicate port elimination, now fully subsumed by core capability interfaces)

## Context

21 个 store-like 接口散在 6 个包:port 的 9 个 Repo、contract.StoreView(43 方法)、agent.ExecutorStore + RunnerFactoryStore、wire.containerRuntimeStore、domain.SessionSummaryStore。消费者只用 3-5 个方法却依赖整个 mega-interface。

## Decision

收敛为 3 个能力接口,按数据域切分:
- `core.SessionStore` — 会话/消息/run/event/pending-action(31 方法)
- `core.IdentityStore` — 设备/配对(7 方法)
- `core.ArtifactStore` — artifacts/summaries/OAuth(8 方法)

保留 2 个 ISP 窄 facet(EventAppender 1 方法、SessionSummaryStore 2 方法)供只需要单方法的消费者使用。

store 包通过 type alias 让 domain.X 和 core.X 成为同一 Go 类型,零方法签名变更同时满足新旧接口。

## Consequences

- **正面**:接口从 21→3+2;消费者只 import 需要的能力接口;StoreView 43 方法 mega-interface 消除
- **负面**:SessionStore 31 方法仍较大,但按数据域聚合是合理边界
- **风险**:无——编译器验证接口实现

## Baseline Sync

- `core/store.go` 创建 3 个能力接口(commit `e4d990a`)
- `store/sqlite_store.go` 添加 compile-time assertions(commit `f9b414e`)
- domain 类型 alias 到 core(commit `f9b414e`)
- 旧接口(port.*Repo/contract.StoreView/agent.ExecutorStore)删除(commit `6453d5c`)
- `store_interface_count_test.go` 更新(commit `3f7d76a`)
