# Implementation Plan: Structural Convergence Refactor

Date: `2026-06-22`
Parent spec: `docs/aegis/specs/2026-06-22-structural-convergence-design.md`

## Goal

提升 acorn 后端可演进性：tool 概念从 4 包降到 2 包，消除 RunnerFactory
文件碎片，消除 duplicate port，放宽 400→800 行守卫。纯结构重构，不改行为、
不改 wire contract、不改 mobile。

## Architecture

Go 1.26 + Eino ADK，单组合根 `app.Container`，consumer-owned ports，
SQLite runtime truth，openapi wire contract。详见 `docs/architecture/ARCHITECTURE.md`。

## Tech Stack

Go 1.26, goimports, golangci-lint, modernc.org/sqlite, chi router, Eino ADK。

## Baseline / Authority Refs

```text
BaselineUsageDraft:
- Required baseline refs: AGENTS.md, docs/architecture/INVARIANTS.md,
  docs/architecture/ARCHITECTURE.md
- Delivered context refs: go.mod, internal/app/container.go,
  internal/domain/domain.go, internal/tools/ports.go,
  internal/tools/operator_question_tool.go, internal/tools/artifact_tools.go,
  tests/architecture/structural_limits_test.go
- Acknowledged before plan refs: tests/architecture/store_boundary_test.go,
  tests/architecture/store_interface_count_test.go,
  tests/architecture/client_projection_boundary_test.go,
  tests/architecture/runtime_split_test.go
- Cited in plan refs: import edge analysis (go build verified)
- Missing refs: none
- Decision: continue
```

```text
Requirement Ready Check:
- Requirement source refs: docs/aegis/specs/2026-06-22-structural-convergence-design.md
- Goals and scope refs: spec §1 Task Intent
- User / scenario refs: single owner (ycvk), self-hosted agent backend
- Requirement item refs: spec §4 (3 layers: tool merge, file merge, port+guard)
- Acceptance / verification criteria refs: spec §1 Success evidence (5 items)
- Open blocker questions: none — user approved hard cutover, all movable incl guards
- Decision: ready
```

## Compatibility Boundary

- **必须保持**: openapi wire contract、mobile-kotlin、config schema、
  SQLite schema、Eino ADK 依赖、所有行为语义
- **必须更新**: import 路径、包名、文件位置、架构守卫测试、INVARIANTS.md、
  ARCHITECTURE.md、AGENTS.md 相关段落
- **不新增**: 依赖、类型、接口、mock、fallback

## Architecture Integrity Lens

```text
- Invariant: single composition root; consumer-owned ports; openapi wire;
  direct_response only; SQLite runtime truth; store adapter isolation
- Canonical owner / contract: domain owns core types + base ports;
  store owns persistence ports; runtime owns execution
- Responsibility overlap: 4 tool packages → 2; RunnerFactory 7-file split → 3;
  duplicate ports (OperatorQuestionContext/ArtifactContext) → domain.ToolCallContextBridge
- Higher-level simplification: tool packages collapse to contract+impl;
  ports unify to existing domain.ToolCallContextBridge (zero new types)
- Retirement / falsifier: merged packages revert if import cycle;
  runner merge reverts if cognitive load increases; guard change validated by tests
- Verdict: proceed
```

## Plan Pressure Test

```text
- Owner / contract / retirement: tooling→tool rename is mechanical (30 files);
  tools→toolset rename is mechanical (3 importer packages);
  runtime/tool→runtime is in-package promotion (4 importers)
- Architecture integrity / higher-level path: port unification uses existing
  domain.ToolCallContextBridge, no new type
- Verification scope: go build + make test at each step
- Task executability: each task has exact files and commands
- Pressure result: proceed
```

## Plan-Time Complexity Check

