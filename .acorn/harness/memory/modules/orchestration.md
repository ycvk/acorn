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
last_updated: 2026-05-30
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
- 最近改动: 2026-05-30 `orchestration.ExecuteRound` 成为 direct_response 和 ActNode 共享的模型→工具 round primitive；`RoundOptions.BeforeToolCall` 是工具提交前的显式校验 hook，`CallSite` 保留 provider usage 归因。`single_agent` / `plan_execute` 的 ActNode 不再维护自有工具 streaming loop。

## 硬约束（不可违反）

1. 当前 public root modes 是 `direct_response`、`plan_execute`；`single_agent` 只作为内部 child-run / verifier / eval 执行模式。
2. Context assembly / memory context / rehydration packet 预算必须使用后端统一 token counter。
3. `BudgetGovernor` 是 context pressure 的唯一计算入口。
4. 新增 root/graph 模型→工具执行语义时优先扩展 `ExecuteRound`，不要在 ActNode 或 direct_response 里恢复第二套 tool loop；高风险工具校验必须在 `BeforeToolCall` pre-submit hook 中 fail-loud。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
