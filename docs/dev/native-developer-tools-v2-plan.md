---
doc_type: implementation-plan
slug: native-developer-tools-v2-plan
component: native-developer-tools
status: completed
summary: Native Developer Tools v2 P0 的分阶段推进计划
tags: [runtime, tools, developer-experience, plan]
last_reviewed: 2026-05-20
related_docs:
  - docs/dev/native-developer-tools-v2.md
---

# Native Developer Tools v2 P0 Plan

Status: completed on 2026-05-20.

## Scope

本计划只实现 P0：

- 持久 terminal/process session
- operator question/decision tool
- run artifact store
- edit/test/git workflow tools

明确不做 repo context、LSP、repo map、codeintel、browser/CDP、ADB、scheduler、plugin gateway。

## Phase 1: Contract And Storage Foundation

Status: implemented 2026-05-20.

### Outcome

Tool taxonomy、SQLite schema 和 storage/domain ports 已有 P0 工具所需的稳定边界。Phase 1 不改 OpenAPI/mobile projection；后续工具只接入这些边界，不再发明局部 store 或临时事件形态。

### Build

- 扩展 `internal/tooling`:
  - `ResourceScopeProcess`
  - `ResourceScopeArtifact`
  - `ResourceScopeOperator`
  - process/artifact/operator side effects
  - contract validation tests
- 新增 artifact domain:
  - metadata record
  - file-backed content writer/reader
  - sha256/size verification
  - range read
  - run/session list
- 新增 terminal/process domain:
  - terminal session record
  - status enum
  - process group metadata
  - log refs
  - exit record
- 扩展 SQLite schema:
  - `artifacts`
  - `terminal_sessions`
  - `terminal_session_logs` or equivalent log metadata table
- 扩展 store validation，只验证当前 canonical schema。
- 扩展 `toolresult.SideEffectRef` usage so tool results can backlink to artifacts and terminal sessions.
- 写 architecture/dev docs linkage comments only where code would otherwise be ambiguous.

### Likely Files

- `internal/store/artifacts.go`
- `internal/terminalsession/`
- `internal/tooling/specs.go`
- `internal/tooling/contracts.go`
- `internal/toolresult/toolresult.go`
- `internal/store/sqlite/store_schema.go`
- `internal/store/sqlite/store_schema_validate.go`
- `internal/store/sqlite/store_*_test.go`

### Done When

- New contracts fail loudly when required fields are missing.
- SQLite validates the new canonical tables.
- Artifact metadata can be created/read/listed in tests.
- Terminal session metadata can be created/read/listed in tests.
- Tool result side effects preserve artifact/session refs round trip.

### Verify

```bash
go test ./internal/tooling ./internal/toolresult ./internal/store/sqlite ./internal/app ./internal/runtime
git diff --check
```

Implemented verification:

```bash
go test ./internal/tooling ./internal/store ./internal/store/sqlite ./internal/app ./internal/runtime
make lint
make format-check
git diff --check
```

## Phase 2: Artifact Tools

Status: implemented on 2026-05-20.

### Outcome

Acorn can persist run-scoped artifacts and expose them to the model and remote clients by id. Large logs, verification output, reports, JSON summaries, and diff snapshots no longer need to be stuffed into model context.

### Build

- Add `internal/store.ArtifactService` with file-backed content and SQLite metadata.
- Add native tools:
  - `artifact_write`
  - `artifact_read`
  - `artifact_list`
- Register tools through `RunnerFactory`/catalog with explicit `ToolContract`.
- Add artifact summaries to RunDetail/workbench projection.
- Add OpenAPI schemas and mobile generated client sync for artifact summaries.
- Ensure artifact write side effects are present in ToolResultLedger records.

### Likely Files

- `internal/store/artifacts.go`
- `internal/tools/artifact_tools.go`
- `internal/runtime/runner_factory_toolset.go`
- `internal/runtime/tool_specs.go`
- `internal/app/runtime_workbench_service.go`
- `internal/web/runtime_workbench_dto.go`
- `docs/openapi.yaml`
- `mobile/lib/src/api/acorn_api.dart`

### Done When

- A tool can write an artifact and returns `artifact_id`.
- A later tool can read a bounded range from that artifact.
- RunDetail lists artifact summaries for the run.
- Generated mobile client is in sync.

### Verify

```bash
go test ./internal/store ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store/sqlite
python3 mobile/tool/generate_openapi_client.py --check
git diff --check
```

Implemented verification:

```bash
go test ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store ./internal/store/sqlite ./internal/tooling
python3 mobile/tool/generate_openapi_client.py --check
flutter test
flutter analyze
make lint
make format-check
make test
git diff --check
```

