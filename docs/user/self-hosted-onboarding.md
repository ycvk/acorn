---
title: Self-hosted onboarding
status: current
last_reviewed: 2026-10-09
---

# Self-hosted Onboarding

Acorn's primary product path is a single-user self-hosted backend with authenticated mobile clients. The backend owns runtime truth: threads, runs, events, pending approvals, sourced personal memories and commitments, the knowledge base, and skills.

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
- writes config and the default persona (`persona.md`) under the installing user's `~/.acorn`;
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

If you pass both the model and Voyage keys at install time, the script starts the service immediately:

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | OPENAI_API_KEY=your-provider-key VOYAGE_API_KEY=your-voyage-key sh
```

Without both keys, the script installs files only. Edit the env file and start the service yourself:

```bash
sudoedit ~/.acorn/acorn.env
sudo systemctl enable --now acorn
```

The env file is intentionally small:

```dotenv
OPENAI_API_KEY=your-provider-key
VOYAGE_API_KEY=your-voyage-key
```

## Model configuration

Set `providers[].api` to `responses`, `chat_completions`, or `anthropic`. The default uses GPT-6 Astra through Responses. Acorn retains native reasoning and tool-call content throughout a run and approval resumes.

| Model | API | Context window | Output budget | Reasoning effort |
| --- | --- | ---: | ---: | --- |
| `gpt-6-astra` | `responses` | 1050000 | 128000 | low, medium, high, xhigh, max |
| `claude-opus-5-5` | `anthropic` | 1000000 | 128000 | low, medium, high, xhigh, max |
| `grok-4.7` | `responses` | 500000 | 128000 | low, medium, high, xhigh |

Set `context.window_tokens` to the chosen model's context window and `providers[].max_output_tokens` to the desired output budget. Grok's 128000 is a working budget; its published contract has no separate text output limit. Anthropic requires an explicit output budget. Astra and Opus use their model-native sampling: omit `temperature`. Other models can specify it, including an explicit zero. Unlisted model IDs use the owner's configured context and output values.

`context.compact_margin_tokens` defaults to 32000 and `mask_after_turns` to 8. Before compaction, Acorn reserves output tokens, the presence block and the additional margin from the context window. Known models reserve their output limit when `max_output_tokens` is omitted. Tool results are cleared at three quarters of that input budget. Token estimates use `o200k_base`; they approximate other providers.

`runtime.run_timeout_seconds` and provider `timeout_seconds` default to 0, which leaves the total duration unrestricted. `idle_timeout_seconds` defaults to 300 and resets when response data arrives; 0 disables it. Runs remain cancellable from the app, and `agent.max_iterations` defaults to 100. `wake.daily_tokens: 0` leaves the autonomous token budget unrestricted; `wake.daily_limit` still limits the number of autonomous wakes.

The provider output setting is `max_output_tokens`. Update the YAML before starting this version. Complete pending runs before upgrading the runtime so every resumed checkpoint uses the current AgenticMessage format.

## Personal memory

Ordinary owner messages, observed tool outcomes and tracked changes become immutable source references. A durable background queue extracts concise facts, creates Voyage embeddings and reconciles related evidence. Facts keep their validity until evidence changes them. Understandings retain their supporting records; thoughts and ongoing concerns have explicit progress states.

Every run loads recent conversation within a token budget, summarizes older messages with source IDs and automatically recalls related memories. Ask for a memory's source to inspect its original version. An explicit “remember this” saves synchronously. Corrections retain the earlier version and the date the correction took effect. “Forget this” removes the selected content from future agent recall, summaries and derived context; original chat and knowledge files remain available to the owner.

The defaults use `voyage-4` with 1024 dimensions:

```yaml
memory:
  daily_tokens: 100000
  batch_tokens: 8192
  context_tokens: 8192
  history_tokens: 32768
  embedding:
    base_url: https://api.voyageai.com/v1
    api_key: ${VOYAGE_API_KEY}
    model: voyage-4
    dimensions: 1024
