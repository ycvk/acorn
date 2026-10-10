# Acorn

<div align="center">
  <img src="docs/assets/acorn-logo.png" alt="Acorn logo" width="260">
  <br><br>
  <a href="https://github.com/ycvk/acorn/actions/workflows/ci.yml"><img src="https://github.com/ycvk/acorn/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ycvk/acorn/releases/latest"><img src="https://img.shields.io/github/v/release/ycvk/acorn?label=release" alt="Latest Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-blue.svg" alt="License: Apache-2.0"></a>
</div>

Acorn is a self-hosted personal agent for one owner and their devices.

Run Acorn on your own server, pair your phone, and talk to an agent with a persona and sourced personal memory. It keeps the appointments it makes with you, wakes up on its own when one is due, files what you share from your phone into a markdown knowledge base, follows feeds, GitHub repositories and web pages for you with a briefing every morning, and pushes to your phone when you should know something. Its state stays on your server.

## Features

- Single-owner self-hosted backend for personal deployments.
- Authenticated `/v1` API with one-time device pairing.
- Android mobile control surface for threads, chat with live run streaming, approvals, push notifications, sharing from other apps, the knowledge base, and settings.
- Persistent runs, run events, pending actions, artifacts, personal memory, and skills.
- An editable persona and sourced facts, revisable understandings, open thoughts, ongoing concerns, and appointments with individually recorded occurrences.
- Commitments: "remind me in three days" wakes the agent in the same thread at that time, also after a restart.
- Push notifications through Firebase Cloud Messaging, with an hourly cap and quiet hours.
- Scheduled night reflections and idle thoughts, with daily wake and reported-token budgets.
- Opt-in phone notification capture by app, with a durable device-scoped upload queue and signals in the agent's context and morning briefing.
- Watches on RSS and Atom feeds (RSSHub routes included), GitHub releases and issues, and parts of web pages such as prices. New items wake the agent right away or wait for a morning briefing note pushed to your phone.
- A knowledge base of markdown notes that keeps every revision: share a link from your phone and the agent writes a note; read and search it in the app.
- Automatic memory extraction and keyword, semantic, relationship and temporal recall; explicit correction, forgetting, and source inspection; full-text search over notes.
- Tool calls that need your sign-off pause on your phone and continue on the server after you decide, even across restarts.
- Linux `amd64` and `arm64` release tarballs (pure Go cross-compilation, no CGO).
- Signed Android APK published with each GitHub Release.

## Install On A VPS

Acorn's supported deployment path is a Linux release tarball managed by `systemd`. Docker is not required, and the server does not need to build from source.

Install the latest release on Debian or Ubuntu:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | sh
```

The installer installs Acorn's host dependencies and creates the systemd service.

Install and start the service in one step:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | OPENAI_API_KEY=your-provider-key VOYAGE_API_KEY=your-voyage-key sh
```

The installer creates:

| Path | Purpose |
| --- | --- |
| `/opt/acorn/acorn` | Release binary |
| `/usr/local/bin/acorn` | Global command wrapper |
| `~/.acorn/acorn.yaml` | Backend configuration |
| `~/.acorn/acorn.env` | Provider secrets |
| `~/.acorn/skills` | Skills, one directory with a `SKILL.md` each |
| `/srv/acorn/workspace` | Runtime storage: SQLite database, persona and shared images |
| `/etc/systemd/system/acorn.service` | `systemd` service |

The installer uses the user that runs the script. On a typical root VPS install, Acorn reads `/root/.acorn/acorn.yaml` and `/root/.acorn/acorn.env`. Commands such as `acorn pair` and `acorn doctor` use the same config unless you pass `-c`.

If you did not pass both `OPENAI_API_KEY` and `VOYAGE_API_KEY`, edit the environment file and start the service:

```bash
sudoedit ~/.acorn/acorn.env
sudo systemctl enable --now acorn
```

Verify the backend:

```bash
curl http://127.0.0.1:8080/healthz
acorn doctor
```

Create a pairing QR for the mobile app:

```bash
acorn pair --server-url https://acorn.example.com --qr
```

Print the same mobile connection details for manual entry:

```bash
acorn pair --server-url https://acorn.example.com
```

For remote access, keep Acorn bound to `127.0.0.1:8080` and expose it through Tailscale, a reverse proxy, or a tunnel. See [Self-hosted Onboarding](docs/user/self-hosted-onboarding.md) for the full deployment guide.

## Android App

Each GitHub Release includes a signed Android APK:

```bash
VERSION_URL=$(curl -fsSLo /dev/null -w '%{url_effective}' https://github.com/ycvk/acorn/releases/latest)
VERSION=${VERSION_URL##*/}
curl -fL -O "https://github.com/ycvk/acorn/releases/download/${VERSION}/acorn_mobile_${VERSION}_android.apk"
curl -fL -O "https://github.com/ycvk/acorn/releases/download/${VERSION}/acorn_mobile_${VERSION}_android.apk.sha256"
sha256sum -c "acorn_mobile_${VERSION}_android.apk.sha256"
```

