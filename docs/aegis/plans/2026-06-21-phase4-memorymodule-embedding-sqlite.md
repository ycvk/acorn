# Acorn 重构 - Phase 4: MemoryModule 清理 + Embedding+SQLite 检索

Date: 2026-06-21
Parent Spec: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md`
Depends on: Phase 1 + 2 + 3

## Goal

砍掉 Bleve + FAISS + CGO 依赖。替换为 OpenAI embedding + SQLite BLOB 向量存储 + 纯 Go 暴力余弦相似度检索。简化 memory records(去掉 procedure 双轨制 / relation / lifecycle)。

## Architecture

```text
本阶段范围:
  memorymodule/
    删除: bleve_index*.go / semantic_search.go / semantic_rebuild.go / bleve_disabled.go
    删除: procedure_learning.go / mutation_plan.go / mutation_apply.go(如简化)
    删除: selection.go(简化 active selection)
    重写: semantic.go — embedding 调用 + SQLite 向量存储
    重写: search.go — embedding + FTS5 fallback
    重写: types.go — 删除 ProcedureRecord / relation 类型
    重写: frontmatter.go — 简化 Record V2 frontmatter
    重写: service.go / prepare.go — 适配新检索
    保留: fact_learning.go / history.go / embedder_openai.go / instruction.go / config.go

新增:
  memorymodule/
    vector_store_sqlite.go — VectorStore 接口 + SQLite 实现
