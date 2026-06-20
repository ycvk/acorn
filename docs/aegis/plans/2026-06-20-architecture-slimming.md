# Architecture Slimming: decision 包删除 + Plane 合并 + ToolContract 瘦身 + 文档瘦身

## Goal

删除被自己声明废弃的 `internal/decision` 包（"Intent routing was removed (P0-C)"，实际是 4 分支选择器却建了表+profile+事件+CLI 子系统），合并 ContextPlane/OrchestrationPlane 双层 plane 转发，精简 `ToolContract` 8 维 contract schema，激进瘦身 AGENTS.md 到 ~5KB。

**不触碰 `internal/runtime/plan/`（plan_execute 子系统）**——用户明确要求本轮不动。这意味着 plane 合并必须保留 `BuildSingleAgent`/`BuildPlanExecute` 方法签名，ToolContract 的 `PlanPolicy` 字段必须保留（`PlanPolicyRequireActivePlan` 被 `act_node.go:428` + `plan_evidence.go:776` 消费）。

## Architecture

- **单一组合根不变**：`internal/app.Container` 仍是唯一实例化具体实现的地方。
- **run 装配链简化**：`Executor → RunnerFactory.buildRun → resolveRunSelection(内联) → buildAssembly → DefaultPlane.Build{Direct,SingleAgent,PlanExecute}`。删除 `decision.Engine`/`ProfileService`/`Decide()` 层，删除 `persistDecisionAndEmit`/`emitDecisionEvents`。
- **Plane 合并边界**：`orchestration.DefaultPlane` 保留（plan_execute 依赖其 `BuildSingleAgent`/`BuildPlanExecute`），但 `runtime.orchestrationPlane` 接口和 `runner_orchestration.go` 的委托层内联到 `runner_build_assembly.go`。`contextplane.ContextPlane` 接口保留（`Assemble` 被 plan 路径的 `assembleContext` 消费），但 `ContextPlane` 不再持有 `DecisionRecord`。
- **ToolContract 精简**：保留 `Name/Source/Kind/Category/ResourceScope/PlanPolicy/Health`（均被实际消费），`Profiles []ToolProfile` 保留（`SpecsForProfile` 被 plan/orchestration/contextplane 广泛使用），删除 `Loading`（eager/deferred split 由 `tool_lifecycle.go` 基于 `Kind` 决定，不需要 contract 声明 loading policy）。
- **SQLite hard cutover**：新增 migration `v2_drop_decision_tables`，DROP `run_decisions` 表，DROP `run_context_snapshots` 的 `decision_profile_hash`/`decision_action`/`decision_skill_id` 三列。旧 DB 升级后历史 decision 记录不可用。`validateSchema` 同步移除 `run_decisions` 和对应列校验。

## Tech Stack

Go 1.26, Eino ADK, SQLite (modernc.org/sqlite), golangci-lint, goimports。无前端改动（mobile/OpenAPI 不涉及）。

## Baseline/Authority Refs

- `AGENTS.md` — 当前硬边界描述（将被瘦身）
- `docs/architecture/runtime-context-memory-decision.md` — decision 包现状描述（将被更新）
- `internal/decision/engine.go:19` — "Intent routing was removed (P0-C)" 自声明
- `internal/runtime/resume.go:10-22` — root mode routing 事实
- 用户确认：plan_execute 本轮不动、decision 表 hard cutover、文档激进瘦身

## Compatibility Boundary

- **SQLite schema**：drop 表+列，旧 DB 需 migration。`validateSchema` fail-loud 不变。
- **CLI**：`acorn decision check`/`acorn decision inspect` 命令删除。用户需改用 `acorn doctor`。
- **`decision.md` 文件**：`decision.md` workspace profile 文件不再被读取。用户需手动删除（installer 不强制删）。
- **RunEvent**：`skill.selected` 事件的 `decision_profile_hash` payload 字段删除。mobile client 如消费此字段需同步（检查 openapi/mobile）。
- **内部 API**：`Container.InspectRunDecision`/`DecisionProfile()` 方法删除。
- **不变**：`/v1` remote client contract、OpenAPI、mobile DTO、run 执行语义、plan_execute、ContextSession/CompactionEngine/BudgetGovernor。

## Verification

