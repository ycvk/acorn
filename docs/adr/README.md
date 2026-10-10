# Architecture Decision Records

ADR 记录 Acorn 架构方向级决策的"为什么"。current-state 架构真相在 `AGENTS.md` 与 `docs/architecture/INVARIANTS.md`;ADR 不是 current truth,是决策记录。

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [0001](0001-ambient-agent-direction.md) | 从 reactive agent 转向 ambient agent | Superseded by 0003 | 2026-06-27 |
| [0002](0002-three-layer-memory.md) | 三层记忆架构 — Active Memory + Archive + Periodic Review | Superseded by 0003 | 2026-06-28 |
| [0003](0003-personal-agent-direction.md) | 个人代理方向 — 唤醒驱动、工作记忆、知识库与信息追踪 | Accepted | 2026-10-02 |
| [0004](0004-personal-memory.md) | 个人记忆的证据与行动模型 | Accepted | 2026-10-09 |
| [0005](0005-knowledge-in-sqlite.md) | 知识库笔记存进 SQLite | Accepted | 2026-10-10 |

## Status 约定

- **Proposed** — 方向已定,未实施或部分实施
- **Accepted** — 已实施并成为现状
- **Superseded** — 被后续 ADR 取代
