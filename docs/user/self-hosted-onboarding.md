---
title: Self-hosted onboarding
status: current
last_reviewed: 2026-05-19
---

# Self-hosted Onboarding

Acorn's primary product path is a single-user self-hosted backend with authenticated mobile clients. The backend owns runtime truth: threads, runs, events, pending approvals, memory, skills, tool results, context boundaries, and workspace mutation state.

This path installs Acorn as a Linux binary managed by `systemd`. It does not create a hosted account, public unauthenticated API, multi-user boundary, Docker service, or packaged execution sandbox.

## 1. Install

On a Debian/Ubuntu VPS, install the latest public release directly from GitHub Release assets:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | sh
```

The installer:

- installs common host tools with `apt-get`: `ca-certificates`, `curl`, `git`, `ripgrep`, `python3`, `make`, and `bash`;
- resolves the latest GitHub Release tag from `https://github.com/ycvk/acorn/releases/latest`;
- detects `amd64` or `arm64` from the VPS architecture;
- downloads `acorn_${VERSION}_linux_${ARCH}.tar.gz` and its `.sha256`;
- verifies the outer release checksum and package `CHECKSUMS`;
- installs `/opt/acorn/acorn` plus `/opt/acorn/lib/linux_${ARCH}/libfaiss*.so*`;
- installs `/usr/local/bin/acorn` as a wrapper command;
- creates the `acorn` system user;
- writes config under `/var/lib/acorn/.acorn`;
- installs `/etc/systemd/system/acorn.service`.

Acorn's binary default config path is `~/.acorn/acorn.yaml`. The systemd unit sets `HOME=/var/lib/acorn` and pins the same user-scoped config path explicitly:

```text
/var/lib/acorn/.acorn/acorn.yaml
```

The default environment file is:

```text
/var/lib/acorn/.acorn/acorn.env
```

If you pass the provider key at install time, the script starts the service immediately:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | OPENAI_API_KEY=your-provider-key sh
```

Without `OPENAI_API_KEY`, the script installs files only. Edit the env file and start the service yourself:

```bash
sudoedit /var/lib/acorn/.acorn/acorn.env
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

Install files without starting `systemd`:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | ACORN_START_SERVICE=0 sh
```

## 3. Runtime Defaults

The installed service uses:

- `/opt/acorn/acorn` for the release binary and bundled FAISS libraries.
- `/usr/local/bin/acorn` as the command wrapper.
- `/var/lib/acorn` as the `acorn` service user's home.
- `/var/lib/acorn/.acorn/acorn.yaml` for config.
- `/var/lib/acorn/.acorn/acorn.env` for provider secrets.
- `/var/lib/acorn/.acorn` for runtime storage, SQLite state, generated skills, and the Bleve+FAISS index.
- `/srv/acorn/workspace` for the operator workspace that tools may read and mutate.
- `127.0.0.1:8080` for the HTTP listener.

If you intentionally serve directly on a trusted private interface, edit `/var/lib/acorn/.acorn/acorn.yaml` and set:

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

Check runtime readiness as the service user:

```bash
sudo -u acorn HOME=/var/lib/acorn acorn doctor -c /var/lib/acorn/.acorn/acorn.yaml
```

If you modify memory records directly, explicitly rebuild the semantic index:

```bash
sudo -u acorn HOME=/var/lib/acorn acorn memory semantic rebuild -c /var/lib/acorn/.acorn/acorn.yaml --json
```

Bleve+FAISS is a rebuildable retrieval index. SQLite still owns runtime persisted truth, and file-backed `facts/`, `skills/`, and `history/` remain durable memory truth.

## 5. Pair Mobile

Generate a one-time pairing payload on the server:

```bash
sudo -u acorn HOME=/var/lib/acorn acorn pair \
  -c /var/lib/acorn/.acorn/acorn.yaml \
  --server-url https://acorn.example.com \
  --qr
```

The QR contains compact JSON:

```json
{"pairing_code":"ABCD-EFGH-IJKL-MNOP","expires_at":"2026-05-15T12:00:00Z","server_url":"https://acorn.example.com"}
```

Flutter mobile can scan this terminal QR with the in-app camera scanner, or the same server URL and pairing code can be entered manually.

Machine-readable output is available for scripts:

```bash
sudo -u acorn HOME=/var/lib/acorn acorn pair \
  -c /var/lib/acorn/.acorn/acorn.yaml \
  --server-url https://acorn.example.com \
  --json
```

Pairing codes are short-lived and one-time. The HTTP API does not expose pairing-code creation. After pairing, the device receives a bearer token once; the backend stores only token hashes.

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

## 8. Backup

Stop the backend before filesystem-level backups:

```bash
sudo systemctl stop acorn
sudo tar -czf /var/backups/acorn-state.tgz -C /var/lib/acorn/.acorn .
sudo tar -czf /var/backups/acorn-workspace.tgz -C /srv/acorn/workspace .
sudo systemctl start acorn
```

## 9. Current Limits

- Host commands are host dependencies. If the model tries to run a command that is not installed on the VPS, the command fails explicitly when used.
- The backend records notification wake-up facts and push delivery status, but concrete APNs/FCM network adapters are not configured by this binary service path.
- The Flutter app does not yet include platform notification plugins.
- Mobile is a remote control surface. It does not execute runs locally, own memory truth, or merge offline runtime state.
- The old React/Vite Web client has been removed; Flutter mobile is the product control surface.