```text
Complexity Budget:
- Artifact class: structural refactor (rename + merge + relocate)
- Target files: ~60 Go files across internal/
- Current pressure: 4 tool packages, 7 runner files, 2 duplicate ports, 400-line guard
- Projected post-change pressure: 2 tool packages, 3 runner files, 0 duplicate ports, 800-line guard
- Budget result: within-budget
- Planned governance: per-step go build + final make test

Plan-Time Complexity Check:
- Target files: internal/tooling/*, internal/tools/*, internal/runtime/tool/*,
  internal/runtime/toolset/*, internal/runtime/runner*.go, internal/tools/browser_service*.go,
  internal/app/client_service*.go, internal/domain/domain.go, tests/architecture/structural_limits_test.go
- Existing size / shape signals: tooling 601 LOC/6 files, tools 4597/24 files,
  runtime/tool 2011/11 files, runtime/toolset 482/4 files
- Owner fit: domain owns ports; tool owns contracts; toolset owns implementations
- Add-in-place risk: none — pure relocation
- Better file boundary: merge by ownership (contract vs impl), not by size cap
- Recommendation: edit-in-place with rename (no new files except merged outputs)
```

## Risks

- **R1: import cycle after promotion** — `runtime/tool` 提升到 `runtime` 根后，
  原有 `runtime/tool` → `runtime` 的引用消失（同包），但 `runtime` 根文件
  可能新增对原 `tool` 子包符号的引用。**缓解**: 每步后 `go build ./...`。
- **R2: 包名 `tool` 与 `einotool` alias 混淆** — 不冲突（包名 vs alias），
  但可读性上 `tool.Catalog` 和 `einotool.BaseTool` 可能混淆。**缓解**: 
  接受，`tool` 是 acorn 自己的契约层，`einotool` 是 eino SDK。
- **R3: git blame 历史断** — rename 会让 blame 显示为新文件。**缓解**: 
  用 `git mv` + `goimports` 保持可追溯。
- **R4: 守卫测试 800 行后可能有人写超长文件** — 这是 tradeoff，用 review 把关。

## Retirement

- `internal/tooling` 目录删除，内容移到 `internal/tool`
- `internal/tools` 目录删除，内容移到 `internal/toolset`
- `internal/runtime/tool` 目录删除，内容移到 `internal/runtime`
- `internal/runtime/toolset` 目录删除，内容移到 `internal/runtime`
- `tools.OperatorQuestionContext` 类型删除，引用改 `domain.ToolCallContextBridge`
- `tools.ArtifactContext` 类型删除，引用改 `domain.ToolCallContextBridge`
- `structural_limits_test.go` 的 `structFileMaxLines` 400 删除，改 800
- 旧 runner 碎片文件删除（`runner_build.go` 等），内容合入 `runner.go`
- INVARIANTS.md "port 重复是故意的" 条目删除

无 compat alias、无 deprecated path、无 fallback。纯 hard cutover。

---

## Tasks

### Task 0: 基线验证

**Files**: 无修改
**Why**: 确认起点全绿，后续每步有对照
**Verification**: `make build && make test && make lint && make format-check`

- [ ] Step 1: `cd /Users/ycvk/GolandProjects/acorn && go build ./...`
- [ ] Step 2: `go test ./...`
- [ ] Step 3: `make lint && make format-check`
- [ ] Step 4: `git status --short`（确认工作树干净）
- [ ] Step 5: `git checkout -b refactor/structural-convergence`

---

### Task 1: duplicate port 消除（最小步，先验证 cycle）

**Files**:
- Modify: `internal/domain/domain.go`（无修改——`ToolCallContextBridge` 已存在）
- Modify: `internal/tools/operator_question_tool.go`
- Modify: `internal/tools/artifact_tools.go`
- Modify: `internal/tools/catalog_builders.go`
- Modify: `internal/tools/workflow_tools.go`
- Modify: `internal/tools/workflow_tools_verification.go`
- Modify: `internal/tools/web_search_tool.go`
- Modify: `internal/tools/web_fetch_tool.go`
- Modify: `internal/tools/browser_tool.go`
- Modify: `internal/tools/tools.go`（`CatalogConfig` 字段类型）
- Modify: `internal/runtime/runner_toolset.go`（`artifactToolBridge` 赋值）
- Modify: 相关 `_test.go` 文件（import + 类型引用）

