# ADR-0011: 结构守卫文件行数上限 400 → 800

Date: 2026-06-22
Status: Accepted
Supersedes: (none)

## Context

`tests/architecture/structural_limits_test.go` 强制每个文件 ≤400 行。
该守卫的本意是防止文件膨胀，但实际后果是**机械拆分**：

- `RunnerFactory` 47 个方法散在 7 文件（`runner.go` + `runner_build/selection/toolset/orchestration/mcp/emit`），
  全是同一 struct 的方法，人为割裂
- `browser_service` 拆成 5 文件（`browser_service` + `_navigate/_events/_scripts/_scan`），共 ~1100 行
- `client_service` 拆成 5 文件（`client_service` + `_thread/_message/_run/_event`）

文件数上去了，认知负担没下去——读 `RunnerFactory.buildRun` 要在 7 个文件间跳。
守卫把"大文件"换成了"碎片文件"，净复杂度没降。

## Decision

将 `structFileMaxLines` 从 400 放宽到 800。

- `RunnerFactory` 7 文件 → 5 文件（`runner.go` 655 行 + mcp/selection/toolset/emit 独立保留）
- `browser_service` 5 文件 → 3 文件（`browser_service.go` 626 行 + navigate + events）
- `client_service` 5 文件 → 1 文件（588 行）

保留守卫本身——仍防止失控膨胀，只是上限更合理。用 review 补充机械约束。

## Consequences

- **正面**：消除碎片化文件，读代码不跨文件跳；RunnerFactory 核心逻辑集中在 `runner.go`
- **负面**：800 行内可能容纳更多复杂度，需 review 把关
- **风险**：低——合并后的文件均在 588-655 行，离 800 有余量

## Baseline Sync

- `tests/architecture/structural_limits_test.go`: `const structFileMaxLines = 800`（commit `0625b0d`）
- `docs/architecture/INVARIANTS.md` 更新 "≤400 行" → "≤800 行"（commit `ce2fddd`）
- 合并后的文件均通过 `make test-architecture` 守卫验证
