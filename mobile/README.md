# Acorn Mobile

Flutter control surface for a single-user self-hosted Acorn backend. The app is a remote client over authenticated `/v1`; it does not run agents locally and does not own runtime truth.

## App Shell

After pairing, the app uses a chat-first Flutter shell seeded from FlutterClaw UI/app organization and rewritten for Acorn's authenticated `/v1` backend contract. The app uses four destinations:

- Chat: primary surface for backend threads, message send, run start, and live assistant output.
- Threads: backend thread list/create/delete/open.
- Approvals: pending backend actions from the inbox aggregate, with detail/decision flow.
- Settings: connected server, device id, backend model/workspace projection, and disconnect.

Chat streaming follows the backend mobile live event subset through `GET /v1/runs/{run_id}/events?after_seq=0&follow=true`. The hand-written `lib/src/api/run_event_stream.dart` validates the SSE envelope and projects assistant deltas into the active assistant bubble. Visible assistant output comes from `assistant_delta.delta`; backend-provided reasoning from `assistant_delta.reasoning` and persisted `content.parts[]` reasoning renders in a separate collapsed Thinking section. Assistant messages render GitHub-flavored Markdown through `flutter_markdown_plus` inside `lib/src/features/chat/assistant_markdown.dart`; code blocks have a copy action, long assistant messages use a bounded internal Markdown viewport, and `http` / `https` links open through `url_launcher`, while user messages stay plain text. Runtime trace events such as tool progress, plan steps, context pressure, memory, skill, procedure, MCP, sampling, and subagent lifecycle are diagnostic-only backend events and are not accepted by the live stream parser. Pending approval detail reads and decides through the `/v1/pending-actions` endpoints. The generated Dart client in `lib/src/api/acorn_api.dart` is produced from `../docs/openapi.yaml`.

## Material 3 UI

The mobile shell uses Flutter Material 3 as the active design system. `lib/src/ui/theme/acorn_theme.dart` owns the app `ThemeData`, `ColorScheme.fromSeed`, component themes, spacing/radius constants, and semantic status colors. Reusable Material-backed widgets live under `lib/src/ui/widgets/`:

- `acorn_surfaces.dart`: tonal surfaces, bottom composer surface, and reusable icon containers.
- `acorn_status.dart`: status dots, status pills, and error banners.
- `list_rows.dart`, `empty_state.dart`, and `message_widgets.dart`: shared list, empty, chat bubble, activity row, status footer, Thinking section, and typing primitives.

Feature screens should consume these widgets instead of adding screen-local visual containers, raw status colors, or one-off component styling. The mobile app remains a dense backend control surface, not a marketing shell.

## Pairing

Generate a pairing QR on the self-hosted backend:

```bash
sudo -u acorn HOME=/var/lib/acorn acorn pair \
  -c /etc/acorn/acorn.yaml \
  --server-url https://acorn.example.com \
  --qr
```

Open the mobile app, choose `Scan pairing QR`, and point the camera at the terminal QR. The scanner imports the `server_url` and `pairing_code` fields into the connect form; pairing still uses the authenticated `/v1` remote-client contract after the user confirms with `Pair device`.

Manual server URL and pairing code entry remains available.

## Boundary

The shell borrows native mobile interaction patterns from FlutterClaw, but Acorn mobile stays a backend control surface. Do not add FlutterClaw's on-device agent loop, gateway, providers, channels, tools, MCP client, sandbox, WhatsApp integration, analytics/background runtime, local memory truth, unauthenticated `/v1` access, `/api` aliases, mock success paths, or WebView shims.

FlutterClaw is MIT-licensed. Attribution for copied or substantially derived structure/patterns is tracked in `THIRD_PARTY_NOTICES.md`.

## Verification

```bash
python3 tool/generate_openapi_client.py --check
flutter test
flutter analyze
flutter build apk --debug
```

## Android Release Signing

Debug APKs are build-proof artifacts only. Release APKs must be signed through `mobile/android/key.properties`; the Android Gradle config does not fall back to debug signing for release builds.

Local `mobile/android/key.properties` format:

```properties
storePassword=...
keyPassword=...
keyAlias=...
storeFile=app/release.jks
storeType=pkcs12
```

`key.properties` and keystore files are ignored by git. GitHub Release builds create those files from repository secrets:

- `ANDROID_RELEASE_KEYSTORE_BASE64`
- `ANDROID_RELEASE_KEYSTORE_PASSWORD`
- `ANDROID_RELEASE_KEY_ALIAS`
- `ANDROID_RELEASE_KEY_PASSWORD`