**Why**: `OperatorQuestionContext` 和 `ArtifactContext` 与 `domain.ToolCallContextBridge`
签名完全相同（`CurrentRunID/CurrentSessionID/CurrentToolCallID`）。tools 已 import domain，
无 cycle。直接用 `domain.ToolCallContextBridge` 替换这两个重复定义。

**Impact/Compatibility**: tools 包内 + runtime/runner_toolset.go 的类型引用变更。无行为变化。

**具体操作**:

1. 删除 `internal/tools/operator_question_tool.go` 第 17-21 行的 `OperatorQuestionContext` 接口定义
2. 删除 `internal/tools/artifact_tools.go` 第 15-19 行的 `ArtifactContext` 接口定义
3. 全局替换 `OperatorQuestionContext` → `domain.ToolCallContextBridge`（在 tools 包内文件）
4. 全局替换 `ArtifactContext` → `domain.ToolCallContextBridge`（在 tools 包内文件）
5. 确保 `internal/tools/operator_question_tool.go` 和 `internal/tools/artifact_tools.go` 有 `import "github.com/ycvk/acorn/internal/domain"`（operator_question_tool.go 已有，artifact_tools.go 需新增）
6. `internal/tools/tools.go` 的 `CatalogConfig` 结构体字段 `OperatorContext OperatorQuestionContext` → `OperatorContext domain.ToolCallContextBridge`，`ArtifactContext ArtifactContext` → `ArtifactContext domain.ToolCallContextBridge`；确认 import domain
7. `internal/runtime/runner_toolset.go` 第 220 行 `ArtifactContext: artifactToolBridge{}` 不变（`artifactToolBridge` 已实现 `CurrentToolCallID` 等方法，自动满足 `domain.ToolCallContextBridge`）
8. 更新所有引用这两个类型的 `_test.go` 文件

**Verification**:
- `go build ./...`
- `go test ./internal/tools/... ./internal/runtime/... ./internal/app/...`
- `grep -rn 'OperatorQuestionContext\|ArtifactContext' internal/ --include='*.go'`（应为 0 结果）

- [ ] Step 1: 删除两个重复接口定义
- [ ] Step 2: 全局替换类型引用为 `domain.ToolCallContextBridge`
- [ ] Step 3: `go build ./...`
- [ ] Step 4: `go test ./internal/tools/... ./internal/runtime/... ./internal/app/...`
- [ ] Step 5: `git add -A && git commit -m "refactor: eliminate duplicate ports, use domain.ToolCallContextBridge"`

---

### Task 2: 守卫放宽 400 → 800

**Files**:
- Modify: `tests/architecture/structural_limits_test.go`

**Why**: 400 行守卫制造碎片化文件。800 行允许合并 runner/browser/client 碎片。

**具体操作**:

1. `tests/architecture/structural_limits_test.go` 第 32 行：`const structFileMaxLines = 400` → `const structFileMaxLines = 800`

**Verification**:
- `go test ./tests/architecture/...`

- [ ] Step 1: 改常量 400 → 800
- [ ] Step 2: `go test ./tests/architecture/...`
- [ ] Step 3: `git add -A && git commit -m "refactor: relax structural file limit 400 to 800"`

---

### Task 3: `internal/tooling` → `internal/tool`（包 rename）

**Files**:
- Rename: `internal/tooling/` → `internal/tool/`（6 文件：builtin_registry.go, catalog.go, contracts.go, progress.go, skills.go, specs.go）
- Modify: 30 个 import 该包的文件（替换 import 路径 + 包引用前缀 `tooling.` → `tool.`）

**Why**: `tooling` vs `tools` vs `tool` vs `toolset` 命名撞车。契约层统一叫 `tool`。