```

## Baseline / Authority Refs

- Spec §3.7 记忆系统简化
- `internal/memorymodule/semantic.go` — 当前 Bleve+FAISS 实现(删除)
- `internal/memorymodule/embedder_openai.go` — 当前 OpenAI embedding HTTP 调用(保留)

## Compatibility Boundary

- `memorymodule.Service` 接口签名不变(`Prepare` / `Search` / `AppendHistory` / `BuildMemoryInstruction`)
- `memorymodule.LocalService` 构造函数参数变化(去掉 Bleve/FAISS 依赖)
- SQLite `memory_vectors` 表在 Phase 1 已创建(或本阶段创建)
- file-backed memory 目录结构不变:`facts/` + `history/`
- `remember` / `memory_search` 工具接口不变

## Verification

- `go build ./internal/memorymodule && go test ./internal/memorymodule` 通过
- `go build ./...` 通过(app/web 在 Phase 5 修复)
- 无 CGO 依赖:`go build -tags "" ./...` 通过

---

## Task 1: 删除 Bleve + FAISS 文件

**Files:**
- Delete: `internal/memorymodule/bleve_index.go`
- Delete: `internal/memorymodule/bleve_index_rebuild.go`
- Delete: `internal/memorymodule/bleve_index_search.go`
- Delete: `internal/memorymodule/bleve_index_test.go`
- Delete: `internal/memorymodule/bleve_disabled.go`
- Delete: `internal/memorymodule/semantic_search.go`
- Delete: `internal/memorymodule/semantic_search_test.go`
- Delete: `internal/memorymodule/semantic_rebuild.go`
- Delete: `internal/memorymodule/semantic_rebuild_test.go`
- Delete: `internal/memorymodule/semantic_test.go`
- Delete: `internal/memorymodule/index.go`(如果是 Bleve index 实现)

**Why:** 这些文件全部依赖 Bleve 或 FAISS。

**Verification:** `go build ./internal/memorymodule`(会有编译错误,Task 2-4 修复)

### Steps

- [ ] **1.1 删除上述所有文件**
- [ ] **1.2 Commit**:`refactor(memorymodule): delete bleve/faiss files`

---

## Task 2: 简化 types.go + frontmatter.go

**Files:**
- Modify: `internal/memorymodule/types.go`
- Modify: `internal/memorymodule/frontmatter.go`
- Modify: `internal/memorymodule/config.go`
- Test: `internal/memorymodule/fact_learning_test.go`

**Why:** 删除 ProcedureRecord / relation / lifecycle 类型。简化 Record V2 frontmatter。

**Impact/Compatibility:** `types.go` 删除:`ProcedureRecord`、`RelationType` 及其常量、`ProcedureActivation`、`SearchExplain`(或简化)。`Record` 结构体简化:删除 `EvidenceRefs`、`Relations`、`Validity` 字段。保留:`Status`、`Tags`、`Created`、`Updated`、`SourceRun`、`SourceRefs`。`frontmatter.go` 简化:解析 status/tags/created/updated/source_run/source_refs。删除 relation / procedure origin 解析。`config.go` 简化:删除 bleve 配置。

**Verification:** `go build ./internal/memorymodule`

### Steps

- [ ] **2.1 重写 `types.go`**:`Record` 删除 `EvidenceRefs`、`Relations`、`Validity`。删除 `ProcedureRecord`、`RelationType` 常量、`ProcedureActivation`。保留 `Kind`(fact/history)、`SearchItem`、`PrepareResult`、`MemoryIndex`、`SkillTreeIndex`(如果需要)。
- [ ] **2.2 重写 `frontmatter.go`**:解析只保留:status、tags、created、updated、source_run、source_refs。删除 relation / procedure origin / evidence_refs 解析。未知 frontmatter key 返回 error(fail-loud 保留)。
- [ ] **2.3 重写 `config.go`**:删除 `Bleve` 配置字段。只保留 embedding 配置。
- [ ] **2.4 更新 `fact_learning_test.go`**:匹配简化后的 Record frontmatter。
- [ ] **2.5 运行验证**:`go build ./internal/memorymodule`(会有错误,Task 3-4 修复)
- [ ] **2.6 Commit**:`refactor(memorymodule): simplify types and frontmatter - remove procedure/relation`

---

## Task 3: 新建 vector_store_sqlite.go + 重写 semantic.go

**Files:**
- Create: `internal/memorymodule/vector_store_sqlite.go`
- Modify: `internal/memorymodule/semantic.go`
- Modify: `internal/memorymodule/embedder_openai.go`(保留,可能微调)
- Test: `internal/memorymodule/vector_store_sqlite_test.go`

**Why:** 实现 spec §3.7 方案 A:OpenAI embedding → SQLite BLOB → 纯 Go 暴力余弦相似度。

**Impact/Compatibility:** 新增 `VectorStore` 接口和 SQLite 实现。`semantic.go` 重写:不再有 `SemanticIndex` Bleve 接口,改为 `VectorStore` + embedding 调用。`embedder_openai.go` 保留(复用现有 HTTP 调用逻辑)。

**Verification:** `go test ./internal/memorymodule -run TestVectorStore`

### Steps

- [ ] **3.1 写 `vector_store_sqlite.go`**:
  ```go
  package memorymodule

  import (
      "context"
      "database/sql"
      "encoding/binary"
      "fmt"
      "math"
      "strings"
  )

  // VectorStore stores and retrieves embedding vectors in SQLite.
  type VectorStore interface {
      Store(ctx context.Context, ref string, kind Kind, contentHash string, vector []float32, model string, dimensions int) error
      Search(ctx context.Context, queryVector []float32, limit int) ([]VectorSearchResult, error)
      Delete(ctx context.Context, ref string) error
      Rebuild(ctx context.Context, records []VectorRecord) error
  }

  type VectorSearchResult struct {
      Ref     string
      Kind    Kind
      Score   float64
  }

  type VectorRecord struct {
      Ref         string
      Kind        Kind
      ContentHash string
      Vector      []float32
      Model       string
      Dimensions  int
  }

  type sqliteVectorStore struct {
      db *sql.DB
  }

  func NewSQLiteVectorStore(db *sql.DB) VectorStore {
      return &sqliteVectorStore{db: db}
  }

  func (s *sqliteVectorStore) Store(ctx context.Context, ref string, kind Kind, contentHash string, vector []float32, model string, dimensions int) error {
      blob := floatsToBlob(vector)
      _, err := s.db.ExecContext(ctx,
          `INSERT INTO memory_vectors (ref, kind, content_hash, vector_blob, model, dimensions, created_at)
           VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
           ON CONFLICT(ref) DO UPDATE SET content_hash=excluded.content_hash, vector_blob=excluded.vector_blob, model=excluded.model, dimensions=excluded.dimensions`,
          ref, string(kind), contentHash, blob, model, dimensions)
      return err
  }

  func (s *sqliteVectorStore) Search(ctx context.Context, queryVector []float32, limit int) ([]VectorSearchResult, error) {
      rows, err := s.db.QueryContext(ctx,
          `SELECT ref, kind, vector_blob FROM memory_vectors ORDER BY created_at DESC`)
      if err != nil {
          return nil, err
      }
      defer rows.Close()

      var results []VectorSearchResult
      for rows.Next() {
          var ref, kindStr string
          var blob []byte
          if err := rows.Scan(&ref, &kindStr, &blob); err != nil {
              return nil, err
          }
          vec := blobToFloats(blob)
          score := cosineSimilarity(queryVector, vec)
          results = append(results, VectorSearchResult{Ref: ref, Kind: Kind(kindStr), Score: score})
      }
      return rows.Err()
  }

  // cosineSimilarity computes pure-Go cosine similarity. For a few thousand
  // records this is <10ms.
  func cosineSimilarity(a, b []float32) float64 {
      var dot, normA, normB float64
      n := len(a)
      if len(b) < n { n = len(b) }
      for i := 0; i < n; i++ {
          dot += float64(a[i]) * float64(b[i])
          normA += float64(a[i]) * float64(a[i])
          normB += float64(b[i]) * float64(b[i])
      }
      if normA == 0 || normB == 0 {
          return 0
      }
      return dot / (math.Sqrt(normA) * math.Sqrt(normB))
  }

  func floatsToBlob(v []float32) []byte {
      buf := make([]byte, 4*len(v))
      for i, f := range v {
          binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
      }
      return buf
  }

  func blobToFloats(b []byte) []float32 {
      n := len(b) / 4
      v := make([]float32, n)
      for i := 0; i < n; i++ {
          v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
      }
      return v
  }
  ```
  (注意:实际实现需要修正 `Search` 的 return nil, err 语法错误和 rows.Err() 的正确使用)
- [ ] **3.2 重写 `semantic.go`**:`SemanticService` 改为组合 `embedder` + `VectorStore`。`Search` 逻辑:embed query → `vectorStore.Search` → 返回 results。`Rebuild` 逻辑:遍历 facts/ → embed each → `vectorStore.Rebuild`。删除所有 Bleve/FAISS 引用。删除 `ErrBleveFAISSSupportNotBuilt`。
- [ ] **3.3 审查 `embedder_openai.go`**:保留。如果接口不匹配新 `VectorStore`,微调。
- [ ] **3.4 写 `vector_store_sqlite_test.go`**:测试:1) Store + Search; 2) Delete; 3) Rebuild; 4) cosineSimilarity 正确性; 5) 空向量处理。
- [ ] **3.5 确认 SQLite `memory_vectors` 表 schema**:如果 Phase 1 没创建,在 `store_schema_bootstrap.go` 添加:
  ```sql
  CREATE TABLE IF NOT EXISTS memory_vectors (
      ref TEXT PRIMARY KEY,
      kind TEXT NOT NULL,
      content_hash TEXT NOT NULL,
      vector_blob BLOB NOT NULL,
      model TEXT NOT NULL,
      dimensions INTEGER NOT NULL,
      created_at TEXT NOT NULL DEFAULT (datetime('now'))
  );
  ```
- [ ] **3.6 运行验证**:`go build ./internal/memorymodule && go test ./internal/memorymodule -run TestVectorStore`。
- [ ] **3.7 Commit**:`feat(memorymodule): replace bleve/faiss with embedding+sqlite vector store`

---

## Task 4: 重写 search.go + prepare.go + service.go

**Files:**
- Modify: `internal/memorymodule/search.go`
- Modify: `internal/memorymodule/prepare.go`
- Modify: `internal/memorymodule/index_service.go`
- Modify: `internal/memorymodule/fact_learning.go`
- Modify: `internal/memorymodule/history.go`
- Modify: `internal/memorymodule/instruction.go`
- Modify: `internal/memorymodule/util.go`
- Test: `internal/memorymodule/service_test.go`
- Test: `internal/memorymodule/prepare_degrade_test.go`

**Why:** 适配新检索方案。删除 procedure learning / mutation plan / mutation apply。

**Impact/Compatibility:** `search.go`:`Search` 逻辑:如果 embedding 配置 → embed query → `vectorStore.Search` → 返回;否则 fallback 到简单关键词匹配(或返回空)。`prepare.go`:`Prepare` 逻辑:扫描 facts/ → 按 tag/keyword 匹配 → 注入 model context。删除 procedure activation 逻辑。`index_service.go`:`BuildIndex` 简化:只索引 facts/ + history/。删除 relation boost / source_ref_backlink。`fact_learning.go`:`CreateFact` 简化:写入 facts/ + 更新 embedding 向量(如果配置)。删除 relation / evidence_refs。`history.go`:`AppendHistory` 简化:追加 Record V2 history 事件。删除 relation。`instruction.go`:`BuildMemoryInstruction` 保留。`util.go`:保留辅助函数。

**Verification:** `go build ./internal/memorymodule && go test ./internal/memorymodule`

### Steps

- [ ] **4.1 重写 `search.go`**:`Search` 方法:embedding search(如果配置) + 关键词 fallback。删除 `SearchExplain`(或简化为不返回 explain)。删除 `applySourceRefBoost` / `applyRelationBoost`。
- [ ] **4.2 重写 `prepare.go`**:`Prepare` 简化:扫描 facts/ → 按 tag/keyword 匹配 → 返回 `PrepareResult`。删除 procedure activation。
- [ ] **4.3 重写 `index_service.go`**:`BuildIndex` 简化:索引 facts/ + history/。删除 `applySourceRefBoost` / `applyRelationBoost` / `applySourceRefRecord` / `applyRelationRecord` / `relationBoost`。保留 `GetRecordByRef` / `findRecordInDir`。
- [ ] **4.4 重写 `fact_learning.go`**:`CreateFact` 简化:写入 facts/ + 更新 embedding(如果配置)。删除 relation / evidence_refs 处理。
- [ ] **4.5 审查 `history.go`**:`AppendHistory` 适配简化后的 Record V2。删除 relation 处理。
- [ ] **4.6 删除 `procedure_learning.go`、`mutation_plan.go`、`mutation_apply.go`、`selection.go`**。
- [ ] **4.7 更新 `service_test.go`**:删除 relation / procedure / bleve 测试。保留 embedding + keyword search 测试。
- [ ] **4.8 更新 `prepare_degrade_test.go`**:适配新降级逻辑(embedding 未配置 → 关键词 fallback 或空)。
- [ ] **4.9 运行验证**:`go build ./internal/memorymodule && go test ./internal/memorymodule`。修复编译错误直到通过。
- [ ] **4.10 Commit**:`refactor(memorymodule): rewrite search/prepare for embedding+sqlite, remove procedure`

---

## Task 5: 删除 go.mod 中 bleve/faiss 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Why:** 代码不再引用 bleve/faiss。清理依赖。

**Verification:** `go build ./...` 通过且无 CGO

### Steps

- [ ] **5.1 运行**:`go mod tidy`。确认 bleve/faiss 依赖被移除。
- [ ] **5.2 检查**:`grep -E "bleve|faiss" go.mod`。应该为空。
- [ ] **5.3 检查 CGO**:`go build -tags "" ./...`。应该通过(无 CGO 依赖)。
- [ ] **5.4 Commit**:`chore: remove bleve/faiss dependencies from go.mod`

---

## Task 6: 全量编译检查

### Steps

- [ ] **6.1 运行**:`go build ./internal/memorymodule ./internal/contextplane ./internal/runtime ./internal/orchestration`。必须通过。
- [ ] **6.2 运行**:`go test ./internal/memorymodule ./internal/contextplane ./internal/runtime ./internal/orchestration`。必须通过。
- [ ] **6.3 运行**:`go build ./... 2>&1 | head -50`。记录 app/web 的编译错误(Phase 5 修复)。
- [ ] **6.4 验证无 CGO**:`CGO_ENABLED=0 go build ./...`。应该通过。
- [ ] **6.5 Commit**:`chore: phase 4 memorymodule cleanup + embedding+sqlite complete`
