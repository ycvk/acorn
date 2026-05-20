---
doc_type: dev-plan
slug: native-developer-tools-v2
component: native-developer-tools
status: implemented
summary: Native Developer Tools v2 的 P0 设计合同和工具边界
tags: [runtime, tools, developer-experience, mobile]
last_reviewed: 2026-05-20
---

# Native Developer Tools v2

Status: P0 implemented on 2026-05-20.

## 目标

Native Developer Tools v2 的 P0 目标是让 Acorn 更像一个可日常使用的 self-hosted developer agent backend，而不是让模型靠 `run_command` 和 prose 自己临时拼工作流。

本阶段只补四类高频原生能力：

- 持久 terminal/process session
- operator question/decision tool
- run artifact store
- edit/test/git workflow tools

P0 不做 repo context、LSP、repo map、persistent code index、`/v1/codeintel/*`、browser/CDP、ADB、scheduler、plugin gateway 或第二套 memory/store。

## 当前基线

当前 Acorn 已有的后端基线必须复用，不重建：

- `ToolContract` 是工具 loading、execution、result、boundary、projection 的唯一工具合同入口。
- `ToolExecutionScheduler` 拥有工具并发和 path conflict 调度。
- `ToolResultLedger` 是模型可见工具结果的持久事实层。
- SQLite 是 runs、events、plans、pending actions、tool results、context boundaries 等 runtime truth 的本地事实源。
- `memorymodule` file-backed records 是长期 memory truth。
- `/v1` 和 generated mobile client 是 remote client wire contract。
- mobile 是后端事实的 control surface，不拥有 runtime truth。

## 不做

这些不是 P0 的一部分：

- 不恢复旧 `internal/codeintel`、repo-map/symbol 模型工具、codeintel SQLite index 或 `/v1/codeintel/*`。
- 不引入 LSP server 管理、语言服务器缓存、项目级代码索引或 embedding-based code search。
- 不新增 legacy `/api` alias、debug-only API、auth bypass 或 local/dev access fallback。
- 不把 artifacts 写入 memory records，也不把 memory records 当 artifact store。
- 不做 shell 命令 pattern scanner、mock success、silent fallback、dual-read、dual-write 或兼容 adapter。
- 不把 mobile 变成离线执行客户端，不让 mobile 推断 tool/process/artifact 状态。
- 不做通用 host process manager。`process_*` 只管理 Acorn 自己启动并登记的 process/session；任意 host inspection 仍可由显式 `run_command` 完成。

## 设计原则

### 1. ToolContract first

每个 P0 工具必须先有完整 `ToolContract`，不能只把函数塞进 catalog。

新工具需要明确：

- `Kind`: 默认 `native`。
- `Category`: terminal/process 用 `execute`，artifact read/list 用 `read`，artifact write 用 `write`，operator question 用 `integration`。
- `ResourceScope`: P0 需要新增 `process`、`artifact`、`operator` scope，而不是把所有东西塞进 `workspace_file`。
- `PlanPolicy`: 写 workspace、启动/写入/signal 长期 process、`multi_edit`、`run_verification` 要求 active plan；artifact read/write/list、operator question、status/list 可以不要求 active plan。
- `ParallelPolicy`: artifact read/list 可以 read-only 并发；artifact write、process signal、terminal write、multi-edit 必须 serial 或 write-scoped。
- `SideEffects`: P0 需要新增 process/artifact/operator 相关 side effect，不允许用空 side effects 掩盖真实行为。
- `Result`: 大输出默认 `preview_ref` 或 `ref_only`，完整内容必须可从 ledger/artifact 重新读取。

### 2. Tool result is model-visible truth

工具成功或失败都必须形成模型可见 tool result。普通工具失败是 failed tool result，不是 run failure。

只有这些情况才是 run/runtime failure：

- 工具 catalog 或 contract 构造失败。
- SQLite/file-backed artifact/process store 写入失败。
- tool result ledger 写入失败。
- run/session/context lifecycle 缺失。
- OpenAPI/mobile projection 无法从后端事实投影。

不要吞错后返回 `(mock) ok`、空 success、fallback summary 或由 assistant prose 代替真实 tool result。

### 3. Artifacts are run evidence, not memory

Artifact 是 run/session 证据，不是长期 memory。

Artifact content 存在 runtime storage 的文件系统目录；SQLite 只保存 metadata 和索引：

- `artifact_id`
- `run_id`
- `session_id`
- `source_tool_result_ref`
- `kind`
- `title`
- `mime_type`
- `relative_path`
- `size_bytes`
- `sha256`
- `created_at`

ToolResultLedger side effects 必须能 backlink 到 artifact ids。RunDetail/mobile 只消费后端 artifact projection。

### 4. Terminal sessions own their process groups

`terminal_session_*` 只管理 Acorn 启动的 session。每个 session 有独立 process group、cwd、env whitelist、stdout/stderr or PTY log、status 和 exit record。

长期 session 的 stdout/stderr 不直接塞进 tool result。读取日志时返回 bounded preview，加 artifact/log ref 和 offset。完整日志由 artifact/session log store 读取。

交互式 session 使用 PTY；非交互长期命令可以使用 stdout/stderr pipe。平台不支持 PTY 时对应工具显式不可用，不能静默降级成一次性 `run_command`。

### 5. Operator questions go through pending actions

`ask_operator` 是模型向用户请求信息或决策的唯一 P0 原生工具。

它必须走后端 pending action truth：

- 创建 pending action。
- 写入 RunEvent。
- mobile 展示问题和选项。
- 用户回答后写 decision payload。
- runtime resume 后把回答作为 tool result 返回给模型。

