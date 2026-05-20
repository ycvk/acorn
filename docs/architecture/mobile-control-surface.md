---
doc_type: architecture
status: current
last_reviewed: 2026-05-20
slug: mobile-control-surface
---

# Mobile Control Surface

## Current Shape

The mobile client lives in `mobile/` and is a Flutter app for single-user self-hosted Acorn. It is a product-grade remote control surface over authenticated `/v1`; it does not execute runs locally, does not own runtime truth, and does not maintain a second message lifecycle.

Current entrypoints:

```text
mobile/lib/main.dart
  -> AcornApp
  -> ProviderScope
  -> ConnectionController
  -> InboxController / ThreadsController / ApprovalsController
  -> ChatController
  -> generated AcornApiClient
  -> RunEventStreamClient
  -> authenticated /v1 backend
```

The generated API/model file is:

```text
mobile/lib/src/api/acorn_api.dart
```

It is produced by:

```bash
python3 mobile/tool/generate_openapi_client.py
```

`--check` is the contract drift check and compares the generated Dart file against `docs/openapi.yaml`.

Current mobile shell structure:

```text
mobile/lib/app.dart

mobile/lib/src/core/
  connection_controller.dart
  connection_profile.dart
  connection_store.dart
  providers.dart

mobile/lib/src/api/
  acorn_api.dart
  run_event_stream.dart

mobile/lib/src/features/
  pairing/
  shell/
  home/
  inbox/
  chat/
  threads/
  runs/
  approvals/
  settings/

mobile/lib/src/ui/
  theme/
  widgets/
```

`ConnectionController` owns connection lifecycle, the active API/RunEvent clients, pairing, and disconnect. `ShellController` owns tab selection. Feature controllers own feature state: `InboxController` owns `/v1/inbox`, `ThreadsController` owns thread list/active thread, `ApprovalsController` owns pending-action detail/decision commands, `ChatController` owns chat message loading, send/run start, foreground RunEvent streaming projection, and chat-local errors, and `RunDetailController` owns per-run detail loading/cache/error state. `mobile/lib/src/ui/` owns reusable theme and widgets for status pills, empty states, section headers, list rows, and chat rendering. Feature files own backend-backed screens only.

The current state split keeps streaming assistant deltas inside `ChatController`; Home, Threads, Approvals, Settings, and shell navigation do not rebuild for every streamed token. Inbox refreshes notify only inbox consumers, thread list mutations notify only thread consumers, pending-action decisions notify the approval sheet plus the inbox refresh they trigger, and run-detail refreshes notify only consumers of the affected `runId`.

## Material 3 UI System

The Flutter mobile shell uses Material 3 as the active UI system. `mobile/lib/src/ui/theme/acorn_theme.dart` owns `ThemeData(useMaterial3: true)`, `ColorScheme.fromSeed`, component theme defaults, spacing/radius constants, and semantic status color tokens. The app uses system/Material typography through `ThemeData.textTheme` and keeps status vocabulary centralized as success, warning, info, neutral, and error.

Reusable presentation widgets live under `mobile/lib/src/ui/widgets/`:

```text
acorn_surfaces.dart   # Material-backed tonal surfaces, bottom surface, tonal icon, scanner instruction surface
acorn_status.dart     # StatusDot, StatusPill, InlineStatusLabel, ErrorBanner
empty_state.dart      # shared empty state
list_rows.dart        # shared Material list row
message_widgets.dart  # chat bubble, activity row, run status footer, Thinking section, typing indicator
```

Feature screens consume these widgets and backend facts. They should not define their own status palette, invent a second visual primitive layer, or infer backend state from local UI state.

## Connection Boundary

`PairingScreen` asks for server URL, pairing code, and device name. It can also open an in-app camera QR scanner and import the existing Acorn pairing payload into the server URL and pairing code fields. Pairing still calls `POST /v1/devices:pair` through the generated client. On success, the app stores only the explicit connection profile:

```text
server_url
device_id
access_token
```

`SecureConnectionStore` persists that profile through `flutter_secure_storage`. `MemoryConnectionStore` exists for tests only. The app never falls back to unauthenticated `/v1`, local dev bypass, or Web local state.

`ConnectionController` owns one active `AcornApiClient` and one active `RunEventStreamClient` per connection profile and closes both on disconnect/dispose. Pairing uses a temporary unauthenticated client and closes it after the exchange.

Self-hosted onboarding generates pairing payloads through the operator CLI:

```bash
acorn pair -c /config/acorn.yaml --server-url https://acorn.example.com --qr
```

The QR payload contains `server_url`, `pairing_code`, and `expires_at`. The mobile scanner parses this payload, fills the connect fields, and leaves final pairing confirmation to the user. Manual server URL and pairing code entry remains available.

## Surfaces

Current mobile surfaces:

