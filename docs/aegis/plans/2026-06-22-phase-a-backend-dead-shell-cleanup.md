# Phase A:后端残留死壳清理

Date: 2026-06-22
Goal: 删除大重构后遗留的 mode 路由壳、死文件、skill lifecycle/assess 企业级机制、capability_service 冗余 readiness 层
Architecture: Go 1.26 + Eino ADK 单用户自托管 agent 后端
Tech Stack: Go, SQLite (modernc.org/sqlite), chi/v5, OpenAPI 3.1.0
Baseline/Authority Refs: `docs/aegis/specs/2026-06-22-backend-cleanup-kotlin-migration-design.md`
Compatibility Boundary: OpenAPI schema 不动(`/v1/*` + RunEvent);SQLite schema 不动;后端 tool/runtime/context/memory 不动
Verification: `make test && make lint && make format-check && make test-architecture` + `python3 mobile/tool/generate_openapi_client.py --check`

## Plan Basis

Spec: `docs/aegis/specs/2026-06-22-backend-cleanup-kotlin-migration-design.md` §4 Phase A
Scope: 纯删除死壳 + 收拢 mode 路由为固定 direct_response,不新增功能

## BaselineUsageDraft

- Required baseline refs: `docs/aegis/specs/2026-06-22-backend-cleanup-kotlin-migration-design.md`
- Delivered context refs: AGENTS.md(硬约束入口)
- Acknowledged before plan refs: `internal/events/events.go`, `internal/app/client_service.go`, `internal/runtime/runner.go`, `internal/runtime/run.go`, `internal/skills/lifecycle_tools.go`
- Cited in plan refs: 全部 task 文件路径
- Missing refs: 无
- Decision: continue

## Architecture Integrity Lens

- Invariant: runtime 只有一个 root mode (direct_response);OpenAPI wire contract 不变;SQLite schema 不变
- Canonical owner: `internal/events/events.go` 不再拥有 `OrchestrationMode` 类型;mode 不再出现在任何参数链
- Responsibility overlap: `assembleRunnerByMode` 是空包装(只调 `newDirectResponseRunner`),`buildAssembly` 的 `mode` 参数硬编码为 `ModeDirectResponse` — 删包装层,直接调
- Higher-level simplification: `ExecuteRequest.OrchestrationMode` 字段在 executor 中完全未使用(`buildExecuteRunner` 不传它到 `RunnerBuildRequest`)— 删字段,删 `parseClientRunMode`
- Retirement: `BuildSkillLifecycleTools` 零非测试调用者(死代码);`skill.lifecycle` event kind 是 diagnostic-only 不投影到 mobile;`RoutingFixture` 只被 `acorn skills check --fixtures` 用
- Verdict: proceed

## Plan Pressure Test

- Owner / contract / retirement: mode 路由壳 owner = `events.OrchestrationMode`;retirement = 删类型 + 全链路参数;contract = OpenAPI `mode` 字段保持接受空值/direct_response(向后兼容旧 Flutter client)
- Architecture integrity: `assembleRunnerByMode` 空包装删后直接调 `newDirectResponseRunner`;`buildAssembly` 的 `mode` 参数删
- Verification scope: `make test` + `make test-architecture` + openapi `--check` + `acorn doctor` + `acorn smoke`
- Task executability: 每个任务有精确文件+行号+代码
- Pressure result: proceed

## Plan-Time Complexity Check

- Artifact class: dead code removal + parameter chain collapse
- Target files: events/app/cli/runtime/skills/contextplane
- Current pressure: mode 参数穿越 5 个包但只有一个值;skill lifecycle ~990 行死代码
- Projected post-change pressure: 降低(routing 路径变直)
- Budget result: within-budget
- Recommendation: edit-in-place + delete files

## Files

