---
doc_type: architecture
status: current
last_reviewed: 2026-06-22
slug: mobile-control-surface
---

# Mobile Control Surface

## Current Shape

The mobile client lives in `mobile-kotlin/` and is a Kotlin + Jetpack Compose app for single-user self-hosted Acorn. It is a product-grade remote control surface over authenticated `/v1`; it does not execute runs locally, does not own runtime truth, and does not maintain a second message lifecycle.

Current entrypoints:

```text
mobile-kotlin/app/src/main/java/io/ycvk/acorn/MainActivity.kt
  -> AcornApp (Hilt application)
  -> ShellViewModel (bottom nav)
  -> ThreadsViewModel / ApprovalsViewModel / SettingsViewModel
  -> ChatViewModel / RunDetailViewModel
  -> generated AcornApiClient (openapi-generator-cli -g kotlin)
  -> RunEventStreamClient (hand-written OkHttp SSE)
  -> authenticated /v1 backend
```

The generated API/model files live under:

```text
mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/
  apis/          — ClientApi, DevicesApi, HealthApi, MemoryApi, SkillsApi
  models/        — DTOs generated from docs/openapi.yaml
  infrastructure/ — ApiClient, Serializer, Moshi adapters
```

They are produced by:

```bash
cd mobile-kotlin && ./tool/generate_openapi_client.sh
```

`--check` is the contract drift check and compares generated files against `docs/openapi.yaml`.

Current mobile shell structure:

```text
mobile-kotlin/app/src/main/java/io/ycvk/acorn/
  AcornApp.kt, MainActivity.kt
  core/
    auth/AuthController.kt, SecureStore.kt
    di/AppModule.kt (Hilt)
    sse/RunEventStreamClient.kt, RunEventProjection.kt
    state/ConnectionState.kt
    theme/Color.kt, Theme.kt, Type.kt
  data/repository/AuthRepository.kt
  feature/
    pairing/PairingScreen.kt
    shell/AcornShell.kt, ShellViewModel.kt
    threads/ThreadsScreen.kt, ThreadsViewModel.kt
    chat/ChatScreen.kt, ChatViewModel.kt, ChatMessage.kt
    approvals/ApprovalsScreen.kt, ApprovalsViewModel.kt
    runs/RunDetailScreen.kt, RunDetailViewModel.kt
    settings/SettingsScreen.kt, SettingsViewModel.kt
```

`AuthController` owns connection lifecycle, the active API/SSE clients, pairing, and disconnect. `ShellViewModel` owns bottom-nav tab selection. Feature ViewModels own feature state: `ThreadsViewModel` owns thread list/active thread, `ApprovalsViewModel` owns pending-action detail/decision commands, `ChatViewModel` owns chat message loading, send/run start, foreground RunEvent streaming projection, and chat-local errors, and `RunDetailViewModel` owns per-run detail loading/cache/error state. Feature screens own backend-backed UI only.

The current state split keeps streaming assistant deltas inside `ChatViewModel`; Threads, Approvals, Settings, and shell navigation do not rebuild for every streamed token.

## Material 3 UI System

The mobile shell uses Material 3 via Jetpack Compose. `core/theme/Theme.kt` owns the Acorn color scheme, `Color.kt` defines semantic status colors, and `Type.kt` defines typography. The app uses Material 3 component slots (TopAppBar, NavigationBar, Card, ListItem) and keeps status vocabulary centralized as success, warning, info, neutral, and error.

Feature screens consume Compose Material 3 components and backend facts. They should not define their own status palette, invent a second visual primitive layer, or infer backend state from local UI state.

## Connection Boundary

`PairingScreen` asks for server URL, pairing code, and device name. Pairing calls `POST /v1/devices:pair` through the generated client. On success, the app stores only the explicit connection profile:

```text
server_url
device_id
access_token
```

`SecureStore` persists that profile through `EncryptedSharedPreferences`. The app never falls back to unauthenticated `/v1`, local dev bypass, or Web local state.

`AuthController` owns one active `ApiClient` and one active `RunEventStreamClient` per connection profile and closes both on disconnect/dispose. Pairing uses a temporary unauthenticated client and closes it after the exchange.

