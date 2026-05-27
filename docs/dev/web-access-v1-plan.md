---
doc_type: implementation-plan
slug: web-access-v1-plan
component: web-access
status: implemented
summary: Phased implementation plan for Web Access v1
tags: [runtime, tools, web, browser, plan]
last_reviewed: 2026-05-21
related_docs:
  - docs/dev/web-access-v1.md
---

# Web Access v1 Plan

Status: implemented on 2026-05-21.

## Scope

Implement the approved Web Access v1 design:

- `web_fetch`
- `web_search` with Tavily
- one deferred native `browser` action tool backed by `chromedp`
- shared URL/network policy
- Markdown artifacts and compact mobile-safe projections
- builtin workflow skill for progressive loading

The plan permits destructive refactoring when it is directly required by the
approved design. It does not use compatibility aliases, mock success paths,
silent fallback, or external CLI wrappers.

## Phase 1: Contract, Config, And URL Policy

Status: implemented on 2026-05-21.

### Outcome

The repo has stable config and contract primitives for outbound web access and
browser interaction, plus a tested shared URL policy. No half-implemented tool is
registered yet.

### Build

- Extend config:
  - `web_access`
  - `web_access.search`
  - `browser`
- Expand `web_access.search.api_key` from environment variables.
- Resolve `browser.executable_path` relative to config dir when it is path-like.
- Validate config shape without requiring Tavily key or browser executable for
  global execution readiness.
- Extend tool taxonomy:
  - `ResourceScopeWeb`
  - `ResourceScopeBrowser`
  - `ToolSideEffectWebRead`
  - `ToolSideEffectBrowserRead`
  - `ToolSideEffectBrowserInteract`
- Add shared URL policy package:
  - scheme validation.
  - host parsing.
  - DNS resolution seam for tests.
  - public/private/loopback/link-local/metadata rejection.
  - explicit private-network opt-in for RFC1918/ULA only.
- Update example configs and docs.

### Done When

- Default config carries Web Access defaults.
- Example configs load.
- Unknown config fields still fail through strict YAML.
- ToolContract accepts web/browser scopes and side effects.
- URL policy tests cover public, private, loopback, link-local, metadata,
  unsupported scheme, DNS result, and private-network opt-in behavior.

### Verify

```bash
go test ./internal/config ./internal/tooling ./internal/webaccess
git diff --check
```

Implemented verification:

```bash
go test ./internal/config ./internal/tooling ./internal/webaccess ./internal/tools ./internal/runtime ./internal/app
make format-check
make lint
git diff --check
```

## Phase 2: `web_fetch`

Status: implemented on 2026-05-21.

### Outcome

The model can load `web_fetch`, fetch a public URL, and receive bounded
Markdown-backed evidence with raw and Markdown artifacts.

### Build

- Add `internal/webaccess` fetcher service.
- Implement context-bound HTTP client with timeout and redirect validation.
- Add extraction package:
  - `codeberg.org/readeck/go-readability/v2`
  - `github.com/JohannesKaufmann/html-to-markdown/v2`
  - `golang.org/x/net/html`
- Support extraction modes:
  - `auto`
  - `readability`
  - `full_page_markdown`
  - `visible_text`
- Persist raw response artifact and Markdown artifact.
- Register deferred `web_fetch` native tool with explicit contract.
- Add focused `httptest` coverage.

### Done When

- `web_fetch` fails loudly for blocked URLs, unsupported schemes, network
  failures, response limit breaches, and extraction failures.
- `auto` extraction reports the actual method and warning.
- `readability` mode fails instead of switching method.
- Tool result contains source identity and artifact refs.

### Verify

```bash
go test ./internal/webaccess ./internal/tools ./internal/runtime ./internal/store/sqlite
```

Implemented verification:

```bash
go test ./internal/webaccess ./internal/tools ./internal/runtime ./internal/tooling ./internal/app
```

