# Acorn 架构总入口

Acorn 是 **single-user self-hosted agent backend + authenticated remote client API + Kotlin mobile control surface**。后端以 Go/Eino 运行 agent、工具、file-backed memory 和可选的 embedding+SQLite 语义检索。SQLite 是 runtime 事实来源（~8 张表）；file-backed memory 是长期记忆事实。当前产品 control surface 是 `mobile-kotlin/` Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 authenticated `/v1`。

## 主链

```text
operator CLI / authenticated remote clients
  -> app Container
  -> remote client contracts (/healthz + /v1)
  -> runtime Executor (consumer-owned store ports)
  -> RunnerFactory.buildRun (per-run assembly)
  -> ContextPlane + direct_response
  -> SQLite adapter / persisted truth
  -> Kotlin mobile control surface
```

## 主要包职责

- `internal/app/` — 装配 app service、runtime executor、run resume service、api dependencies；composition root。
- `internal/runtime/` — Executor（session/run 创建、执行、finalization）+ RunnerFactory（per-run assembly）+ direct_response assembly + ExecuteRound + tool audit/validator + StreamItem 投影逻辑。
- `internal/runtime/tooldispatch/` — SafeParallelToolsNode、streaming executor、scheduler、side-effect extraction、ToolInvoker/StreamingExecutor 接口。
- `internal/runtime/factextract/` — fact extraction + memory file tools + memory search/remember tools。
- `internal/stream/` — Stream* 值类型、StreamItem→event 投影、typed accessors、AgentEvent→StreamItem 转换、assistant streaming。
- `internal/contextplane/` — run 上下文装配、observation masking、LLM auto-compact、deferred tool loading、tool lifecycle。
- `internal/toolkit/` — 工具契约层（ToolContract/Catalog/ToolSpec/loading+execution policy）。
- `internal/toolset/` — 工具实现层（file/git/browser/web/command/artifact/memory 工具实现）。
- `internal/memory/` — file-backed memory（facts/history）、search、prepare、semantic retrieval（embedding + SQLite 暴力余弦相似度）。
- `internal/store/` — SQLite adapter + 跨包 store-facing records、sentinel errors（sessions/messages/runs/events/pending_actions/devices/pairing_codes/owner_profile）。
- `internal/api/` — `/v1` client surface + device bearer auth middleware；live RunEvent 从 `events` 表投影 mobile live subset。
- `mobile-kotlin/` — Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 `/v1`；不执行 runtime、不维护第二套 message lifecycle。

## 子架构文档

- [runtime-execution.md](runtime-execution.md) — Executor、run lifecycle。
- [runtime-orchestration.md](runtime-orchestration.md) — direct_response assembly、ExecuteRound。
- [runtime-context-memory-decision.md](runtime-context-memory-decision.md) — ContextPlane、hybrid context、MemoryModule。
- [data-web-store.md](data-web-store.md) — SQLite truth、events/runs、remote client DTO/API。
- [mobile-control-surface.md](mobile-control-surface.md) — Kotlin app、generated client、事实边界。
- [self-hosted onboarding](../user/self-hosted-onboarding.md) — VPS binary service、pairing、storage。

## 边界与术语

架构不变量（带测试引用）见 [INVARIANTS.md](INVARIANTS.md)；术语表见 [GLOSSARY.md](GLOSSARY.md)。
