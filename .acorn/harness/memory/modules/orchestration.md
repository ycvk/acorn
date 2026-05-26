---
type: architecture_module
id: orchestration
status: stable
path: internal/orchestration
interfaces:
  - from: runtime
    contract: "run selection policy + OrchestrationPlane"
  - to: contextplane
    contract: "OrchestrationPlane -> ContextSession"
  - to: providers
    contract: "model provider routing"
owner_run: run_abc123
last_updated: 2026-05-26
---

# Orchestration

## 职责

Run 编排与调度层。负责 run selection policy、root mode 路由、provider 选择。

## 核心组件

- `OrchestrationPlane`：编排平面
- `run selection policy`：决定 run 用哪个 mode、哪个 provider、哪个 model profile
- Root mode 路由：`direct_response` / `plan_execute`

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认 `single_agent` 只作为内部 child-run / verifier / eval 模式

## 硬约束（不可违反）

1. 当前 public root modes 是 `direct_response`、`plan_execute`；`single_agent` 只作为内部 child-run / verifier / eval 执行模式。
2. Context assembly / memory context / rehydration packet 预算必须使用后端统一 token counter。
3. `BudgetGovernor` 是 context pressure 的唯一计算入口。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
