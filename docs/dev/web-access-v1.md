---
doc_type: dev-plan
slug: web-access-v1
component: web-access
status: approved
summary: Web search, fetch, and backend-owned browser tools for Acorn runs
tags: [runtime, tools, web, browser, artifacts]
last_reviewed: 2026-05-21
---

# Web Access v1

Status: approved for implementation on 2026-05-21.

Implementation status: Web Access v1 P0 is implemented on 2026-05-21. The
implemented runtime surface is `web_search`, `web_fetch`, and the single
deferred `browser` action tool described below.

## Goal

Web Access v1 gives Acorn runs first-class access to public web sources and
interactive browser state without turning mobile into a debug trace viewer or
making the model rely on ad hoc shell commands.

The capability surface is three deferred native tools:

- `web_search`: discover candidate public web sources through a configured
  search provider.
- `web_fetch`: fetch a known URL over HTTP(S), extract readable Markdown, and
  persist source artifacts.
- `browser`: operate a backend-owned, run-scoped Chromium session for pages that
  need JavaScript, interaction, screenshots, console, or network inspection.

These tools are runtime tools. They are not a mobile remote-browser API, not an
MCP gateway, and not an external CLI wrapper.

## Current Baseline

Acorn already has the runtime foundations this feature must reuse:

- `ToolContract` is the loading, execution, result, boundary, and projection
  contract.
- `ToolExecutionScheduler` owns tool ordering and parallel policy (read_only / serial).
- Tool results stay in the message stream; observation masking replaces old results with placeholders.
- Runtime artifacts are run evidence and are stored through the artifact store.
- `/v1` and the generated mobile client expose backend projections; mobile does
  not own runtime truth.
- `load_tools` already supports deferred tool definitions.

Native Developer Tools v2 intentionally excluded browser/CDP. Web Access v1 is a
new feature line, not a retroactive expansion of that P0 scope.

## Tool Boundaries

### `web_search`

`web_search` discovers candidate public URLs.

P0 provider:

- Tavily only.
- `web_access.search.provider` keeps the provider name explicit, but unsupported
  providers fail validation.
- Missing `web_access.search.api_key` is not an execution-readiness failure for
  Acorn as a whole. Calling `web_search` without it returns a failed tool result.

`web_search` output is structured result metadata:

- title
- URL
- snippet
- published time when provided
- rank
- provider metadata needed for diagnostics
- provider raw-response artifact id when persisted

Search snippets are not final evidence. Factual answers must follow with
`web_fetch` or `browser.scan` unless the user only asked for search results.

Returned URLs must pass the shared Web Access URL policy before the model sees
them as usable results. Filtered results are summarized by count/reason, not
returned as alternate targets.

### `web_fetch`

`web_fetch` fetches a specific HTTP(S) URL and produces source-backed evidence.

It does:

- HTTP GET with context, timeout, redirect handling, and shared URL policy.
- raw response artifact.
- readability-compatible extraction.
- cleaned HTML to Markdown conversion.
- Markdown artifact and bounded preview.
- metadata such as final URL, title, site name, published time, fetched time,
  content type, content length, extraction method, and content hash.

It does not:

- execute JavaScript.
- keep cookies or login state.
- click forms.
- take screenshots.
- bypass anti-bot systems.
- silently upgrade to `browser`.

If a page requires JavaScript, `web_fetch` reports that clearly. The model may
then load and call `browser`.

### `browser`

`browser` is one native action tool, not many tool names.

P0 actions:

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
- `console` with explicit `start`, `list`, `stop`
- `network` with explicit `start`, `list`, `stop`
- `close`

P0 does not expose:

- arbitrary JavaScript evaluation.
- raw CDP.
- CSP stripping.
- cookie read/write.
- persistent browser profiles.
- user Chrome profile attach.

Internally the implementation may use CDP, but the model only sees high-level,
ledger-friendly actions.

## Browser Runtime

P0 browser runtime is `chromedp` controlling an operator-provided
Chrome/Chromium executable.

Acorn does not bundle Chromium in the release tarball. The operator installs
Chrome/Chromium and configures:

```yaml
browser:
  executable_path: /usr/bin/chromium
  headless: true
  default_timeout_seconds: 20
```

Browser sessions are run-scoped:

- first `browser` call in a run creates the run browser session.
- subsequent browser actions in that run reuse the session.
- run end, failure, interruption cleanup closes the browser and removes temp
  profile state.
- cookies, localStorage, sessionStorage, element refs, and snapshot cache do not
  persist across runs.

Only explicit artifacts persist: scan Markdown, screenshots, PDFs later if
implemented, and selected debug artifacts later if implemented.

## Scan And Snapshot

`browser.scan` and `browser.snapshot` are separate by design.

`scan` gives the model page content:

- reads rendered page content.
- extracts readable Markdown with the same extraction pipeline as `web_fetch`.
- writes a Markdown artifact.
- returns a bounded Markdown preview and artifact metadata.

`snapshot` gives the model operation targets:

- returns a compressed list of visible, actionable elements.
- creates run-local element refs such as `@e1`.
- binds refs to browser session, tab, URL, and snapshot generation.
- stores only the bounded tool result in the message stream.
- does not persist full DOM, full accessibility tree, backend DOM node ids, or
  run-end snapshot dumps.

`click`, `fill`, and `select` prefer `ref`. P0 also permits one explicit CSS
`selector` as an escape hatch. The caller must provide exactly one of `ref` or
`selector`. Selector matches of zero or multiple elements fail instead of
guessing.

## Extraction

Content extraction must use mature libraries rather than handwritten page
heuristics.

Planned extraction stack:

- `codeberg.org/readeck/go-readability/v2`
- `github.com/JohannesKaufmann/html-to-markdown/v2`
- `golang.org/x/net/html`

Default artifact format is Markdown, not plain text or raw HTML. Markdown keeps
headings, lists, tables, links, and code blocks useful to the model while
avoiding raw page noise.

Supported extraction modes:

- `auto`
- `readability`
- `full_page_markdown`
- `visible_text`

`auto` may fall from readability to full-page Markdown, but the actual method
and any warning must be explicit in the tool result and artifact metadata.
Explicit `readability` failure fails the tool call; it does not silently switch
methods.

## Evidence And Citation

Every webpage content artifact has two identities:

- user-facing source identity: URL, final URL, title, site name, published time
  if available, fetched/scanned time.
- Acorn evidence identity: tool result ref, artifact id, extraction method,
  content hash, and raw artifact id when available.

Assistant answers cite URLs and titles for users. Artifact ids are internal
runtime evidence and are exposed through RunDetail/mobile artifact views when
needed.

## Network Policy

`web_fetch`, `web_search` returned URLs, and `browser.open` share one outbound
URL policy.

Default allowed targets:

- `http`
- `https`
- public DNS/IP targets only

Default rejected targets:

- `localhost`, `.localhost`, loopback, unspecified, multicast.
- RFC1918 and IPv6 private/ULA addresses unless explicitly allowed.
- link-local addresses, including cloud metadata IPs.
- unsupported schemes such as `file`, `ftp`, `gopher`, and Unix sockets.
- DNS results or main-frame redirects that resolve to rejected targets.

P0 does not implement `robots.txt` crawler policy and does not attempt anti-bot
bypass. Acorn is fetching explicit user/model requested URLs, not crawling
sites recursively.

## Configuration

Existing `web` remains inbound Acorn server configuration:

```yaml
web:
  listen_addr: 127.0.0.1:8080
  allowed_origins: []
```

Outbound web access uses `web_access`:

```yaml
web_access:
  user_agent: "Acorn/0.x (+https://github.com/ycvk/acorn)"
  timeout_seconds: 20
  max_response_bytes: 10485760
  allow_private_networks: false
  search:
    provider: tavily
    api_key: ${TAVILY_API_KEY}
    timeout_seconds: 10
    max_results: 10
```

Browser runtime uses `browser`:

```yaml
browser:
  executable_path: ""
  headless: true
  default_timeout_seconds: 20
```

`browser.executable_path` is optional at config load time. Calling `browser`
without it returns a failed tool result. Acorn startup must not fail just because
browser access is unconfigured.

## Loading And Skills

All Web Access v1 tools are deferred:

- `web_fetch`: deferred reason `web_access`
- `web_search`: deferred reason `web_access`
- `browser`: deferred reason `web_access`

The builtin `skills/web_browser_research/SKILL.md` workflow skill guides when to
call `load_tools` and how to sequence search, fetch, browser scan, snapshot,
interaction, and screenshots. The skill is SOP only. It does not execute an
external CLI or own runtime truth.

## Mobile Projection

P0 does not add `/v1/browser/*` APIs.

Mobile consumes existing run activity and artifact projection:

- visible compact rows for open, scan, screenshot, explicit console/network, and
  failures.
- low/no visible rows for successful snapshot and low-level interaction steps.
- artifact previews for Markdown and screenshots.

Raw browser lifecycle, CDP logs, full snapshots, and network/console spam are
diagnostic data, not the default mobile product surface.

## `agent-browser-cli`

`agent-browser-cli` is a useful reference for workflow semantics:

- `scan` versus `snapshot`.
- short-lived element refs.
- screenshots/PDF as files.
- wait and monitor separation.
- real browser profile attach as a local developer workflow.

It is not the P0 implementation base. Acorn P0 does not depend on a Chrome
extension, broad `<all_urls>` permissions, user Chrome profile attach, CSP
stripping, or an external loopback daemon.

An optional future adapter may support local desktop Chrome attach for developer
workflows. That adapter is outside Web Access v1 P0.

## Explicit Non-Goals

P0 does not implement:

- MCP browser provider.
- external CLI browser foundation.
- persistent browser profiles.
- cookie management.
- raw JavaScript or raw CDP tool exposure.
- Browserbase/cloud browser provider.
- bundled Chromium release assets.
- cross-run web cache.
- recursive crawler.
- mobile remote browser controller.
- repo context, LSP, or code intelligence.
