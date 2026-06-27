---
adr: 0002
title: 三层记忆架构 — Active Memory + Archive + Periodic Review
status: Accepted
date: 2026-06-28
supersedes: []
---

# ADR-0002: 三层记忆架构

## Context

Acorn 的记忆模块在 ambient 化过程中补了混合检索、reindex、技能自固化，但记忆架构本身有一个根本缺口：**没有分层**。

当前所有 facts 是平等的，靠 Search 匹配按需注入。问题是：
- 没有容量上限 — facts 无限 append，没有 consolidate 机制，会无限膨胀
- 没有 frozen snapshot 注入 — 关键事实（owner 身份、环境、偏好）不会始终可见，靠搜索匹配才出现
- 没有 background review — agent 被教了 ambient 循环里的 Record 步骤，但没有周期性触发，完全靠 agent 自觉

对比 Hermes Agent 的三层记忆：
1. Persistent Memory（MEMORY.md + USER.md）— 有容量上限的策展记忆，注入 system prompt
2. Session Search — 完整对话历史，免费、无限量，按需检索
3. Background Review — 每 N 轮触发一次 LLM 判断"有没有值得记住的"

## Decision

将 Acorn 的记忆重构为三层：

### Layer 1: Active Memory（frozen snapshot，注入 system prompt）

- `facts/user/` 下的 non-retired user-scoped facts，按字符上限截取最前面的条目
- 每个 run 开始时注入 system prompt，run 内不变化（保 prefix cache）
- agent 通过 `remember` / `memory_replace_span` 管理
- 容量上限：默认 2200 字符（~800 tokens），和 Hermes 一致
- 超限时整条跳过（不截断），放不下的 fact 留在可检索的 Archive 里

### Layer 2: Archive（history，append-only 事件日志）

- **已完成**：每次 run 完成后写 fallback summary（rune-safe，零 LLM 成本）
- 混合检索（keyword + embedding via sqlite-vec）覆盖 history
- agent 用 `search_runs` / `memory_search` 按需检索

### Layer 3: Periodic Review（background review，每 N 轮触发）

- 每 N 个 run（默认 5）触发一次 LLM 调用
- LLM 判断：这 N 个 run 产生了什么值得持久化的事实？
- 有就调 `remember` 写入 facts，没有就跳过
- 计数器在 Executor 里，内存态（serve 重启归零）
- 异步执行（不阻塞 run 收尾）
- 可以跑在更便宜的 model 上（通过 config 配置 review model）

## 不做

- 不引入外部记忆 provider（Hindsight/Mem0/Letta）— ADR-0001 已否决
- 不做 knowledge graph / entity extraction — 单 owner 场景过重
- 不做 write_approval gate — 单 owner 语义，agent 固化给自己用

## Consequences

### 正面
- 关键事实始终可见（Layer 1），不依赖搜索匹配
- facts 有容量上限，不会无限膨胀
- review 周期性触发，不依赖 agent 自觉
- 成本可控：review 每 N 轮一次 LLM 调用，不是每 run 一次

### 负面 / 风险
- Layer 1 改了 facts 注入路径，是 run 装配链的结构改动
- Layer 3 引入新的 LLM 调用（但频率低，每 N 轮一次）
- review 计数器内存态，serve 重启归零（可接受，单 owner 进程重启不频繁）

## References

- Hermes Agent memory: https://hermes-agent.nousresearch.com/docs/user-guide/features/memory
- Acorn ADR-0001: ambient agent direction
