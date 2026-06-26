---
doc_type: dev-plan
slug: native-developer-tools-v2
component: native-developer-tools
status: implemented
summary: Native Developer Tools v2 的 P0 设计合同和工具边界
tags: [runtime, tools, developer-experience, mobile]
last_reviewed: 2026-06-26
---

# Native Developer Tools v2

Status: P0 implemented on 2026-05-20.

## 目标

Native Developer Tools v2 的 P0 目标是让 Acorn 更像一个可日常使用的 self-hosted developer agent backend，而不是让模型靠 `run_command` 和 prose 自己临时拼工作流。

当前 P0 保留三类高频原生能力：

- operator question/decision tool
- run artifact store
- edit/test/git workflow tools

P0 不做 repo context、LSP、repo map、persistent code index、`/v1/codeintel/*`、browser/CDP、ADB、scheduler、plugin gateway 或第二套 memory/store。

## 当前基线

当前 Acorn 已有的后端基线必须复用，不重建：

- `ToolContract` 是工具 loading、execution、result、boundary、projection 的唯一工具合同入口。
- `ToolExecutionScheduler` 拥有工具并发和 path conflict 调度。
- Tool results stay in the message stream; observation masking replaces old results with placeholders. No durable ledger.
- SQLite 是 runs、events、messages、pending actions 等 runtime truth 的本地事实源（10 张表）。
- `internal/memory` file-backed records 是长期 memory truth。
- `/v1` 和 generated mobile client 是 remote client wire contract。
- mobile 是后端事实的 control surface，不拥有 runtime truth。

## 不做

这些不是 P0 的一部分：

- 不恢复旧 `internal/codeintel`、repo-map/symbol 模型工具、codeintel SQLite index 或 `/v1/codeintel/*`。
- 不引入 LSP server 管理、语言服务器缓存、项目级代码索引或 embedding-based code search。
- 不新增 legacy `/api` alias、debug-only API、auth bypass 或 local/dev access fallback。
- 不把 artifacts 写入 memory records，也不把 memory records 当 artifact store。
- 不做 shell 命令 pattern scanner、mock success、silent fallback、dual-read、dual-write 或兼容 adapter。
- 不把 mobile 变成离线执行客户端，不让 mobile 推断 tool/artifact 状态。
- 不做持久 terminal/session manager 或通用 host process manager。需要 host-level process inspection 或一次性命令执行时，模型必须显式调用 `run_command`。

## 设计原则

### 1. ToolContract first

每个 P0 工具必须先有完整 `ToolContract`，不能只把函数塞进 catalog。

新工具需要明确：

- `Kind`: 默认 `native`。
- `Category`: artifact read/list 用 `read`，artifact write 用 `write`，operator question 用 `integration`，`run_command` 用 `execute`。
- `ResourceScope`: P0 使用 `artifact`、`operator` 和现有 workspace/mutation scope，而不是把所有东西塞进 `workspace_file`。
- `PlanPolicy`: 写 workspace、`multi_edit`、`run_verification` 要求 active plan；artifact read/write/list、operator question 可以不要求 active plan。
- `ParallelPolicy`: artifact read/list 可以 read-only 并发；artifact write、multi-edit、`run_command` 必须 serial 或 write-scoped。
- Side effects and large-result handling are runtime/ledger behavior, not `ToolContract` fields. Tools that mutate state must emit concrete side-effect refs through the tool result ledger; large outputs must remain rereadable from the ledger/artifact path instead of being replaced by empty success text.

### 2. Tool result is model-visible truth

工具成功或失败都必须形成模型可见 tool result。普通工具失败是 failed tool result，不是 run failure。

只有这些情况才是 run/runtime failure：

- 工具 catalog 或 contract 构造失败。
- SQLite/file-backed artifact store 写入失败。
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

Tool side effects backlink to artifact ids. RunDetail/mobile 只消费后端 artifact projection.

### 4. Process execution stays explicit

当前 hard cut 删除了 `terminal_session_*` 和 `process_*` 工具。Acorn 不维护持久 shell session、PTY log store 或 process-status projection。

一次性命令执行仍走 `run_command`，长输出证据通过 artifact 工具显式落盘。需要长期交互式 shell 时，应作为新的功能重新设计，不复用已删除的 terminal session 表或 DTO。

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
- Tool side-effect refs


`multi_edit` 必须一次生成一个 mutation checkpoint。`run_verification` 必须保存 command、exit code、stdout/stderr artifact refs 和 normalized verification summary。`git_summary` 只投影 status/diffstat/key paths，不自动 stage、commit 或 merge。

### 7. Mobile follows `/v1`

涉及 operator question、artifact projection 的 client-visible 字段时，必须同步：

- `docs/openapi.yaml`
- generated `mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/`
- mobile parser/projection tests
- relevant widget/controller tests

Mobile 不读取 artifact 文件系统，不解析 memory markdown，不猜 tool 或 artifact 状态。

## P0 Native Tool Set

### Shell/process

| Tool | Purpose | Notes |
|---|---|---|
| `run_command` | 执行显式一次性 shell 命令 | 非持久 session；取消必须清理子进程组 |

Acorn 当前不提供持久 terminal session API，也不扫描或管理任意 host process。

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

- 新增 `ResourceScopeArtifact`
- 新增 `ResourceScopeOperator`
- 新增 artifact/operator side effect constants
- 新增 artifact side-effect refs for `Tool results`
- 为 artifact/operator tools 在 `runtimeToolSpec` 中设置显式 policy
- 新增 SQLite canonical tables，而不是复用 old/legacy tables
- 新增 `/v1` artifact/operator projections，只描述 active remote contract

P0 不允许的 contract 扩展：

- `memory.semantic.enabled` 之类开关。
- codeintel/repo-map endpoint 或 model tool。
- debug-only API。
- silent disabled health 状态来假装工具存在。
- mobile-only DTO 或 handwritten parallel models。

## Validation Doctrine

每个 slice 至少要有：

```bash
go test ./internal/tools ./internal/runtime ./internal/store ./internal/api ./internal/core
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check
git diff --check
```

涉及 mobile projection 或 pending action UI 时还要跑：

```bash
cd mobile-kotlin && ./gradlew test
cd mobile-kotlin && ./gradlew lint
```

提交前仍以 repo 级 gate 为准：

```bash
make lint
make format-check
```