**Impact/Compatibility**: 纯 import 路径变更。无行为变化。

**importer 清单**（实测 30 文件，全部无 alias）:
```
internal/app/skill_eligibility.go
internal/app/capability_service_snapshot.go
internal/contextplane/tool_lifecycle.go
internal/contextplane/types.go
internal/runtime/runner_mcp.go
internal/runtime/runner.go
internal/runtime/run.go
internal/runtime/runner_toolset.go
internal/runtime/runner_orchestration.go
internal/runtime/runner_selection.go
internal/runtime/runner_build.go
internal/runtime/orchestration/types.go
internal/runtime/orchestration/direct_response_builder.go
internal/runtime/tool/audit.go
internal/runtime/tool/catalog.go
internal/runtime/tool/safe_parallel_tools_node.go
internal/runtime/tool/scheduler.go
internal/runtime/tool/streaming_tool_executor.go
internal/runtime/toolset/toolset.go
internal/tools/native_search_tools.go
internal/tools/workflow_tools.go
internal/tools/operator_question_tool.go
internal/tools/native_read_tools.go
internal/tools/web_search_tool.go
internal/tools/artifact_tools.go
internal/tools/workflow_tools_verification.go
internal/tools/progress_tool.go
internal/tools/native_mutation_tools.go
internal/tools/browser_tool.go
internal/tools/command_tool.go
internal/tools/web_fetch_tool.go
```

**具体操作**:

1. `git mv internal/tooling internal/tool`
2. 所有文件中 `"github.com/ycvk/acorn/internal/tooling"` → `"github.com/ycvk/acorn/internal/tool"`
3. 所有文件中 `tooling.` → `tool.`（包引用前缀）
4. `package tooling` → `package tool`（6 个文件头）

**注意**: `einotool "github.com/cloudwego/eino/components/tool"` alias 不变，它是 eino SDK 的 alias，不冲突。

**Verification**:
- `go build ./...`
- `go test ./...`
- `grep -rn 'internal/tooling' . --include='*.go'`（应为 0）

- [ ] Step 1: `git mv internal/tooling internal/tool`
- [ ] Step 2: sed 替换 package 声明 + import 路径 + 前缀
- [ ] Step 3: `go build ./...`
- [ ] Step 4: `go test ./...`
- [ ] Step 5: `git add -A && git commit -m "refactor: rename internal/tooling to internal/tool"`

---

### Task 4: `internal/tools` → `internal/toolset`（包 rename）

**Files**:
- Rename: `internal/tools/` → `internal/toolset/`（24 文件）
- Modify: 3 个 importer 包（`internal/providers/mcp`, `internal/runtime`, `internal/runtime/toolset`）

**Why**: 实现层统一叫 `toolset`，与契约层 `tool` 对应。

**Impact/Compatibility**: 纯 import 路径变更。无行为变化。

**importer 清单**（实测 3 包）:
```
internal/providers/mcp/ (需查具体文件)
internal/runtime/ (runner_toolset.go 等)
internal/runtime/toolset/toolset.go
```

**具体操作**:

1. `git mv internal/tools internal/toolset`
2. 所有文件中 `"github.com/ycvk/acorn/internal/tools"` → `"github.com/ycvk/acorn/internal/toolset"`
3. 所有文件中 `tools.` → `toolset.`（包引用前缀）
4. `package tools` → `package toolset`（24 个文件头）
5. `tools_test.go` → `toolset_test.go`（测试文件名也要改，Go 要求 `package_test`）

**注意**: rename 后 `internal/runtime/toolset` 还在（下一个 Task 才删），此时 `internal/toolset`（实现层）和 `internal/runtime/toolset`（Toolset 容器）并存——这是中间态，Task 5 会消除 `runtime/toolset`。

**Verification**:
- `go build ./...`
- `go test ./...`
- `grep -rn 'internal/tools"' . --include='*.go'`（应为 0）

