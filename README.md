<div align="center">
  <img src="docs/assets/acorn-logo.png" alt="Acorn" width="200">

  <h1>Acorn</h1>

  <p><strong>A self-hosted personal agent that lives on your server and answers on your phone.</strong></p>

  <a href="https://github.com/ycvk/acorn/actions/workflows/ci.yml"><img src="https://github.com/ycvk/acorn/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ycvk/acorn/releases/latest"><img src="https://img.shields.io/github/v/release/ycvk/acorn?label=release" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-blue.svg" alt="License: Apache-2.0"></a>
</div>

Acorn is an agent for one person. It runs as a single Go binary on your own server, keeps everything in a SQLite database there, and you talk to it from an Android app. It remembers what you tell it and can show where each memory came from, keeps the appointments it makes with you, follows feeds and pages, and has a Linux machine and a browser of its own for work that needs hands. Anything with consequences outside that machine waits for your approval on the phone.

## Highlights

- **Memory with sources.** Facts, inferences and the agent's own thoughts each trace back to the message, tool result or note they came from, with revisions over time. Correct or forget a memory from the app; deleting a conversation forgets it too.
- **A computer of its own.** The agent works in an isolated Podman container with a shell, files and Chromium driven through [BrowserSkill](https://github.com/Tencent/BrowserSkill). When a site needs you to sign in, you open the agent's browser in the app and do it there; passwords never reach the model.
- **Asks before it acts.** Your rules block, allow or require approval for any tool. Shell commands, browser actions and MCP calls the rules leave open are reviewed by [Jev](https://typesafe.ai); paying, sending, deleting or changing settings waits for you. Approve or decline right on the notification. Runs continue on the server, even across restarts.
- **Keeps appointments.** "Remind me in three days" wakes the agent in the same conversation at that time. Scheduled night reflection and idle thoughts revisit open questions, within daily budgets you set.
- **Follows things for you.** RSS and Atom feeds (RSSHub included), GitHub releases and issues, and parts of web pages such as prices. New items reach you right away or in a morning briefing.
- **A knowledge base.** Share a link or an image from any app and the agent files it as a note. Every edit is kept as a revision; read and search notes in the app.
- **Learns procedures.** After finishing work you are likely to ask for again, the agent saves the steps as a skill, once you approve, and tidies its skills during night reflection.
- **Yours to run.** Pure Go, no CGO, Linux `amd64` and `arm64`. One SQLite file, nightly backups, a signed Android APK with every release.

## How it works

```
 Android app ── /v1 + device token ──▶ acorn serve ─────────▶ model provider
                                        │  SQLite: runs, memory, notes, skills
                                        │  scheduler: appointments, watches, briefing
                                        │  push via Firebase Cloud Messaging
                                        └─▶ workstation container: shell, files, Chromium
```

Each run is one [Eino](https://github.com/cloudwego/eino) `ChatModelAgent`. Before every model call it gets a token-budgeted view of the conversation, the memories relevant to it and a short "presence" note of the time, open appointments and concerns. The API contract is [docs/openapi.yaml](docs/openapi.yaml); the Kotlin client in the app is generated from it.

## Requirements

- A Debian or Ubuntu server with systemd, `amd64` or `arm64`. 2 GB of RAM is enough. The installer adds Podman for the workstation.
- A model provider: the OpenAI Responses API, a Chat Completions compatible API, or the Anthropic API.
- A [Voyage AI](https://www.voyageai.com) key for memory embeddings.
- An Android phone (Android 8.0 or newer).

Optional: a TypeSafe key for action review, a Tavily key for web search, a Firebase project for push notifications, an RSSHub instance and a GitHub token for watches.

## Quick start

Install the latest release and start the service:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | OPENAI_API_KEY=your-provider-key VOYAGE_API_KEY=your-voyage-key sh
```

The installer puts the binary in `/opt/acorn`, writes `~/.acorn/acorn.yaml` and `~/.acorn/acorn.env`, keeps data in `/srv/acorn`, builds the workstation image and installs the `acorn`, `acorn-workstation` and nightly backup services. Running it again upgrades Acorn and leaves your config, secrets and data as they are.

Check that everything is ready:

```bash
curl http://127.0.0.1:8080/healthz
acorn doctor
```

Acorn listens on the server only. Reach it from your phone through a reverse proxy with TLS, Tailscale or a tunnel, then pair the app:

```bash
acorn pair --server-url https://acorn.example.com --qr
```

Download `acorn_mobile_<version>_android.apk` from the [latest release](https://github.com/ycvk/acorn/releases/latest), install it and scan the code. The phone receives a device token; the server keeps only its hash.

The [self-hosted guide](docs/user/self-hosted-onboarding.md) covers every step in detail, including reverse proxy setup, backups and restore.

## Configuration

`acorn.yaml` holds the settings and refers to secrets in `acorn.env` as `${VAR}`. A provider looks like this:

```yaml
providers:
  - name: primary
    api: responses            # responses, chat_completions or anthropic
    model: gpt-6-astra
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}
    enabled: true
```

Tool rules are glob patterns. `block` wins over `require`, and `require` over `allow`:

```yaml
approval:
  require: ["mcp__*", skill_write, skill_import, skill_retire]
  block: []
  allow: []
```

Unknown keys stop the backend, so a typo is reported instead of ignored. The guide documents the persona, memory budgets, wake limits, quiet hours, watches and action review.

Push notifications need your own Firebase project: the released APK cannot receive pushes sent with another project's credentials, so build the app with your Firebase values as the guide describes. Everything else works without push.

## Development

Prerequisites: Go 1.27, `golangci-lint` and `goimports`; JDK 21 and the Android SDK for the app.

```bash
make build                     # bin/acorn
./bin/acorn init -c configs/acorn.local.yaml
make serve                     # serves with configs/acorn.local.yaml
```

Before sending a change:

```bash
make format-check && make lint && make test
make test-architecture         # dependency direction and contract guards
go test -race ./...
```

Changing the API means updating `docs/openapi.yaml`, then `make generate` and, in `mobile-kotlin/`, `./tool/generate_openapi_client.sh`. App checks run from `mobile-kotlin/` with `./gradlew test assembleDebug`.

| Path | What lives there |
| --- | --- |
| `cmd/acorn`, `internal/cli` | The `acorn` command |
| `internal/wire` | Composition root, the only place concrete implementations are built |
| `internal/core` | Domain types and the interfaces other packages depend on |
| `internal/runtime` | Runs: agent assembly, middleware, approval, event projection |
| `internal/memory`, `internal/store` | Sourced memory and recall; SQLite storage and schema migrations |
| `internal/tools`, `internal/skills` | The agent's tools and skills |
| `internal/workstation`, `deploy/workstation` | The workstation API and its container image |
| `internal/wake`, `internal/watch`, `internal/notify` | Scheduler, watches and push |
| `internal/knowledge`, `internal/webaccess`, `internal/mcp` | Knowledge base, web access, MCP providers |
| `internal/api` | The `/v1` HTTP API |
| `mobile-kotlin` | The Android app (Kotlin, Jetpack Compose) |

## Documentation

- [Self-hosted guide](docs/user/self-hosted-onboarding.md)
- [API contract](docs/openapi.yaml)
- [Architecture decisions](docs/adr/README.md)
- [Architecture invariants](docs/architecture/INVARIANTS.md)
- [Engineering constraints](AGENTS.md)

## Status

Acorn is built for a single owner and is developed in the open. It runs daily on the author's own server; expect configuration and API changes between minor versions, recorded in each release and in the guide's migration notes.

## License

[Apache License 2.0](LICENSE)
