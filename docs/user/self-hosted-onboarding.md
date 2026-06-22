---
title: Self-hosted onboarding
status: current
last_reviewed: 2026-06-21
---

# Self-hosted Onboarding

Acorn's primary product path is a single-user self-hosted backend with authenticated mobile clients. The backend owns runtime truth: threads, runs, events, pending approvals, memory, skills, and workspace mutation state.

This path installs Acorn as a Linux binary managed by `systemd`. It does not create a hosted account, public unauthenticated API, multi-user boundary, Docker service, or packaged execution sandbox.

## 1. Install

On a Debian/Ubuntu VPS, install the latest public release directly from GitHub Release assets:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | sh
```

The installer:

- installs common host tools with `apt-get`: `ca-certificates`, `curl`, `git`, `ripgrep`, `python3`, `make`, `bash`;
- resolves the latest GitHub Release tag from `https://github.com/ycvk/acorn/releases/latest`;
- detects `amd64` or `arm64` from the VPS architecture;
- downloads `acorn_${VERSION}_linux_${ARCH}.tar.gz` and its `.sha256`;
- verifies the outer release checksum and package `CHECKSUMS`;
- installs `/opt/acorn/acorn` (pure Go binary, no shared libraries);
- installs `/usr/local/bin/acorn` as a global wrapper command;
- writes config under the installing user's `~/.acorn`;
- installs bundled native skills under `~/.acorn/skills`;
- installs `/etc/systemd/system/acorn.service`.

Acorn's binary default config path is `~/.acorn/acorn.yaml`. The installer keeps that rule: it resolves the user that runs the script and sets the `systemd` service `HOME` to that user's home. On a typical root VPS install, the service uses:

```text
/root/.acorn/acorn.yaml
```

The default environment file is:

```text
/root/.acorn/acorn.env
```

If you pass the provider key at install time, the script starts the service immediately:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | OPENAI_API_KEY=your-provider-key sh
```

Without `OPENAI_API_KEY`, the script installs files only. Edit the env file and start the service yourself:

```bash
sudoedit ~/.acorn/acorn.env
sudo systemctl enable --now acorn
```

The env file is intentionally small:

```dotenv
OPENAI_API_KEY=your-provider-key
```

## 2. Installer Options

Pin a release version:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | ACORN_VERSION=vX.Y.Z sh
```

Force architecture:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | ACORN_ARCH=arm64 sh
```

Skip host package installation after installing dependencies yourself:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | ACORN_INSTALL_HOST_TOOLS=0 sh
```

Only use this after installing `curl`, `tar`, `sha256sum`, `systemctl`, `git`, `ripgrep`, `python3`, `make`, `bash`.

Install files without starting `systemd`:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | ACORN_START_SERVICE=0 sh
```

## 3. Runtime Defaults

The installed service uses:

- `/opt/acorn/acorn` for the release binary (pure Go, no shared libraries).
- `/usr/local/bin/acorn` as the global command wrapper.
- the installing user's home as the service `HOME`.
- `~/.acorn/acorn.yaml` for config.
- `~/.acorn/acorn.env` for provider secrets.
- `~/.acorn/skills` for bundled native skills and user-local skills.
- `~/.acorn` for runtime storage, SQLite state, and generated skills.
- `/srv/acorn/workspace` for the operator workspace that tools may read and mutate.
- `127.0.0.1:8080` for the HTTP listener.

The wrapper runs service-backed operator commands such as `acorn pair`, `acorn doctor`, `acorn memory`, `acorn skills`, and `acorn smoke` against the same installer-owned `~/.acorn/acorn.yaml` when you do not pass an explicit `-c` config path. If you install as root, that means `/root/.acorn/acorn.yaml`.

If you intentionally serve directly on a trusted private interface, edit `~/.acorn/acorn.yaml` and set:

```yaml
web:
  listen_addr: 0.0.0.0:8080