- Connect: scan the Acorn pairing QR or enter server URL / pairing code manually, pair a device, and persist the connection profile.
- Home: primary post-pairing attention surface over `/v1/inbox`, ordered by pending approvals, active runs, recent terminal runs, and system readiness.
- Chat: thread detail surface for backend message send, run start, live assistant streaming, backend-provided reasoning display, assistant Markdown rendering, and exceptional blocking activity rows.
- Threads: list/create/delete backend threads and open them in Chat as a pushed detail route.
- Run Inspector: run detail surface over `GET /v1/runs/{run_id}/detail`, projecting run summary, persisted RunEvents, artifacts, and terminal sessions.
- Approvals: list pending backend actions from the inbox aggregate and open the existing approval detail flow.
- Run stream: read `GET /v1/runs/{run_id}/events?after_seq=0&follow=true` and project persisted RunEvent SSE into the active assistant bubble.
- Pending approval: read `GET /v1/pending-actions/{action_id}` and decide through `POST /v1/pending-actions/{action_id}:decide`.
- Settings: display connected server, device ID, backend model projection, workspace projection, and disconnect.

The connected shell uses four bottom destinations: Home, Threads, Approvals, and Settings. Chat is not a global tab; it is opened from a selected/new thread. Run Inspector is opened from Home run rows and other run-linked surfaces. Each destination reloads backend truth through its feature controller; the UI does not infer run state, approval state, thread state, or readiness from local screen state.

Home run rows render the backend-owned `RunSummary` projection from `/v1/inbox`: `thread_title`, `preview`, `last_event_label`, `attention_level`, and `duration_ms` are produced by `internal/app.InboxService`, declared in `docs/openapi.yaml`, and consumed through the generated Dart client. The mobile app does not generate thread titles, summarize runs, or classify run attention from local messages.

## Live RunEvent Streaming

`RunEventStreamClient` is hand-written because the generated OpenAPI Dart client does not model streaming `text/event-stream` transport. It is still constrained by the OpenAPI RunEvent schema and validates the live SSE envelope:

```text
id: <RunEvent.event_id>
event: <RunEvent.type>
data: <full RunEvent JSON>
```

The mobile streaming projection is:

- `assistant.delta` appends `data.assistant_delta.delta` to the live assistant bubble and appends `data.assistant_delta.reasoning` to the same assistant item's separate reasoning field.
- `agent.message` and `run.completed` replace/finalize assistant text when `data.message.content` exists and replace reasoning when `data.message.reasoning` exists.
- `run.failed` and `run.interrupted` finalize the assistant bubble with explicit status.
- Routine tool, memory, skill, procedure, plan, context compression, and subagent events stay out of the chat transcript. Run failures, interruptions, input requests, decision blockers, and non-normal context pressure may render as compact activity rows because they affect the next owner action. Full runtime event history belongs in Run Inspector.
- Malformed JSON, SSE id/type mismatch, unsupported event type, or wrong run id throws a visible client error.
- Assistant message text renders GitHub-flavored Markdown through `flutter_markdown_plus`; code blocks expose a copy action, long assistant messages use a bounded internal Markdown viewport, `http` and `https` links open through `url_launcher`, and user messages remain plain text.
- Backend-provided reasoning renders only in a collapsed Material Thinking section on assistant messages. The client does not infer reasoning from prose, token counts, or local state.
- Persisted thread reloads consume generated `Message.contentParts`; `kind: reasoning` parts become the same assistant reasoning field.

This is a foreground follow surface only. Backend persisted RunEvent and message state remain the durable facts.

## FlutterClaw Seed Boundary

FlutterClaw is used as an MIT-licensed seed/reference for native mobile shell organization, theme/token separation, bottom navigation, message composition, empty states, and settings grouping. Attribution lives in `mobile/THIRD_PARTY_NOTICES.md`.

Acorn does not import FlutterClaw runtime code or domain architecture. The mobile app does not include FlutterClaw-style on-device gateway, local agent loop, provider registry, channel adapters, sandbox, MCP client, local tools, or mobile-owned memory. Those concerns remain backend-owned Acorn runtime facts.

## Notification Wake-up

The backend contract for push wake-up is active:

```text
PUT    /v1/devices/{device_id}/push-token
DELETE /v1/devices/{device_id}/push-token/{provider}
```

The generated mobile API client exposes push-token register/revoke methods. The token write response never echoes the token value. Push is a wake-up signal only: payload data contains notification id, kind, and `reload=inbox`; the app must reload `/v1/inbox`, RunDetail, or RunEvent cursor for truth.

The current Flutter UI has not integrated iOS/Android platform notification plugins yet. Backend notification records and delivery statuses are current truth; local notification rendering remains outside the current mobile shell.

## Data Rules

- OpenAPI is the only wire contract. Mobile DTO/model changes must regenerate `mobile/lib/src/api/acorn_api.dart`.
- Backend readiness, run status, pending approval state, thread/message state, and event timelines come from backend projection.
- Backend push notification wake-up support is implemented, but mobile platform notification plugin integration is not.
- Offline-first run execution, local truth merge, embedded Web client, skill authoring, memory editing, provider config editing, and trace explorer are outside the current mobile shell.

## Verification

Mobile-specific checks:

```bash
python3 mobile/tool/generate_openapi_client.py --check
```

From `mobile/`:

```bash
flutter test
flutter analyze
flutter build apk --debug
```

Repo submit gates still require the root checks from README/AGENTS.