- `make format-check && make lint`（CI 门禁）
- `make test`（全量）
- `go test ./internal/runtime ./internal/decision ./internal/contextplane ./internal/orchestration ./internal/store/sqlite ./internal/app ./internal/cli ./internal/config ./internal/tooling`（受影响包）
- `go build ./cmd/acorn`（编译通过）
- 手动验证：`./bin/acorn doctor` 输出不再含 decision 字段；`./bin/acorn smoke "hello"` 正常执行 direct_response

## Retirement

- `internal/decision/` 整包删除（engine.go/types.go/action.go/profile.go + 对应 test）
- `internal/cli/decision.go` 删除
- `decision.md` 从 repo 删除
- `run_decisions` 表 + `run_context_snapshots.decision_*` 列删除
- `Container.InspectRunDecision`/`DecisionProfile()` 删除
- `RuntimeDeps.DecisionProfiles`/`RunnerFactoryOptions.DecisionProfileService` 删除
- `ContextPlane.AssembleRequest.DecisionRecord` 删除
- `model.RunContextSnapshot` 的 3 个 `Decision*` 字段删除
- `runtime/helpers_decision.go` 删除（逻辑内联到 `runner_build_selection.go`）
- AGENTS.md 的「不要复活的旧设计」段落移除（git history 保留）
- docs/architecture/ 同步更新

---

## Tasks

### Task 1: SQLite migration — drop run_decisions + run_context_snapshots.decision_* 列

**Files:**
- Modify: `internal/store/sqlite/store_schema.go`
- Modify: `internal/store/sqlite/store_run.go`
- Modify: `internal/model/history.go`
- Modify: `internal/store/sqlite/store_runs_test.go`
- Modify: `internal/store/sqlite/store_schema_test.go`

**Why:** decision 包删除后，`run_decisions` 表和 `run_context_snapshots` 的 3 个 decision 列无写入方，必须 drop 以保持 schema 真相一致。

**Impact/Compatibility:** 旧 DB 升级丢失历史 decision 记录。`validateSchema` fail-loud 移除 `run_decisions` 校验。

**Verification:**
```bash
go test ./internal/store/sqlite/...
go build ./cmd/acorn
```

**Steps:**

1. **Write test** — 在 `store_schema_test.go` 新增 `TestMigrateDropsDecisionTables`，验证 migration 后 `run_decisions` 表不存在、`run_context_snapshots` 不含 `decision_profile_hash`/`decision_action`/`decision_skill_id` 列。先验证 RED。

2. **Verify RED** — `go test -run TestMigrateDropsDecisionTables ./internal/store/sqlite/`，确认失败（migration 还没写）。

3. **Implement migration** — 在 `store_schema.go` 的 `migrateV2()` 末尾追加 `s.dropDecisionTables()` 调用。新增方法（仿 `dropRemovedRuntimeTables` 模式）：
```go
func (s *Store) dropDecisionTables() (err error) {
	const version = "v2_drop_decision_tables"
	var count int
	row := s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version)
	if err := row.Scan(&count); err == nil && count > 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin decision table drop: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("decision table drop rollback: %w", rollbackErr))
		}
	}()
	if _, err := tx.Exec(`DROP TABLE IF EXISTS run_decisions`); err != nil {
		return fmt.Errorf("drop run_decisions: %w", err)
	}
	// SQLite 不支持 DROP COLUMN IF EXISTS，直接 ALTER（3.35+），modernc.org/sqlite 支持
	for _, col := range []string{"decision_profile_hash", "decision_action", "decision_skill_id"} {
		if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE run_context_snapshots DROP COLUMN %s", col)); err != nil {
			// 列可能已不存在（二次运行），忽略
			if !strings.Contains(err.Error(), "no such column") {
				return fmt.Errorf("drop column %s: %w", col, err)
			}
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record decision table drop migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit decision table drop: %w", err)
	}
	return nil
}
```
同步更新 `validateSchema()`：从 `requiredTables` 删除 `"run_decisions"` 条目；`"run_context_snapshots"` 的列列表删除 3 个 decision 列。

4. **Update model + store SQL** — `model/history.go` 的 `RunContextSnapshot` 删除 `DecisionProfileHash`/`DecisionAction`/`DecisionSkillID` 三字段。`store_run.go` 的 `SaveRunContextSnapshot`/`LoadRunContextSnapshot` SQL 删除对应列和参数。`store_runs_test.go` 更新 snapshot 测试。

5. **Verify GREEN** — `go test ./internal/store/sqlite/... && go build ./cmd/acorn`。

