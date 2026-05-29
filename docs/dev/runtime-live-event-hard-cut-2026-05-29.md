---
doc_type: implementation-plan
status: active
last_reviewed: 2026-05-29
slug: runtime-live-event-hard-cut-2026-05-29
---

# Runtime Live Event Hard Cut

## Problem

`/v1/runs/{run_id}/events` currently exposes too much runtime trace as the mobile live contract. The backend persists many useful diagnostic events, but OpenAPI, mobile stream validation, chat projection, run detail UI, and app projection tests all have to understand MCP lifecycle, sampling, skill lifecycle, procedure activation, memory preparation, context pressure, plan steps, subagents, and tool progress. Most of those events are hidden by the mobile chat projection, so the public contract carries complexity that the product surface does not use.

This is a product and architecture mismatch: Acorn is a single-user self-hosted backend with mobile as the current control surface, not a multi-tenant observability product or trace explorer.

## Best-Practice Basis

- Go package design should present responsibilities from the consumer's point of view and avoid vague catch-all API surfaces: https://go.dev/blog/package-names
- OpenAPI `oneOf` + discriminator should describe the real consumer contract, not every internal event shape that happens to be persisted: https://spec.openapis.org/oas/latest.html
- Observability traces/logs are diagnostic signals and should stay separate from the user workflow API unless the product explicitly exposes a diagnostic view: https://opentelemetry.io/docs/concepts/observability-primer/

## Current Live Findings

- SQLite `events` is still the correct durable runtime trace store.
- `internal/clientevents.ProjectRunEvent` is the over-wide boundary: it promotes internal trace kinds into `/v1` `RunEvent`.
- `mobile/lib/src/features/chat/chat_models.dart` uses only assistant, terminal, resume, elicitation, operator question, and decision-blocker events for the foreground chat path.
- `mobile/lib/src/api/run_event_stream.dart` validates many event types that the mobile UI suppresses.
- `after_seq` is a persisted event cursor. Filtering live events without advancing the cursor over filtered diagnostics would make follow mode repeatedly scan the same diagnostics.

## Target Contract

`/v1/runs/{run_id}/events` becomes a mobile live surface. It streams only:

- `run.started`
- `assistant.delta`
- `agent.message`
- `run.completed`
- `run.failed`
- `run.interrupted`
- `run.resume_requested`
- `elicitation.pending`
- `elicitation.decided`
- `operator_question.pending`
- `operator_question.decided`
- `decision_blocked`

Everything else remains persisted in SQLite for backend trace, RunDetail diagnostics, workbench projection, and architecture/debug use, but it is no longer part of the live mobile RunEvent contract.

## Phase Plan

## Progress

- 2026-05-29 Phase 1 completed: live RunEvent and diagnostic trace are separated across backend projection, `/v1` SSE, OpenAPI, generated mobile client, mobile stream validation, chat projection, RunDetail summary, tests, and docs.
- 2026-05-29 Phase 2 started and completed a narrow ownership cleanup: removed duplicated root-runtime and `runtime/api` tool argument validators, moved the single validator implementation into `internal/runtime/tool`, made it package-private, and deleted the duplicate root-runtime `JSONSerializer`. `internal/runtime/api` now only contains cross-runtime context/plan/serializer contracts.
- 2026-05-29 Phase 3 completed: `/v1` RunDetail no longer exposes raw unsupported diagnostic event payloads; mobile diagnostics read only live events plus trace/workbench summaries.

### Phase 1: Live Contract Hard Cut

- Add an explicit `clientevents.IsLiveRunEventKind` allowlist.
- Make `LoadRunEventsAfter` return a batch containing both live events and the latest scanned persisted cursor.
- Filter diagnostic events from SSE while advancing `after_seq` over them.
- Make `RunDetail` expose live events and trace/workbench summaries without promoting raw diagnostic payloads into the mobile contract.
- Shrink OpenAPI `RunEvent` oneOf/discriminator to the live set.
- Regenerate the mobile client and shrink `RunEventStreamClient` validation to the same live set.
- Update tests so removed trace kinds are rejected from `ProjectRunEvent` and mobile stream validation.

Acceptance:

- `go test ./internal/app ./internal/web`
- `python3 mobile/tool/generate_openapi_client.py --check`
- `flutter test` from `mobile/`
- `go test ./...`
- `make format-check`
- `make lint`
- `git diff --check`

### Phase 2: Runtime Package Ownership Cleanup

- Re-check `internal/runtime` file groups after Phase 1 reduces the public event surface.
- Keep orchestration/runtime/contextplane boundaries that have real ownership.
- Merge only mechanical wrappers or package splits whose names no longer describe a consumer-facing responsibility.
- Do not create a new generic `runtime/api` or `runtime/types` package.
- Move tool-only implementation out of shared runtime API surfaces when it has no non-tool consumer.

Acceptance:

- Smaller import graph with no new compatibility layers. Current result: `internal/runtime/api` is reduced to `api.go`, `plan.go`, and `serializer.go`; tool argument validation is owned by `internal/runtime/tool`.
- Existing runtime root modes and child-agent contracts unchanged.
- Targeted runtime/orchestration/contextplane tests plus full checks.

### Phase 3: Diagnostics View Boundary

- Collapse RunDetail to trace summary + workbench facts for mobile; do not expose raw diagnostic event payloads through `/v1`.
- Remove mobile UI affordances that count or label internal diagnostic event types.
- Keep backend trace data queryable through operator/dev tooling rather than default mobile UX.

Acceptance:

- Mobile UI does not need labels for internal trace kinds.
- Debug data remains explicit and non-default.

## Non-Goals

- Do not delete persisted runtime events.
- Do not remove tool result ledger, plan evidence, context boundaries, or trace summary.
- Do not add a compatibility `/api`, debug-only HTTP endpoint, fallback event parser, or silent mobile heuristic.