Open the app, scan or enter the pairing payload, and the device receives a bearer token. The token is shown once; the server stores only token hashes.

## Configuration

Local configuration starts from the starter config that `acorn init` writes:

```bash
make build
./bin/acorn init -c configs/acorn.local.yaml
$EDITOR configs/acorn.local.yaml
```

Minimal provider configuration:

```yaml
providers:
  - name: primary
    api: responses
    model: gpt-6-astra
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: true
```

Provider keys can reference environment variables. Missing provider credentials are reported by readiness checks instead of being silently ignored.

Tools whose names match `approval.require` pause the run until you accept or decline the call on your phone. Patterns use glob syntax and default to the browser and every MCP tool:

```yaml
approval:
  require:
    - browser
    - "mcp__*"
```

## API

Remote clients use the authenticated `/v1` API. Common endpoints include:

| Endpoint | Purpose |
| --- | --- |
| `GET /healthz` | Process health |
| `POST /v1/devices:pair` | Exchange a pairing code for a device token |
| `GET /v1/inbox` | Mobile-friendly aggregate of active work and pending approvals |
| `GET /v1/threads` | List threads |
| `POST /v1/threads` | Create a thread |
| `POST /v1/threads/{thread_id}/messages` | Append a user message |
| `POST /v1/threads/{thread_id}/runs` | Start a run |
| `GET /v1/runs/{run_id}/events?follow=true` | Replay and follow mobile live run events |
| `GET /v1/runs/{run_id}/detail` | Fetch a full run detail view |
| `GET /v1/pending-actions` | List pending approvals |
| `POST /v1/pending-actions/{action_id}:decide` | Accept or decline a pending action |

The full client contract is defined in [docs/openapi.yaml](docs/openapi.yaml). The Kotlin + Jetpack Compose mobile client is generated from that file.

## Develop From Source

Prerequisites:

- Go 1.27
- `golangci-lint`
- `goimports`
- Kotlin + Jetpack Compose, if you work on the mobile app

Run the backend locally:

```bash
make doctor
make serve
```

Pair a local mobile client or API client:

```bash
go run ./cmd/acorn pair -c configs/acorn.local.yaml --server-url http://127.0.0.1:8080 --qr
```

Run checks before sending changes:

```bash
make test
make format-check
make lint
(cd mobile-kotlin && ./tool/generate_openapi_client.sh --check)
git diff --check
```

Mobile checks run from `mobile-kotlin/`:

```bash
./gradlew test
./gradlew lint
./gradlew assembleDebug
```

## Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/acorn/` | CLI entrypoint |
| `internal/wire/` | Composition root — container wiring, the only place concrete implementations are instantiated |
| `internal/core/` | Layer 0 domain types, store interfaces, tool contracts — zero internal imports |
| `internal/runtime/` | Executor, RunnerFactory, Eino ChatModelAgent assembly, presence, approval and tool-error middleware, skill backend, StreamItem projection |
| `internal/tools/` | Tool implementations (artifact, operator, personal memory, notify, knowledge, web, browser), ToolRegistry |
| `internal/store/` | SQLite persisted state (modernc.org/sqlite; one writer connection, a read-only pool for memory reads), including personal memory, knowledge notes and full-text search |
| `internal/memory/` | Sourced extraction, consolidation, hybrid recall, thread summaries and per-call budgets |
| `internal/presence/` | Presence rendering, persona, cron parsing |
| `internal/wake/` | Scheduler inside `serve`: commitments, watches and the morning briefing |
| `internal/notify/` | FCM HTTP v1 client and push sender (hourly cap, quiet hours) |
| `internal/knowledge/` | Knowledge base: note paths, revisions, image attachments, agent-side exclusion filtering |
| `internal/watch/` | Watch checker: feeds, GitHub, page snapshots, failure backoff |
| `internal/mcp/` | MCP provider manager |
| `internal/webaccess/` | Web fetcher, Tavily search, content extraction, shared outbound URL policy |
| `internal/skills/` | File-backed skill loader |
| `internal/config/` | Config struct, defaults, validation |
| `internal/cli/` | CLI command dispatch |
| `internal/api/` | HTTP server, `/healthz`, `/v1` |
| `mobile-kotlin/` | Kotlin + Jetpack Compose mobile app |
| `skills/` | Built-in Acorn skill seed pack |
| `docs/` | User guide, OpenAPI contract, architecture invariants, and ADRs |

## Documentation

- [Self-hosted Onboarding](docs/user/self-hosted-onboarding.md)
- [OpenAPI contract](docs/openapi.yaml)
- [Architecture and engineering constraints](AGENTS.md)
- [Architecture invariants](docs/architecture/INVARIANTS.md)
- [Architecture decision records](docs/adr/README.md)

## License

Acorn is released under the [Apache License 2.0](LICENSE).
