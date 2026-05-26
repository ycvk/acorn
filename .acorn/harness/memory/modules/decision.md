---
type: architecture_module
id: decision
status: stable
path: internal/decision
interfaces:
  - from: runtime
    contract: "runtime decision evaluation"
owner_run: run_abc123
last_updated: 2026-05-26
---

# Decision

## 职责

运行时决策配置管理。`decision.md` 是 Acorn runtime 的决策配置文件。

## 核心组件

- `internal/decision`：决策规则解析与执行
- `decision.md`：项目根目录的决策配置

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认 `decision.md` 是 runtime 配置文件，不是协作文档

## 硬约束（不可违反）

1. `decision.md` 是 Acorn runtime 的决策配置文件，不是协作文档。
2. 决策配置变更需要验证其对 runtime 行为的影响。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
