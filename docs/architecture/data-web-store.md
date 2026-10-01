---
doc_type: architecture
status: current
last_reviewed: 2026-10-01
slug: data-web-store
---

# Data, Remote Client, Store

## SQLite Persisted Truth

`internal/store` stores runtime truth as the local SQLite adapter (modernc.org/sqlite, single connection) with 10 required tables:

- sessions, session_messages, runs, events
- pending_actions, artifacts, schema_migrations
- single-owner device auth records: devices, pairing_codes, and mcp_oauth_tokens

The schema lives in `store_schema_bootstrap.go`; `schemaRequiredTables` lists the columns each table must have, and opening a database that lacks one fails loudly.

Cross-package store-facing records and sentinel errors live in `internal/core`. Consumers depend on the core ports:

- app services use `core.SessionStore` and `core.IdentityStore`.
- runtime uses `core.SessionStore`, `core.ArtifactStore`, and runtime-owned seams.
- the MCP manager uses `core.ArtifactStore` for OAuth tokens and `core.SessionStore` for sampling/elicitation pending actions.

Production code may directly import `internal/store` only from the composition root: `internal/wire/container.go`.

## Remote Client Memory Surface

`/v1/memory/*` exposes file-backed facts and history from `internal/memory`. Remote clients read memory through the API; they do not write memory files directly. The `remember` and `memory_create_file` tools are the agent-owned write paths.
