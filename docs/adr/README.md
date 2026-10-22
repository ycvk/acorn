# Architecture Decision Records

ADR 记录 Acorn 架构方向级决策的"为什么"。current-state 架构真相在 `AGENTS.md` 与 `docs/architecture/INVARIANTS.md`;ADR 不是 current truth,是决策记录。

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [0001](0001-ambient-agent-direction.md) | 从 reactive agent 转向 ambient agent | Superseded by 0003 | 2026-06-27 |
| [0002](0002-three-layer-memory.md) | 三层记忆架构 — Active Memory + Archive + Periodic Review | Superseded by 0003 | 2026-06-28 |
| [0003](0003-personal-agent-direction.md) | 个人代理方向 — 唤醒驱动、工作记忆、知识库与信息追踪 | Accepted | 2026-10-02 |
| [0004](0004-personal-memory.md) | 个人记忆的证据与行动模型 | Accepted | 2026-10-09 |
| [0005](0005-knowledge-in-sqlite.md) | 知识库笔记存进 SQLite | Accepted | 2026-10-10 |
| [0006](0006-agent-workstation.md) | agent 的工作机 — 在 VPS 上有一台能动手的隔离电脑 | Accepted | 2026-10-12 |
| [0007](0007-agent-maintained-skills.md) | agent 维护自己的技能 | Accepted | 2026-10-12 |
| [0008](0008-persona-in-app.md) | 人格存进 SQLite，owner 在 App 里编辑 | Accepted | 2026-10-15 |
| [0009](0009-background-work-on-concerns.md) | 后台把关切推进到成品 | Accepted | 2026-10-17 |
| [0010](0010-package-tools-and-triggers.md) | 技能包带上工具与触发器 | Accepted | 2026-10-17 |
| [0011](0011-views-in-replies.md) | 回复里的界面 | Accepted | 2026-10-21 |
| [0012](0012-live-updates-for-runs.md) | 进行中的 run 在 App 外可见 | Accepted | 2026-10-21 |
| [0013](0013-news-desk.md) | 新闻编辑部：收集、整理与早安卡精选 | Proposed | 2026-10-22 |

## Status 约定

- **Proposed** — 方向已定,未实施或部分实施
- **Accepted** — 已实施并成为现状
- **Superseded** — 被后续 ADR 取代
