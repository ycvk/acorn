---
doc_type: architecture
status: current
last_reviewed: 2026-05-15
slug: data-web-store
---

# Data, Remote Client, Store

## SQLite Persisted Truth

`internal/store/sqlite` stores runtime truth as the local SQLite adapter:

- sessions, messages, runs, events
- pending actions
- plans and plan evidence
- tool results with arguments, previews, full text, side-effect refs, and evidence backlinks
- conversation segments
- working checkpoints
- run archives and session summaries
- single-owner device auth records: owner profile, devices, and one-time pairing codes

SQLite does not keep active legacy memory stores, codeintel indexes, or filesystem rollback snapshots. The old core-memory, episodic-memory, knowledge-fact, knowledge-search, skill-patch-history, run-snapshot, and codeintel tables/readers have been removed; schema migrations drop those tables if they are present in an older local database. There is no `acorn memory migrate` CLI path.

Cross-package store-facing records and sentinel errors live in `internal/store`, not in `internal/store/sqlite`. App/runtime/provider packages own the ports they consume:

- app services use narrow ports such as `clientStore`, `traceStore`, and purpose-specific service store ports.
- runtime uses `executorStore`, `runnerFactoryStore`, `planRecordStore`, and `toolAuditStore`.
- MCP provider exports `TokenStore` and `PendingActionStore` as provider contracts.

Production code may directly import `internal/store/sqlite` only from the app composition root that opens and wires the adapter: `internal/app/container.go`. `internal/architecture/store_boundary_test.go` scans production Go imports and fails if sqlite is imported elsewhere.

## Remote Client Memory Surface

`internal/web` exposes file-backed memory resources:

- `GET /v1/memory/facts`
- `GET /v1/memory/skills`
- `GET /v1/memory/history`
- `GET /v1/memory/search`

These handlers call `app.MemoryService`, which wraps `memorymodule.Service`. DTOs are `MemoryRecord`, `MemoryRecordListResponse`, `MemorySearchItem`, and `MemorySearchResponse`.

Removed memory/evolution resources:

- `/v1/memory/core`
- `/v1/memory/candidates`
- `/v1/history/search`
- `/v1/reflections*`
- old memory candidate schemas
- old reflection schemas
- old KnowledgeFact/CoreMemoryBlock schemas

OpenAPI tests explicitly assert those removed paths and schemas do not return.

## Remote Client Device Auth

`internal/app.DeviceAuthService` owns the single-owner self-hosted auth rules. It generates pairing codes, hashes pairing/device secrets, pairs devices, authenticates bearer tokens, lists devices, and revokes devices through a narrow store port.

SQLite persists only non-secret auth facts:

- `owner_profile` with the single `owner` row
- `pairing_codes` with `code_hash`, `expires_at`, `used_at`, and `created_at`
- `devices` with `device_id`, name/platform metadata, `token_hash`, `created_at`, `last_seen_at`, and `revoked_at`

Active remote auth endpoints are:

- `POST /v1/devices:pair` without bearer auth, guarded by a valid one-time pairing code created by `acorn pair`
- `GET /v1/devices` with bearer auth
- `DELETE /v1/devices/{device_id}` with bearer auth

All other `/v1` routes are protected by `internal/web` device auth middleware. Missing, malformed, or unknown bearer tokens return `unauthenticated`; revoked device tokens return `device_revoked`. There is no local/dev fallback and no unauthenticated HTTP endpoint for creating pairing codes.

## Mobile Inbox Aggregate

`internal/app.InboxService` owns the current mobile inbox projection. It reads existing durable facts and does not create a second event or pending-action store:

- `pending_actions` from `ListPendingActions`, projected as `PendingActionSummary`
- active root/client runs from `runs` where `status=running`, `session_id` is present, and `parent_run_id` is empty
- recent terminal root/client runs where status is `succeeded`, `interrupted`, or `failed`
- system readiness from `CapabilitiesService.Snapshot`

`GET /v1/inbox` exposes that aggregate as `InboxResponse` under the same device bearer auth as other protected `/v1` resources. Run summaries include backend-projected `thread_title`, `preview`, `last_event_label`, `attention_level`, and `duration_ms` so mobile can render the owner cockpit without local run/thread heuristics. Pending action summaries include thread/run identifiers and accept/decline options for attention display; source list/detail/decide behavior is owned by the pending approval contract below.

## Pending Approval Source Contract

`internal/app.PendingActionService` is the approval source contract owner. It now exposes list, detail, and decide behavior over the same SQLite `pending_actions` rows:

- `GET /v1/pending-actions` returns `PendingActionListResponse` for actionable pending records.
- `GET /v1/pending-actions/{action_id}` returns `PendingActionDetail` with payload, reason/rule metadata, thread/run identifiers, and accept/decline options.
- `POST /v1/pending-actions/{action_id}:decide` remains the only mutation path.

Detail for a non-pending action returns `pending_action_already_decided`; missing rows return `pending_action_not_found`; invalid payload JSON or unsupported action kinds surface as projection failures. Clients must not reconstruct approval state from assistant prose or RunEvent timelines when the source row is absent.

## Remote Client Skills Boundary