### mode 路由删除
- `internal/events/events.go` — 删 `OrchestrationMode` 类型 + 常量 + `Normalize()`
- `internal/app/client_helpers.go` — 删 `ErrClientInvalidRunMode` sentinel
- `internal/app/client_service.go` — 删 `parseClientRunMode` + `CreateRun` 的 `mode` 参数 + `OrchestrationMode` 字段
- `internal/app/run_once.go` — 删 `RunOnce` 的 `mode` 参数 + `parseClientRunMode` 调用
- `internal/cli/smoke.go` — 删 `--mode` flag + `displayMode`
- `internal/cli/cli.go` — 改 help 文本(L67)
- `internal/runtime/runner.go` — 删 `buildAssembly` 的 `mode` 参数
- `internal/runtime/run.go` — 删 `assembleRunnerByMode` 包装层,直接调 `newDirectResponseRunner`;删 `buildAssembly` 调用的 `mode` 参数
- `internal/runtime/api/api.go` — 删 `ExecuteRequest.OrchestrationMode` 字段
- `internal/web/handler_helpers.go` — 删 `ErrClientInvalidRunMode` 错误分支
- `internal/web/handlers_run.go` — 改 `CreateRun` 调用(删 mode 参数)
- `internal/app/client_service_test.go` — 删 invalid mode 测试用例

### 死文件删除
- `internal/contextplane/compression_token_counter.go` — 删整个文件(零引用)
- `.artifacts/faiss-native/` — 删整个目录(13MB)

### skill lifecycle/assess 删除
- `internal/skills/lifecycle_tools.go` — 删整个文件(零非测试调用者)
- `internal/skills/writer_lifecycle.go` — 删 `UpdateSkillLifecycle` + `validateLifecycleEvidence` + `applyLifecycleUpdate` + `LifecycleUpdate` 类型;保留 `ReadSkillFile` / `WriteSkillFile` / `normalizeCreateInput` / `buildNormalizedCreateInput` / `applyCreateInputDefaults`
- `internal/skills/health.go` — 删 `RoutingFixture` + `RoutingFixtureResult` + `runRoutingFixtures` + `runRoutingFixture` + `hasRoutingMetadata` + `hasWeakRoutingMetadata` + `duplicateTriggerKeys`;简化 `BuildHealthReport` 只检查 loader problem + eligibility
- `internal/skills/model.go` — 删 `LifecycleStatus`(5 态) + `AssessmentVerdict`(3 态) + `SkillAssessment` + `LifecycleEvent` + `LifecycleUpdate` + `EvidenceRefs` 相关字段
- `internal/skills/loader.go` — 删 `SkillLoader` 接口的 `UpdateSkillLifecycle` 方法
- `internal/app/skill_service.go` — 删 `Health` 方法的 `fixtures` 参数 + `RoutingFixture` 调用
- `internal/cli/skills.go` — 删 `--fixtures` flag + `loadRoutingFixtures`
- `internal/stream/accessors.go` — 删 `GetSkillLifecycle` + `StreamSkillLifecycle` 相关
- `internal/stream/stream_types.go` — 删 `StreamKindSkillLifecycle`
- `internal/stream/payloads.go` — 删 `StreamSkillLifecycle` 类型
- `internal/stream/projection.go` — 删 `StreamKindSkillLifecycle` case

### capability_service 简化
- `internal/app/capability_service_snapshot.go` — 删 `providerReadinessFromCapability` + `providerStartupReason` + `resolveProviderStatuses` + `mcpProviderParallelPolicy`

## Tasks

### Task 1: 删 mode 路由壳 — events + app + cli 层

**Files:** `internal/events/events.go`, `internal/app/client_helpers.go`, `internal/app/client_service.go`, `internal/app/run_once.go`, `internal/cli/smoke.go`, `internal/cli/cli.go`
**Why:** 只有一个 mode (direct_response),保留 `OrchestrationMode` 类型 + `parseClientRunMode` + `--mode` flag 是死壳
**Impact/Compatibility:** OpenAPI `mode` 字段保持不变(后端接受空值);`acorn smoke` 不再接受 `--mode`;`RunOnce` 签名改
**Verification:** `go build ./...` + `go test ./internal/events ./internal/app ./internal/cli`

#### Step 1: 删 `OrchestrationMode` 类型

File: `internal/events/events.go`

删除 L17-25:
```go
type OrchestrationMode string

const (
	ModeDirectResponse OrchestrationMode = "direct_response"
)

func (m OrchestrationMode) Normalize() OrchestrationMode {
	return m
}
```

