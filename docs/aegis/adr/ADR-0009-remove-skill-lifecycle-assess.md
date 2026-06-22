# ADR-0009: 删 skill lifecycle/assess 企业级机制

Date: 2026-06-22
Status: Accepted
Supersedes: (none)

## Context

Acorn 的 skill 是只读 markdown + 简单关键词匹配，无 lifecycle/evidence/assess。但代码里残留了一套企业级 skill lifecycle 机制：

- `lifecycle_tools.go`：生命周期工具实现
- `RoutingFixture`：路由夹具
- `LifecycleStatus` 5 态枚举（draft/active/deprecated/retired/archived）
- `AssessmentVerdict` 3 态枚举（pass/fail/waived）
- `StreamKindSkillLifecycle` 事件流类型

这些机制在当前「skill 是只读 markdown」的模型下没有任何调用方，是过度设计。

## Decision

硬删除整个 skill lifecycle/assess 机制：

- 删 `lifecycle_tools.go`
- 删 `RoutingFixture`
- 删 `LifecycleStatus` 5 态 + `AssessmentVerdict` 3 态
- 删 `StreamKindSkillLifecycle`

**关键修正**：`Replaces` 字段在初次删除中被误删，后恢复。`Replaces` ≠ `ReplacedBy`：
- `Replaces`：skill 超取代（新 skill 声明它取代了哪个旧 skill）
- `ReplacedBy`：lifecycle 退休（旧 skill 声明它被哪个新 skill 退休）

`Replaces` 是 skill 元数据的合法字段，保留；`ReplacedBy` 是 lifecycle 机制的一部分，删除。

## Consequences

- **正面**：消除无调用方的企业级机制，skill 模型回归「只读 markdown + 关键词匹配」的简单本质
- **负面**：无——如果未来需要 skill lifecycle，重新引入即可
- **风险**：无——`Replaces` 字段已恢复，skill 元数据完整

## Baseline Sync

- `internal/skills/lifecycle_tools.go` 已删除
- `internal/skills/` 中 `RoutingFixture` / `LifecycleStatus` / `AssessmentVerdict` / `StreamKindSkillLifecycle` 已删除
- `Replaces` 字段已恢复（commit `c529a3b`）
- `go build ./...` + `go test ./...` 通过
