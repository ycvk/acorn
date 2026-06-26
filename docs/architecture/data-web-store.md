---
doc_type: architecture
status: current
last_reviewed: 2026-06-26
slug: data-web-store
---

# Data, Remote Client, Store

## SQLite Persisted Truth

`internal/store` stores runtime truth as the local SQLite adapter (10 required tables):

- sessions, session_messages, runs, events
- pending_actions, artifacts, schema_migrations
- single-owner device auth records: devices, pairing_codes, and mcp_oauth_tokens

Schema migrations drop legacy tables (plans, plan_evidence, plan_steps, tool_results, context_boundaries, conversation_segments, run_archives, working_checkpoints, provider_usage, run_decisions) if present in an older local database. There is no `acorn memory migrate` CLI path — old data is cleared on redeploy.

Cross-package store-facing records and sentinel errors live in `internal/core`. App/runtime/provider packages own the ports they consume:

- app services use `core.SessionStore` and `core.IdentityStore` directly; the former narrow per-service store interfaces (threadStore, runStore, etc.) have been eliminated.
- runtime uses `core.SessionStore`, `core.ArtifactStore`, and runtime-owned seams.
- MCP provider exports `TokenStore` and `PendingActionStore` as provider contracts.

Production code may directly import `internal/store` only from the app composition root: `internal/wire/container.go`.

## Remote Client Memory Surface

`/v1/memory/*` exposes file-backed facts and history from `internal/memory`. Remote clients read memory through the API; they do not write memory files directly. The `remember` and `memory_create_file` tools are the agent-owned write paths.

