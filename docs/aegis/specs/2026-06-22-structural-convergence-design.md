# Structural Convergence Design Spec

Date: `2026-06-22`
Status: `design proposal, pending user approval`
Scope: `architecture`

## 1. Task Intent

- **Goal**: 提升 acorn 后端的可演进性——改一处功能不再牵连多个包;新人能在一个
  逻辑边界内读懂相关代码。不是推倒重来,是证据驱动的定向收敛。
- **Success evidence**:
  1. "tool" 概念从 4 个包降到 2 个,命名不再撞车
  2. `RunnerFactory` 47 方法从 7 文件收敛到 3 文件,读 `buildRun` 不跨文件跳
  3. duplicate port 从 5 降到 0(`OperatorQuestionContext`/`ArtifactContext` 上移 domain)
  4. 400 行守卫放宽到 800 行,消除碎片化文件
  5. 全部测试通过;架构守卫测试更新后仍保护真 invariant
- **Stop condition**: 上述 5 条达成,且 `make build && make test && make lint && make format-check` 全绿。
- **Non-goals**:
  - 不改 openapi wire contract
  - 不改 mobile-kotlin
  - 不改 store 层结构(已最干净)
  - 不改 contextplane 的 masking/auto-compact/assembly 机制分离
  - 不改 memorymodule 的 file-backed + semantic 分离
  - 不引入新依赖;不替换 Eino ADK

## 2. Baseline

### 2.1 BaselineUsageDraft

```text
BaselineUsageDraft:
- Required baseline refs: AGENTS.md, docs/architecture/INVARIANTS.md, docs/architecture/ARCHITECTURE.md
- Delivered context refs: go.mod, internal/app/container.go, internal/store/store.go,
  internal/domain/domain.go, internal/runtime/runner*.go, internal/tools/ports.go,
  internal/tools/operator_question_tool.go, internal/tools/artifact_tools.go
- Acknowledged before plan refs: tests/architecture/*
- Cited in design refs: import edge analysis (go build verified)
- Missing refs: none
- Decision: continue
```

### 2.2 当前架构事实(已验证)

- 34000 行非测试 Go,15 个 internal 包,124 个测试文件,7 个架构守卫测试
- 单组合根 `app.Container`;consumer-owned ports;openapi wire contract
- `direct_response` 唯一编排模式
- import 方向已实测:
  - `domain` 不依赖任何 internal 包(纯 kernel)
  - `tools` 依赖 `domain`/`store`/`tooling`/`webaccess`/`workspace`,**不依赖** `runtime`
  - `runtime` 单向依赖 `tools`
  - `skills` 只依赖 `config`,被 6 包依赖
- **INVARIANTS.md 声称 "合并 port 会创建 import cycle" 已过时**——当前代码 tools 已 import domain,domain 不 import tools,无 cycle。`OperatorQuestionContext`/`ArtifactContext` 可安全上移。

### 2.3 真问题(证据驱动)

| # | 问题 | 证据 |
|---|------|------|
| P1 | 4 个 "tool" 包命名撞车 | `tooling`(601) + `tools`(4597) + `runtime/tool`(2011) + `runtime/toolset`(482) 共 7691 行,概念边界模糊 |
| P2 | RunnerFactory 47 方法散 7 文件 | `runner.go` + `runner_build/selection/toolset/orchestration/mcp/emit`,全是同一 struct 方法 |
| P3 | 400 行守卫制造碎片 | `browser_service_*.go` 5 文件、`client_service_*.go` 5 文件,人为割裂 |
| P4 | duplicate port 被误判为不可改 | `OperatorQuestionContext`/`ArtifactContext` 与 domain 基 port 结构相同,tools 已 import domain,cycle 不存在 |

## 3. Architecture Integrity Lens

```text
- Invariant: single composition root; consumer-owned ports; openapi wire contract;
  direct_response only; SQLite is runtime truth; store adapter isolation
- Canonical owner / contract: domain owns core types + base ports; store owns
  persistence ports + sentinels; runtime owns execution + orchestration
- Responsibility overlap: 4 tool packages; RunnerFactory 7-file split; duplicate
  ports guarded by stale invariant claim
- Higher-level simplification: tool packages → 2 (contract + impl); ports → domain;
  runner files → 3; guard 400 → 800
- Retirement / falsifier: merged packages revert if import cycle appears; runner
  merge reverts if cognitive load increases; guard change validated by tests
- Verdict: proceed
```

## 4. Proposed Convergence (3 layers)

### Layer 1: tool 包合并 (4 → 2)

| 现状 | 行数 | 目标 | 理由 |
|------|------|------|------|
| `internal/tooling` (ToolContract/Catalog/specs) | 601 | → `internal/tool` | 契约层 |
| `internal/tools` (file/git/browser/web/command 实现) | 4597 | → `internal/toolset` | 实现层 |
| `internal/runtime/tool` (scheduler/validator/audit/stream) | 2011 | → 提升,合入 `internal/runtime` 根 | 执行运行时,本属 runtime |
| `internal/runtime/toolset` (Toolset 容器 + memory tools) | 482 | → 合入 `internal/runtime` 根 | 同上 |

**结果**: `internal/tool`(契约) + `internal/toolset`(实现)。`runtime/tool` 和 `runtime/toolset` 消失,内容并入 `runtime` 根。

**包重命名映射**(全 hard cutover,不留 alias):
- `internal/tooling` → `internal/tool`
- `internal/tools` → `internal/toolset`
- `internal/runtime/tool/*` → `internal/runtime/*`
- `internal/runtime/toolset/*` → `internal/runtime/*`

