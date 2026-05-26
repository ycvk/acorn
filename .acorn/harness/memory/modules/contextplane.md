---
type: architecture_module
id: contextplane
status: stable
path: internal/contextplane
interfaces:
  - from: orchestration
    contract: "ContextSession, BootstrapRequest, ModelCallRequest"
  - to: memorymodule
    contract: "memorymodule.Search/Prepare"
  - to: runtimehistory
    contract: "RunEvent persistence"
owner_run: run_abc123
last_updated: 2026-05-26
---

# ContextPlane

## 职责

Root-run model input owner。负责 context assembly、memory context、rehydration packet 预算。不允许绕过它维护第二套 message lifecycle。

## 核心组件

- `ContextSession` interface：root-run model input 的统一入口
- `BudgetGovernor`：context pressure 的唯一计算入口
- `CompactionEngine`：compact 规则所有者（summary prompt、structured continuation validation、preserved tail、tool-call/tool-result pair preservation）
- `contextBoundaryID()` / `sanitizeBoundaryIDPart()`：边界 ID 防御性清理

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 增加 `sanitizeBoundaryIDPart` 防御性字符白名单过滤

## 硬约束（不可违反）

1. `ContextSession` 是 root-run model input owner。root mode 不允许绕过它维护第二套 message lifecycle。
2. `BudgetGovernor` 是 context pressure 的唯一计算入口。不要恢复 `threshold_pct`、raw percentage trigger、字符估算或 client-local pressure 估算。
3. Context assembly / memory context / rehydration packet 预算必须使用后端统一 token counter；不要恢复字符串级 `trimToBudget`、`rune/4` 估算或 silent drop active context。
4. Tool output 是模型可见 tool result truth；不要恢复 `toolOutputCompressor` 或在 audit wrapper 里截断真实工具输出。需要回收上下文时只用 durable `tool_result_ref` 过期替换。
5. `CompactionEngine` 拥有 compact 规则：summary prompt、structured continuation validation、preserved tail、tool-call/tool-result pair preservation 和 compression metrics 不能散落回 middleware。
6. contextplane post-compact rehydration helper 拥有 packet 恢复。compact 后不能只靠 summary 继续，也不能扫描 workspace 猜 recent files。
7. `ContextBoundary` 是 durable compact boundary truth。`context.compressed` 只是 RunEvent projection，不能作为 loader truth。
8. Reactive compact 只处理真实 provider/model context overflow，并且只允许同 provider/options 一次重试。其他 provider/runtime/tool/parser 错误必须显式失败。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