---

### Task 2: 删除 decision 包 + store ports + container wiring

**Files:**
- Delete: `internal/decision/` (整包)
- Modify: `internal/runtime/store_ports.go` — 删除 `SaveRunDecision`/`LoadRunDecision`
- Modify: `internal/app/container_store_ports.go` — 删除 `LoadRunDecision` + `decision` import
- Modify: `internal/app/container_runtime_deps.go` — 删除 `decisionProfileService` + `DecisionProfileService` 传递
- Modify: `internal/app/container.go` — 删除 `profiles` 字段 + `DecisionProfile()`/`InspectRunDecision()` 方法
- Modify: `internal/runtime/runtime_deps.go` — 删除 `DecisionProfiles` 字段
- Modify: `internal/runtime/run.go` — 删除 `DecisionProfileService` from `RunnerFactoryOptions`
- Modify: `internal/runtime/runner_build.go` — 删除 `resolveDecisionProfiles` + `decisionProfiles` 参数链
- Modify: `internal/store/sqlite/store_oauth.go` — 删除 `SaveRunDecision`/`LoadRunDecision`
- Delete: `internal/cli/decision.go`
- Modify: `internal/cli/cli.go` — 删除 `decision` case
- Delete: `decision.md`
- Modify: `internal/cli/cli_test.go` — 删除 decision CLI 测试

**Why:** decision 包是 4 分支选择器 + profile/事件/表/CLI 子系统，AGENTS.md 自己声明 intent routing 已废弃。skill 选择已由 `skills.RetrieveCandidates` 独立完成。

**Impact/Compatibility:** `acorn decision` 命令消失。`Container.InspectRunDecision`/`DecisionProfile()` 消失。内部 consumer 全部迁移到内联逻辑（Task 3）。

**Verification:**
```bash
go build ./cmd/acorn
go test ./internal/app/... ./internal/runtime/... ./internal/store/...
```

**Steps:**

1. **Write test** — 暂跳过 RED（删除型任务，编译即验证）。

2. **Delete decision package + CLI** — `rm -rf internal/decision/ internal/cli/decision.go decision.md`。从 `cli.go` 的 switch 删除 `case "decision": return runDecision(...)`。从 usageText 删除 decision 相关行。

3. **Delete store methods** — `store_oauth.go` 删除 `SaveRunDecision`/`LoadRunDecision`（行 436-498）。删除 `decision` import。

4. **Delete store ports** — `store_ports.go` 的 `ExecutorStore` 删除 `SaveRunDecision`/`LoadRunDecision` 两行 + `decision` import。`container_store_ports.go` 删除 `LoadRunDecision` 行 + `decision` import。

5. **Delete container wiring** — `container_runtime_deps.go` 删除 `decisionProfileService` 字段、`decision.NewProfileService(ws.Root())` + `Load()` 调用、`DecisionProfileService:` 传参。`container.go` 删除 `profiles` 字段、`DecisionProfile()` 方法、`InspectRunDecision()` 方法。

6. **Delete runtime deps wiring** — `runtime_deps.go` 删除 `DecisionProfiles` 字段 + `CloneForWorkspace` 里的 `decision.NewProfileService`。`run.go` 的 `RunnerFactoryOptions` 删除 `DecisionProfileService`。`runner_build.go` 删除 `resolveDecisionProfiles` + `assembleRuntimeDeps`/`buildRuntimeDeps` 的 `decisionProfiles` 参数链。

7. **Verify GREEN** — `go build ./cmd/acorn && go test ./internal/app/... ./internal/store/sqlite/... ./internal/cli/...`。此时 `internal/runtime/` 仍引用 `decision` 包（Task 3 处理），预期 runtime 包编译失败，先不跑 runtime 测试。

---

### Task 3: 内联 run selection 逻辑到 runtime 包，删除 decision 依赖

**Files:**
- Delete: `internal/runtime/helpers_decision.go`
- Modify: `internal/runtime/runner_build_selection.go` — 内联 decision 逻辑
- Modify: `internal/runtime/runner_toolset_emit_skill.go` — 删除 `emitDecisionEvents`
- Modify: `internal/runtime/runner_toolset_skill.go` — 删除 `recommendedSkillsFromMatches`（不再需要 `decision.RecommendedSkill`）
- Modify: `internal/runtime/runner_catalog.go` — 删除 `decisionRecord` 字段使用
- Modify: `internal/contextplane/types.go` — `AssembleRequest` 删除 `DecisionRecord`
- Modify: `internal/contextplane/run_context_snapshot.go` — 删除 `decision` import + `record *decision.Record` 参数
- Modify: `internal/runtime/runner_build_assembly.go` — 删除 `decisionRecord` 传递

