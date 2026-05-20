---
doc_type: plan
status: active
created: 2026-05-20
last_reviewed: 2026-05-20
slug: mobile-control-surface-v2
---

# Mobile Control Surface v2 Plan

## Problem

The current Flutter app is a usable mobile MVP, but its information
architecture still behaves like a chat client with adjacent admin tabs. That is
not the right shape for Acorn's current product truth: a single-owner
self-hosted backend controlled from a phone.

The mobile client should be an owner attention cockpit. Opening the app should
answer, in order:

1. What needs my decision?
2. What is running now?
3. What just completed or failed?
4. Where can I continue work?

Chat remains important, but it is one detail surface under a thread/run, not the
global home screen.

## Current Constraints

- `/v1` and `docs/openapi.yaml` are the only mobile wire contract.
- Mobile never executes runs locally and never maintains a second runtime truth.
- `/v1/inbox` is the backend-owned attention aggregate for pending actions,
  active runs, recent terminal runs, and system status.
- `/v1/runs/{run_id}/detail` is the backend-owned run deep-dive aggregate.
- Routine runtime events must not flood the chat transcript.
- Backend facts must be projected by backend services or generated DTOs. Mobile
  must not infer thread/run/readiness truth from prose, token counts, message
  length, or local heuristics.

## P0 Scope

### 1. Home/Inbox-First Shell

Replace the global Chat tab with Home.

Bottom destinations:

- Home
- Threads
- Approvals
- Settings

Home consumes `InboxResponse` and renders the owner attention stack:

- Needs action: pending approvals, highest priority.
- Running now: active runs.
- Recently finished: recent terminal runs.
- System: readiness summary and model/workspace status.

Home rows open source-owned detail surfaces:

- Pending approval rows route to the approval flow.
- Run rows route to Run Inspector.
- Thread continuation routes to Chat.

### 2. Chat Becomes A Detail Surface

Chat is entered from a selected thread or newly created thread. It is not a
global tab.

The transcript only renders user/assistant conversation plus exceptional
blocking feedback:

- assistant reasoning stays inside the collapsed Thinking section;
- routine tool, memory, skill, procedure, plan, and subagent events stay out of
  chat;
- run failures, interruptions, input requests, and context pressure warnings may
  remain inline because they affect the next user action.

Keyboard behavior is part of this scope. The pushed Chat route must resize
around the soft keyboard and avoid bottom overflow.

### 3. Run Inspector

Add a dedicated Run Inspector surface backed by `RunDetail`.

Initial sections:

- Summary: status, mode, thread, timing.
- Activity: all persisted run events as a lazy timeline.
- Artifacts: `RunArtifact` projection.
- Terminal sessions: `RunTerminalSession` projection.

Run Inspector is where detailed runtime observability belongs. It must not read
legacy endpoints or parse assistant prose.

### 4. Performance State Boundary

The first implementation slice reduces unnecessary rebuild pressure.

P0 behavior:

- Streaming chat output should no longer keep Chat mounted as an always-visible
  tab inside the shell.
- The shell should render Home/Threads/Approvals/Settings as attention surfaces.
- Chat message loading, send/run start, live RunEvent projection, and chat-local
  errors are owned by `ChatController`, not the connection boundary.
- Streaming assistant deltas notify only the pushed Chat route. Home, Threads,
  Approvals, Settings, and shell navigation do not rebuild per token.
- Inbox state is owned by `InboxController`; Home, Approvals badge/list, and
  Settings consume its `/v1/inbox` projection directly.
- Thread list and active thread state are owned by `ThreadsController`; Home,
  Threads, Chat, and Run Inspector consume that controller instead of app-wide
  state.
- Pending-action detail and decision commands are owned by
  `ApprovalsController`; decisions refresh `InboxController` after the backend
  source endpoint accepts the decision.
- Run detail state is owned by `RunDetailController` and cached by `runId`;
  Run Inspector consumes that controller instead of storing a widget-local
  future or making `ConnectionController` own run-detail state.
- Shell tab selection is owned by `ShellController`; connection changes and tab
  changes no longer notify the same controller boundary.

P0 state split is complete: connection/bootstrap, shell navigation, inbox,
threads, approvals, chat, and run detail now have separate controller
boundaries.

Do not fake performance by dropping events, truncating tool results, suppressing
backend errors, or adding silent fallback paths.

## Backend Projection Contract

Home uses backend-projected `RunSummary` fields instead of deriving display
truth from local thread state or assistant prose:

```yaml
RunSummary:
  thread_title: string
  preview: string
  last_event_label: string
  attention_level: enum[normal, needs_action, failed, running]
  duration_ms: integer
```

These fields are owned by `internal/app.InboxService`, exposed through
`docs/openapi.yaml`, and regenerated into `mobile/lib/src/api/acorn_api.dart`.
Mobile renders them directly and does not maintain local title/run-summary
heuristics.

## Explicit Non-Goals

- Offline-first run execution.
- Mobile-owned message lifecycle.
- A Web-compatible navigation model.
- Runtime event suppression at the backend.
- Push notification platform integration in this P0 slice.
- Provider/config editing from mobile.
- Memory editing from mobile.

## Implementation Order

1. Add this plan as the P0 contract.
2. Add Home and make it the first shell destination.
3. Make Threads/New Thread open pushed Chat routes.
4. Add Run Inspector and wire Home run rows to it.
5. Update architecture docs and mobile tests.
6. Run `python3 mobile/tool/generate_openapi_client.py --check`,
   `flutter test`, `flutter analyze`, and repo submit gates.