```

`daily_tokens` bounds background memory processing; 0 is unlimited. Processing pauses until the next owner-local day when its budget is exhausted. Run-time retrieval, thread summaries and compaction are recorded against that run. `acorn doctor` shows pending and failed processing, oldest queued work, the last completed job, the index generation and its state. A missing model or Voyage key leaves pairing and diagnostics available while execution reports its readiness error.

Before upgrading the memory schema, finish or explicitly cancel active and interrupted runs, then stop Acorn. Use the candidate release binary to run `./acorn memory preflight -c ~/.acorn/acorn.yaml --json` before replacing the installed executable. This checks SQLite integrity and pending work through a read-only connection. Startup migrates stored memories and appointment states transactionally; queued historical processing continues after startup. Existing owner data, persona, devices and knowledge files retain their ownership. An existing installer environment file is preserved; add `VOYAGE_API_KEY` to it before enabling execution.

To change embedding model or dimensions, edit the configuration, stop all Acorn processes and rebuild:

```bash
sudo systemctl stop acorn
acorn memory reindex
acorn doctor
sudo systemctl start acorn
```

Reindex uses an exclusive data-directory lock. An interrupted rebuild retains its generation and processing progress; invoke the same command to resume it. Queries become available once every current record has a vector for the configured model and dimensions. Retrieval failures identify the failed stage, and memory saving is confirmed only after persistence succeeds.

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
- `~/.acorn/persona.md` for the agent's persona.
- `~/.acorn/skills` for bundled native skills and user-local skills.
- `~/.acorn/knowledge` for the knowledge base: markdown notes in a git repository.
- `~/.acorn` for runtime storage and SQLite state.
- `/srv/acorn/workspace` as the workspace root that holds seed and workspace skills.
- `127.0.0.1:8080` for the HTTP listener.

The wrapper runs service-backed operator commands such as `acorn pair`, `acorn doctor`, `acorn skills`, and `acorn smoke` against the same installer-owned `~/.acorn/acorn.yaml` when you do not pass an explicit `-c` config path. If you install as root, that means `/root/.acorn/acorn.yaml`.

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

The mobile app can scan this terminal QR with the in-app camera scanner, or the same server URL and pairing code can be entered manually.

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

`browser.executable_path` and `TAVILY_API_KEY` are optional at backend startup. Without an executable path `browser` is disabled, and without a key `web_search` is disabled; `acorn doctor` lists each disabled tool with the missing setting. `web_fetch` does not require Tavily.

## 9. Persona, Timezone and Commitments

`~/.acorn/persona.md` is the agent's persona: who it is and how it talks to you. Edit it freely; every run reads it. A run fails with the file path when the file is missing or empty. `acorn init` writes the default persona and keeps an existing one.

Set your timezone so the agent reads and schedules times the way you say them:

```yaml
owner:
  timezone: Asia/Shanghai   # IANA name; default UTC
```

When you ask for something later ("remind me in three days to read X"), the agent makes a commitment with `schedule_wake`. The backend wakes it at that time in the same thread, also after a restart. Related limits, shown with their defaults:

```yaml
presence:
  max_tokens: 4000   # size of the working-memory block in each model call
wake:
  daily_limit: 20    # commitment wakes per local day; 0 turns them off