Codeintel/repo-map is removed from the active backend. There is no `internal/codeintel` service, no codeintel SQLite index, no model-facing `repo_map` / symbol tools, and no remote client `/v1/codeintel/*` resource.

The remote client skills surface is read-only:

- `GET /v1/skills`
- `GET /v1/skills/{id}`
- `GET /v1/skills/{id}/files`

Skill create/patch/delete is not a remote client mutation path. Those operations remain explicit operator/CLI actions instead of OpenAPI resources.

Native skill creation and curation now run through runtime tools rather than Web mutations: `skill_create` writes generated skill packages, and `skill_assess` applies evidence-backed lifecycle frontmatter updates to mutable sources. The client skills surface still reads the resulting filesystem truth and lifecycle metadata.

## Client RunEvent Loop

`internal/app/client_service.go` and `internal/web/handlers_client.go` provide the `/v1` remote client loop:

- threads
- messages
- runs
- mobile live run-event stream
- pending-action list/detail/decisions
- mobile inbox aggregate
- run detail aggregate
- system/tools/settings
- checkpoint resources

`POST /v1/threads/{thread_id}/runs` accepts `CreateRunRequest` with optional `skill_id` and explicit public root `mode` (`direct_response`, `plan_execute`). The request mode is written into runtime orchestration truth before execution starts; `single_agent` is internal child-run execution mode and is not accepted as a public create-run request. The returned run still projects persisted internal/legacy `single_agent` runs through the client-facing `Run.mode` mapping as `agent`.

RunDetail is intentionally narrow. It returns run/thread facts, the mobile live event subset, backend trace summary, and run artifacts loaded through the client service artifact store port. It does not expose runtime workbench, plan DTOs, raw diagnostic event payloads, mutation checkpoint summaries, rollback summaries, provider usage, git status, or context-economy internals.

RunEvent projection is strict and intentionally narrow. `/v1/runs/{run_id}/events` emits only the mobile live subset: run lifecycle, assistant deltas/messages, terminal status, resume requests, elicitation/operator-question events, and `decision_blocked`. Runtime trace events such as tool progress, skill/procedure lifecycle, memory preparation, context pressure/compression, plan/step updates, subagents, MCP lifecycle, and sampling remain persisted diagnostics in SQLite and aggregate into backend trace summaries; they are not part of the live mobile contract or raw `/v1` RunDetail payload.

`after_seq` is the exclusive persisted cursor over SQLite `events.sequence`. Because the endpoint filters diagnostics, the server may advance the returned polling cursor across persisted events it did not emit. Clients persist `{run_id,last_seq}`, receive backlog before foreground follow events, and do not maintain a second run-event truth store.

`memory.prepared` is runtime trace truth for prepared file-backed memory. The old `memory.lens` client projection and run-detail `memory_trace` aggregate are removed.

## Client Data Boundary

Mobile generated types come from `docs/openapi.yaml`:

```bash
python3 mobile/tool/generate_openapi_client.py
```

The Flutter mobile client generates its Dart API/model layer from the OpenAPI contract rather than hand-writing parallel DTOs. Mobile projection code must consume only the file-backed memory endpoints listed above and must not call removed core/candidate/reflection/history-search resources or reconstruct memory state from raw events.

`GET /v1/system/status` and the `system` field inside `GET /v1/inbox` are also part of that client data boundary. The public payload carries typed `runtime_readiness` and `provider_readiness`; legacy `execution_ready` / `execution_error` fields have been removed. Mobile DTOs and generated client types project these fields directly from `CapabilitiesService.Snapshot`; client code must not reinvent runtime-vs-provider semantics from prose, message text, or local heuristics.

## Notification Wake-up Truth

SQLite now persists mobile wake-up state:

- `device_push_tokens`: authenticated device-owned APNs/FCM token registration. `token_value` is backend-private and never returned by `/v1`.
- `notifications`: durable wake-up facts such as `pending_action`.
- `notification_deliveries`: per-device delivery attempts with `pending`, `sent`, `failed`, or `not_configured`.

`internal/app.NotificationService` owns token registration and pending-action wake-up creation. `internal/app` also owns the APNs/FCM dispatcher port; when no concrete dispatcher is configured, delivery rows are updated to `not_configured` instead of pretending push succeeded.

MCP elicitation pending actions are wired through a notification-aware pending action store wrapper passed via `runtime.RunnerFactoryOptions.MCPPendingActionStore`. Runtime still does not know APNs/FCM details; it only receives the same pending-action store contract.

## Store Boundary

Store code persists records and performs schema migrations. It does not decide memory admission, assemble prompts, choose skills, or hide corrupted rows behind empty results. Runtime/app/web services own business projection through their own ports.

`internal/store` is allowed to hold shared persistence records where multiple consumers need the same durable shape, such as plan records, OAuth tokens, pending-action input, and store sentinel errors. The sqlite package aliases or implements these records; it no longer owns them as cross-package API.

Procedure learning and memory admission are owned by `internal/memorymodule` and its file-backed records. Runtime no longer owns a separate auto-crystallization pipeline or SQLite insight-index adapter; any learned procedure must enter through the active memory/skill lifecycle contracts instead of a second store path.