**Why:** decision 包删除后，run selection 的 4 分支逻辑直接内联到 `runner_build_selection.go`，不再需要持久化 decision record、不再 emit decision 事件。skill 选择仍由 `skills.RetrieveCandidates` 完成，`SelectedSkill` 仍存在，但不再经过 `decision.Engine.Decide()`。

**Impact/Compatibility:** `skill.selected` RunEvent payload 不再含 `decision_profile_hash`。`run_context_snapshots` 不再存 decision 字段（Task 1 已处理列）。resume 路径不再 `LoadRunDecision`——改为从 `run_context_snapshots` 的 `DecisionSkillID`（已删）或 run record 的 `skill_id` 恢复 selected skill。

**Verification:**
```bash
go build ./cmd/acorn
go test ./internal/runtime/... ./internal/contextplane/... ./internal/orchestration/...
```

**Steps:**

1. **Write test** — 在 `runner_build_selection_test.go`（新建或更新现有 test）验证：无 skill input + 无 working context → action=`inspect_first`；有 explicit skill → selected skill；有 top recommended skill → selected skill；缺 required capability → block。验证不再调用 `SaveRunDecision`。

2. **Verify RED** — 确认测试因 `decision` 包已删而编译失败。

3. **Inline selection logic** — 在 `runner_build_selection.go` 新增内联函数：
```go
type runSelection struct {
	selectedSkill  *SelectedSkill
	// decisionRecord 不再持久化，仅用于 ContextPlane snapshot 内联
}

func (f *RunnerFactory) resolveRunSelectionByDecision(ctx context.Context, req RunnerBuildRequest, caps *runCapabilities) (*runSelection, error) {
	if caps == nil {
		return nil, fmt.Errorf("run capabilities are required")
	}
	hasWorkingContext, err := f.hasWorkingContext(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	discovered, err := f.retrieveSkillCandidates(req, caps)
	if err != nil {
		return nil, err
	}
	// 内联 decision 逻辑：缺能力 → block；explicit skill → execute_with_skill；
	// top recommended → execute_with_skill；缺 working context → inspect_first
	if hasCapabilityFailure(discovered) {
		return nil, fmt.Errorf("decision blocked execution: missing_required_capability")
	}
	selected, err := resolveSelectedSkill(req, discovered, caps.stableSkills, hasWorkingContext)
	if err != nil {
		return nil, err
	}
	if err := emitSkillSelectionEvents(ctx, f.deps.Store, req, selected, discovered); err != nil {
		return nil, err
	}
	return &runSelection{selectedSkill: selected}, nil
}

func resolveSelectedSkill(req RunnerBuildRequest, discovered []SkillMatch, stableSkills []skills.Spec, hasWorkingContext bool) (*SelectedSkill, error) {
	if explicitID := strings.TrimSpace(req.SkillID); explicitID != "" {
		if match, ok := findEligibleSkillByID(discovered, explicitID); ok {
			return selectedSkillFromMatch(match, stableSkills, true), nil
		}
		return nil, fmt.Errorf("explicit skill %q not eligible", explicitID)
	}
	if top, ok := topRecommendedSkill(discovered); ok {
		return selectedSkillFromMatch(top, stableSkills, false), nil
	}
	if !hasWorkingContext {
		return nil, errInspectFirst
	}
	return nil, nil
}
```
新增 `errInspectFirst = errors.New("decision requires operator confirmation: missing_context")`。

4. **Update resume path** — `resolveRunSelectionByResume` 不再 `LoadRunDecision`。改为从 run record + `run_context_snapshots` 恢复：查 `LoadRunContextSnapshot` 拿 `WorkingCheckpointSkillID`，或直接从 run 的 `input` + session 重建 selection。简化为：resume 时若 `req.SkillID` 非空则重新 resolve，否则返回空 selection（direct_response resume 无需 skill）。

5. **Delete helpers_decision.go + emitDecisionEvents** — `rm internal/runtime/helpers_decision.go`。`runner_toolset_emit_skill.go` 删除 `emitDecisionEvents` 函数（行 149-172）。`runner_toolset_skill.go` 删除 `recommendedSkillsFromMatches`（不再需要转 `decision.RecommendedSkill`）。

