---
type: architecture_module
id: memorymodule
status: stable
path: internal/memorymodule
interfaces:
  - from: contextplane
    contract: "memorymodule.Search/Prepare"
  - to: store
    contract: "SQLite persistence"
  - to: skills
    contract: "ProcedureRecord for learned procedures"
owner_run: run_abc123
last_updated: 2026-05-26
---

# MemoryModule

## 职责

长期记忆与语义检索。管理 file-backed `facts/`、`skills/`、`history/` 和 Bleve+FAISS 语义检索索引。

## 核心组件

- Canonical Memory Record V2：frontmatter 承载 status/tags/created/updated、validity window、`source_run`、`source_refs`、`evidence_refs` 和 typed relations（`supports`、`derived_from`、`supersedes`、`contradicts`）
- `Search` / `Prepare`：语义检索接口
- `SearchExplain`：可选检索解释
- `acorn memory semantic rebuild`：显式重建索引命令

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认 semantic retrieval 配置是必需的 `memory.semantic`

## 硬约束（不可违反）

1. 长期记忆是 file-backed `facts/`、`skills/`、`history/`。
2. Canonical Memory Record V2 frontmatter 必须承载完整元数据。
3. Search、Prepare、list 和 semantic projection 默认按 active records 工作；inactive/retired 只能通过显式 include 参数查看。
4. Semantic retrieval 配置是必需的 `memory.semantic`：独立 OpenAI-compatible embedding base_url/model/api_key/dimensions/timeout/batch_size + Bleve path/index_name。
5. 不存在 `memory.semantic.enabled` 开关。
6. semantic `Search` / `Prepare` 失败时不能 fallback 到关键词搜索或 fake vectors。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
