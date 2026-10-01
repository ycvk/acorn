---
doc_type: architecture
status: current
last_reviewed: 2026-10-01
slug: tools
---

# Tools

## Contract And Registry

Every tool is described by a `core.ToolContract` (name, source, kind, category, loading policy, execution policy). Incomplete contracts fail catalog construction. Native tools are registered into the unified `core.ToolRegistry` at startup (`tools.RegisterNativeTools`); MCP tools are registered into the same registry when their provider connects, namespaced by provider. MCP resource/prompt tools are built per run from the MCP manager.

- Built-in tools (`internal/tools/builtin_registry.go`, all eager): `memory_search`, `memory_read_file`, `memory_list_files`, `memory_create_file`, `memory_replace_span`, `remember`, `search_runs`, `worldstate_update`, `worldstate_load`, `skill_list`, `skill_view`, `skill_create`, `load_tools`, `ask_operator`.
- Configured local tools (`internal/tools/configured.go`): workspace read (`read_file`, `list_files`, `search_text`, `inspect_git_status`, `inspect_git_diff`, `git_summary`), workspace mutation (`create_file`, `replace_span`, `apply_unified_patch`, `multi_edit`, `rollback_workspace_checkpoint`), `run_command`, `run_verification`, artifacts (`artifact_write`, `artifact_read`, `artifact_list`), and web access (`web_search`, `web_fetch`, `browser`).
- Deferred tools (`web_search`, `web_fetch`, `browser`) become visible only after the model calls `load_tools`.

A tool whose backing service is absent (no workspace, no artifact store, no browser executable) is registered for visibility but resolves to no instance.

## Execution

`internal/tools/dispatch` owns scheduling. `read_only` tools run in parallel. `serial` tools with a `PathArg` run in parallel only when their paths do not overlap; `serial` tools without paths run alone. Unknown/deferred tool calls and ordinary tool failures are model-visible failed tool results, not run failures.

`tools.ClassifyRisk` is a rule-based gate: a high-risk tool call is intercepted before execution and the model is told to obtain approval through `ask_operator`.

## Workspace, Commands, Artifacts

- Mutation tools create a scoped checkpoint; `rollback_workspace_checkpoint` restores it explicitly.
- `run_command` runs one explicit command (no persistent shell session); cancellation kills the whole process group.
- `run_verification` runs test/lint/build checks and returns `kind`, `status`, `exit_code`, `summary`, and stdout/stderr artifact ids; a non-zero exit is a failed tool result.
- Artifacts are run evidence: content lives under runtime storage, SQLite `artifacts` stores metadata. Artifacts are not memory.
- `ask_operator` creates a pending action; the answer returns to the model as the tool result after resume.

## Web Access

`internal/webaccess` provides the fetcher, Tavily search client, readability + HTML-to-Markdown extractor, and the shared outbound URL policy; `internal/tools` exposes them as tools.

- `web_search`: Tavily only; missing `web_access.search.api_key` is a failed tool result, not a startup failure.
- `web_fetch`: HTTP(S) GET, readable Markdown extraction, raw + Markdown artifacts. No JavaScript, cookies, or silent upgrade to `browser`.
- `browser`: one action tool over a run-scoped chromedp session against an operator-installed Chromium (`browser.executable_path`). Actions: `status`, `open`, `tabs`, `scan`, `snapshot`, `click`, `fill`, `press`, `select`, `screenshot`, `console`, `network`, `close`. No raw JavaScript/CDP, cookie access, or persistent profiles.
- URL policy (shared by all three): only `http`/`https`. Unspecified, loopback, link-local, and multicast addresses are always rejected; private (RFC 1918 / ULA) addresses are rejected unless `web_access.allow_private_networks` is set. The check applies to resolved DNS results and redirects too.
