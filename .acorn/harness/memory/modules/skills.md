---
type: architecture_module
id: skills
status: stable
path: internal/skills
interfaces:
  - from: runtime
    contract: "skill_create, skill_assess lifecycle events"
  - to: memorymodule
    contract: "ProcedureRecord for learned procedures"
  - to: store
    contract: "SQLite skill lifecycle persistence"
owner_run: run_abc123
last_updated: 2026-05-26
---

# Skills

## 职责

Native skill 生命周期管理。file-backed loader 管理 repo `./skills`（release seed pack）、`~/.acorn/skills`（installed）、`{runtime.storage_dir}/skills/generated`（generated）、`./.acorn/skills/workspace`（workspace）。

## 核心组件

- `internal/skills` file-backed loader
- `skill_create` / `skill_assess`：active runtime lifecycle action
- `skill.lifecycle` RunEvent：visibility truth
- skill pack governance：dry-run、dependency closure、receipt/hash

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认 `skill_assess` 是唯一 active runtime lifecycle action

## 硬约束（不可违反）

1. repo `./skills` 是 Acorn release seed pack 源和本地开发 builtin source。
2. 新 skill 默认由 `skill.creator` + `skill_create` 生成到 `{runtime.storage_dir}/skills/generated`，或显式写到 `./.acorn/skills/workspace`。
3. 不要把 generated skill 写回 repo root `skills/`。
4. `lifecycle_status: verified` 对非 builtin skill 必须有 `evidence_refs`。
5. 没有 evidence 的结果只能是 `draft`、`unverified` 或 `needs_eval`。
6. `skill_assess` 是唯一 active runtime lifecycle action；不要再恢复 `skill_eval` / `skill_curate`。
7. executable native skills 归 `internal/skills`；learned procedures 归 `internal/memorymodule/skills` 的 `ProcedureRecord`。
8. skill health / routing fixture / skill pack governance 是 deterministic 检查，不是 lifecycle promotion。
9. `skill.lifecycle` 是 RunEvent visibility truth；OpenAPI/mobile generated client 和 mobile projection 必须同步。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
