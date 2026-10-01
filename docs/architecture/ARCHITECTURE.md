# Acorn 架构总入口

Acorn 是 **single-user self-hosted agent backend + authenticated remote client API + Kotlin mobile control surface**。后端以 Go/Eino 运行 agent、工具、file-backed memory。SQLite 是 runtime 事实来源（10 张表）；file-backed memory 是长期记忆事实。当前产品 control surface 是 `mobile-kotlin/` Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 authenticated `/v1`。

## 主链

```text
operator CLI / authenticated remote clients / triggers (webhook, cron)
  -> wire Container
  -> remote client contracts (/healthz + /v1)
  -> runtime Executor (consumer-owned store ports: core.SessionStore/IdentityStore/ArtifactStore)
  -> per-run assembly (RunnerFactory.buildRun: chat model, capabilities, context, skills)
  -> Plane + direct_response + Session
  -> SQLite adapter / persisted truth
  -> Kotlin mobile control surface
```

## 主要包职责（14 个 internal 包）

- `internal/core/` — Layer 0。核心 domain 类型（RunRecord/EventRecord/SessionRecord/PendingActionRecord/Stream* payload）+ context plumbing + store ports（SessionStore/IdentityStore/ArtifactStore）+ 工具契约（ToolContract/ToolSpec/ToolRegistry）。零内部导入。
- `internal/runtime/` — Layer 3。Executor（session/run 创建、执行、finalization）+ per-run assembly + direct_response + ExecuteRound + Plane + Session（masking + auto-compact）+ StreamItem→event 投影 + tool audit/validator + periodic memory review。
- `internal/store/` — SQLite adapter + ArtifactService。
- `internal/tools/` — ToolRegistry 实现 + 工具实现（file/git/browser/web/command/artifact/operator/worldstate）+ 风险闸门；`internal/tools/dispatch` 是工具调度（SafeParallelToolsNode、streaming executor、scheduler、side-effect extraction）。
- `internal/memory/` — file-backed memory（facts/history）、Active Memory、混合检索、WorldState。
- `internal/mcp/` — MCP provider manager、transport、OAuth/elicitation handlers；MCP sampling（协议 2026-07-28 弃用）不支持。
- `internal/api/` — `/v1` client surface + device bearer auth + live RunEvent 投影 + Thread/Run/Event/Inbox services。
- `internal/triggers/` — `serve` 进程内常驻的 webhook/cron trigger scheduler。
- `internal/wire/` — Container 组合根；唯一允许直接持有 sqlite adapter 的 composition root。
- `internal/config/` · `internal/workspace/` · `internal/skills/` · `internal/webaccess/` — 配置、workspace checkpoint、skill loader、web fetch/search/URL policy。
- `internal/cli/` — operator CLI 命令。
- `mobile-kotlin/` — Kotlin + Jetpack Compose app，通过 openapi-generator 生成的 client 消费 `/v1`。

## 子架构文档

- [runtime-execution.md](runtime-execution.md) — Executor、run lifecycle。
- [runtime-orchestration.md](runtime-orchestration.md) — direct_response、ExecuteRound、hybrid context。
- [runtime-context-memory-decision.md](runtime-context-memory-decision.md) — Plane、Session、memory。
- [tools.md](tools.md) — 工具契约、调度、workspace/artifact、web access。
- [data-web-store.md](data-web-store.md) — SQLite truth、events/runs、remote client memory surface。
- [mobile-control-surface.md](mobile-control-surface.md) — Kotlin app、generated client、事实边界。
- [self-hosted onboarding](../user/self-hosted-onboarding.md) — VPS binary service、pairing、storage。

## 边界与术语

架构不变量（带测试引用）见 [INVARIANTS.md](INVARIANTS.md)；术语表见 [GLOSSARY.md](GLOSSARY.md)；方向级决策见 [ADR](../adr/README.md)。
