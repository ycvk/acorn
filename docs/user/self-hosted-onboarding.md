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

- installs `ca-certificates` and `curl` with `apt-get`;
- resolves the latest GitHub Release tag from `https://github.com/ycvk/acorn/releases/latest`;
- detects `amd64` or `arm64` from the VPS architecture;
- downloads `acorn_${VERSION}_linux_${ARCH}.tar.gz` and its `.sha256`;
- verifies the outer release checksum and package `CHECKSUMS`;
- installs `/opt/acorn/acorn` (pure Go binary, no shared libraries);
- installs `/usr/local/bin/acorn` as a global wrapper command;
- writes config under the installing user's `~/.acorn`;
- enables `acorn-backup.timer`, a nightly backup into `/srv/acorn/backups`;
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

Running the script again upgrades in place. It keeps the existing config and env file, then restarts `acorn` (and `acorn-workstation`) on the new release. A run still working when `acorn` stops ends as failed; a run waiting for your decision keeps waiting and continues after the restart.

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

A model in the table uses its context window from the table. Any other model ID needs `providers[].context_window_tokens`, its context window; a model in the table refuses it. Set `providers[].max_output_tokens` to the desired output budget. Grok's 128000 is a working budget; its published contract has no separate text output limit. Anthropic requires an explicit output budget. Astra and Opus use their model-native sampling: omit `temperature`. Other models can specify it, including an explicit zero.

Before compaction, Acorn reserves the output tokens, the 4000-token presence block and a 32000-token margin from the context window. Known models reserve their output limit when `max_output_tokens` is omitted. Tool results are cleared at three quarters of that input budget, keeping the latest eight rounds of tool calls. Token estimates use `o200k_base`; they approximate other providers.

Provider `timeout_seconds` defaults to 0, which leaves the total duration of a model call unrestricted. `idle_timeout_seconds` defaults to 300 and resets when response data arrives; 0 disables it. A run has no total time limit, makes at most 100 model calls and can be stopped from the app.

The provider output setting is `max_output_tokens`. Update the YAML before starting this version. Complete pending runs before upgrading the runtime so every resumed checkpoint uses the current AgenticMessage format.

## Personal memory

Ordinary owner messages, observed tool outcomes and tracked changes become immutable source references. A durable background queue extracts concise facts from the owner's own messages, creates Voyage embeddings and reconciles related evidence; tool outcomes and tracked changes stay citable sources without being extracted. A task you hand the agent, such as finding a book or installing a skill, stays in its conversation; only durable facts stated with it, such as preferences, plans and standing instructions, become memory. Facts keep their validity until evidence changes them. Understandings retain their supporting records; thoughts and ongoing concerns have explicit progress states.

Every run loads recent conversation within a token budget, summarizes older messages with source IDs and automatically recalls related memories. Ask for a memory's source to inspect its original version. An explicit “remember this” saves synchronously. Corrections retain the earlier version and the date the correction took effect. “Forget this” removes the selected content from future agent recall, summaries and derived context; original chat and knowledge files remain available to the owner.

The app's 知识 tab has a 记忆 view listing what Acorn currently remembers, newest change first, with search and a filter for facts, inferences and thoughts. Open a memory to see the quotes it rests on and where each came from; tap one to open its conversation or note. A fact can be corrected in your own words, which become its evidence while the earlier wording stays in its history. Forgetting a memory from the app works like asking in chat: a fact takes its quotes and the inferences drawn from them along, an inference or thought goes alone.

Deleting a conversation makes Acorn forget it too: what was said and the tool results in it leave its memory, with the facts and inferences that rested only on them, and the conversation's runs are removed. Notes and skills written there stay, and its reminders and watches keep running in a new conversation. A conversation Acorn is still working in cannot be deleted until the run ends.

The defaults use `voyage-4` with 1024 dimensions:

```yaml
memory:
  embedding:
    base_url: https://api.voyageai.com/v1
    api_key: ${VOYAGE_API_KEY}
    model: voyage-4
    dimensions: 1024
```