6. **Clean ContextPlane** — `contextplane/types.go` 的 `AssembleRequest` 删除 `DecisionRecord *decision.Record` + `decision` import。`run_context_snapshot.go` 的 `createSnapshot` 删除 `record *decision.Record` 参数，删除 `snapshot.DecisionProfileHash`/`DecisionAction` 赋值。`Assemble` 调用处更新。

7. **Update runner_catalog.go** — 删除 `decisionRecord` 变量（行 125）和 `assembleContext` 的 `selection.decisionRecord` 传递。

8. **Verify GREEN** — `go build ./cmd/acorn && go test ./internal/runtime/... ./internal/contextplane/... ./internal/orchestration/...`。

---

### Task 4: ToolContract 瘦身 — 删除 Loading 字段

**Files:**
- Modify: `internal/tooling/contracts.go` — 删除 `Loading` 字段 + `LoadingPolicy` 类型
- Modify: `internal/tooling/specs.go` — 删除 loading 相关赋值
- Modify: `internal/tooling/builtin_registry.go` — 删除 loading 赋值
- Modify: `internal/contextplane/tool_lifecycle.go` — eager/deferred split 改为基于 `Kind`（read/inspect 类 eager，其余 deferred）
- Modify: 对应 test 文件

**Why:** `ToolContract.Loading` 有 `Mode`/`Reason` 两字段，但 eager/deferred 的实际判断在 `tool_lifecycle.go:splitToolDefinitions` 基于 `Kind` 做的（read/inspect eager，其余 deferred），contract 声明的 loading policy 没被消费。

**Impact/Compatibility:** `splitToolDefinitions` 逻辑不变（已基于 Kind），只是不再从 contract 读 loading 字段。test 里 `ToolContract{Loading: ...}` 字面量需删除。

**Verification:**
```bash
go test ./internal/tooling/... ./internal/contextplane/...
go build ./cmd/acorn
```

**Steps:**

1. **Write test** — 在 `tool_lifecycle_test.go` 验证：read_file/list_files/search_text eager；create_file/run_command/browser deferred。验证不依赖 `ToolContract.Loading`。

2. **Verify RED** — 确认现有测试不依赖 Loading 字段。

3. **Delete Loading field** — `contracts.go` 删除 `Loading LoadingPolicy` 字段 + `LoadingPolicy`/`LoadingMode` 类型。`Validate()` 删除 loading 校验。`specs.go`/`builtin_registry.go` 删除 loading 赋值。

4. **Verify splitToolDefinitions uses Kind** — 检查 `tool_lifecycle.go:splitToolDefinitions` 已基于 `spec.Kind`（read/inspect → eager）。若已如此，无需改逻辑，只删 contract 字段。若不是，改为基于 Kind。

5. **Update tests** — 删除 test 里 `Loading: tooling.LoadingPolicy{...}` 字面量。

6. **Verify GREEN** — `go test ./internal/tooling/... ./internal/contextplane/... && go build ./cmd/acorn`。

---

### Task 5: 合并 orchestrationPlane 接口层 — 内联到 runner_build_assembly

**Files:**
- Modify: `internal/runtime/runner_orchestration.go` — 删除 `orchestrationPlane` 接口 + `defaultOrchestrationPlaneDeps` 委托层
- Modify: `internal/runtime/runner_build_assembly.go` — 直接调用 `orchestration.DefaultPlane` 方法
- Modify: `internal/runtime/runner_build.go` — 删除 `orchestrationPlane` 构造
- Modify: `internal/runtime/runtime_deps.go` — 删除 `Orchestration` 字段
- Modify: `internal/runtime/runner_orchestration_graph.go` — 直接调用 plan graph builder

**Why:** `runtime.orchestrationPlane` 接口 + `defaultOrchestrationPlaneDeps` 是测试注入 seam，但实际只有一个实现 `orchestration.DefaultPlane`。`buildAssembly` → `f.deps.Orchestration.BuildDirectResponse` → `DefaultPlane.BuildDirectResponse` 的两层转发无价值。

**Impact/Compatibility:** `DefaultPlane` 方法签名保留（plan_execute 依赖）。测试若注入 mock `orchestrationPlane` 需改为注入 `*orchestration.DefaultPlane` 或用 `RunnerFactoryOptions.Handlers` 注入。

