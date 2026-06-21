# ADR-0003: 砍掉 Bleve + FAISS，改为 embedding + SQLite 暴力检索

Date: 2026-06-21
Status: Accepted
Supersedes: (none)

## Context

Acorn 原有 Bleve + FAISS 语义检索链：CGO + FAISS C 库 + `build-faiss-artifacts.sh` + 跨平台编译 + bleve_disabled fallback。这是整个项目最重的依赖链——release build 固定 `-tags "bleve_faiss vectors"` + FAISS libs，缺 artifact/CGO/build tags 显式失败。单用户几千条记录，暴力检索 <10ms。

## Decision

替换为方案 A：OpenAI embedding 调用 → 向量存 SQLite `memory_vectors` 表 BLOB 列 → 纯 Go 暴力余弦相似度检索。

- 复用现有 `embedder_openai.go` 的 HTTP 调用逻辑
- 新增 `vector_store_sqlite.go`：实现 `VectorStore` 接口（`Store` / `Search` / `Delete`）
- 零 CGO，零 build tags，零 cross-compile 复杂度
- rebuild：遍历 facts/ → 调 embedding API → 存 SQLite

删除：
- 11 个 bleve/faiss 文件
- `ProcedureRecord` / relation / evidence_refs 复杂类型
- `build-faiss-artifacts.sh` / `run-with-faiss-artifacts.sh` / `deploy/faiss.version`
- `bleve_faiss_release_guard_test.go`
- Makefile 的 `dev-faiss-*` targets

## Consequences

- **正面**：零 CGO 依赖，编译时间大幅降低，release 变为纯 Go build，跨平台零障碍
- **负面**：暴力检索 O(n) 复杂度——单用户几千条无问题，但不适合万级以上数据量
- **风险**：embedding API 依赖外部 provider——未配置时 Search 降级为 keyword matching（FTS5 fallback），零召回是合法 baseline

## Baseline Sync

- `go.mod` 已删除 bleve/faiss 依赖
- `CGO_ENABLED=0 go build` 通过
- `internal/memorymodule/` 已重写为 embedding + SQLite
- `docs/architecture/INVARIANTS.md` 已更新
