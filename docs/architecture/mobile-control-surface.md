---
doc_type: architecture
status: current
last_reviewed: 2026-05-22
slug: mobile-control-surface
---

# Mobile Control Surface

## Current Shape

The mobile client lives in `mobile/` and is a Kotlin app for single-user self-hosted Acorn. It is a product-grade remote control surface over authenticated `/v1`; it does not execute runs locally, does not own runtime truth, and does not maintain a second message lifecycle.

Current entrypoints:

```text
mobile-kotlin/app/src/main/java/io/ycvk/acorn/main.dart
  -> AcornApp
  -> Hilt
  -> ConnectionController
  -> InboxController / ThreadsController / ApprovalsController
  -> ChatController
  -> generated AcornApiClient
  -> RunEventStreamClient
  -> authenticated /v1 backend
```

The generated API/model file is:

```text
mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/api/acorn_api.dart
```

It is produced by:

```bash
python3 mobile/tool/generate_openapi_client.py
```

`--check` is the contract drift check and compares the generated Dart file against `docs/openapi.yaml`.

Current mobile shell structure:

```text
mobile-kotlin/app/src/main/java/io/ycvk/acorn/app.dart

mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/core/
  connection_controller.dart
  connection_profile.dart
  connection_store.dart
  providers.dart

mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/api/
  acorn_api.dart
  run_event_stream.dart

mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/features/
  pairing/
  shell/
  inbox/
  chat/
  threads/
  runs/
  approvals/
  settings/

mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/ui/
  theme/
  widgets/
```

`ConnectionController` owns connection lifecycle, the active API/RunEvent clients, pairing, and disconnect. `ShellController` owns tab selection. Feature controllers own feature state: `InboxController` owns `/v1/inbox`, `ThreadsController` owns thread list/active thread, `ApprovalsController` owns pending-action detail/decision commands, `ChatController` owns chat message loading, send/run start, foreground RunEvent streaming projection, and chat-local errors, and `RunDetailController` owns per-run detail loading/cache/error state. `mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/ui/` owns reusable theme and widgets for status pills, empty states, section headers, list rows, and chat rendering. Feature files own backend-backed screens only.

The current state split keeps streaming assistant deltas inside `ChatController`; Threads, Approvals, Settings, and shell navigation do not rebuild for every streamed token. Inbox refreshes notify only inbox consumers, thread list mutations notify only thread consumers, pending-action decisions notify the approval sheet plus the inbox refresh they trigger, and run-detail refreshes notify only consumers of the affected `runId`.

## Material 3 UI System

The Kotlin mobile shell uses Material 3 as the active UI system. `mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/ui/theme/acorn_theme.dart` owns the FlexColorScheme-backed `ThemeData`, Acorn color scheme values, component sub-themes, spacing/radius constants, and semantic status color tokens. The app uses system/Material typography through `ThemeData.textTheme` and keeps status vocabulary centralized as success, warning, info, neutral, and error.

The connected UI should read as a Material control surface: app bars use standard icon actions, settings uses grouped `Card` + `ListTile` sections, chat uses paper-like assistant surfaces and primary-container user messages, and the composer is a bottom Material surface. Avoid oversized custom pill cards for ordinary settings rows, chat reasoning blocks, or toolbar actions; reserve prominent filled/tonal controls for primary actions and status.

Reusable presentation widgets live under `mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/ui/widgets/`:

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

`SecureConnectionStore` persists that profile through `EncryptedSharedPreferences`. `MemoryConnectionStore` exists for tests only. The app never falls back to unauthenticated `/v1`, local dev bypass, or Web local state.

`ConnectionController` owns one active `AcornApiClient` and one active `RunEventStreamClient` per connection profile and closes both on disconnect/dispose. Pairing uses a temporary unauthenticated client and closes it after the exchange.

Self-hosted onboarding generates pairing payloads through the operator CLI:

```bash
acorn pair -c /config/acorn.yaml --server-url https://acorn.example.com --qr
```

The QR payload contains `server_url`, `pairing_code`, and `expires_at`. The mobile scanner parses this payload, fills the connect fields, and leaves final pairing confirmation to the user. Manual server URL and pairing code entry remains available.

## Surfaces

Current mobile surfaces:

- Connect: scan the Acorn pairing QR or enter server URL / pairing code manually, pair a device, and persist the connection profile.
- Chat: thread detail surface for backend message send, run start, live assistant streaming, backend-provided reasoning display, assistant Markdown rendering, and exceptional blocking activity rows.
- Threads: first shell destination and thread-continuation surface. It uses `/v1/inbox` only for high-priority owner context (server readiness, pending decision count, active/attention runs) while still making backend threads the primary action path; it lists/creates/deletes backend threads and opens them in Chat as a pushed detail route.
- Run detail: secondary detail surface over `GET /v1/runs/{run_id}/detail`, projecting run/thread summary, live event activity, issue signals, and user-meaningful artifacts. Raw diagnostic event payloads, trace summaries, runtime workbench facts, and plan DTOs are not exposed through the mobile contract.
- Approvals: list pending backend actions from the inbox aggregate and open the existing approval detail flow.
- Run stream: read `GET /v1/runs/{run_id}/events?after_seq=0&follow=true` and project the mobile live RunEvent subset into the active assistant bubble.
- Pending approval: read `GET /v1/pending-actions/{action_id}` and decide through `POST /v1/pending-actions/{action_id}:decide`.
- Settings: display connected server, device ID, backend model projection, workspace projection, and disconnect.

The connected shell uses three bottom destinations: Threads, Approvals, and Settings. Chat is not a global tab; it is opened from a selected/new thread. Threads remains the first screen, not a separate Home/Inbox feed; its header and priority cards consume backend-owned inbox projections without creating local run or approval truth. Each destination reloads backend truth through its feature controller; the UI does not infer run state, approval state, thread state, or readiness from local screen state.

## Live RunEvent Streaming

`RunEventStreamClient` is hand-written because the generated OpenAPI Dart client does not model streaming `text/event-stream` transport. It is still constrained by the OpenAPI RunEvent schema and validates the live SSE envelope:

```text
id: <RunEvent.event_id>
event: <RunEvent.type>
data: <full RunEvent JSON>
```

The mobile streaming projection only accepts the live OpenAPI RunEvent subset:

- `assistant.delta` appends `data.assistant_delta.delta` to the live assistant bubble and appends `data.assistant_delta.reasoning` to the same assistant item's separate reasoning field.
- `agent.message` and `run.completed` replace/finalize assistant text when `data.message.content` exists and replace reasoning when `data.message.reasoning` exists.
- `run.failed` and `run.interrupted` finalize the assistant bubble with explicit status.
- `run.resume_requested`, `elicitation.pending`, `operator_question.pending`, and `decision_blocked` may render compact activity rows because they affect the next owner action.
- Routine tool, memory, skill, procedure, plan, context compression, MCP, sampling, and subagent events are diagnostic-only backend events. They stay out of the live stream and the default chat transcript.
- Malformed JSON, SSE id/type mismatch, unsupported event type, or wrong run id throws a visible client error.
- Assistant message text renders GitHub-flavored Markdown through `compose-markdown`; code blocks expose a copy action, long assistant messages use a bounded internal Markdown viewport, `http` and `https` links open through `url_launcher`, and user messages remain plain text.
- Backend-provided reasoning renders only in a collapsed Material Thinking section on assistant messages. The client does not infer reasoning from prose, token counts, or local state.
- Persisted thread reloads consume generated `Message.contentParts`; `kind: reasoning` parts become the same assistant reasoning field.

This is a foreground follow surface only. Backend persisted event and message state remain the durable facts, while diagnostic runtime history stays backend-owned. Mobile receives only the live subset and user-meaningful artifacts rather than raw diagnostic payloads or trace summaries.

## Kotlin + Jetpack ComposeClaw Seed Boundary

Kotlin + Jetpack ComposeClaw is used as an MIT-licensed seed/reference for native mobile shell organization, theme/token separation, bottom navigation, message composition, empty states, and settings grouping. Attribution lives in `mobile/THIRD_PARTY_NOTICES.md`.

Acorn does not import Kotlin + Jetpack ComposeClaw runtime code or domain architecture. The mobile app does not include Kotlin + Jetpack ComposeClaw-style on-device gateway, local agent loop, provider registry, channel adapters, sandbox, MCP client, local tools, or mobile-owned memory. Those concerns remain backend-owned Acorn runtime facts.

## Data Rules

- OpenAPI is the only wire contract. Mobile DTO/model changes must regenerate `mobile-kotlin/app/src/main/java/io/ycvk/acorn/src/api/acorn_api.dart`.
- Backend readiness, run status, pending approval state, thread/message state, and event timelines come from backend projection.
- Mobile refreshes backend truth through `/v1/inbox`, RunDetail, and RunEvent cursors; there is no active push-notification wire contract.
- Offline-first run execution, local truth merge, embedded Web client, skill authoring, memory editing, provider config editing, and trace explorer are outside the current mobile shell.

## Verification

Mobile-specific checks:

```bash
python3 mobile/tool/generate_openapi_client.py --check
```

From `mobile/`:

```bash
./gradlew test
./gradlew lint
./gradlew assembleDebug
```

Repo submit gates still require the root checks from README/AGENTS.