- [ ] Step 1: `git mv internal/tools internal/toolset`
- [ ] Step 2: sed 替换 package 声明 + import 路径 + 前缀
- [ ] Step 3: 测试文件 rename (`tools_test.go` → `toolset_test.go` 等)
- [ ] Step 4: `go build ./...`
- [ ] Step 5: `git add -A && git commit -m "refactor: rename internal/tools to internal/toolset"`

---

### Task 5: `internal/runtime/tool` → `internal/runtime`（提升子包到根）

**Files**:
- Move: `internal/runtime/tool/*.go` → `internal/runtime/*.go`（11 文件）
- Delete: `internal/runtime/tool/` 目录
- Modify: 4 个 import 该子包的文件（`runtime/runner_mcp.go`, `runner.go`, `runner_toolset.go`, `runner_orchestration.go`）
- Modify: `tests/architecture/structural_limits_test.go`（删除 `internal/runtime/tool` 条目）

**Why**: `runtime/tool` 是执行运行时（scheduler/validator/audit/stream），本属 runtime。提升后符号在同包内直接引用，不需前缀。

**Impact/Compatibility**: import 路径变更 + 同包符号引用去前缀。

**具体操作**:

1. `git mv internal/runtime/tool/*.go internal/runtime/`
2. 删除空目录 `internal/runtime/tool/`
3. 11 个文件的 `package tool` → `package runtime`
4. 4 个 importer 文件删除 `import "github.com/ycvk/acorn/internal/runtime/tool"` 行
5. 4 个 importer 文件中 `tool.X` → `X`（同包直接引用）
6. `tests/architecture/structural_limits_test.go` 第 16 行删除 `"internal/runtime/tool",`
7. **文件名碰撞检查**: `runtime/tool/catalog.go` → `runtime/catalog.go`——根目录无 `catalog.go`，OK。逐个检查：
   - `assistant_stream.go` — 根无此文件 ✓
   - `audit.go` — 根无此文件 ✓
   - `catalog.go` — 根无此文件 ✓
   - `fact_extractor.go` — 根无此文件 ✓
   - `mcp_namespace.go` — 根无此文件 ✓
   - `safe_parallel_tools_node.go` — 根无此文件 ✓
   - `scheduler.go` — 根无此文件 ✓
   - `side_effects.go` — 根无此文件 ✓
   - `streaming_assistant_stream.go` — 根无此文件 ✓
   - `streaming_tool_executor.go` — 根无此文件 ✓
   - `validator.go` — 根无此文件 ✓
8. 更新相关 `_test.go`：`runtime/tool/*_test.go` → `runtime/*_test.go`，package 声明改，import 去前缀

**Verification**:
- `go build ./...`
- `go test ./internal/runtime/...`
- `go test ./tests/architecture/...`
- `grep -rn 'runtime/tool"' . --include='*.go'`（应为 0）

- [ ] Step 1: `git mv internal/runtime/tool/*.go internal/runtime/`
- [ ] Step 2: 改 package 声明 + 去前缀 + 删 import
- [ ] Step 3: 删守卫测试条目
- [ ] Step 4: `go build ./... && go test ./internal/runtime/... ./tests/architecture/...`
- [ ] Step 5: `git add -A && git commit -m "refactor: promote runtime/tool subpackage to runtime root"`

---

### Task 6: `internal/runtime/toolset` → `internal/runtime`（提升子包到根）

**Files**:
- Move: `internal/runtime/toolset/*.go` → `internal/runtime/*.go`（4 文件）
- Delete: `internal/runtime/toolset/` 目录
- Modify: 2 个 import 该子包的文件（`runtime/runner.go`, `runner_toolset.go`）
- Modify: `tests/architecture/structural_limits_test.go`（删除 `internal/runtime/toolset` 条目）

**Why**: `runtime/toolset` 的 `Toolset` 容器和 memory tools 本属 runtime。

**Impact/Compatibility**: import 路径变更 + 同包符号引用去前缀。