Self-hosted onboarding generates pairing payloads through the operator CLI:

```bash
acorn pair -c /config/acorn.yaml --server-url https://acorn.example.com --qr
```

The QR payload contains `server_url`, `pairing_code`, and `expires_at`.

## Surfaces

Current mobile surfaces:

- **Pairing**: enter server URL / pairing code, pair a device, persist connection profile.
- **Chat**: thread detail surface for backend message send, run start, live assistant streaming, backend-provided reasoning display, assistant Markdown rendering, and exceptional blocking activity rows.
- **Threads**: first shell destination and thread-continuation surface. Lists/creates/deletes backend threads and opens them in Chat.
- **Run detail**: secondary detail surface over `GET /v1/runs/{run_id}/detail`, projecting run/thread summary, live event activity, issue signals, and user-meaningful artifacts.
- **Approvals**: list pending backend actions and open the approval detail flow.
- **Run stream**: read `GET /v1/runs/{run_id}/events?after_seq=0&follow=true` and project the mobile live RunEvent subset into the active assistant bubble.
- **Pending approval**: read `GET /v1/pending-actions/{action_id}` and decide through `POST /v1/pending-actions/{action_id}:decide`.
- **Settings**: display connected server, device ID, backend model projection, workspace projection, and disconnect.

The connected shell uses three bottom destinations: Threads, Approvals, and Settings. Chat is not a global tab; it is opened from a selected/new thread. Each destination reloads backend truth through its feature ViewModel; the UI does not infer run state, approval state, thread state, or readiness from local screen state.

## Live RunEvent Streaming

`RunEventStreamClient` is hand-written because the generated OpenAPI Kotlin client does not model streaming `text/event-stream` transport. It is constrained by the OpenAPI RunEvent schema and validates the live SSE envelope:

```text
id: <RunEvent.event_id>
event: <RunEvent.type>
data: <full RunEvent JSON>
```

`RunEventProjection` maps SSE events into `ChatState` (sealed `RunEventPacket` discriminated by `type`):

- `assistant_delta` appends `data.assistant_delta.delta` to the live assistant bubble and appends `data.assistant_delta.reasoning` to the same assistant item's separate reasoning field.
- `agent_message` and `run_completed` replace/finalize assistant text when `data.message.content` exists and replace reasoning when `data.message.reasoning` exists.
- `run_failed` and `run_interrupted` finalize the assistant bubble with explicit status.
- `run_resume_requested`, `elicitation_pending`, `operator_question_pending`, and `decision_blocked` may render compact activity rows because they affect the next owner action.
- Malformed JSON, SSE id/type mismatch, unsupported event type, or wrong run id throws a visible client error.
- Assistant message text renders GitHub-flavored Markdown through `compose-markdown`; code blocks expose a copy action, `http`/`https` links open through `url_launcher`.
- Backend-provided reasoning renders only in a collapsed Material Thinking section on assistant messages.
- Persisted thread reloads consume generated `Message.contentParts`; `kind: reasoning` parts become the same assistant reasoning field.

`MessagePartAdapter` is a custom Moshi adapter that handles OpenAPI oneOf `MessagePart` as a discriminated union on the `kind` field (text/reasoning/result/decision/disclosure/work_status/technical_detail_link). The generated interface cannot be deserialized by Moshi directly; the adapter is registered in `Serializer.kt`.

This is a foreground follow surface only. Backend persisted event and message state remain the durable facts. Mobile receives only the live subset and user-meaningful artifacts.

## Data Rules

- OpenAPI is the only wire contract. Mobile DTO/model changes must regenerate `mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/`.
- Backend readiness, run status, pending approval state, thread/message state, and event timelines come from backend projection.
- Mobile refreshes backend truth through `/v1/inbox`, RunDetail, and RunEvent cursors; there is no active push-notification wire contract.
- Offline-first run execution, local truth merge, embedded Web client, skill authoring, memory editing, provider config editing, and trace explorer are outside the current mobile shell.

## Verification

Mobile-specific checks:

```bash
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check
```

From `mobile-kotlin/`:

```bash
./gradlew test
./gradlew assembleDebug
```

Repo submit gates still require the root checks from README/AGENTS.
