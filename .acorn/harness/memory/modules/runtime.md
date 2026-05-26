---
type: architecture_module
id: runtime
status: stable
path: internal/runtime
interfaces:
  - from: cli
    contract: "CLI commands -> Executor"
  - to: contextplane
    contract: "Executor -> RunnerFactory -> ContextSession"
  - to: orchestration
    contract: "run selection policy + OrchestrationPlane"
owner_run: run_abc123
last_updated: 2026-05-26
---

# Runtime

## 职责

执行链核心。当前 runtime 主链：`Executor -> RunnerFactory/runBuilder -> run selection policy + ContextPlane + OrchestrationPlane -> ContextSession -> SQLite/file-backed memory`。

## 核心组件

- `Executor`：执行入口
- `RunnerFactory` / `runBuilder`：run 构建与调度
- `run selection policy`：run 路由策略
- `ContextPlane`：context 管理层（见 `memory/modules/contextplane.md`）
- `OrchestrationPlane`：编排层（见 `memory/modules/orchestration.md`）

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认运行链完整

## 硬约束（不可违反）

1. 当前 public root modes 是 `direct_response`、`plan_execute`；`single_agent` 只作为内部 child-run / verifier / eval 执行模式。
2. Tool lifecycle fail-loud：unknown、disabled、deferred-before-load 是模型可见 failed tool result；runtime wiring/storage/model failure 是 run failure。
3. Tool result lifecycle 必须写入 durable ledger；ledger wiring/storage 失败是 run failure。
4. workspace checkpoint / rollback side effects 只能从后端 ledger/workbench projection 消费。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