**具体操作**:

1. `git mv internal/runtime/toolset/*.go internal/runtime/`
2. 删除空目录 `internal/runtime/toolset/`
3. 4 个文件的 `package toolset` → `package runtime`
4. **文件名碰撞检查**: 
   - `toolset.go` — 根无此文件 ✓
   - `memory_tools.go` — 根无此文件 ✓
   - `memory_tools_remember.go` — 根无此文件 ✓
   - `memory_tools_search.go` — 根无此文件 ✓
5. 2 个 importer 文件删除 import 行，`toolset.X` → `X`
6. 守卫测试删除 `"internal/runtime/toolset",`
7. 更新 `_test.go`

**注意**: Task 5 之后，原 `runtime/toolset` 中的 `toolset.go` 里 `import "github.com/ycvk/acorn/internal/tooling"` 已在 Task 3 改成 `internal/tool`。原 `runtime/toolset` 里的 `import "github.com/ycvk/acorn/internal/tools"` 已在 Task 4 改成 `internal/toolset`。提升后这两个 import 仍在同文件，不变。

**Verification**:
- `go build ./...`
- `go test ./internal/runtime/...`
- `go test ./tests/architecture/...`
- `grep -rn 'runtime/toolset"' . --include='*.go'`（应为 0）

- [ ] Step 6.1: `git mv internal/runtime/toolset/*.go internal/runtime/`
- [ ] Step 6.2: 改 package + 去前缀 + 删 import
- [ ] Step 6.3: 删守卫测试条目
- [ ] Step 6.4: `go build ./... && go test ./internal/runtime/... ./tests/architecture/...`
- [ ] Step 6.5: `git add -A && git commit -m "refactor: promote runtime/toolset subpackage to runtime root"`

---

### Task 7: RunnerFactory 文件合并（7 → 3）

**Files**:
- Merge: `runner_build.go` + `runner_toolset.go` + `runner_orchestration.go` + `runner_emit.go` → `runner.go`
- Keep: `runner_mcp.go`（MCP 隔离）
- Keep: `runner_selection.go`（skill 选择隔离）
- Delete: `runner_build.go`, `runner_toolset.go`, `runner_orchestration.go`, `runner_emit.go`
- Update tests: `runner_build_selection_test.go`, `runner_factory_skills_test.go`（合并相关引用）

**Why**: 47 个方法散 7 文件，全是同一 struct。合并到 800 行上限内。

**Impact/Compatibility**: 无行为变化。文件合并。

**具体操作**:

1. 确认合并后 `runner.go` 行数：
   - `runner.go`(308) + `runner_build.go`(216) + `runner_toolset.go`(250) + `runner_orchestration.go`(165) + `runner_emit.go`(200) = 1139 行
   - **超 800 行**——需要拆成 2 文件而非 1
   - 拆法：`runner.go`（核心装配 + build：~700 行）+ `runner_toolset.go`（工具集构建：~250 行）
   - 最终 4 文件：`runner.go` + `runner_toolset.go` + `runner_mcp.go` + `runner_selection.go`
   
   修正：7 → 4 文件，不是 3。`runner_build/orchestration/emit` 合入 `runner.go`（308+216+165+200=889 行——仍超 800）。再修正：`runner_emit.go` 独立保留（事件发射逻辑独立），5 文件。
   
   **最终方案**: 7 → 4 文件
   - `runner.go`（核心 + build + orchestration：308+216+165=689 行）
   - `runner_toolset.go`（工具集：250 行，保留）
   - `runner_mcp.go`（MCP：156 行，保留）
   - `runner_selection.go`（skill 选择：267 行，保留）
   - `runner_emit.go`（事件发射：200 行，保留）
   
   实际是 7 → 5 文件（合并 build + orchestration 进 runner.go）。