#### Step 2: 删 `ErrClientInvalidRunMode` sentinel

File: `internal/app/client_helpers.go`

删除 L15:
```go
	ErrClientInvalidRunMode   = errors.New("client run mode is invalid")
```

注意:如果 `events` import 因删 `OrchestrationMode` 而变为未使用,需同时删除该 import。

#### Step 3: 删 `parseClientRunMode` + 改 `CreateRun` 签名

File: `internal/app/client_service.go`

删除 `parseClientRunMode` 函数(L160-170):
```go
func parseClientRunMode(raw string) (events.OrchestrationMode, error) {
	mode := events.OrchestrationMode(strings.TrimSpace(raw))
	switch mode {
	case "":
		return "", nil
	case events.ModeDirectResponse:
		return mode, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrClientInvalidRunMode, raw)
	}
}
```

改 `CreateRun` 签名(L89),删 `mode` 参数:
```go
// Before:
func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, mode, input string) (*Run, error) {

// After:
func (s *ClientService) CreateRun(ctx context.Context, threadID, skillID, input string) (*Run, error) {
```

删 `CreateRun` 内 `parseClientRunMode` 调用(L95-98):
```go
	orchestrationMode, err := parseClientRunMode(mode)
	if err != nil {
		return nil, err
	}
```

删 `ExecuteRequest` 构造中的 `OrchestrationMode` 字段(L145):
```go
		OrchestrationMode: orchestrationMode,
```

#### Step 4: 改 `RunOnce` 签名

File: `internal/app/run_once.go`

改 `RunOnce` 签名(L29),删 `mode` 参数:
```go
// Before:
func (c *Container) RunOnce(ctx context.Context, input, mode string) (*RunOnceResult, error) {

// After:
func (c *Container) RunOnce(ctx context.Context, input string) (*RunOnceResult, error) {
```

删 `parseClientRunMode` 调用(L37-40):
```go
	orchestrationMode, err := parseClientRunMode(mode)
	if err != nil {
		return nil, err
	}
```

删 `ExecuteRequest` 构造中的 `OrchestrationMode` 字段(L47):
```go
		OrchestrationMode: orchestrationMode,
```

删过时注释(L27-28):
```go
// An empty mode resolves to direct_response. Only the public root modes are
// accepted; the internal single_agent mode is rejected by parseClientRunMode.
```

#### Step 5: 删 smoke `--mode` flag

File: `internal/cli/smoke.go`

删除 `mode` flag 定义(L22):
```go
	mode := fs.String("mode", "", "orchestration mode: direct_response (default) or plan_execute")
```

删除 `displayMode` 逻辑(L33-36):
```go
	displayMode := strings.TrimSpace(*mode)
	if displayMode == "" {
		displayMode = "direct_response"
	}
```

改 `RunOnce` 调用(L41):
```go
// Before:
	result, err := container.RunOnce(ctx, text, *mode)

// After:
	result, err := container.RunOnce(ctx, text)
```

#### Step 6: 改 help 文本

File: `internal/cli/cli.go`

改 L67:
```go
// Before:
  acorn smoke [-c path] [--json] [--mode direct_response|plan_execute] "task input"

// After:
  acorn smoke [-c path] [--json] "task input"
```

#### Step 7: 删 `web/handler_helpers.go` 的 `ErrClientInvalidRunMode` 分支

File: `internal/web/handler_helpers.go`

删除 L24-25:
```go
	case errors.Is(err, app.ErrClientInvalidRunMode):
		s.respondBadRequest(w, r, err.Error())
```

#### Step 8: 改 `web/handlers_run.go` 的 `CreateRun` 调用

File: `internal/web/handlers_run.go`

找到 `CreateRun` 调用,删 `mode` 参数:
```go
// Before (大致):
	run, err := s.clientService.CreateRun(ctx, threadID, skillID, mode, input)

// After:
	run, err := s.clientService.CreateRun(ctx, threadID, skillID, input)
```

具体行号需读文件确认。

#### Step 9: 删 `client_service_test.go` invalid mode 测试

