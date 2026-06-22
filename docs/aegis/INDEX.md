# Aegis Workspace Index

## Specs
- [2026-06-21 Acorn 全面重构设计](specs/2026-06-21-acorn-refactor-design.md) — backend + frontend architecture refactor, scope: full
- [2026-06-22 后端残留清理 + Kotlin 移动端迁移](specs/2026-06-22-backend-cleanup-kotlin-migration-design.md) — dead-shell removal + Flutter→Kotlin migration

## Plans
- [Phase 1: 后端基础层清理](plans/2026-06-21-phase1-baseline-layer-cleanup.md) — config/store/events/tooling schema simplification
- [Phase 2: Runtime + Orchestration 清理](plans/2026-06-21-phase2-runtime-orchestration-cleanup.md) — delete plan_execute/single_agent, keep direct_response only
- [Phase 3: ContextPlane 清理 + Hybrid Context](plans/2026-06-21-phase3-contextplane-hybrid-context.md) — masking + auto-compact + re-inject
- [Phase 4: MemoryModule 清理 + Embedding+SQLite](plans/2026-06-21-phase4-memorymodule-embedding-sqlite.md) — replace bleve/faiss with embedding+sqlite
- [Phase 5: App + Web + OpenAPI 清理](plans/2026-06-21-phase5-app-web-openapi-cleanup.md) — remove mcp server/plan/skill-lifecycle API
- [Phase 6: Flutter 重写 + Release 简化](plans/2026-06-21-phase6-flutter-rewrite-release.md) — modular flutter + pure go release

## ADRs
- [ADR-0001: 砍掉 plan_execute / single_agent 编排模式](adr/ADR-0001-remove-plan-execute-single-agent.md) — keep direct_response only
- [ADR-0002: 砍掉 CompactionEngine，改为 hybrid masking + auto-compact](adr/ADR-0002-hybrid-context-masking-auto-compact.md) — observation masking + LLM summary + re-inject
- [ADR-0003: 砍掉 Bleve + FAISS，改为 embedding + SQLite 暴力检索](adr/ADR-0003-embedding-sqlite-replace-bleve-faiss.md) — zero CGO semantic search
- [ADR-0004: 砍掉 MCP server mode](adr/ADR-0004-remove-mcp-server-mode.md) — keep MCP client only
- [ADR-0005: SQLite schema 从 ~23 表精简到 ~8 表](adr/ADR-0005-sqlite-schema-simplify.md) — drop dead tables, clean cutover
- [ADR-0006: Release 简化为纯 Go build](adr/ADR-0006-pure-go-release.md) — no FAISS/CGO/build tags
