---
type: architecture_decision
id: dec_001
topic: semantic retrieval store
decision: "only bleve+faiss, no pgvector/lancedb fallback"
status: active
rationale: "Bleve+FAISS 只作为可重建 retrieval index，不是 SQLite persisted truth。引入第二套 store 会增加同步复杂度和运维成本。"
supersedes: []
created: 2026-05-26
last_updated: 2026-05-26
---

# Decision: Semantic Retrieval Store

## 结论

Acorn 的语义检索只使用 Bleve + FAISS。不允许引入 pgvector、PGLite、LanceDB 或任何其他向量存储作为 fallback 或并行方案。

## 上下文

- `internal/memorymodule` 的 file-backed `facts/`、`skills/`、`history/` 是 L0 记忆真相（Canonical Memory Record V2）。
- Bleve+FAISS 是从这些 V2 records 重建的可重建索引。
- SQLite 只保存 `context_boundaries`、runs/events/pending_actions 等运行时事实。

## 约束

1. Release 打包固定包含 Bleve+FAISS。
2. FAISS native artifact、CGO toolchain、`bleve_faiss vectors` build tags 或 packaged shared libs 缺失必须显式失败。
3. 不能回退普通 non-FAISS 包。
4. `acorn memory semantic rebuild` 显式从 canonical records 重建。
5. semantic `Search` / `Prepare` 失败时不能 fallback 到关键词搜索或 fake vectors。

## 影响模块

- `internal/memorymodule`
- `scripts/build-release.sh`
- `.github/workflows/release.yml`
