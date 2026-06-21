# Aegis Workspace Index

## Specs
- [2026-06-21 Acorn 全面重构设计](specs/2026-06-21-acorn-refactor-design.md) — backend + frontend architecture refactor, scope: full

## Plans
- [Phase 1: 后端基础层清理](plans/2026-06-21-phase1-baseline-layer-cleanup.md) — config/store/events/tooling schema simplification
- [Phase 2: Runtime + Orchestration 清理](plans/2026-06-21-phase2-runtime-orchestration-cleanup.md) — delete plan_execute/single_agent, keep direct_response only
- [Phase 3: ContextPlane 清理 + Hybrid Context](plans/2026-06-21-phase3-contextplane-hybrid-context.md) — masking + auto-compact + re-inject
- [Phase 4: MemoryModule 清理 + Embedding+SQLite](plans/2026-06-21-phase4-memorymodule-embedding-sqlite.md) — replace bleve/faiss with embedding+sqlite
- [Phase 5: App + Web + OpenAPI 清理](plans/2026-06-21-phase5-app-web-openapi-cleanup.md) — remove mcp server/plan/skill-lifecycle API
- [Phase 6: Flutter 重写 + Release 简化](plans/2026-06-21-phase6-flutter-rewrite-release.md) — modular flutter + pure go release