File: `internal/app/client_service_test.go`

删除 invalid mode 测试用例(L801-805 附近):
```go
		_, err = service.CreateRun(ctx, thread.ID, "", tt.mode, "")
		if !errors.Is(err, ErrClientInvalidRunMode) {
			t.Fatalf("CreateRun error = %v, want ErrClientInvalidRunMode", err)
		}
		if _, loadErr := store.LoadRun(ctx, "run_invalid_mode"); !errors.Is(loadErr, storecore.ErrRunNotFound) {
			t.Fatalf("LoadRun after invalid mode = %v, want ErrRunNotFound", loadErr)
		}
```

以及该 test case 的 table entry(mode = "invalid")。

#### Step 10: 验证

```bash
go build ./...
go test ./internal/events ./internal/app ./internal/cli ./internal/web
```

---

### Task 2: 删 mode 路由壳 — runtime 层

**Files:** `internal/runtime/api/api.go`, `internal/runtime/run.go`, `internal/runtime/runner.go`
**Why:** `ExecuteRequest.OrchestrationMode` 在 executor 中未使用;`assembleRunnerByMode` 是空包装;`buildAssembly` 的 `mode` 参数硬编码
**Impact/Compatibility:** runtime 内部参数链收拢,无外部影响
**Verification:** `go build ./...` + `go test ./internal/runtime`

#### Step 1: 删 `ExecuteRequest.OrchestrationMode` 字段

File: `internal/runtime/api/api.go`

删除 L55:
```go
	OrchestrationMode events.OrchestrationMode
```

同时删除 `ParentRunID` 和 `Depth` 字段(L56-57)如果也无引用:
```go
	ParentRunID       string
	Depth             int
```
需先确认这两个字段是否有引用。用 `grep -rn 'ParentRunID\|\.Depth' internal/runtime/` 确认。

#### Step 2: 删 `assembleRunnerByMode` 包装层

File: `internal/runtime/run.go`

删除 `assembleRunnerByMode` 函数(L155-157):
```go
func (f *RunnerFactory) assembleRunnerByMode(ctx context.Context, req RunnerBuildRequest, mode events.OrchestrationMode, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	return f.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
}
```

改 L48 调用,直接调 `newDirectResponseRunner`:
```go
// Before:
	active, err = f.assembleRunnerByMode(ctx, req, events.ModeDirectResponse, chatModel, capabilityAssembly)

// After:
	active, err = f.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
```

删 `events` import 如果不再使用。

#### Step 3: 删 `buildAssembly` 的 `mode` 参数

File: `internal/runtime/runner.go`

改 `buildAssembly` 签名(L231-238),删 `mode` 参数:
```go
// Before:
func (f *RunnerFactory) buildAssembly(
	ctx context.Context,
	mode events.OrchestrationMode,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {

// After:
func (f *RunnerFactory) buildAssembly(
	ctx context.Context,
	req RunnerBuildRequest,
	catalog *tooling.Catalog,
	chatModel einomodel.BaseChatModel,
	contextResult *contextplane.AssembleResult,
) (*orchestration.RunAssembly, error) {
```

改 L65 调用:
```go
// Before:
	agentAssembly, err := f.buildAssembly(ctx, events.ModeDirectResponse, req, capabilities.catalog, chatModel, contextResult)

// After:
	agentAssembly, err := f.buildAssembly(ctx, req, capabilities.catalog, chatModel, contextResult)
```

删 `events` import 如果不再使用。

#### Step 4: 验证

```bash
go build ./...
go test ./internal/runtime
```

---

### Task 3: 删死文件 + 死目录

**Files:** `internal/contextplane/compression_token_counter.go`, `.artifacts/faiss-native/`
**Why:** `compression_token_counter.go` 零引用;`.artifacts/faiss-native/` 是 FAISS 残留(13MB)
**Impact/Compatibility:** 无影响(零引用)
**Verification:** `go build ./...` + 确认目录不存在

#### Step 1: 删 `compression_token_counter.go`

```bash
rm internal/contextplane/compression_token_counter.go
```

#### Step 2: 删 `.artifacts/faiss-native/`