不允许从 assistant prose、mobile local state 或 RunEvent timeline 反推答案。没有 pending action row 就没有 operator answer truth。

### 6. Edit/test/git tools build on existing workspace truth

P0 不重写 workspace mutation 系统。

新增 edit/test/git workflow 必须复用：

- workspace root/path resolution
- mutation checkpoint
- explicit rollback
- `inspect_git_status`
- `inspect_git_diff`
- ToolResultLedger evidence refs
- plan evidence backlinks

`multi_edit` 必须一次生成一个 mutation checkpoint。`run_verification` 必须保存 command、exit code、stdout/stderr artifact refs 和 normalized verification summary。`git_summary` 只投影 status/diffstat/key paths，不自动 stage、commit 或 merge。

### 7. Mobile follows `/v1`

涉及 operator question、artifact、terminal/process projection 的 client-visible 字段时，必须同步：

- `docs/openapi.yaml`
- generated `mobile/lib/src/api/acorn_api.dart`
- mobile parser/projection tests
- relevant widget/controller tests

Mobile 不读取 artifact 文件系统，不解析 memory markdown，不猜 process status。

## P0 Native Tool Set

### Terminal/process

| Tool | Purpose | Notes |
|---|---|---|
| `terminal_session_start` | 启动持久 shell 或长期命令 | 返回 `terminal_session_id`、status、cwd、log artifact refs |
| `terminal_session_write` | 向交互式 session 写入 stdin | 只允许 Acorn-owned session |
| `terminal_session_read` | 按 offset/tail 读取 session log | 返回 preview、offset、artifact/log refs |
| `terminal_session_signal` | interrupt/terminate/kill session process group | 写明确 signal/status |
| `terminal_session_close` | 关闭 session 并 finalize 状态 | 不删除日志/artifacts |
| `terminal_session_list` | 列出当前 run/session 可见 terminal sessions | read-only |
| `process_status` | 查询 Acorn-owned process/session 状态 | read-only |

`process_*` 不扫描或管理任意 host process。需要 host-level process inspection 时，模型必须显式调用 `run_command`。

### Operator interaction

| Tool | Purpose | Notes |
|---|---|---|
| `ask_operator` | 向 mobile/operator 提问并等待回答 | 支持 question、choices、freeform answer |

`ask_operator` 的结果必须是结构化 tool result，例如：

```json
{
  "action_id": "operator_question:run_...:call_...",
  "status": "answered",
  "decision": "answer",
  "selected_option_id": "continue",
  "answer": "Use the focused path."
}
```

### Artifacts

| Tool | Purpose | Notes |
|---|---|---|
| `artifact_write` | 写 run-scoped artifact | 用于报告、日志、JSON、diff、test output |
| `artifact_read` | 读取 artifact range | bounded preview |
| `artifact_list` | 列出当前 run/session artifacts | read-only |

Artifact ids 是后端生成的 opaque ids；模型和 mobile 不能从路径拼 id。

### Edit/test/git workflow

| Tool | Purpose | Notes |
|---|---|---|
| `multi_edit` | 原子多文件/多 span 编辑 | 一个 checkpoint，失败不部分成功 |
| `run_verification` | 运行 test/lint/build/format-check 并产出 verification evidence | 大输出进 artifact |
| `git_summary` | 聚合 status、diffstat、changed paths 和 optional scoped diff refs | 不 stage、不 commit |

`run_verification` 不是 `run_command` 的 fallback 包装。它是把验证命令输出标准化成 plan evidence、tool result refs 和 artifacts 的 workflow tool。

当前实现细节：

- `multi_edit` 只改已有 workspace 文件；所有 line span 先统一校验，单文件内 span 不允许重叠，成功时返回 `checkpoint_id`、`checkpoint_paths` 和 `verified_diff_stat`。
- `run_verification` 返回 `kind`、`status`、`command`、`cwd`、`exit_code`、`duration_ms`、`summary`、`stdout_artifact_id`、`stderr_artifact_id`；非零退出是 `status=failed` 的工具结果，不是 runtime failure。
- `git_summary` 返回 `entries`、`changed_paths` 和 `diff_stat`；只有 `include_diff=true` 时写 `diff_artifact_id`，且绝不 stage、commit、merge。

## Contract Changes

P0 允许的 contract 扩展：

- 新增 `ResourceScopeProcess`
- 新增 `ResourceScopeArtifact`
- 新增 `ResourceScopeOperator`
- 新增 process/artifact/operator side effect constants
- 新增 artifact 和 terminal session side-effect refs for `ToolResultLedger`
- 为 artifact/process/operator tools 在 `runtimeToolSpec` 中设置显式 policy
- 新增 SQLite canonical tables，而不是复用 old/legacy tables
- 新增 `/v1` artifact/process/operator projections，只描述 active remote contract

P0 不允许的 contract 扩展：

- `memory.semantic.enabled` 之类开关。
- codeintel/repo-map endpoint 或 model tool。
- debug-only API。
- silent disabled health 状态来假装工具存在。
- mobile-only DTO 或 handwritten parallel models。

## Validation Doctrine

每个 slice 至少要有：

```bash
go test ./internal/tooling ./internal/tools ./internal/runtime ./internal/contextplane ./internal/store/sqlite ./internal/app ./internal/web
python3 mobile/tool/generate_openapi_client.py --check
git diff --check
```

涉及 mobile projection 或 pending action UI 时还要跑：

```bash
cd mobile && flutter test
cd mobile && flutter analyze
```

提交前仍以 repo 级 gate 为准：

```bash
make lint
make format-check
```
