# ADR-0001: 砍掉 plan_execute / single_agent 编排模式

Date: 2026-06-21
Status: Accepted
Supersedes: (none)

## Context

Acorn 原有三套编排模式：`direct_response`、`plan_execute`、`single_agent`。plan_execute 贡献了 `internal/runtime/plan/` 整个子包（5615 LOC）、`ChildAgentExecutor`、`SubagentExecutor`、verifier child run、plan evidence ledger。single_agent 贡献了 graph builder + ActNode。作为单用户自托管系统，这些模式在日常使用中几乎不触发——模型自己分步即可。

## Decision

只保留 `direct_response` 模式。删除 plan_execute + single_agent 的全部代码：
- `internal/runtime/plan/` 目录（15 个 .go 文件）
- `internal/runtime/graph/` 目录
- `SubagentExecutor` + `ChildAgentExecutor` + verifier
- `orchestration.SingleAgentRequest` / `PlanExecuteRequest` / `PlanStore` 等类型
- SQLite `plans` / `plan_evidence` / `plan_steps` 表
- `run_decisions` 表和 run selection by decision 逻辑

## Consequences

- **正面**：减少 ~6000 LOC，消除 child workspace 隔离复杂度，简化 run selection 为简单 skill 匹配
- **负面**：失去了显式 plan graph 的可审计性——模型不再先生成计划再执行
- **风险**：复杂多步任务可能不如 plan_execute 可控，但依赖模型自身的分步推理能力（GPT-4o+ 足够）

## Baseline Sync

- `docs/architecture/INVARIANTS.md` 已更新：单一编排模式 direct_response
- `docs/architecture/runtime-orchestration.md` 需要更新（待做）
- 架构守卫 `action_round_sharing_test.go` 和 `assembly_consolidation_test.go` 已删除