```bash
rm -rf .artifacts/faiss-native/
```

检查 `.artifacts/` 是否为空,如果为空也删:
```bash
rmdir .artifacts/ 2>/dev/null || true
```

#### Step 3: 检查 `.gitignore` 和 CI 是否引用 `.artifacts/`

```bash
grep -rn '.artifacts' .gitignore .github/ Makefile scripts/ 2>/dev/null
```

如有引用,清理。

#### Step 4: 验证

```bash
go build ./...
test ! -d .artifacts/faiss-native/
```

---

### Task 4: 删 skill lifecycle/assess 死代码

**Files:** `internal/skills/lifecycle_tools.go`, `internal/skills/writer_lifecycle.go`, `internal/skills/health.go`, `internal/skills/model.go`, `internal/skills/loader.go`, `internal/app/skill_service.go`, `internal/cli/skills.go`, `internal/stream/accessors.go`, `internal/stream/stream_types.go`, `internal/stream/payloads.go`, `internal/stream/projection.go`
**Why:** `BuildSkillLifecycleTools` 零非测试调用者;`skill.lifecycle` 是 diagnostic-only event 不投影到 mobile;`RoutingFixture` 只被 `acorn skills check --fixtures` 用。单用户不需要企业级 skill 知识管理。
**Impact/Compatibility:** `acorn skills check --fixtures` flag 删除;`skill_assess` tool 从未 wire 到 runtime,无影响;`skill.lifecycle` event 不在 mobile live contract
**Verification:** `go build ./...` + `go test ./internal/skills ./internal/app ./internal/cli ./internal/stream`

#### Step 1: 删 `lifecycle_tools.go` 整个文件

```bash
rm internal/skills/lifecycle_tools.go internal/skills/lifecycle_tools_test.go
```

#### Step 2: 删 `writer_lifecycle.go` 中 lifecycle 部分

File: `internal/skills/writer_lifecycle.go`

删除:
- `LifecycleUpdate` 类型(L13-18)
- `UpdateSkillLifecycle` 函数(L20-50)
- `validateLifecycleEvidence` 函数(L52-61)
- `applyLifecycleUpdate` 函数(L63-74)

保留:
- `ReadSkillFile`(L76-103)
- `WriteSkillFile`(L105-140)
- `normalizeCreateInput`(L142-152)
- `buildNormalizedCreateInput`(L154-176)
- `applyCreateInputDefaults`(L178-206)

#### Step 3: 删 `model.go` 中 lifecycle/assessment 类型

File: `internal/skills/model.go`

删除:
- `LifecycleStatus` 类型 + 5 个常量(L24-32)
- `AssessmentVerdict` 类型 + 3 个常量(L51-57)
- `SkillAssessment` 结构体(L59-68)
- `LifecycleEvent` 结构体(L70-81)
- `Spec` 中的 `LifecycleStatus` / `EvidenceRefs` / `ReplacedBy` / `UpdatedByRunID` 字段

保留:
- `Origin` + 常量
- `Source` + 常量
- `ResourceSpec`
- `CreatorOutput`
- `Spec`(基础字段)
- `View`

#### Step 4: 删 `loader.go` 接口的 `UpdateSkillLifecycle` 方法

File: `internal/skills/loader.go`

删除 L35:
```go
	UpdateSkillLifecycle(context.Context, string, LifecycleUpdate) (*Spec, error)
```

#### Step 5: 简化 `health.go`

File: `internal/skills/health.go`

删除:
- `RoutingFixture` 类型(L33-38)
- `RoutingFixtureResult` 类型(L40-48)
- `HealthReport` 中的 `Fixtures` 字段
- `runRoutingFixtures` 方法(L195-208)
- `runRoutingFixture` 函数(L210-262)
- `candidateIDs` 函数(L264-273)
- `hasRoutingMetadata` 函数(L275-280)
- `hasWeakRoutingMetadata` 函数(L282-288)
- `duplicateTriggerKeys` 函数(L290-302)