```

Then restart:

```bash
sudo systemctl restart acorn
```

## 4. Verify

Check process health:

```bash
curl http://127.0.0.1:8080/healthz
```

Check runtime readiness (static config validation — `acorn doctor` never calls the model):

```bash
acorn doctor
```

Prove a task actually executes end-to-end with a real model call:

```bash
acorn smoke "hello, are you working?"
```

`acorn smoke` exits non-zero on any non-succeeded status, so it catches a wrong `api_key` or unreachable `base_url` that static validation cannot. (Building from source without the installer? Run `acorn init` first to scaffold `~/.acorn/acorn.yaml`.)

Semantic memory retrieval is OFF by default — backend runs proceed with no memory recall. To enable it, configure `memory.semantic.embedding` in `~/.acorn/acorn.yaml` (model, base_url, api_key, dimensions are required), restart the service, and build the index:

```bash
acorn memory semantic rebuild --json
```

The embedding index is stored in SQLite `memory_vectors` table. SQLite owns runtime persisted truth, and file-backed `facts/` and `history/` remain durable memory truth.

## 5. Pair Mobile

Generate a one-time pairing payload on the server:

```bash
acorn pair --server-url https://acorn.example.com --qr
```

For manual entry in the mobile app, print the server URL and pairing code without a QR:

```bash
acorn pair --server-url https://acorn.example.com
```

The QR contains compact JSON:

```json
{"pairing_code":"ABCD-EFGH-IJKL-MNOP","expires_at":"2026-05-15T12:00:00Z","server_url":"https://acorn.example.com"}
```

Flutter mobile can scan this terminal QR with the in-app camera scanner, or the same server URL and pairing code can be entered manually.

Machine-readable output is available for scripts:

```bash
acorn pair --server-url https://acorn.example.com --json
```

Pairing codes are short-lived and one-time. The HTTP API does not expose pairing-code creation. After pairing, the device receives a bearer token once; the backend stores only token hashes.

### Token without a phone, and device recovery

To drive `/v1` from a script (or without a phone), mint a bearer token directly on the box in one step:

```bash
acorn token issue --json   # prints {device_id, name, platform, access_token}
```

The token is shown once. Use it as `Authorization: Bearer <token>` against `/v1`.

List and revoke paired devices (runs against local SQLite — no token needed, so a lost token is recoverable from the box):

```bash
acorn devices list
acorn devices revoke DEVICE_ID   # the device's bearer token stops authenticating immediately
```

## 6. Android APK

The same GitHub Release publishes the signed Android APK:

```bash
VERSION_URL=$(curl -fsSLo /dev/null -w '%{url_effective}' https://github.com/ycvk/acorn/releases/latest)
VERSION=${VERSION_URL##*/}
curl -fL -O "https://github.com/ycvk/acorn/releases/download/${VERSION}/acorn_mobile_${VERSION}_android.apk"
curl -fL -O "https://github.com/ycvk/acorn/releases/download/${VERSION}/acorn_mobile_${VERSION}_android.apk.sha256"
sha256sum -c "acorn_mobile_${VERSION}_android.apk.sha256"
```

## 7. Remote Access

Choose one explicit remote boundary:

- **Tailscale**: listen on a private interface or `0.0.0.0:8080`, restrict access through the tailnet, and pair with `http://<tailnet-host>:8080` or a Tailscale HTTPS name.
- **Reverse proxy**: keep Acorn bound to `127.0.0.1:8080`, terminate TLS in Caddy/Nginx/Traefik, and pair with the public HTTPS origin.
- **Cloudflare Tunnel**: keep Acorn bound to `127.0.0.1:8080`, tunnel to that local origin, and pair with the tunnel HTTPS URL.
- **LAN only**: bind to `0.0.0.0:8080` only on a trusted network and pair with the LAN IP.

Do not expose `/v1` without device auth. Acorn does not provide a local/dev auth bypass, and missing, malformed, unknown, or revoked bearer tokens fail explicitly.

## 8. Optional Web Access

Acorn can use native runtime tools for public web research:

- `web_search` uses Tavily for search discovery.
- `web_fetch` fetches a specific public HTTP(S) URL and stores raw/Markdown artifacts.
- `browser` drives a backend-owned Chromium session for pages that need JavaScript, interaction, screenshots, console, or network inspection.

Release packages do not bundle Chrome/Chromium. To enable browser actions on a Debian/Ubuntu VPS, install Chromium yourself:

```bash
sudo apt-get update
sudo apt-get install -y chromium
```

Then configure the executable path and optional Tavily key in `~/.acorn/acorn.yaml`:

```yaml
web_access:
  search:
    provider: tavily
    api_key: ${TAVILY_API_KEY}

browser:
  executable_path: /usr/bin/chromium
  headless: true
  default_timeout_seconds: 20
```

Add the search key to `~/.acorn/acorn.env` if you want search:

```dotenv
TAVILY_API_KEY=your-tavily-key
```

Restart after editing config or env:

```bash
sudo systemctl restart acorn
```

`browser.executable_path` and `TAVILY_API_KEY` are optional at backend startup. Calling `browser` without an executable path or `web_search` without a key fails explicitly as a tool result. `web_fetch` does not require Tavily.

## 9. Backup

Stop the backend before filesystem-level backups:

```bash
sudo systemctl stop acorn
sudo tar -czf /var/backups/acorn-state.tgz -C ~/.acorn .
sudo tar -czf /var/backups/acorn-workspace.tgz -C /srv/acorn/workspace .
sudo systemctl start acorn
```

## 10. Current Limits

- Host commands are host dependencies. If the model tries to run a command that is not installed on the VPS, the command fails explicitly when used.
- Web search requires a configured Tavily API key. Browser actions require an operator-installed Chrome/Chromium executable.
- The mobile app refreshes backend truth through `/v1/inbox`, RunDetail, and RunEvent cursors; this release path does not include APNs/FCM push notification registration.
- Mobile is a remote control surface. It does not execute runs locally, own memory truth, or merge offline runtime state.
