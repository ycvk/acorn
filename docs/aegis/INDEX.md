# Aegis Workspace Index

## ADRs
- [ADR-0001: 砍掉 plan_execute / single_agent 编排模式](adr/ADR-0001-remove-plan-execute-single-agent.md) — keep direct_response only
- [ADR-0002: 砍掉 CompactionEngine，改为 hybrid masking + auto-compact](adr/ADR-0002-hybrid-context-masking-auto-compact.md) — observation masking + LLM summary + re-inject
- [ADR-0003: 砍掉 Bleve + FAISS，改为 embedding + SQLite 暴力检索](adr/ADR-0003-embedding-sqlite-replace-bleve-faiss.md) — zero CGO semantic search
- [ADR-0004: 砍掉 MCP server mode](adr/ADR-0004-remove-mcp-server-mode.md) — keep MCP client only
- [ADR-0005: SQLite schema 从 ~23 表精简到 ~8 表](adr/ADR-0005-sqlite-schema-simplify.md) — drop dead tables, clean cutover
- [ADR-0006: Release 简化为纯 Go build](adr/ADR-0006-pure-go-release.md) — no FAISS/CGO/build tags
- [ADR-0007: 移动端 Flutter → Kotlin + Jetpack Compose](adr/ADR-0007-mobile-flutter-to-kotlin-compose.md) — Hilt + Material 3 + OkHttp SSE + openapi-generator-cli
- [ADR-0008: 删 mode 路由壳](adr/ADR-0008-remove-mode-routing-shell.md) — OrchestrationMode + parseClientRunMode + assembleRunnerByMode
- [ADR-0009: 删 skill lifecycle/assess 企业级机制](adr/ADR-0009-remove-skill-lifecycle-assess.md) — lifecycle_tools + RoutingFixture + LifecycleStatus + AssessmentVerdict
- [ADR-0010: tool 包从 4 合并到 2](adr/ADR-0010-tool-packages-merge-to-toolkit-toolset.md) — toolkit 契约 + toolset 实现, runtime/tool + runtime/toolset 提升到 runtime 根
- [ADR-0011: 结构守卫文件行数上限 400 → 800](adr/ADR-0011-structural-file-limit-400-to-800.md) — 消除碎片化文件, RunnerFactory 7→5, browser_service 5→3, client_service 5→1
- [ADR-0012: 消除 duplicate port](adr/ADR-0012-eliminate-duplicate-ports.md) — 推翻"不可合并"声明, OperatorQuestionContext/ArtifactContext 统一为 domain.ToolCallContextBridge

## Specs
- [Structural Convergence Design Spec](specs/2026-06-22-structural-convergence-design.md) — 4→2 tool packages, runner file merge, duplicate port elimination, guard 400→800
- [Radical Refactor Design Spec](specs/2026-06-23-radical-refactor-design.md) — god-package split, dead code purge, structural debt elimination
- [Modular Refactor Design Spec](specs/2026-06-23-modular-refactor-design.md) — RunnerFactory god-object split, toolkit+toolset merge, store interface consolidation, client_service split, stream/domain type convergence

## Plans
- [Structural Convergence Refactor Plan](plans/2026-06-22-structural-convergence.md) — 11 tasks: port elimination, guard relax, tool/toolset rename, runtime subpackage promotion, file merges, docs
- [Runtime God-Package Split Plan](plans/2026-06-23-runtime-split.md) — split runtime into stream + tooldispatch + factextract sub-packages
- [Modular Refactor Plan](plans/2026-06-23-modular-refactor.md) — 5 phases: stream/domain type convergence, toolkit+toolset merge, store interface consolidation, client_service split, RunnerFactory god-object split