## Phase 3: Terminal And Process Sessions

Status: implemented on 2026-05-20.

### Outcome

Acorn can start long-running or interactive terminal sessions, stream or poll their logs, send input/signals, and preserve their results as run evidence.

### Build

- Add terminal session service:
  - start command or shell
  - process group ownership
  - PTY for interactive session
  - stdout/stderr or PTY log files
  - status transitions
  - signal handling
  - close/finalize
- Add native tools:
  - `terminal_session_start`
  - `terminal_session_write`
  - `terminal_session_read`
  - `terminal_session_signal`
  - `terminal_session_close`
  - `terminal_session_list`
  - `process_status`
- Logs should be represented as artifacts or artifact-compatible log refs.
- Replace no existing `run_command` behavior. `run_command` remains the short command escape hatch.
- Ensure startup/finalization errors fail loudly.

### Likely Files

- `internal/terminal/` or `internal/processes/`
- `internal/tools/terminal_session_tools.go`
- `internal/tools/command_tool.go`
- `internal/runtime/runner_factory_toolset.go`
- `internal/runtime/tool_specs.go`
- `internal/store/sqlite/store_terminal_sessions.go`
- `internal/app/runtime_workbench_service.go`
- `internal/web/runtime_workbench_dto.go`

### Done When

- A run can start `make test` or a dev server as a persistent session.
- The model can read session output by offset/tail without full log injection.
- The model can interrupt/terminate the Acorn-owned process group.
- RunDetail shows active and terminal sessions with status and log refs.
- Session logs survive process exit and can still be read.

### Verify

```bash
go test ./internal/terminal ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store/sqlite
git diff --check
```

Implemented verification:

```bash
go test ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store ./internal/store/sqlite ./internal/tooling
python3 mobile/tool/generate_openapi_client.py --check
flutter test
flutter test test/api_client_test.dart
flutter analyze
make lint
make format-check
make test
git diff --check
```

## Phase 4: Operator Question Tool

Status: implemented on 2026-05-20.

### Outcome

The model can ask the operator a structured question through the backend/mobile control surface, and resume with a structured tool result.

### Build

- Extend pending action payload/decision contract for operator answers:
  - question
  - choices
  - freeform answer
  - selected choice
  - no separate notes field; freeform `answer` is the operator-authored text channel
- Add native tool:
  - `ask_operator`
- Implement tool as stateful interrupt backed by pending action truth.
- Extend `/v1/pending-actions/{action_id}:decide` request schema to carry answer payload.
- Update mobile pending action UI to answer choices/freeform, not only accept/decline.
- Emit persisted events for question created and answered.
- Return answered payload as the model-visible tool result on resume.

### Likely Files

- `internal/events/`
- `internal/app/pending_action_service.go`
- `internal/store/sqlite/store_pending_actions.go`
- `internal/tools/operator_tools.go`
- `internal/runtime/tool_specs.go`
- `internal/web/handlers_client.go`
- `internal/web/client_dto.go`
- `docs/openapi.yaml`
- `mobile/lib/src/api/acorn_api.dart`
- `mobile/lib/src/features/*pending*`

### Done When

- A run calling `ask_operator` becomes interrupted/pending with a visible mobile action.
- Mobile can answer with a choice or freeform value.
- Resumed run receives the answer as a tool result.
- Missing/decided/invalid pending action rows fail explicitly.

### Verify

```bash
go test ./internal/events ./internal/app ./internal/store/sqlite ./internal/tools ./internal/runtime ./internal/web
python3 mobile/tool/generate_openapi_client.py --check
cd mobile && flutter test
cd mobile && flutter analyze
git diff --check
```

Implemented verification:

```bash
go test ./internal/events ./internal/store/sqlite ./internal/app ./internal/tools ./internal/runtime ./internal/tooling ./internal/toolresult ./internal/web
python3 mobile/tool/generate_openapi_client.py --check
cd mobile && flutter test
cd mobile && flutter analyze
cd mobile && flutter build apk --debug
make lint
make format-check
make test
git diff --check
```

## Phase 5: Edit/Test/Git Workflow Tools

Status: implemented on 2026-05-20.

### Outcome

Acorn has higher-level workflow tools for common development loops without hiding the underlying diff, test command, checkpoint, or tool result evidence.

### Build

- Add `multi_edit`:
  - multiple file spans in one request
  - one mutation checkpoint
  - atomic application
  - verified changed paths/diffstat
- Add `run_verification`:
  - command plus verification kind: `test`, `lint`, `build`, `format_check`, `custom`
  - normalized status
  - stdout/stderr artifact refs
  - exit code
  - plan evidence backlink