The `memory.daily_tokens` [setting](#settings) bounds background memory processing; 0 is unlimited. Processing pauses until the next owner-local day when its budget is exhausted; raising the budget or lifting its cap puts that day's deferred work back in line. Run-time retrieval, thread summaries and compaction are recorded against that run. `acorn doctor` shows pending and failed processing, oldest queued work, the last completed job, the index generation and its state. A missing model or Voyage key leaves pairing and diagnostics available while execution reports its readiness error.

An existing installer environment file is preserved; add `VOYAGE_API_KEY` to it before enabling execution.

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

Only use this after installing `ca-certificates`, `curl`, `tar`, `sha256sum` and `systemctl`.

Skip the agent workstation (no Podman, no `acorn-workstation.service`):

```bash
curl -fsSL https://github.com/ycvk/acorn/releases/latest/download/install-release.sh | ACORN_INSTALL_WORKSTATION=0 sh
```

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
- `/srv/acorn/workspace` for runtime storage (`runtime.storage_dir`): the SQLite database with memories, knowledge notes, skills, the persona and your settings, and images shared from the phone under `attachments/`.
- `127.0.0.1:8080` for the HTTP listener.

The wrapper runs service-backed operator commands such as `acorn pair`, `acorn doctor`, `acorn skills`, `acorn settings` and `acorn smoke` against the same installer-owned `~/.acorn/acorn.yaml` when you do not pass an explicit `-c` config path. If you install as root, that means `/root/.acorn/acorn.yaml`.

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
- **Reverse proxy**: keep Acorn bound to `127.0.0.1:8080`, terminate TLS in Caddy/Nginx/Traefik, and pair with the public HTTPS origin. Let request bodies through up to the file cap (`files.max_upload_mib`, 50 MiB by default), for example `client_max_body_size 51m;` in Nginx, whose default of 1 MiB refuses most files and photos.
- **Cloudflare Tunnel**: keep Acorn bound to `127.0.0.1:8080`, tunnel to that local origin, and pair with the tunnel HTTPS URL.
- **LAN only**: bind to `0.0.0.0:8080` only on a trusted network and pair with the LAN IP.

Do not expose `/v1` without device auth. Acorn does not provide a local/dev auth bypass, and missing, malformed, unknown, or revoked bearer tokens fail explicitly.

## 8. Optional Web Access

Acorn can use native runtime tools for public web research:

- `web_search` uses Exa for search discovery.
- `web_fetch` fetches a specific public HTTP(S) URL and stores raw/Markdown artifacts. It reads HTML, plain text, XML and JSON; an RSS, Atom or JSON feed comes back as a list of its entries.

Pages that need JavaScript or interaction open in a real browser on the agent's workstation; see [Agent Workstation](#15-agent-workstation).

Configure the optional Exa key in `~/.acorn/acorn.yaml`:

```yaml
web_access:
  search:
    provider: exa
    api_key: ${EXA_API_KEY}
```

Add the search key to `~/.acorn/acorn.env` if you want search:

```dotenv
EXA_API_KEY=your-exa-key
```

Restart after editing config or env:

```bash
sudo systemctl restart acorn
```

`EXA_API_KEY` is optional at backend startup. Without a key `web_search` is disabled; `acorn doctor` lists each disabled tool with the missing setting. `web_fetch` does not require Exa.

## 9. Persona, Settings and Commitments

The persona is who the agent is and how it talks to you. It lives in the database: the first start saves a default persona as revision 1, and every run reads the current revision when it starts. In the app, Settings → 人格 shows the persona and its history: edit it there, compare any revision with the current one, or restore it. Restoring saves that revision's content as a new revision. On the server, show it or replace it with the contents of a file:

```bash
acorn persona show
```

```bash
acorn persona set my-persona.md
```

Every save is a new revision on top of the one it replaces and fails when the persona changed in the meantime. A persona is trimmed, must not be empty and holds at most 8 KiB, because it goes into every model call. The agent cannot change it; preferences you tell it in conversation become memories.

The app asks what Acorn should call you when it first connects and opens only once you answer. Change the name later in the app, under 设置 → Acorn → 怎么称呼你, or on the server:

```bash
acorn owner set 温遥
acorn owner show
```

The name tells the agent it is you when shared material, notifications or memories name you, and facts it learns about you are written with it. It holds at most 40 characters on one line; `acorn owner set ""` leaves you unnamed, and the app asks again the next time it opens. The next conversation uses a new name; facts already written keep theirs.

### Settings

Your time zone, the times of the morning briefing and the reflections, push quiet hours, daily budgets and approval rules are settings. They live in the database, and only you change them: in the app under 设置 → 作息、预算与审批, where each one says what it does and each budget shows what it used today, or on the server with `acorn settings`. A change applies at its next use without a restart: the scheduler's next check, the next run, the next push or the next model call in the background.

| Setting | What it does | Default |
| --- | --- | --- |
| `owner.timezone` | IANA time zone for showing times, counting a day's budgets and reading times you give such as "8 tomorrow" | UTC |
| `briefing.at` | Morning briefing time; empty turns it off | 08:00 |
| `thinking.night_at` | Night reflection time; empty turns it off | 03:00 |
| `thinking.wander_at` | Idle thought times, up to six | none |
| `notify.quiet_hours` | Pushes inside this window wait until it ends | 23:00–08:00 |
| `notify.max_per_hour` | Pushes allowed in any hour | 6 |
| `wake.daily_limit` | Autonomous wakes per day; 0 turns them off | 20 |
| `wake.daily_tokens` | Tokens autonomous wakes may use per day; 0 means no cap | 0 |
| `memory.daily_tokens` | Tokens background memory work may use per day; 0 means no cap | 100000 |
| `news.daily_tokens` | Tokens sorting news may use per day; 0 means no cap | 100000 |
| `approval.block` | Tools refused outright | none |
| `approval.require` | Tools that wait for your approval each time | `mcp__*`, `skill_write`, `skill_import`, `skill_retire` |
| `approval.allow` | Tools that run without review | none |

```bash
acorn settings show
acorn settings set owner.timezone Asia/Shanghai
acorn settings set thinking.wander_at 10:00 15:00
acorn settings set notify.quiet_hours 23:00 08:00
acorn settings reset news.daily_tokens
```

A list takes any number of values, and none empties it; quiet hours take a start and an end, or nothing to turn them off; `acorn settings set briefing.at ""` turns the briefing off. `acorn settings show` marks the settings at their default, which follow the default of the version you run, and shows what each budget used today. Moving a time to earlier today runs that routine at the scheduler's next check if it has not run today. On the day you change the time zone, a daily budget or routine can count one day more or less. The agent cannot change settings; asked to, it tells you where they are.

### Commitments

When you ask for something later ("remind me in three days to read X"), the agent makes a commitment with `schedule_wake`. The backend wakes it at that time in the same thread, also after a restart. These wakes count toward `wake.daily_limit` and `wake.daily_tokens`.

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
   ```

   The hourly cap and quiet hours are the `notify.max_per_hour` and `notify.quiet_hours` [settings](#settings).

5. Build the app with your Firebase values in `mobile-kotlin/local.properties`, then install that APK:

   ```properties
   acorn.firebase.projectId=your-project-id
   acorn.firebase.appId=1:1234567890:android:abcdef
   acorn.firebase.apiKey=your-api-key
   acorn.firebase.senderId=1234567890
   ```

   The same values can come from `ACORN_FIREBASE_PROJECT_ID`, `ACORN_FIREBASE_APP_ID`, `ACORN_FIREBASE_API_KEY` and `ACORN_FIREBASE_SENDER_ID`. The release workflow reads them from the repository secrets of the same names; an APK built without them shows push as not configured.

After pairing, allow notifications when the app asks. Settings (the gear on the 今天 tab) shows whether push is registered. Notifications held during quiet hours are sent when they end, except those for an approval or question you have answered in the meantime. Tapping a notification opens its thread. When a run stops for your approval or answer, a notification arrives right away; tool approvals carry 批准 and 拒绝 so you can decide without opening the app, and tapping the notification opens the thread with the decision on screen. These notifications count toward `notify.max_per_hour`; past the limit the decision still waits on the 今天 tab. Notification text passes through Google's servers; the agent keeps it to a short summary and leaves details in the thread.

### Runs in progress

A run you start from the phone shows as a silent ongoing notification while it runs: a message, a view's button or choice, something shared from another app, or a run that continues after you approve or answer on the phone. It names the conversation and the current step and has 停止 to stop the run. On Android 16 QPR1 and later the system promotes it to a Live Update, with a status bar chip such as 搜索中 or 回复中 and a card at the top of the shade and on the lock screen; on older Android, or with Live Updates for Acorn turned off in system settings, it stays an ordinary ongoing notification. It goes away when the run ends or stops to wait for you, in which case the approval notification arrives as usual. When the app is not in front at the end, a notification with the opening of the reply, or the reason the run failed, takes its place; a run you stopped leaves none. Runs the agent starts on its own and runs started from other devices show no progress. This works without Firebase: the app reads the run's events in a data sync foreground service, which Android 15 and later limit to six hours a day.

## 11. Config Migration

Config is parsed strictly: unknown keys stop the backend, and the error names each with its line. `acorn` commands read the same file and stop the same way, and the installer keeps an existing config as it is. When an upgrade stops on old keys:

1. Stop Acorn with `sudo systemctl stop acorn`. systemd restarts a failed service every few seconds, so otherwise it starts with default settings as soon as the file loads.
2. Note the values of the settings below that you changed from their defaults.
3. Remove the old keys from `~/.acorn/acorn.yaml`.
4. Save the noted values with `acorn settings set`, for example `acorn settings set owner.timezone Asia/Singapore`.
5. Start Acorn with `sudo systemctl start acorn` and run `acorn doctor`.

Keys that are now [settings](#settings): the whole `owner`, `briefing`, `thinking`, `wake`, `news` and `approval` blocks, `memory.daily_tokens`, `notify.max_per_hour` and `notify.quiet_hours`.

Keys whose numbers are now built in: the whole `presence`, `context` and `agent` blocks, `runtime.run_timeout_seconds`, `memory.batch_tokens`, `memory.context_tokens`, `memory.history_tokens`, `watch.max_checks_per_tick`, `workstation.command_timeout_seconds`, `workstation.max_output_bytes`, and under `web_access` the keys `user_agent`, `timeout_seconds`, `max_response_bytes`, `search.timeout_seconds` and `search.max_results`. A model outside the [model table](#model-configuration) needs `providers[].context_window_tokens`.

Keys of earlier releases:

- `agent.system_prompt` (put lasting instructions in the persona)
- the whole `triggers` block (webhooks and crons; ask the agent for a recurring commitment instead)
- `tools.mutation` and `tools.run_command`
- `mcp.providers[].tool_safety` (use the `approval.require` setting)
- the whole `browser` block (the workstation runs the browser)
- the whole `skills` block (skills live in the database)
- `owner.name` (set it in the app or with `acorn owner set`)

Earlier data under `~/.acorn` (`facts/`, `history/`, `worldstate/`, `vectors.db`, `skills/`) is no longer read. Delete it once you no longer need it.

`{storage_dir}/persona.md` is no longer read either; the upgrade starts with the default persona. If you had edited the file, save it as the persona and then delete it:

```bash
acorn persona set /srv/acorn/workspace/persona.md
```

The same goes for `persona.md` in a snapshot made before this upgrade, after restoring it.

## 12. Knowledge Base and Sharing

The agent keeps longer material in a knowledge base of markdown notes stored in the SQLite database. Every write keeps a new revision with the run that made it, and personal memory cites the exact revision it learned from. `acorn doctor` shows the note count and where shared images are kept.

Share from any Android app to get something into it: pick Acorn in the system share sheet, add a note if you like, and send. The backend opens a new conversation for the share, and the agent fetches the link, writes a note under `inbox/` and replies with its path. Open the conversation to tell the agent more about it. Images are stored under `attachments/` in the storage directory, and the agent sees the picture itself whenever that conversation is in its context; the app shows them in the shared conversation and inside the notes that embed them. The app's 知识 tab lists recent notes, searches them, and opens a note to read. Delete a note by swiping it left in the list or from its ⋯ menu: like deleting a conversation, it makes Acorn forget every revision of the note and what was drawn from it. Images the note embeds stay with the conversation that shared them.

### Files in Conversations

Tap + next to the message box to send the agent photos or files with your message. 图片 sends photos resized for viewing; 文件 sends any file unchanged. The agent sees supported pictures (JPEG, PNG, WebP, GIF up to 10 MiB) directly, and every file is copied to its workstation under `~/uploads/`, where it can unpack, read or convert it. Ask for a file in return, such as a public-domain book to find and download, and the agent sends it from its workstation with `send_file`: it appears under the reply, where a tap opens it in another app and the download button saves it to your phone. Files stay with their conversation and are deleted with it.

```yaml
files:
  max_upload_mib: 50   # largest file you can send; 1 to 1024
  max_send_mib: 100    # largest file the agent can send you; 1 to 1024
```

When an answer is several items with the same fields or numbers worth comparing, such as what changed across releases, a reading list or a trend, the agent shows it as a view under its reply: tables, lists of cards, links, badges, images from the conversation or the knowledge base, and bar or line charts, drawn by the app in its own style. The backend checks every view before it is shown, including that its links pass the same web policy as the agent's fetches, and views are deleted with their conversation.

A view can also offer next steps as buttons or a list to pick from, for example 整理成笔记 after the agent reads a long article. Pressing a button or sending a choice puts it in the conversation as your message, the button's words or 选了：… with what you picked, and the agent starts on it. Each button and choice can be sent once; afterwards the view shows what you chose. The agent can update a view it showed earlier, and the app redraws it in place.

Tap the pin on a view to keep it on the Today page as a panel; a tap on the panel opens its conversation, and the pin takes it off again. The agent can keep a panel current without being woken: ask it to track something, such as the latest release of a repository, and it writes a skill package whose trigger checks on a schedule and prints the view's data. While the view is pinned, each check replaces its data, and the panel says when it last changed. Data the view cannot show leaves the panel as it was and counts as a failure of the trigger.

## 13. Watches and the Morning Briefing

Ask the agent to keep an eye on something and it creates a watch: "follow the Go blog", "tell me when golang/go has a new release", "watch the price on this page and tell me right away when it drops". The backend checks each watch on its own schedule (hourly by default, at least every 15 minutes) and only involves the model when something is new. The first check of a watch is the baseline, so what is already there is not reported.

Every morning with something to say, the agent writes a briefing in the Briefings conversation, shows it on the 今天 tab, and pushes a short summary. Items of your watches take turns for the briefing's 100 places, so one busy watch cannot crowd out the others; the rest wait for the next morning. It covers today's commitments, what it remembered from you since the last briefing, facts that take effect this week, last night's reflection, notes written since then, what changed on your watches, and phone notifications worth acting on. A morning with none of that is skipped: no briefing, no push, and the 今天 tab keeps showing the latest one. When something needs your attention on the server — no recent backup, memory jobs that failed after all retries, or memory jobs waiting for more than a day — the briefing says so with a suggested check; on a healthy server it says nothing about it. Watches you asked to hear about right away wake the agent in the conversation where you set them up instead; those wakes count toward `wake.daily_limit`, and changes past the limit wait for the briefing. The briefing comes at the `briefing.at` [setting](#settings) in your time zone.

```yaml
watch:
  rsshub_base_url: http://127.0.0.1:1200   # for rsshub:/route watches
  github_token: ${GITHUB_TOKEN}            # optional; raises GitHub's limit of 60 requests an hour
```

Sources a watch can follow:

- The news items a skill package trigger prints, for a news site without a feed: the agent writes a script that reads the site's list, and a news source of kind `trigger` follows it.
- RSS, Atom or JSON feeds by URL. Give the agent a site's address and it finds the site's feeds with `feed_discover`: the feeds the page names, links on it that look like feeds and common feed paths, each checked, with how many entries a day it carries and when it last published.
- Anything RSSHub can turn into a feed (X, Weibo, hot lists and many more) as `rsshub:/route`. Run your own RSSHub next to Acorn; public instances are often rate limited:

  ```bash
  podman run -d --name rsshub --restart always -p 127.0.0.1:1200:1200 docker.io/diygod/rsshub
  ```

  Set `watch.rsshub_base_url` to its address; the agent then checks a route with `feed_discover rsshub:/route` before following it. Watches and feed discovery reach that address even on loopback or a private network; every other loopback and private address stays blocked, and the agent's `web_fetch` does not reach it.

- A GitHub repository's releases or newly opened issues.
- Part of a web page picked by a CSS selector, such as a price; without a selector the page's main text. Pages that only render with JavaScript need the workstation, whose browser renders them.

A watch that keeps failing (five times in a row) is marked failing and listed in the briefing with its last error. Ask the agent to list, pause, resume or change your watches. `acorn doctor` shows how many watches there are and which are failing.

### News in the briefing

Ask the agent for the news you want each morning, for example "国内要闻、国际与财经、AI 与科技前沿，每天十条左右". It sets up sections with `news_sections`, each with a few lines of what you want from it, most important first, and adds news sources under them: watches in `news` mode. A source whose entries are issues, such as a daily AI digest, is marked as one.

In the background Acorn sorts what the sources publish into stories: each item gets a score from 1 to 5 for general importance and fit with its section, reports of the same event from different sources share a story, and each piece of news on an issue's page becomes an item of its own. Sorting calls the model and counts against its own daily budget, the `news.daily_tokens` [setting](#settings). Once the budget is spent, items wait for the next local day; raising the budget or lifting its cap sorts them the same day. Before each briefing the backend checks every news source again and waits for the sorting to catch up, then offers the model the stories with new items since the last briefing, by section and highest score first, with the previous briefing. The briefing opens with about ten stories across the sections, each a title, one sentence and a link, then 今日关注 with the three that matter most; the push carries those three. Stories it did not pick are dropped, and a story told the day before comes back only with something new. Ask "最近 AI 圈有什么" or "那个停火谈判后来怎样" in any conversation and the agent answers from the stories of the last two weeks with `news_search`. Items older than two weeks keep only what tells them apart, so a feed that still lists them does not bring them back. News sources are not part of memory, and the morning briefing lists items the sorting gave up on under System health.

Shortly after a briefing is written, the backend reads from it which stories it picked and which three are today's focus; the now page's briefing carries them. A story's link must be one of its items for the backend to recognize it; lines it cannot match are counted under System health in the next briefing. You can mark a picked story as more or less of what you want (`PUT /v1/news/stories/{story_id}/reaction`). Nothing runs right away: the next briefing sees your reactions of the last 30 days in each section and weighs them before anything else, and reactions are kept for 30 days.

A new news source starts on trial: Acorn fetches and sorts it, but the briefing leaves it out. Every Sunday night, and on the night a trial source finishes its first week, the night reflection looks at each source's last seven days: items a day, average score, how many scored 4 or 5, how many shared a story with another source, how many stories with its items briefings picked and how many of them were today's focus, and how many you marked more or less. The agent proposes taking a trial source in or stopping one the briefing takes, and each such change waits for your approval in the app. The same night it looks at your reactions of the week by section; when two or more point at the same kind of story, it proposes rewriting that section's preference, which also waits for your approval and shows the preference before and after. Ask in any conversation to try a source and it is added on trial; ask to take it in and you approve it once.

Pick sources someone already ranked: top stories, front pages and daily digests. A feed of hundreds of items a day in order of time gives the model only the last hour or two. Daily digests often appear after 08:00; set `briefing.at` after the ones you follow publish, or the briefing carries the previous issue. Say what you want changed in any conversation ("AI 板块少报融资", "加一个财经源"), and the agent edits the sections and sources; a section change you ask for takes effect at once.

## 14. Backup and Restore

The installer sets `backup.dir` to `/srv/acorn/backups` and enables `acorn-backup.timer`, which runs `acorn backup` every night at about 04:30 while Acorn keeps running. Each run writes a snapshot under `<backup.dir>/snapshots/<UTC time>/`: a consistent copy of the SQLite database, checked for integrity and schema version. Shared images, run artifacts and the files of conversations never change once written, so they are mirrored once into `<backup.dir>/attachments/`, `<backup.dir>/artifacts/` and `<backup.dir>/files/`. The newest `backup.keep` snapshots are kept (14 by default):

```yaml
backup:
  dir: /srv/acorn/backups   # empty turns backups off
  keep: 14
```

The morning briefing reports when the newest snapshot is more than 36 hours old or missing.

Back up by hand, or check the timer:

```bash
acorn backup
```

```bash
systemctl list-timers acorn-backup.timer
```

Restore a snapshot with Acorn stopped; the command refuses while the service has the storage open:

```bash
sudo systemctl stop acorn
```

```bash
acorn restore --from /srv/acorn/backups/snapshots/20261011T043000Z
```

```bash
sudo systemctl start acorn
```

The backups live on the same machine. Copy `/srv/acorn/backups` to another host or include it in your hypervisor's backups. `~/.acorn/acorn.yaml` and `~/.acorn/acorn.env` are not part of a snapshot; keep a copy of your config and secrets yourself.

The database records its schema version. A newer Acorn upgrades an older database on start; an older Acorn refuses a database written by a newer one, so restore a snapshot made before an upgrade together with the release that made it.

## 15. Agent Workstation

The agent has its own Linux computer: a Podman container run by `acorn-workstation.service`. It runs Debian with common command-line tools, Python and internet access; the agent works there as user `agent` with sudo. The agent uses it through two tools, loaded with `tool_search`: `shell` runs a bash command and `files` reads or writes a text file. `send_file` hands you a file from it, and files you send the agent are copied there under `~/uploads/`.

The installer installs Podman, builds the image from the release's `workstation/Containerfile`, adds `ACORN_WORKSTATION_TOKEN` to `acorn.env` and starts the service. The Acorn binary is mounted read-only into the container and serves the workstation API on `127.0.0.1:7810`; every request carries the token. The container cannot see Acorn's config, env, data or model keys.

```yaml
workstation:
  url: http://127.0.0.1:7810            # empty turns shell and files off
  token: ${ACORN_WORKSTATION_TOKEN}
```

A command may run for 300 seconds; output past 32 KiB keeps its start and end.

Files under `/home/agent` live in the `acorn-workstation-home` volume and survive upgrades; `acorn backup` does not include them. Packages installed with `apt` are lost when the image is rebuilt. `acorn doctor` shows whether the workstation answers. To start over with an empty home:

```bash
sudo systemctl stop acorn-workstation
sudo podman volume rm acorn-workstation-home
sudo systemctl start acorn-workstation
```

Work that stays on the workstation runs without approval. Commands and browser actions with consequences elsewhere wait for your approval when action review is on; see [Approval Rules and Action Review](#approval-rules-and-action-review).

### Browsers

The workstation runs its own Chromium on a virtual display, with the [BrowserSkill](https://github.com/Tencent/BrowserSkill) extension, and the agent drives it with the `bsk` command through `shell`. It is always available; the agent uses it for pages that need JavaScript or interaction, and the same browser renders `web_rendered` watches. `acorn doctor` shows whether it is connected.

You can watch and use this browser from the app: the globe on the 今天 tab opens Agent 的浏览器. Tapping the picture taps the page, dragging up or down scrolls it, and the bar at the bottom types into the field you tapped. To drag something on the page, such as a slider that asks you to prove you are human, start dragging sideways, or hold your finger still for a moment and then drag in any direction; the page gets your finger's movement as you made it, a moment later, and the picture is a little softer until you lift. While you watch, pages are laid out for your phone's screen.

When a task needs a site you have an account on, the agent notifies you; tapping the notification opens the site's sign-in page in the browser, over the conversation. Sign in, go back, and tell the agent you are done. Logins stay in the `acorn-workstation-home` volume across restarts and upgrades, and the agent can use every site signed in there; sign out in the same view to take a site back. Submitting forms, sending, buying, deleting or changing account settings still wait for your approval on your phone when action review is on.

### Skills

Skills follow the [Agent Skills](https://agentskills.io) format: a `SKILL.md` with `name` and `description` frontmatter, plus optional `scripts/`, `references/` and `assets/`. Acorn ships its own skills, BrowserSkill's `browser-skill`, and `skill-creator`, which teaches the agent to write good skills. Before each run, the files of the skills in use are copied to `~/skills/<name>/` on the workstation, where the agent reads them and runs their scripts; skills with files need the workstation.

To add a published skill, point the agent at it: it downloads the package to the workstation, reads it, and imports it once you approve. On the server you can install one yourself:

```bash
acorn skills import ./pdf.zip
```

`--replace` replaces a skill of the same name. `acorn skills list`, `acorn skills inspect <name>` and `acorn skills check` show what the agent has.

In the app, 设置 → 技能 lists every skill, retired ones included, and says which ones the next conversation leaves out and why. A skill's page shows its `SKILL.md`, its files, its package and every revision with who wrote it; a revision the agent wrote links to its conversation. Open a revision to see what it changed and how it differs from the one in use, both computed on the server. From there you can roll back to that revision, and on the skill's page retire or restore it. Each of these asks for confirmation and saves a new revision by you on top of the one you read; if the agent changed the skill meanwhile, the app reloads it and you confirm again. Your changes need no approval, and the next run uses them. To write or install skills, ask the agent; it does so with `skill_write` and `skill_import`.

#### Packages with tools and triggers

A package can bring its own tools and wake the agent on its own schedule. An `acorn-package.yaml` beside `SKILL.md` declares a stdio MCP server, triggers, or both; each runs on the workstation from the package's directory. The workstation image has `uv` and Node.js, so the usual `uvx` and `npx` commands work:

```yaml
mcp:
  command: uvx
  args: [mcp-server-fetch]
triggers:
  - name: price
    schedule: "*/30 * * * *"      # cron, in owner.timezone
    run: scripts/check_price.py   # a command, run from the package's directory
    deliver: immediate            # or digest
secrets: [PRICE_API_KEY]
```

Unknown fields are rejected. When the agent writes or imports such a package, the approval shows the server's command, each trigger's schedule and command, and the secrets they need before the skill itself. Like everything the package runs, they run as the same user as the agent's own commands.

While the skill is in use, its MCP server runs on the workstation, restarts when the skill gets a new revision and stops when the skill is retired; its tools are named `mcp__<skill>__<tool>` and appear only in runs that have the skill. Like any MCP tool they meet your approval rules first, so with the default `mcp__*` in `require` every call waits for your approval. A package with an MCP server cannot take the name of a provider in `mcp.providers`, and Acorn refuses to start when you later add a provider named like one.

A trigger's command runs at each time of its schedule, bounded by the 300 seconds a command may run. Each line it prints is an event, `{"key": "...", "text": "..."}`, a news item for the news source that follows the trigger, `{"item": {"key": "...", "title": "...", "url": "...", "summary": "...", "published": "..."}}`, or data for the pinned views that name the trigger as their source, `{"view_data": {...}}`; printing nothing means nothing happened, and the model is not called. Acorn handles each key of a trigger once, so the command can print everything it still sees and only new keys count. Put the deterministic check in the command: compare the price with a threshold, list the items not seen yet. Events of an `immediate` trigger start a run in the package's own conversation, named after the skill, and the agent loads the skill to handle them; these runs count toward `wake.daily_limit` and `wake.daily_tokens`, and events over the limit, or whose run could not start, go to the next morning briefing. Events of a `digest` trigger only go to the briefing. Events are kept as external sources the agent can cite; like phone notifications, nothing in them changes the agent's rules. A trigger whose command fails five times in a row is listed under System health in the briefing and retries after a growing wait. Retiring the skill stops its triggers.

`secrets` lists environment variables the package needs. Set each on the skill's page in the app, under 密钥, or on the server, where the value is read from standard input or typed without echo:

```bash
acorn skills secret set price-watch PRICE_API_KEY < key.txt
```

Secrets are write-only: the app shows which ones are set, never a value, and can clear one. The value is stored in the database and kept in backups. It goes only to the package's MCP server and trigger commands, as an environment variable; the model, approvals, the app and the logs never see it, but commands the agent runs on the workstation run as the same user and can read it. Until every secret is set, the skill and its tools stay out of runs and its triggers do not run. The skill's page in the app and `acorn doctor` read the same status: each package's server (ready, not running yet, failed with the reason, or the secrets it still needs, under Package tools in doctor) and each trigger (its next time, its failures, or the secrets it still needs, under Package triggers).

`uvx` and `npx` fetch the server's code the first time it starts, so the first run that uses a new package can wait up to 90 seconds; if the server is not up by then, that run goes without the skill. The fetched code is not what you approved; judge where a package comes from before you approve it.

### Approval Rules and Action Review

Every tool call meets your rules first. They are tool-name glob patterns in three [settings](#settings): `approval.require` pauses a call for your approval on the phone, `approval.block` never runs it and tells the agent the rules refused it, and `approval.allow` runs it without review. `block` wins over `require`, and `require` over `allow`; `require` and `block` cannot match `ask_operator`, which already waits for you. In the app you pick tools from the list or type a pattern. A change applies from the next run.

A skill the agent writes becomes part of every later run's instructions, and an imported skill can carry scripts, an MCP server and triggers that run on the workstation, so `approval.require` holds `skill_write`, `skill_import` and `skill_retire` by default; the approval shows the package's MCP server command, its triggers and its secret names, then the skill's `SKILL.md` and the files it adds. Move them to `approval.allow` to let the agent keep its skills on its own.

Action review judges the shell commands, browser actions and MCP calls no rule settles. It uses Jev, TypeSafe's decision model: create a key at [typesafe.ai](https://typesafe.ai), add it to `~/.acorn/acorn.env` and restart Acorn.

```dotenv
TYPESAFE_API_KEY=your-typesafe-key
```

```yaml
review:
  api_key: ${TYPESAFE_API_KEY}   # empty turns review off
  model: jev-preview
  timeout_seconds: 20
```

Commands that only read, such as reading a page with `bsk observe`, run without review. For a click, fill, key press or select, Acorn sends Jev the element's name and the page it is on, as the agent last observed them, together with what you asked for. Calls that would pay, send, publish, delete, follow, change settings or otherwise change something outside the workstation wait for your approval, and the approval card says what the call does and why. A click on a checkout, payment or security page, text that tries to talk the reviewer into allowing it, an element the agent never observed on that page, and a review that fails also wait for you.

The calls you are asked about, their target and page, and your request go to TypeSafe. `acorn doctor` shows whether review works.

## 16. Current Limits

- Host commands are host dependencies. If the model tries to run a command that is not installed on the VPS, the command fails explicitly when used.
- Web search requires a configured Exa API key. Browser actions require the agent workstation.
- The mobile app refreshes backend truth through `/v1/inbox`, thread messages, and RunEvent cursors. Push needs your own Firebase project and an APK built with its values; there is no APNs support.
- Autonomous wakes share the daily count and optional token limits in the settings.
- Watches cannot follow pages that need a login. GitHub watches cover releases and newly opened issues only.
- Knowledge search matches words (full-text); there is no semantic search yet. The text of a note describes a shared image; search and memory read that text, not the picture.
- Mobile is a remote control surface. It does not execute runs locally, own runtime truth, or merge offline runtime state.

## Idle Thinking and Phone Notifications

Night reflection runs once per owner-local day at the `thinking.night_at` [setting](#settings), and idle thought at each time in `thinking.wander_at`. It reviews changed evidence, understandings awaiting review, open thoughts, concerns and unfinished appointment occurrences in the Thoughts thread. `think` records or ends a thought; `concern` records progress and review time; `settle` completes an occurrence with execution evidence. Each idle-thought slot advances one due open concern, the one revised longest ago, in the conversation where it was raised, so the advance continues from earlier progress there. A concern whose conversation has a run still working or waiting for your approval waits for a later slot. Deleting a conversation drops the concerns raised in it.

When you ask for something to be made, such as a document of a project's recent changes, the concern records its outcome: what you get and how to tell it is done. Idle thought then works on it on the workstation, in `~/work/<concern id>/`, and revises the concern after each step; a call that needs your approval waits for it and continues once you approve. The result arrives in the conversation where you asked, with one push, and the concern waits for you. Only your confirmation in that conversation resolves it. Deleting that conversation also removes the concern's directory from the workstation before the next run.

Set a positive `wake.daily_tokens` to impose a token budget on autonomous wakes. The budget counts provider-reported usage of successful main-model calls in autonomous runs, by the owner's local day. It is checked before starting a run; an already running task can exceed the remaining budget. Summarization calls, failed streams and calls without usage are not included. Owner messages, captures and morning briefings are outside this budget. Run `acorn doctor` to inspect today's wake count, reported tokens, calls without usage and notification counts by app.

In the app's settings, under **手机通知**, read the data destination, grant notification access, and choose individual apps under **转发哪些应用**. The selection starts empty. Acorn captures ordinary notifications from those apps while paired, stores at most 500 on the device, and starts uploading within one minute of the first arrival. Each request contains at most 100 entries. Failed requests retain their batch and show an error with **重新上传**. Disconnecting or pairing another identity clears waiting notifications; turning an app off removes its waiting entries. Android notification access remains under your control in system settings.

Notifications do not wake the agent. The latest ten received within six hours can appear in its current context; up to fifty since the previous morning briefing appear in the next briefing input, with a count for additional entries. Briefing instructions select actionable items and omit marketing, passwords and verification codes. Raw notifications are removed after seven days. Content already included in conversation records, context snapshots or notes remains with those records. Selected notification text is sent to your server and model provider.

Notification capture uses Android's [NotificationListenerService](https://developer.android.com/reference/android/service/notification/NotificationListenerService). Device background restrictions can delay delivery; waiting entries are retried when the listener reconnects, the app opens, another notification arrives or you choose retry.

Android 15 can redact notification text classified as sensitive, including detected one-time passwords. Acorn receives the content Android exposes; hidden content remains hidden. Read protected details in the source app. See [Android 15 notification protections](https://developer.android.com/about/versions/15/behavior-changes-all#otp-redaction).
