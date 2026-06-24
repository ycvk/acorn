# Acorn 架构总入口

Acorn 是 **single-user self-hosted agent backend + authenticated remote client API + Kotlin mobile control surface**。后端以 Go/Eino 运行 agent、工具、file-backed memory 和可选的 embedding+SQLite 语义检索。SQLite 是 runtime 事实来源（~8 张表）；file-backed memory 是长期记忆事实。当前产品 control surface 是 `mobile-kotlin/` Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 authenticated `/v1`。

## 主链

```text
operator CLI / authenticated remote clients
  -> wire Container
  -> remote client contracts (/healthz + /v1)
  -> runtime Executor (consumer-owned store ports: core.SessionStore/IdentityStore/ArtifactStore)
  -> per-run assembly (ModelBuilder/CapabilityAssembler/ContextAssembler/MCPAssembler/SkillSelector/RunEmitter/ToolAssembler)
  -> Plane + direct_response
  -> SQLite adapter / persisted truth
  -> Kotlin mobile control surface
```

## 主要包职责（13 个 internal 包）

- `internal/core/` — Layer 0。核心 domain 类型（RunRecord/EventRecord/SessionRecord/PendingActionRecord/Stream* payload + typed accessors）+ context plumbing + ports（SessionStore/IdentityStore/ArtifactStore）+ 工具契约（ToolContract/Catalog/ToolSpec）+ plugin registry 接口。零内部导入。原 domain/port/contract/clientevents 类型收敛于此。
- `internal/runtime/` — Layer 3。Executor（session/run 创建、执行、finalization）+ per-run assembly（7 个 struct）+ direct_response + ExecuteRound + Plane + Session（masking + auto-compact）+ StreamItem→event 投影 + tool audit/validator + tool lifecycle。原 agent/context/stream 合并于此。
- `internal/store/` — SQLite adapter + 跨包 store-facing records、sentinel errors。
- `internal/tools/` — SafeParallelToolsNode、streaming executor、scheduler、side-effect extraction、ToolRegistry 实现 + 工具实现（file/git/browser/web/command/artifact）。
- `internal/memory/` — file-backed memory（facts/history）、search、prepare、semantic retrieval。
- `internal/mcp/` — MCP provider manager、transport、OAuth/elicitation handlers（原 providers/mcp，提升为顶层包）。
- `internal/api/` — `/v1` client surface + device bearer auth + live RunEvent 投影（原 clientevents 合并于此）+ ThreadService/RunService/EventService。
- `internal/wire/` — Container 组合根；唯一允许直接持有 sqlite adapter 的 composition root。
- `internal/config/` · `internal/workspace/` · `internal/skills/` · `internal/webaccess/` — 配置、workspace checkpoint、skill loader、web 工具。
- `internal/cli/` — operator CLI 命令。
- `mobile-kotlin/` — Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 `/v1`。

## 子架构文档

- [runtime-execution.md](runtime-execution.md) — Executor、run lifecycle。
- [runtime-orchestration.md](runtime-orchestration.md) — direct_response assembly、ExecuteRound。
- [runtime-context-memory-decision.md](runtime-context-memory-decision.md) — Plane、hybrid context、MemoryModule。
- [data-web-store.md](data-web-store.md) — SQLite truth、events/runs、remote client DTO/API。
- [mobile-control-surface.md](mobile-control-surface.md) — Kotlin app、generated client、事实边界。
- [self-hosted onboarding](../user/self-hosted-onboarding.md) — VPS binary service、pairing、storage。

## 边界与术语

架构不变量（带测试引用）见 [INVARIANTS.md](INVARIANTS.md)；术语表见 [GLOSSARY.md](GLOSSARY.md)。