- Add `git_summary`:
  - status
  - diffstat
  - changed paths
  - optional scoped diff artifact/ref
  - no stage/commit/merge behavior
- Keep `create_file`, `replace_span`, `apply_unified_patch`, `inspect_git_status`, `inspect_git_diff`, and `run_command` active.

### Likely Files

- `internal/tools/native_mutation_tools.go`
- `internal/tools/native_read_tools.go`
- `internal/tools/verification_tools.go`
- `internal/runtime/plan_evidence.go`
- `internal/runtime/act_node.go`
- `internal/app/runtime_workbench_service.go`
- `internal/web/runtime_workbench_dto.go`

### Done When

- Multi-file edit produces exactly one checkpoint and a clear diffstat.
- Verification command output is persisted as artifact-backed evidence.
- Git summary is available without parsing assistant prose.
- Plan evidence links back to tool result refs for edits and verification.

### Verify

```bash
go test ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store/sqlite
git diff --check
```

Implemented behavior:

- `multi_edit` accepts explicit line spans across one or more existing workspace files, validates all ranges before writing, rejects overlapping spans, writes through temp files, and records exactly one workspace mutation checkpoint.
- `run_verification` accepts `kind` (`test`, `lint`, `build`, `format_check`, `custom`) plus command/cwd/paths, returns normalized `passed`/`failed`/`timed_out`, and persists stdout/stderr as artifact refs.
- `git_summary` returns status entries, changed paths and diffstat, and writes an optional diff artifact when `include_diff=true`.
- Runtime evidence derives checkpoint/test/command/diff evidence from these tool results and backlinks evidence ids into the ToolResultLedger.

Implemented verification:

```bash
go test ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store/sqlite
git diff --check
```

## Phase 6: Final Contract Sync And Release Readiness

Status: implemented on 2026-05-20.

### Outcome

Docs, OpenAPI, generated mobile client, tests, and release packaging agree with the new P0 native tool surface.

### Build

- Update current architecture docs only after implementation is live.
- Update user/dev guides for new tools and mobile behavior.
- Ensure `/v1/tools` or capability projections expose new tools with correct health.
- Ensure installer/release packaging does not need new hidden runtime dependencies.
- If a new PTY dependency is added, verify Linux release build path explicitly.

### Done When

- OpenAPI and generated mobile client are synchronized.
- Mobile can display artifacts, terminal sessions, and operator questions where relevant.
- All repo gates pass.
- No temporary artifacts or unrelated local config are staged.

### Verify

```bash
make lint
make format-check
make test
python3 mobile/tool/generate_openapi_client.py --check
cd mobile && flutter test
cd mobile && flutter analyze
git diff --check
```

Implemented behavior:

- Architecture/dev docs describe artifact, terminal session, operator question, verification artifact, and git diff artifact side-effect truth.
- Capability projection exposes `multi_edit`, `run_verification`, and `git_summary`; tests assert the expanded tool catalog.
- `docs/openapi.yaml` and the generated mobile client required no schema change for Phase 5/6 tool names; the generated client check is clean.
- Phase 5 introduced no new package/release dependency. The existing PTY dependency still compiles for Linux amd64 in `internal/terminalsession`.

Implemented verification:

```bash
go test ./internal/tools ./internal/runtime ./internal/app ./internal/web ./internal/store/sqlite
make lint
make format-check
make test
python3 mobile/tool/generate_openapi_client.py --check
cd mobile && flutter test
cd mobile && flutter analyze
GOOS=linux GOARCH=amd64 go test -c ./internal/terminalsession -o /tmp/acorn-terminalsession-linux-amd64.test
git diff --check
```

## Execution Order

Recommended order:

1. Phase 1: contract/storage foundation
2. Phase 2: artifact tools
3. Phase 3: terminal/process sessions
4. Phase 4: operator question tool
5. Phase 5: edit/test/git workflow tools
6. Phase 6: contract sync and release readiness

Artifacts go before terminal sessions because long-running session logs and verification outputs need the same durable artifact model. Operator questions are isolated but should land before edit/test/git workflow becomes too autonomous, because the model needs a first-class way to ask for human decisions when a plan genuinely requires them.

## Risk Notes

- PTY behavior must be verified on macOS dev and Linux release targets. Unsupported platforms should report explicit tool health, not fallback to `run_command`.
- Terminal session cleanup must be process-group based. Killing only the parent process is not acceptable.
- Artifact paths must be storage-relative and opaque to clients. Do not leak absolute storage paths into OpenAPI.
- Pending action answers must be persisted before resume. Resume without stored answer is a runtime error.
- Verification tools must preserve raw command output as artifacts. A short normalized summary is not enough evidence.
- Mobile generated client drift is a hard blocker for phases that touch `/v1`.