2. 将 `runner_build.go` 内容追加到 `runner.go` 末尾（去掉重复的 `package runtime` 和 import，合并 import block）
3. 将 `runner_orchestration.go` 内容追加到 `runner.go` 末尾
4. `git rm runner_build.go runner_orchestration.go`
5. 检查 `runner.go` 不超 800 行（689 行 ✓）
6. 更新测试文件 import（如有）

**Verification**:
- `go build ./...`
- `go test ./internal/runtime/...`
- `wc -l internal/runtime/runner.go`（≤800）
- `go test ./tests/architecture/...`

- [ ] Step 1: 合并 `runner_build.go` + `runner_orchestration.go` → `runner.go`
- [ ] Step 2: `git rm` 被合并文件
- [ ] Step 3: `go build ./... && go test ./internal/runtime/...`
- [ ] Step 4: `go test ./tests/architecture/...`
- [ ] Step 5: `git add -A && git commit -m "refactor: merge runner_build + orchestration into runner.go"`

---

### Task 8: browser_service 文件合并（5 → 2）

**Files**:
- Merge: `browser_service_navigate.go` + `browser_service_scan.go` + `browser_service_scripts.go` → `browser_service.go`
- Keep: `browser_service_events.go`（console/network 事件流独立）
- Delete: 3 被合并文件

**Why**: 5 文件 ~1100 行人为割裂。合并到 2 文件。

**Impact/Compatibility**: 无行为变化。

**具体操作**:

1. 确认行数：
   - `browser_service.go`(375) + `browser_service_navigate.go`(280) + `browser_service_scan.go`(106) + `browser_service_scripts.go`(162) = 923 行 — 超 800
   - 修正：navigate 独立保留（280 行，导航逻辑重）
   - `browser_service.go`(375) + `scan.go`(106) + `scripts.go`(162) = 643 行 ✓
   - 最终 3 文件：`browser_service.go`(643) + `browser_service_navigate.go`(280) + `browser_service_events.go`(173)

   实际 5 → 3 文件。

2. 将 `browser_service_scan.go` + `browser_service_scripts.go` 内容追加到 `browser_service.go`
3. `git rm browser_service_scan.go browser_service_scripts.go`
4. 合并 import block

**Verification**:
- `go build ./...`
- `go test ./internal/toolset/...`（Task 4 后 tools 已改名 toolset）
- `wc -l internal/toolset/browser_service.go`（≤800）

- [ ] Step 1: 合并 scan + scripts → browser_service.go
- [ ] Step 2: `git rm` 被合并文件
- [ ] Step 3: `go build ./... && go test ./internal/toolset/...`
- [ ] Step 4: `git add -A && git commit -m "refactor: merge browser_service scan + scripts into main file"`

---

### Task 9: client_service 文件合并（5 → 2）

**Files**:
- Merge: `client_service_thread.go` + `client_service_message.go` + `client_service_run.go` + `client_service_event.go` → `client_service.go`
- Delete: 4 被合并文件

**Why**: 5 文件 ~550 行，合到 1 文件。

**Impact/Compatibility**: 无行为变化。

**具体操作**:

1. 确认行数：
   - `client_service.go`(171) + `thread.go`(170) + `message.go`(109) + `run.go`(101) + `event.go`(76) = 627 行 ✓
2. 合并所有内容到 `client_service.go`
3. `git rm` 4 文件
4. 合并 import block

**Verification**:
- `go build ./...`
- `go test ./internal/app/...`
- `wc -l internal/app/client_service.go`（≤800）

- [ ] Step 1: 合并 4 文件 → client_service.go
- [ ] Step 2: `git rm` 被合并文件
- [ ] Step 3: `go build ./... && go test ./internal/app/...`
- [ ] Step 4: `git add -A && git commit -m "refactor: merge client_service subfiles into single file"`

---

### Task 10: 文档更新

**Files**:
- Modify: `docs/architecture/INVARIANTS.md`
- Modify: `docs/architecture/ARCHITECTURE.md`
- Modify: `AGENTS.md`

**Why**: 反映重构后的真实包结构。

