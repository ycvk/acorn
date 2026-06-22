# ADR-0010: tool 包从 4 合并到 2（toolkit 契约 + toolset 实现）

Date: 2026-06-22
Status: Accepted
Supersedes: (none)

## Context

Acorn 的"tool"概念曾拆成 4 个包，命名撞车、边界模糊：

- `internal/tooling`（601 行）— ToolContract / Catalog / ToolSpec 契约
- `internal/tools`（4597 行）— file/git/browser/web/command 工具实现
- `internal/runtime/tool`（2011 行）— scheduler / validator / audit / stream 执行运行时
- `internal/runtime/toolset`（482 行）— Toolset 容器 + memory tools

新人无法从包名分辨 `tooling` vs `tools` vs `tool` vs `toolset` 的职责边界。
`runtime/tool` 和 `runtime/toolset` 是 runtime 的执行子层，不该独立成包。

## Decision

合并为 2 个包：

- `internal/toolkit` — 契约层（ToolContract / Catalog / ToolSpec / loading+execution policy）
- `internal/toolset` — 实现层（file/git/browser/web/command/artifact/memory 工具实现）

`internal/runtime/tool` 和 `internal/runtime/toolset` 提升到 `internal/runtime` 根——
它们是 runtime 的执行子层，提升后符号在同包内直接引用，不需跨包前缀。

### 命名选择：toolkit 而非 tool

原计划用 `internal/tool`（契约）+ `internal/toolset`（实现）。
但 `toolset` 包内有大量局部变量 `tool`（`tool, err := inferProgressTool(...)`，~52 处），
包名 `tool` 会遮蔽变量名。

改用 `toolkit`（契约）+ `toolset`（实现），无变量遮蔽，不需改任何局部变量。

## Consequences

- **正面**：tool 概念从 4 包降到 2 包，命名不再撞车；`runtime/tool` 提升后 runtime 包
  内部符号直接引用，去掉跨包前缀噪音
- **负面**：`runtime` 根文件增多（+15 文件），但 800 行守卫下仍可管理
- **风险**：无——纯 rename + 提升，import 方向不变，无行为变化

## Baseline Sync

- `internal/tooling` → `internal/toolkit`（commit `f17c3c8`）
- `internal/runtime/tool/*` → `internal/runtime/*`（commit `2ece169`）
- `internal/runtime/toolset/*` → `internal/runtime/*`（commit `5599327`）
- `internal/tools` → `internal/toolset`（commit `b14cb01`）
- `tests/architecture/structural_limits_test.go` 更新 `refactorOwnedDirs` 删除已消失目录
- `docs/architecture/ARCHITECTURE.md` + `AGENTS.md` 更新包描述
- `go build ./...` + `go test ./...` + `make lint` + `make format-check` + `make test-architecture` 通过