```

## 10. Push Notifications

The agent reaches you with `notify_owner` through Firebase Cloud Messaging. Without a Firebase project the tool stays registered as disabled and says why; nothing else changes.

1. Create a Firebase project at <https://console.firebase.google.com>.
2. Add an Android app with package name `io.ycvk.acorn`. From its config, note the project ID, the app ID, the API key and the sender ID (project number).
3. In Project settings → Service accounts, generate a new private key. Copy the JSON file to the VPS, for example `~/.acorn/fcm-service-account.json`, and make it readable only by the service user (`chmod 600`).
4. Point the backend at it and restart:

   ```yaml
   notify:
     fcm:
       service_account_file: fcm-service-account.json   # relative to the config directory
     max_per_hour: 6
     quiet_hours:
       start: "23:00"
       end: "08:00"
   ```

5. Build the app with your Firebase values in `mobile-kotlin/local.properties`, then install that APK:

   ```properties
   acorn.firebase.projectId=your-project-id
   acorn.firebase.appId=1:1234567890:android:abcdef
   acorn.firebase.apiKey=your-api-key
   acorn.firebase.senderId=1234567890
   ```

   The same values can come from `ACORN_FIREBASE_PROJECT_ID`, `ACORN_FIREBASE_APP_ID`, `ACORN_FIREBASE_API_KEY` and `ACORN_FIREBASE_SENDER_ID`. The release workflow reads them from the repository secrets of the same names; an APK built without them shows push as not configured.

After pairing, allow notifications when the app asks. Settings shows whether push is registered. Notifications held during quiet hours are sent when they end. Tapping a notification opens its thread. Notification text passes through Google's servers; the agent keeps it to a short summary and leaves details in the thread.

## 11. Config Migration

Config is parsed strictly: unknown keys stop the backend. When upgrading from a release before the personal-agent rework, remove these keys from `~/.acorn/acorn.yaml`:

- `agent.system_prompt` (put lasting instructions in `persona.md`)
- the whole `memory` block
- the whole `triggers` block (webhooks and crons; ask the agent for a recurring commitment instead)
- `tools.mutation` and `tools.run_command`
- `context.preserve_recent_turns`
- `mcp.providers[].tool_safety` (use `approval.require`)

New keys, all optional: `approval.require`, `owner.timezone`, `presence.max_tokens`, `wake.daily_limit`, `notify.max_per_hour`, `notify.quiet_hours`, `notify.fcm.service_account_file`, `knowledge.dir`, `watch.rsshub_base_url`, `watch.github_token`, `watch.max_checks_per_tick`, `briefing.at`. Then run `acorn init --persona-only` to write the default persona, and `acorn doctor` to check the result.

Earlier data under `~/.acorn` (`facts/`, `history/`, `worldstate/`, `vectors.db`, `skills/generated/`) is no longer read. Delete it once you no longer need it.

## 12. Knowledge Base and Sharing

The agent keeps longer material in a knowledge base: markdown notes under `~/.acorn/knowledge`, which is also a git repository. Every change the agent makes is one commit, with the run it came from in the commit message, so `git log` shows what changed and when, and `git revert` undoes it. To keep the notes somewhere else, for example an existing Obsidian vault on the server:

```yaml
knowledge:
  dir: /srv/notes   # empty means {storage_dir}/knowledge
```

The backend needs `git` on the server (the installer installs it); without it the service does not start and says what to install. `acorn doctor` shows the directory, the note count and the git version.

Share from any Android app to get something into it: pick Acorn in the system share sheet, add a note if you like, and send. The backend opens a new conversation for the share, and the agent fetches the link, writes a note under `inbox/` and replies with its path. Open the conversation to tell the agent more about it. Images are stored under `attachments/` in the knowledge base. The app's Knowledge tab lists recent notes, searches them, and opens a note to read.

To read or edit the notes on your computer, clone the repository over SSH and push your changes back; the backend's working copy updates on push:

```bash
git clone ssh://root@your-vps/root/.acorn/knowledge acorn-notes
```

Open the clone in Obsidian or any editor. Notes you add or change on the server directly are picked up the next time the notes are listed or searched.

## 13. Watches and the Morning Briefing

Ask the agent to keep an eye on something and it creates a watch: "follow the Go blog", "tell me when golang/go has a new release", "watch the price on this page and tell me right away when it drops". The backend checks each watch on its own schedule (hourly by default, at least every 15 minutes) and only involves the model when something is new. The first check of a watch is the baseline, so what is already there is not reported.

Every morning the agent writes a briefing note, `briefings/YYYY-MM-DD.md` in the knowledge base, with what changed on your watches, today's commitments and what is open, and pushes a short summary. Watches you asked to hear about right away wake the agent in the conversation where you set them up instead; those wakes count toward `wake.daily_limit`, and changes past the limit wait for the briefing.

```yaml
briefing:
  at: "08:00"                 # owner.timezone; empty turns the briefing off
watch:
  rsshub_base_url: http://127.0.0.1:1200   # for rsshub:/route watches
  github_token: ${GITHUB_TOKEN}            # optional; raises GitHub's limit of 60 requests an hour
  max_checks_per_tick: 5