**具体操作**:

1. **INVARIANTS.md**:
   - 删除 "Consumer-owned port 接口重复是故意的" 整条（第 42-43 行）——cycle 不存在，重复已消除
   - "结构守卫覆盖全包" 条目：`≤400 行` → `≤800 行`
   - "主要包职责" 如有提及 tool/toolset 旧名需更新
   - `refactorOwnedDirs` 条目：删除 `internal/runtime/tool`、`internal/runtime/toolset`（已提升）

2. **ARCHITECTURE.md** "主要包职责" 章节:
   - `internal/tooling` → `internal/tool`
   - `internal/tools` → `internal/toolset`
   - 删除 `internal/runtime/tool` 和 `internal/runtime/toolset` 子包描述
   - `internal/runtime/` 描述增加 tool 执行运行时 + Toolset 容器

3. **AGENTS.md**:
   - "关键包" 段落更新包名
   - "职责边界" 段落更新
   - "关键包" 列表：`internal/tooling` → `internal/tool`，`internal/tools` → `internal/toolset`

4. **structural_limits_test.go**: `refactorOwnedDirs` 已在 Task 5/6 删除条目，确认无残留

**Verification**:
- `go test ./tests/architecture/...`（docs_structure_test 可能检查文档一致性）
- 人工 review 文档

- [ ] Step 1: 更新 INVARIANTS.md
- [ ] Step 2: 更新 ARCHITECTURE.md
- [ ] Step 3: 更新 AGENTS.md
- [ ] Step 4: `go test ./tests/architecture/...`
- [ ] Step 5: `git add -A && git commit -m "docs: update architecture docs for structural convergence"`

---

### Task 11: 最终验证

**Files**: 无修改
**Why**: 确认所有 Success evidence 达成

**Verification**:

1. `make build && make test && make lint && make format-check && make test-architecture`
2. Success evidence 逐条核对：
   - [ ] tool 概念 4 → 2 包：`ls internal/tool internal/toolset`（存在）；`ls internal/tooling internal/tools internal/runtime/tool internal/runtime/toolset`（不存在）
   - [ ] RunnerFactory 7 → 4 文件：`ls internal/runtime/runner*.go`（4 文件）
   - [ ] duplicate port 0：`grep -rn 'OperatorQuestionContext\|ArtifactContext' internal/ --include='*.go'`（0 结果）
   - [ ] 400 → 800：`grep structFileMaxLines tests/architecture/structural_limits_test.go`（800）
   - [ ] 全测试绿
3. `git log --oneline`（确认每个 Task 有独立 commit）

- [ ] Step 1: `make build`
- [ ] Step 2: `make test`
- [ ] Step 3: `make lint && make format-check`
- [ ] Step 4: `make test-architecture`
- [ ] Step 5: 逐条核对 Success evidence + `git log --oneline`

---

## ADR Signals

实现完成后创建：
- **ADR-0010**: tool 包从 4 合并到 2（tool=契约, toolset=实现）
- **ADR-0011**: 400 行守卫放宽到 800
- **ADR-0012**: duplicate port 消除，推翻 "不可合并" 声明

## Self-Review

1. **Spec coverage**: spec §4 Layer 1 → Task 3-6；Layer 2 → Task 7-9；Layer 3 → Task 1-2+10。✓
2. **Placeholder scan**: 无 TBD/TODO。✓
3. **Type consistency**: `domain.ToolCallContextBridge` 签名与被替换接口一致。✓
4. **Compatibility**: openapi/mobile/config/schema 不变，标记明确。✓
5. **Plan-time complexity**: 合并后文件均 ≤800 行（runner.go 689, browser_service 643, client_service 627）。✓
6. **Architecture integrity**: port 统一到已有 domain 类型，无新 owner。✓
7. **Verification**: 每个 Task 有 `go build` + `go test`，最终全量。✓
8. **ADR signals**: 3 条 ADR 保留。✓
