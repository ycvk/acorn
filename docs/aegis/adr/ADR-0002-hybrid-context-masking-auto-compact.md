# ADR-0002: 砍掉 CompactionEngine，改为 hybrid masking + auto-compact

Date: 2026-06-21
Status: Accepted
Supersedes: (none)

## Context

Acorn 原有 `CompactionEngine` + 8 种 rehydration packet（working checkpoint / selected skill / skill catalog / tool state / session summary / prepared memory / plan / recent files）+ `BudgetGovernor` effective window 派生阈值 + reactive compact 重试。这套系统为企业级多轮长会话设计，但单用户日常使用中，现代模型 200K+ context window 很少触发 compact。

## Decision

采用业界主流 hybrid 方案（参考 Claude Code auto-compact + JetBrains 研究）：

1. **Observation masking（默认防线）**：tool result 超 `mask_after_turns`（默认 2）轮后，用占位符替换完整 output。纯内存操作，不写 SQLite。
2. **LLM auto-compact（接近 limit 时触发）**：token 超 `window_tokens - compact_margin`（默认 margin 13000）时，用一次 model 调用生成 summary，替换旧消息。circuit breaker：连续 3 次失败停止。
3. **关键上下文 re-inject（compact 后）**：从 disk/memory 重新注入 system prompt + memory context + skill context（3 种，不是 8 种 packet）。

删除：
- `CompactionEngine` + structured continuation summary + required sections 校验
- 8 种 rehydration packet
- `BudgetGovernor` effective window 派生阈值
- `CompactTriggerReactive` + reactive compact 重试
- `CompressionState` / `CompressionOutcome` / `CompactTrigger` 类型
- `tool_results` durable ledger + `tool_result_ref` marker
- `context_boundaries` 表

## Consequences

- **正面**：contextplane 从 ~3000 LOC 降到 ~1200 LOC，消除 BudgetGovernor 多元 pressure state，compact 逻辑简单可测
- **负面**：compact 后丢失 tool result 原文（只有 summary），不能回溯中间步骤
- **风险**：auto-compact 依赖 LLM summary 质量——如果 summary 丢失关键信息，后续推理可能出错。circuit breaker 防止无限重试

## Baseline Sync

- `docs/architecture/INVARIANTS.md` 已更新：Hybrid context: masking + auto-compact
- `internal/contextplane/` 已重写：`masking.go` + `auto_compact.go` + 简化 `context_session.go`