```

Sources a watch can follow:

- RSS or Atom feeds by URL.
- Anything RSSHub can turn into a feed (X, Weibo, and many more) as `rsshub:/route`. Run your own RSSHub next to Acorn; public instances are often rate limited:

  ```bash
  docker run -d --name rsshub --restart always -p 127.0.0.1:1200:1200 diygod/rsshub
  ```

- A GitHub repository's releases or newly opened issues.
- Part of a web page picked by a CSS selector, such as a price; without a selector the page's main text. Pages that only render with JavaScript need `browser.executable_path`.

A watch that keeps failing (five times in a row) is marked failing and listed in the briefing with its last error. Ask the agent to list, pause, resume or change your watches. `acorn doctor` shows how many watches there are and which are failing.

## 14. Backup

Stop the backend before filesystem-level backups:

```bash
sudo systemctl stop acorn
sudo tar -czf /var/backups/acorn-state.tgz -C ~/.acorn .
sudo tar -czf /var/backups/acorn-workspace.tgz -C /srv/acorn/workspace .
sudo systemctl start acorn
```

## 15. Current Limits

- Host commands are host dependencies. If the model tries to run a command that is not installed on the VPS, the command fails explicitly when used.
- Web search requires a configured Tavily API key. Browser actions require an operator-installed Chrome/Chromium executable.
- The mobile app refreshes backend truth through `/v1/inbox`, thread messages, and RunEvent cursors. Push needs your own Firebase project and an APK built with its values; there is no APNs support.
- Autonomous wakes share the configured daily count and optional token limits.
- Watches cannot follow pages that need a login. GitHub watches cover releases and newly opened issues only.
- Knowledge search matches words (full-text); there is no semantic search yet. Shared images are kept as attachments; the agent sees only their path and your note, not the picture.
- Mobile is a remote control surface. It does not execute runs locally, own runtime truth, or merge offline runtime state.

## Idle Thinking and Phone Notifications

Night reflection runs once per owner-local day at `thinking.night_at`. It reviews changed evidence, understandings awaiting review, open thoughts, concerns and unfinished appointment occurrences in the Thoughts thread. `think` records or ends a thought; `concern` records progress and review time; `settle` completes an occurrence with execution evidence. Idle-thought slots advance one due open concern at a time.

```yaml
wake:
  daily_limit: 20
  daily_tokens: 0      # 0 leaves the token budget unlimited
thinking:
  night_at: "03:00"  # Empty disables night reflection.
  wander_at: []      # For example ["15:00"], up to six distinct local times.
```

Set a positive `daily_tokens` to impose a token budget. The budget counts provider-reported usage of successful main-model calls in autonomous runs, by the owner's local day. It is checked before starting a run; an already running task can exceed the remaining budget. Summarization calls, failed streams and calls without usage are not included. Owner messages, captures and morning briefings are outside this budget. Run `acorn doctor` to inspect today's wake count, reported tokens, calls without usage and notification counts by app.

In the Android settings, open **phone notifications**, read the data destination, grant notification access, and choose individual apps. The selection starts empty. Acorn captures ordinary notifications from those apps while paired, stores at most 500 on the device, and starts uploading within one minute of the first arrival. Each request contains at most 100 entries. Failed requests retain their batch and show an error with **Retry upload**. Disconnecting or pairing another identity clears waiting notifications; turning an app off removes its waiting entries. Android notification access remains under your control in system settings.

Notifications do not wake the agent. The latest ten received within six hours can appear in its current context; up to fifty since the previous morning briefing appear in the next briefing input, with a count for additional entries. Briefing instructions select actionable items and omit marketing, passwords and verification codes. Raw notifications are removed after seven days. Content already included in conversation records, context snapshots or notes remains with those records. Selected notification text is sent to your server and model provider.

Notification capture uses Android's [NotificationListenerService](https://developer.android.com/reference/android/service/notification/NotificationListenerService). Device background restrictions can delay delivery; waiting entries are retried when the listener reconnects, the app opens, another notification arrives or you choose retry.

Android 15 can redact notification text classified as sensitive, including detected one-time passwords. Acorn receives the content Android exposes; hidden content remains hidden. Read protected details in the source app. See [Android 15 notification protections](https://developer.android.com/about/versions/15/behavior-changes-all#otp-redaction).
