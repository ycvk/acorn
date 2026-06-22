---
doc_type: architecture
status: current
last_reviewed: 2026-06-21
slug: data-web-store
---

# Data, Remote Client, Store

## SQLite Persisted Truth

`internal/store/sqlite` stores runtime truth as the local SQLite adapter (~8 tables):

- sessions, messages, runs, events
- pending actions
- single-owner device auth records: owner profile, devices, and one-time pairing codes

Schema migrations drop legacy tables (plans, plan_evidence, plan_steps, tool_results, context_boundaries, conversation_segments, run_archives, working_checkpoints, provider_usage, run_decisions) if present in an older local database. There is no `acorn memory migrate` CLI path — old data is cleared on redeploy.

Cross-package store-facing records and sentinel errors live in `internal/store`, not in `internal/store/sqlite`. App/runtime/provider packages own the ports they consume:

- app services use narrow ports such as `clientStore`, `runResumeStore`, and purpose-specific service store ports.
- runtime uses `executorStore`, `runnerFactoryStore`, and `toolAuditStore`.
- MCP provider exports `TokenStore` and `PendingActionStore` as provider contracts.

Production code may directly import `internal/store/sqlite` only from the app composition root: `internal/app/container.go`. `tests/architecture/store_boundary_test.go` scans production Go imports and fails if sqlite is imported elsewhere.

## Remote Client Memory Surface

`/v1/memory/*` exposes file-backed facts and history from `internal/memorymodule`. Remote clients read memory through the API; they do not write memory files directly. The `remember` and `memory_create_file` tools are the agent-owned write paths.

## Semantic Vector Store

Embedding vectors are stored in the SQLite `memory_vectors` table (BLOB column). The vector store is rebuilt by scanning `facts/` and calling the embedding API. Zero CGO, zero Bleve, zero FAISS.
