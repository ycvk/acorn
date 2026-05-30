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
last_updated: 2026-05-30
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
- 最近改动: 2026-05-30 删除 per-chunk `tool.call.progress` 持久化；`tooling.ProgressTool` 只保留 in-process callback 能力，durable tool truth 由 final tool message、`tool_results` ledger 和 started/succeeded/failed/interrupted audit events 承载。同轮删除 public trace/debug surface，`TraceService` 收口改名为 `RunResumeService`，只负责从 persisted root interrupt contexts 推导 resume targets。2026-05-29 将 mobile live RunEvent contract 从 runtime diagnostic trace 中 hard-cut 出来；`/v1/runs/{run_id}/events` 只投影移动端 live 子集。

## 硬约束（不可违反）

1. 当前 public root modes 是 `direct_response`、`plan_execute`；`single_agent` 只作为内部 child-run / verifier / eval 执行模式。
2. Tool lifecycle fail-loud：unknown、disabled、deferred-before-load 是模型可见 failed tool result；runtime wiring/storage/model failure 是 run failure。
3. Tool result lifecycle 必须写入 durable ledger；ledger wiring/storage 失败是 run failure。
4. workspace checkpoint / rollback side effects 只能从后端 ledger/store-owned projection 消费。
5. 代码变更必须包含可复现的测试或回归验证；缺少测试的变更视为未完成的交付，禁止合并。
6. Runtime diagnostic events（MCP/sampling/skill/procedure/memory/context/plan/subagent 等）可以持久化并进入 backend-only diagnostic summary，但不能自动提升为 mobile live RunEvent，也不能作为 trace summary、raw payload、public plan DTO 或 runtime workbench 聚合暴露到 `/v1` RunDetail；per-chunk tool progress 不持久化为 RunEvent；新增 live event kind 必须同步 `clientevents.IsLiveRunEventKind`、OpenAPI、generated mobile client 和 parser/projection tests。
7. `internal/runtime/api` 只能承载跨 runtime 子包共享的窄 contracts；tool-only 实现必须归 `internal/runtime/tool`，不能以“共享 API”名义复制到 root runtime 或 `runtime/api`。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
