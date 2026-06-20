# Acorn 架构总入口

Acorn 是 **single-user self-hosted agent backend + authenticated remote client API + Flutter mobile control surface**。后端以 Go/Eino 运行 agent、工具、计划、证据、trace、working checkpoint、file-backed memory 和必接入的 Bleve+FAISS semantic retrieval index。SQLite 是 runtime 事实来源；file-backed memory 是长期记忆事实。当前产品 control surface 是 `mobile/` Flutter app，通过 generated Dart client 消费 authenticated `/v1`。

## 主链

```text
operator CLI / authenticated remote clients
  -> app Container
  -> remote client contracts (/healthz + /v1 + optional /mcp)
  -> runtime Executor (consumer-owned store ports)
  -> RunnerFactory.buildRun (per-run assembly)
  -> run selection policy + ContextPlane + OrchestrationPlane
  -> SQLite adapter / persisted truth
  -> Flutter mobile control surface
```

## 主要包职责

- `internal/app/` — 装配 app service、runtime executor、run resume service、web dependencies；唯一允许直接 import sqlite 的 composition root。
- `internal/runtime/` — Executor（session/run 创建、root mode routing、执行、finalization）+ RunnerFactory（per-run assembly）+ toolset/ 子包（memory tools + Toolset 容器）。
- `internal/contextplane/` — run 上下文边界、prepared memory、deferred tool loading、tool lifecycle、budget governance；`compaction/` 子包管 proactive compact + post-compact rehydration。
- `internal/orchestration/` — public root `direct_response` / `plan_execute` assembly + 内部 child-run `single_agent` assembly。
- `internal/memorymodule/` — file-backed memory（facts/skills/history）、search、prepare、semantic retrieval（Bleve+FAISS）。
- `internal/decision/` — 小型 run selection policy（defaults-only，消费 explicit skill + skill candidates + working context）。
- `internal/store/` + `internal/store/sqlite/` — 跨包 store-facing records、ledger contracts、sentinel errors + SQLite adapter（sessions/runs/events/plans/checkpoints/archives/context boundaries/tool results/artifacts）。
- `internal/web/` — `/v1` client surface + device bearer auth middleware；live RunEvent 从 `events` 表投影 mobile live subset。
- `mobile/` — Flutter app，通过 generated Dart client 消费 `/v1`；不执行 runtime、不维护第二套 message lifecycle。

## 子架构文档

- [runtime-execution.md](runtime-execution.md) — Executor、run lifecycle、root mode routing。
- [runtime-orchestration.md](runtime-orchestration.md) — OrchestrationPlane、assembly、child-agent contract。
- [runtime-context-memory-decision.md](runtime-context-memory-decision.md) — ContextPlane、MemoryModule、run selection、tool lifecycle。
- [data-web-store.md](data-web-store.md) — SQLite truth、events/runs/plans、remote client DTO/API。
- [mobile-control-surface.md](mobile-control-surface.md) — Flutter app、generated client、事实边界。
- [self-hosted onboarding](../user/self-hosted-onboarding.md) — VPS binary service、pairing、storage。

## 边界与术语

架构不变量（带测试引用）见 [INVARIANTS.md](INVARIANTS.md)；术语表见 [GLOSSARY.md](GLOSSARY.md)。