简化 `BuildHealthReport`(L72-144):删 `report.runRoutingFixtures(ctx, normalized, fixtures)` 调用(L139),改 `BuildHealthReport` 签名删 `fixtures` 参数:
```go
// Before:
func BuildHealthReport(scan ScanResult, ctx EligibilityContext, fixtures []RoutingFixture) (*HealthReport, error) {

// After:
func BuildHealthReport(scan ScanResult, ctx EligibilityContext) (*HealthReport, error) {
```

简化 `CopyHealthReport`:删 `Fixtures` 字段复制。

#### Step 6: 改 `skill_service.go` 的 `Health` 方法

File: `internal/app/skill_service.go`

改 `Health` 签名(L43),删 `fixtures` 参数:
```go
// Before:
func (s *SkillService) Health(ctx context.Context, fixtures []skills.RoutingFixture) (*skills.HealthReport, error) {

// After:
func (s *SkillService) Health(ctx context.Context) (*skills.HealthReport, error) {
```

改 `BuildHealthReport` 调用(L51):
```go
// Before:
	report, err := skills.BuildHealthReport(*scan, staticSkillEligibilityContext(s.cfg), fixtures)

// After:
	report, err := skills.BuildHealthReport(*scan, staticSkillEligibilityContext(s.cfg))
```

#### Step 7: 改 `cli/skills.go` 的 `check` 命令

File: `internal/cli/skills.go`

删除 `--fixtures` flag(L86):
```go
	fixturesPath := fs.String("fixtures", "", "YAML file with routing fixtures")
```

删除 `loadRoutingFixtures` 调用(L90-93):
```go
	fixtures, err := loadRoutingFixtures(*fixturesPath)
	if err != nil {
		return err
	}
```

改 `Health` 调用(L99):
```go
// Before:
	report, err := service.Health(ctx, fixtures)

// After:
	report, err := service.Health(ctx)
```

删除 `loadRoutingFixtures` 函数(L113-127)。

删 `yaml` import 如果不再使用。

#### Step 8: 删 stream 中 `skill.lifecycle` 相关代码

File: `internal/stream/stream_types.go`

删除:
```go
	StreamKindSkillLifecycle      StreamItemKind = "skill.lifecycle"
```

File: `internal/stream/payloads.go`

删除 `StreamSkillLifecycle` 类型(L77 附近)。

File: `internal/stream/accessors.go`

删除 `GetSkillLifecycle` 方法(L264 附近)。

File: `internal/stream/projection.go`

删除 `StreamKindSkillLifecycle` case(L85-86):
```go
	case StreamKindSkillLifecycle:
		return "skill.lifecycle"
```

#### Step 9: 删/改相关测试

- `internal/skills/lifecycle_tools_test.go` — 已在 Step 1 删
- `internal/skills/health_test.go` — 删 `RoutingFixture` 相关测试,改 `BuildHealthReport` 调用(删 fixtures 参数)
- `internal/skills/loader_sop_test.go` — 删 `UpdateSkillLifecycle` 相关测试
- `internal/skills/recommendation_test.go` — 检查是否引用 lifecycle 类型
- `internal/runtime/stream_skill_payload_test.go` — 删 `StreamKindSkillLifecycle` / `StreamSkillLifecycle` 相关测试
- `internal/app/skill_service.go` 相关测试 — 改 `Health` 调用

#### Step 10: 验证

```bash
go build ./...
go test ./internal/skills ./internal/app ./internal/cli ./internal/stream
```

---

### Task 5: 简化 capability_service MCP provider readiness

**Files:** `internal/app/capability_service_snapshot.go`
**Why:** `providerReadinessFromCapability` / `providerStartupReason` / `resolveProviderStatuses` 是过度的 MCP provider 健康状态分层聚合,单用户不需要
**Impact/Compatibility:** `acorn doctor` 和 `/v1/inbox` 的 provider readiness 输出简化
**Verification:** `go build ./...` + `go test ./internal/app` + `acorn doctor` 输出有效

#### Step 1: 读 `capability_service_snapshot.go` 确认 readiness 相关函数

读 L249-375,确认 `resolveProviderStatuses` / `providerReadinessFromCapability` / `providerStartupReason` 的调用链。

#### Step 2: 简化 `snapshotMCPProviders`