**Verification:**
```bash
go build ./cmd/acorn
go test ./internal/runtime/...
```

**Steps:**

1. **Write test** — 现有 `orchestration_mode_test.go` 已覆盖 mode routing，确认仍通过。

2. **Delete orchestrationPlane interface** — `runner_orchestration.go` 删除 `orchestrationPlane` 接口（行 23-27）+ `defaultOrchestrationPlaneDeps` struct（行 29-34）+ `newDefaultOrchestrationPlane` + 所有 `defaultOrchestrationPlaneDeps` 方法。保留 `buildRunnerAgentHandlers` + `bindSessionID` 等纯函数。

3. **Inline to runner_build_assembly** — `buildAssembly` 直接调用 `f.deps.Orchestration.(*orchestration.DefaultPlane).BuildDirectResponse(...)`，或更好：把 `DefaultPlane` 实例直接存到 `RunnerFactory`。将 `RuntimeDeps.Orchestration orchestrationPlane` 改为 `RuntimeDeps.Orchestration *orchestration.DefaultPlane`。

4. **Update runner_build.go** — `buildRuntimeDeps` 直接 `orchestration.NewDefaultPlane(...)` 而非 `newDefaultOrchestrationPlane`。

5. **Update runtime_deps.go** — `Orchestration` 类型改为 `*orchestration.DefaultPlane`。

6. **Verify GREEN** — `go build ./cmd/acorn && go test ./internal/runtime/...`。

---

### Task 6: AGENTS.md + docs/architecture 激进瘦身

**Files:**
- Modify: `AGENTS.md` — 从 22KB 瘦身到 ~5KB
- Modify: `docs/architecture/runtime-context-memory-decision.md` — 删除 decision 段落
- Modify: `docs/architecture/runtime-orchestration.md` — 更新 plane 合并描述
- Modify: `docs/architecture/ARCHITECTURE.md` — 更新主链描述
- Delete: `.acorn/harness/memory/modules/decision.md` — 已废弃模块记忆

**Why:** AGENTS.md 22KB 中大量「不要复活的旧设计」是 code review 补档，git history 已保留。文档应只描述当前真相，不充当架构警察。

**Impact/Compatibility:** AI 协作约束变薄，但硬边界保留。

**Verification:**
```bash
wc -l AGENTS.md  # 目标 < 120 行（~5KB）
make format-check && make lint
```

**Steps:**

1. **AGENTS.md 重写** — 保留：项目概览（5行）、常用命令（30行）、架构大图（15行）、硬边界当前真相（40行，删除「不要复活的旧设计」全段）、验证要求（15行）、Harness 路由（10行）。删除：「不要复活的旧设计」「旧终端界面」等历史段落、「已知坑」中已删路径的描述。

2. **Update architecture docs** — `runtime-context-memory-decision.md` 删除 `## Decision` 段落 + `## Removed Old Memory Path` 段落（移到 git history）。`runtime-orchestration.md` 更新描述 plane 合并。`ARCHITECTURE.md` 主链描述删除 decision 节点。

3. **Delete harness decision memory** — `rm .acorn/harness/memory/modules/decision.md`。

4. **Verify GREEN** — `wc -l AGENTS.md && make format-check && make lint`。

---

### Task 7: 全量验证 + 清理残留引用

**Files:**
- 检查所有 `internal/decision` 残留引用
- 检查 `decision.Record`/`decision.Engine`/`ProfileService` 残留
- 运行 `make test` + lint + build

**Why:** 确认 hard cutover 无遗漏。

**Verification:**
```bash
grep -rn 'internal/decision' --include='*.go' . | grep -v '_test.go'
grep -rn 'decision\.Record\|decision\.Engine\|ProfileService\|DecisionProfile' --include='*.go' . | grep -v '_test.go'
make format-check && make lint && make test
python3 mobile/tool/generate_openapi_client.py --check
```

**Steps:**

1. **Grep 残留** — 确认无 `internal/decision` import 残留（排除 test）。

2. **全量测试** — `make test`。

3. **Lint + format** — `make format-check && make lint`。

4. **OpenAPI 同步检查** — `python3 mobile/tool/generate_openapi_client.py --check`（确认 decision 删除不影响 openapi，因 decision 不在 /v1 contract）。

5. **手动 smoke** — `./bin/acorn doctor && ./bin/acorn smoke "hello"`。
