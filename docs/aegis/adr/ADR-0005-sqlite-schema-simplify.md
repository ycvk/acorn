# ADR-0005: SQLite schema 从 ~23 表精简到 ~8 表

Date: 2026-06-21
Status: Accepted
Supersedes: (none)

## Context

Acorn 原有 SQLite schema 有 ~23 张表，包含 plan_execute（plans/plan_evidence/plan_steps）、context boundary 持久化（context_boundaries）、tool result ledger（tool_results）、conversation segments、run archives、working checkpoints、provider_usage、run_decisions 等。重构砍掉了对应的功能，这些表成为死表。

## Decision

Schema 降到 ~8 张表：

| 表 | 用途 |
|---|---|
| `sessions` | 会话 |
| `messages` | 消息 |
| `runs` | run 记录 |
| `events` | run 事件流 |
| `pending_actions` | 审批动作 |
| `devices` | 设备认证 |
| `pairing_codes` | 配对码 |
| `owner_profile` | owner |

主动 drop 旧表：`plans` / `plan_evidence` / `plan_steps` / `tool_results` / `context_boundaries` / `conversation_segments` / `run_archives` / `working_checkpoints` / `provider_usage` / `run_decisions`。

`runs` 表删除 `orchestration_mode` / `skill_id` / `depth` / `parent_run_id` / `checkpoint_id` 列。

迁移策略：**清空重来**（spec §3.4），不写 migration。新 schema 在 `store_schema_bootstrap.go` 中定义，`store_schema_drops.go` 主动 drop 旧表。

## Consequences

- **正面**：schema 简单可维护，消除大量死表，SQLite 连接更轻
- **负面**：旧数据不可恢复——VPS 重新部署需要清空 SQLite + 重新 pair 手机
- **风险**：drop 旧表是破坏性操作，但 spec 明确"旧数据清空，不迁移历史"

## Baseline Sync

- `internal/store/sqlite/store_schema_bootstrap.go` 已更新为 8 表
- `store_schema_drops.go` 已添加 drop 语句
- `RunCreateParams` 已简化（删除 OrchestrationMode/ParentRunID/SkillID/Depth/CheckpointID）
- `CreateRun` 签名变为 `(ctx, runID, input)`