## Phase 3: `web_search`

Status: implemented on 2026-05-21.

### Outcome

The model can load `web_search`, query Tavily, and receive filtered candidate
URLs with raw provider evidence.

### Build

- Add Tavily client with context and timeout.
- Read `web_access.search` config.
- Persist raw provider response artifact.
- Filter provider result URLs through the shared URL policy.
- Return structured result metadata and filtered count/reasons.
- Register deferred `web_search` native tool with explicit contract.
- Add fake-server tests; do not call Tavily in tests.

### Done When

- Missing API key produces a failed tool result.
- Unsupported provider fails config validation.
- Search snippets are documented as discovery-only, not final evidence.

### Verify

```bash
go test ./internal/webaccess ./internal/tools ./internal/runtime
```

Implemented verification:

```bash
go test ./internal/webaccess ./internal/tools ./internal/runtime ./internal/tooling ./internal/app
```

## Phase 4: `browser`

Status: implemented on 2026-05-21.

### Outcome

The model can load a single deferred `browser` action tool, operate a
backend-owned run-scoped Chromium session, and persist only explicit artifacts.

### Build

- Add browser runtime service backed by `chromedp`.
- Consume `browser` config and shared URL policy.
- Manage run-scoped browser sessions and temp profile cleanup.
- Implement actions:
  - `status`
  - `open`
  - `tabs`
  - `scan`
  - `snapshot`
  - `click`
  - `fill`
  - `press`
  - `select`
  - `screenshot`
  - `console start/list/stop`
  - `network start/list/stop`
  - `close`
- Keep snapshot refs in memory only.
- Persist scan Markdown and screenshot artifacts.
- Register deferred `browser` native tool with `never_parallel` execution.
- Add unit tests for session/ref/action behavior.
- Add opt-in integration tests gated by `ACORN_BROWSER_TEST_CHROMIUM`.

### Done When

- Missing executable path is a failed tool result.
- Ref expiry is a failed tool result requiring a new snapshot.
- Selector matches of zero or multiple elements fail.
- No raw JS/CDP/cookie action is exposed.
- Run cleanup removes temp browser state.

### Verify

```bash
go test ./internal/tools ./internal/runtime ./internal/store/sqlite
ACORN_BROWSER_TEST_CHROMIUM=/path/to/chromium go test ./internal/tools -run TestIntegrationBrowserOpenSnapshotScan
```

Implemented verification:

```bash
go test ./internal/tools ./internal/runtime ./internal/tooling ./internal/app
```

The Chromium integration test remains opt-in because the release package does
not bundle a browser executable.

## Phase 5: Skill, Projection, And Docs

Status: implemented on 2026-05-21.

### Outcome

The model learns Web Access workflow through a builtin skill, and remote clients
see compact product-grade activity and artifacts instead of raw debug streams.

### Build

- Add builtin `web-browser-research` skill.
- Encode SOP:
  - search discovers.
  - fetch produces evidence.
  - browser handles JS/interaction.
  - scan reads content.
  - snapshot locates controls.
  - screenshot proves visual state.
- Keep the skill as workflow guidance only; execution remains native tools.
- Update RunDetail/mobile projection only if current artifact/activity fields are
  insufficient.
- Update user and developer docs.
- Document that release packages do not bundle Chromium.

### Done When

- Browser/Web tools do not appear in initial context unless loaded.
- Mobile projection stays compact.
- User docs explain Tavily key, Chromium install, and explicit unsupported
  boundaries.

### Verify

```bash
go test ./internal/skills ./internal/runtime ./internal/app ./internal/web
python3 mobile/tool/generate_openapi_client.py --check
make lint
make format-check
git diff --check
```

Implemented verification:

```bash
go test ./internal/skills ./internal/runtime ./internal/app ./internal/web
python3 mobile/tool/generate_openapi_client.py --check
make lint
make format-check
git diff --check
```