保留基本 provider 列表 + startup status,删复杂 readiness 派生。

具体实现需读代码后确认:`ProviderReadinessSummary` 是否保留(简化为 startup_status + auth_status only),还是整个 readiness 层删掉。

#### Step 3: 改 `CapabilitiesService.Snapshot` 调用

如果删了 readiness 层,`Snapshot` 返回的 `SystemCapabilities` 中的 readiness 字段也要调整。

#### Step 4: 改相关测试

`internal/app/capabilities_service_test.go` — 更新 readiness 断言。

#### Step 5: 验证

```bash
go build ./...
go test ./internal/app
# 如有本地 config:
# ./bin/acorn doctor -c configs/acorn.local.yaml
```

---

### Task 6: 全量验证 + 架构守卫

**Files:** 无(验证任务)
**Why:** 确认所有清理后系统仍正常
**Impact/Compatibility:** N/A
**Verification:** 全量测试

#### Step 1: 全量构建 + 测试

```bash
go build ./...
make test
make lint
make format-check
make test-architecture
```

#### Step 2: OpenAPI 合约检查

```bash
python3 mobile/tool/generate_openapi_client.py --check
```

#### Step 3: 检查架构守卫是否需要更新

```bash
go test ./tests/architecture -v
```

如果守卫测试因删除的文件/类型失败,更新守卫。

#### Step 4: 确认无遗留引用

```bash
grep -rn 'OrchestrationMode\|parseClientRunMode\|ErrClientInvalidRunMode\|assembleRunnerByMode\|BuildSkillLifecycleTools\|UpdateSkillLifecycle\|RoutingFixture\|StreamKindSkillLifecycle\|compression_token_counter\|CompressionTokenCounter' --include='*.go' . | grep -v '_test.go'
```

应返回空。

#### Step 5: Commit

```bash
git add -A
git commit -m "refactor: remove mode routing shell + skill lifecycle dead code + faiss artifacts

- Delete OrchestrationMode type + parseClientRunMode + ErrClientInvalidRunMode
- Delete assembleRunnerByMode wrapper, call newDirectResponseRunner directly
- Delete ExecuteRequest.OrchestrationMode field (unused in executor)
- Delete smoke --mode flag + help text plan_execute reference
- Delete compression_token_counter.go (zero references)
- Delete .artifacts/faiss-native/ (13MB FAISS remnant)
- Delete skill lifecycle/assess enterprise mechanism (BuildSkillLifecycleTools
  had zero non-test callers; skill.lifecycle was diagnostic-only)
- Delete RoutingFixture from acorn skills check
- Simplify capability_service MCP provider readiness layer"
```

## Risks

| 风险 | 缓解 |
|---|---|
| `CreateRun` 签名改影响 mobile client | OpenAPI `mode` 字段保持不变,后端接受空值;mobile client 仍可传 mode,后端忽略 |
| `skill_assess` tool 删除影响模型行为 | `BuildSkillLifecycleTools` 从未 wire 到 runtime,模型从未见过 `skill_assess` tool |
| `acorn skills check --fixtures` 删除影响用户 | 单用户工具,fixtures 功能从未被实际使用(spec 明确);help 文本更新 |
| `ParentRunID` / `Depth` 字段删除影响 resume | 需先确认这两个字段是否有引用(Task 2 Step 1 已包含 grep 确认) |
| capability_service 简化影响 doctor 输出 | 保留基本 provider 列表,只删复杂 readiness 派生 |

## Retirement

- `OrchestrationMode` 类型:硬删除,无 compat 路径
- `parseClientRunMode`:硬删除
- `ErrClientInvalidRunMode`:硬删除
- `assembleRunnerByMode`:硬删除,直接调 `newDirectResponseRunner`
- `compression_token_counter.go`:硬删除(零引用)
- `.artifacts/faiss-native/`:硬删除
- `BuildSkillLifecycleTools` / `lifecycle_tools.go`:硬删除(零非测试调用者)
- `UpdateSkillLifecycle`:硬删除
- `RoutingFixture`:硬删除
- `StreamKindSkillLifecycle`:硬删除
- capability_service readiness 复杂层:硬删除
