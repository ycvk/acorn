# Acorn

<div align="center">
  <img src="docs/assets/acorn-logo.png" alt="Acorn logo" width="260">
  <br><br>
  <a href="https://github.com/ycvk/acorn/actions/workflows/ci.yml"><img src="https://github.com/ycvk/acorn/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ycvk/acorn/releases/latest"><img src="https://img.shields.io/github/v/release/ycvk/acorn?label=release" alt="Latest Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache--2.0-blue.svg" alt="License: Apache-2.0"></a>
</div>

Acorn is a self-hosted AI agent backend for one owner and their devices.

Run Acorn on your own server, pair your phone, and use the mobile app to start work, inspect runs, review pending approvals, and keep the agent's durable state under your control.

## Features

- Single-owner self-hosted backend for personal deployments.
- Authenticated `/v1` API with one-time device pairing.
- Android mobile control surface for inbox, threads, runs, approvals, and settings.
- Persistent runs, run events, pending actions, tool results, workspace checkpoints, memory, and skills.
- File-backed long-term memory with local semantic retrieval.
- Linux `amd64` and `arm64` release tarballs with bundled native search libraries.
- Signed Android APK published with each GitHub Release.

## Install On A VPS

Acorn's supported deployment path is a Linux release tarball managed by `systemd`. Docker is not required, and the server does not need to build from source.

Install the latest release on Debian or Ubuntu:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | sh
```

The installer installs Acorn's host dependencies, including the OpenBLAS runtime required by the bundled FAISS search libraries.

Install and start the service in one step:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | OPENAI_API_KEY=your-provider-key sh
```

The installer creates:

| Path | Purpose |
| --- | --- |
| `/opt/acorn/acorn` | Release binary |
| `/usr/local/bin/acorn` | Global command wrapper |
| `~/.acorn/acorn.yaml` | Backend configuration |
| `~/.acorn/acorn.env` | Provider secrets |
| `/srv/acorn/workspace` | Operator workspace |
| `/etc/systemd/system/acorn.service` | `systemd` service |

The installer uses the user that runs the script. On a typical root VPS install, Acorn reads `/root/.acorn/acorn.yaml` and `/root/.acorn/acorn.env`. Commands such as `acorn pair` and `acorn doctor` use the same config unless you pass `-c`.

If you did not pass `OPENAI_API_KEY`, edit the environment file and start the service:

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

Local configuration starts from the example file:

```bash
cp configs/acorn.example.yaml configs/acorn.local.yaml
$EDITOR configs/acorn.local.yaml
```

Minimal provider configuration:

```yaml
providers:
  - name: primary
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: true
```

Provider keys can reference environment variables. Missing provider credentials are reported by readiness checks instead of being silently ignored.

Semantic memory search also requires an embedding endpoint under `memory.semantic`. The release packages include the native search libraries used by the backend; see `configs/acorn.example.yaml` or `configs/acorn.selfhosted.example.yaml` for the complete shape.

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

- Go 1.26
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
python3 mobile-kotlin/tool/generate_openapi_client.sh --check
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
| `internal/app/` | Application container and client-facing services |
| `internal/runtime/` | Executor lifecycle, run builder, direct_response, and resume execution |
| `internal/runtime/tooldispatch/` | Safety-aware parallel tool dispatch, streaming executor, side effects |
| `internal/runtime/factextract/` | Fact extraction and memory file/search/remember tools |
| `internal/stream/` | Stream item types, event projection, accessors, assistant streaming |
| `internal/contextplane/` | Context session, masking, auto-compact, tool lifecycle |
| `internal/memory/` | File-backed memory records and semantic retrieval |
| `internal/store/` | SQLite persisted state |
| `internal/api/` | HTTP server, `/healthz`, `/v1` |
| `mobile-kotlin/` | Kotlin + Jetpack Compose mobile app |
| `skills/` | Built-in Acorn skill seed pack |
| `docs/` | User guides, developer guides, OpenAPI, and architecture notes |

## Documentation

- [Self-hosted Onboarding](docs/user/self-hosted-onboarding.md)
- [OpenAPI contract](docs/openapi.yaml)
- [Architecture overview](docs/architecture/ARCHITECTURE.md)
- [Mobile control surface architecture](docs/architecture/mobile-control-surface.md)

## License

Acorn is released under the [Apache License 2.0](LICENSE).