**风险**: `runtime/tool` 提升后 `runtime` 根文件增多(~2000 行)。600-800 行上限下仍需分文件,但按机制(scheduler/validator/audit/stream)分,不再按"tool 子包"分。

**验证**: go build 通过即无 import cycle。

### Layer 2: RunnerFactory 碎片合并 (7 → 3)

| 现现状 | 目标 |
|--------|------|
| `runner.go`(308) + `runner_build.go`(216) + `runner_toolset.go`(250) + `runner_orchestration.go`(165) + `runner_emit.go`(200) = 1139 行 5 文件 | → `runner.go` (~800) |
| `runner_mcp.go`(156) | → 保留 `runner_mcp.go` (~156,MCP 隔离合理) |
| `runner_selection.go`(267) | → 保留 `runner_selection.go` (~267,skill 选择是独立机制) |

**同样处理**:
- `browser_service_*.go`(5 文件 ~1100 行) → `browser_service.go`(~800) + `browser_service_events.go`(~300,console/network 事件流独立)
- `client_service_*.go`(5 文件 ~550 行) → `client_service.go`(~550,800 内)

### Layer 3: port 提升 + 守卫放宽

**3a. duplicate port 消除**:
- `tools.OperatorQuestionContext` → `domain.OperatorQuestionContext`(或合并进 `ToolCallContextBridge`)
- `tools.ArtifactContext` → `domain.ArtifactContext`(或合并进 `ToolCallContextBridge`)
- 两者结构相同:`CurrentRunID/CurrentSessionID/CurrentToolCallID`,与 `domain.ToolCallContextBridge` 完全一致
- **最优解**: 直接用 `domain.ToolCallContextBridge` 替代这两个重复定义,零新增类型

**3b. 守卫调整**:
- `tests/architecture/structural_limits_test.go`: `refactorOwnedDirs` 上限 400 → 800
- 保留: store 边界守卫、port 数量守卫、import direction 守卫、client projection 守卫
- 更新 `INVARIANTS.md`:
  - 删除 "Consumer-owned port 接口重复是故意的" 条目(cycle 不存在,重复可消除)
  - 更新 "结构守卫覆盖全包" 行数上限
  - 更新 "主要包职责" 反映 tool 包合并

### 不动的(明确列出)

- `internal/store` + `store/sqlite` — 已最干净
- `internal/web` — DTO/handler/converter 结构合理
- `internal/contextplane` — masking/auto-compact/assembly 独立机制
- `internal/memorymodule` — file-backed + semantic 分离清楚
- `internal/app` Container — 组合根
- `internal/domain` — 核心类型(只新增 port 归属,不改现有类型)
- `internal/skills` — file-backed loader,边界清晰
- `internal/providers/mcp` — provider lifecycle,独立子系统
- openapi.yaml + mobile-kotlin — wire contract 不动
- Eino ADK 依赖 — 不替换

## 5. Impact Statement

```text
ImpactStatementDraft:
- Affected layers: internal/tooling, internal/tools, internal/runtime (tool, toolset,
  runner*.go), internal/domain (port 归属), tests/architecture, docs/architecture
- Owners: all internal/, single developer
- Invariants at risk: store boundary (not touched), import direction (verified
  no cycle), client projection (not touched)
- Compat: no wire contract change; no mobile change; no config change
- Non-goals: no behavior change; pure structural refactor
```

## 6. Product Risk Lens

```text
- Value: 可演进性——改功能不再跨 4 个 tool 包;新人能在 2 个包边界内定位代码
- Non-goals: 不追求"架构美观"为本身;不推翻已验证的 invariant
- Trade-offs: 大面积重命名(机械但量大)换取长期可读性;短期 git blame 历史断点
- Decision needed: 用户已批准 hard cutover + 全部可动含守卫测试
```

## 7. 执行顺序(实现阶段用,本 spec 只定义)

1. **port 提升**(最小步,先验证 cycle): `OperatorQuestionContext`/`ArtifactContext` → `domain.ToolCallContextBridge`。build 通过即继续。
2. **守卫放宽**: 400 → 800,更新 structural_limits_test。跑测试基线。
3. **tool 包合并**: `tooling`→`tool`, `tools`→`toolset`。全量 rename + import 更新。
4. **runtime/tool 提升**: `runtime/tool/*` → `runtime/*`,`runtime/toolset/*` → `runtime/*`。
5. **RunnerFactory 文件合并**: 7 → 3。
6. **browser/client service 文件合并**。
7. **文档更新**: INVARIANTS.md + ARCHITECTURE.md + AGENTS.md 相关段落。
8. **全量验证**: `make build && make test && make lint && make format-check`。

每步独立可验证。步骤 1 失败(cycle 出现)则整个 port 提升回退,其余不受影响。

## 8. ADR Signals

- **ADR-0010 candidate**: tool 包从 4 合并到 2,改变模块边界
- **ADR-0011 candidate**: 400 行守卫放宽到 800,改变结构约束
- **ADR-0012 candidate**: duplicate port 消除,推翻 INVARIANTS.md "不可合并" 声明
- 需在实现完成后创建 ADR 记录

## 9. Verification Plan

- 每步后 `go build ./...`
- port 提升后跑 `tests/architecture/store_boundary_test.go`
- 包合并后跑全量 `make test`
- 最终: `make build && make test && make lint && make format-check && make test-architecture`
- 不新增 mock;不修改行为;测试只更新 import 路径和文件边界

## 10. Open Questions

无。所有决策已通过证据确认或用户批准。
