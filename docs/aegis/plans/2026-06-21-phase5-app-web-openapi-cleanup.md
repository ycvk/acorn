# Acorn 重构 - Phase 5: App + Web + OpenAPI 清理

Date: 2026-06-21
Parent Spec: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md`
Depends on: Phase 1-4

## Goal

清理 app 组合根和 web 层。删除 MCP server mode、plan/skill-lifecycle/procedure API、capability service 复杂部分。精简 OpenAPI schema。更新架构守卫测试。

## Architecture

```text
本阶段范围:
  app/       — 删除 mcpServer/serveToolset/skill_service/capability_service 复杂部分
  web/       — 删除 /mcp routes + plan/skill-lifecycle DTO
  docs/openapi.yaml — 精简 schema
  tests/architecture/ — 更新守卫
```

## Baseline / Authority Refs

- Spec §3.9 API 契约简化、§3.12 Release 简化
- `internal/app/container.go` — 组合根
- `internal/web/routes.go` — 路由
- `docs/openapi.yaml` — wire contract

## Compatibility Boundary

- `/v1` routes 保留(threads/runs/events/inbox/pending-actions/memory/skills/system)
- `/healthz` 保留
- `/mcp` server mode 删除
- device auth 不变
- RunEvent live subset 不变

## Verification

- `go build ./... && go test ./...` 通过
- `make lint && make format-check` 通过
- `make test-architecture` 通过

---

## Task 1: 简化 app container 装配

**Files:**
- Modify: `internal/app/container.go`
- Modify: `internal/app/container_runtime_deps.go`
- Modify: `internal/app/container_app_services.go`
- Modify: `internal/app/container_store_ports.go`
- Delete: `internal/app/mcp_wiring.go`(server mode 部分)
- Delete: `internal/app/skill_service.go`(如果不再需要)
- Delete: `internal/app/capability_service.go`
- Delete: `internal/app/capability_service_snapshot.go`
- Delete: `internal/app/skill_eligibility.go`
- Delete: `internal/app/memory_lazy.go`
- Delete: `internal/app/memory_lazy_test.go`
- Delete: `internal/app/memory_wiring.go`
- Delete: `internal/app/container_bleve_faiss_test.go`
- Delete: `internal/app/container_no_bleve_faiss_test.go`
- Modify: `internal/app/executor_factory.go`
- Modify: `internal/app/run_resume_service.go`
- Modify: `internal/app/run_once.go`
- Modify: `internal/app/context_wiring.go`

**Why:** container 装配了已删除的 plan store / child agent executor / mcp server / bleve-faiss / skill lifecycle。

**Impact/Compatibility:** `Container` struct 删除 `mcpServer`、`serveToolset`、`skills`、`capabilities` 字段(或简化)。`buildContainer` 删除 `buildContainerMCPServer`、bleve-faiss lazy wiring。`buildContainerRuntimeDeps` 删除 plan store / child agent executor factory。`buildContainerAppServices` 删除 skill service / capability service 复杂部分。`container_store_ports.go` 删除 plan/skill snapshot store port。

**Verification:** `go build ./internal/app && go test ./internal/app`

### Steps

- [ ] **1.1 重写 `container.go`**:`Container` struct 删除 `mcpServer`、`serveToolset`。保留 `store`、`runnerFactory`、`runController`、`runResume`、`checkpoints`、`client`、`pendingAction`、`memory`、`deviceAuth`、`inbox`。`buildContainer` 删除 `buildContainerMCPServer` 调用。`Close()` 删除 `serveToolset` / `mcpServer` 关闭。
- [ ] **1.2 重写 `container_runtime_deps.go`**:删除 plan store / child agent executor factory 装配。简化为 store + workspace + skills loader + memorymodule service + contextplane + orchestration。
- [ ] **1.3 重写 `container_app_services.go`**:删除 skill service / capability service 装配。保留 client / pendingAction / memory / deviceAuth / inbox / runResume / checkpoints。
- [ ] **1.4 重写 `container_store_ports.go`**:删除 plan/skill snapshot store port。保留 clientStore / runResumeStore / pendingActionCreateStore。
- [ ] **1.5 删除 `mcp_wiring.go`** 的 server mode 部分。如果文件有 client wiring,保留 client 部分。
- [ ] **1.6 删除 `skill_service.go` / `capability_service.go` / `capability_service_snapshot.go` / `skill_eligibility.go` / `memory_lazy.go` / `memory_wiring.go`**。
- [ ] **1.7 删除测试文件**:`memory_lazy_test.go` / `container_bleve_faiss_test.go` / `container_no_bleve_faiss_test.go` / `capabilities_service_test.go`(如果存在)。
- [ ] **1.8 重写 `executor_factory.go`**:删除 plan store / child agent executor 参数。简化为 store + runRuntime + controller。
- [ ] **1.9 重写 `run_resume_service.go`**:删除 plan/single_agent resume 逻辑。保留 direct_response resume。
- [ ] **1.10 重写 `context_wiring.go`**:删除 BudgetGovernor / CompactionEngine wiring。简化为 ContextPlane + ContextSession 装配。
- [ ] **1.11 运行验证**:`go build ./internal/app && go test ./internal/app`。修复编译错误直到通过。
- [ ] **1.12 Commit**:`refactor(app): remove mcp-server/plan/skill-lifecycle/capability/bleve wiring`

---

## Task 2: 简化 web 层

**Files:**
- Modify: `internal/web/routes.go`
- Modify: `internal/web/server.go`
- Modify: `internal/web/dto_run.go`
- Modify: `internal/web/dto_run_detail.go`
- Modify: `internal/web/dto_skill.go`
- Modify: `internal/web/dto_system.go`
- Modify: `internal/web/dto_decision.go`
- Modify: `internal/web/converter_gen.go`
- Modify: `internal/web/converter.go`
- Modify: `internal/web/handlers_resource.go`
- Modify: `internal/web/handlers_run.go`
- Modify: `internal/web/handlers_skills_test.go`
- Modify: `internal/web/openapi_test.go`
- Modify: `internal/web/client_handlers_test.go`

**Why:** web 层有 plan / skill-lifecycle / procedure / context-boundary 相关 DTO 和 handler。需要删除。

**Impact/Compatibility:** `routes.go` 删除 `/mcp` route group。`dto_run.go` 删除 `orchestration_mode` / `skill_id` / `depth` / `parent_run_id` 字段。`dto_system.go` 删除 provider readiness 中的 plan/skill 相关字段。`converter_gen.go` 删除 plan/skill-lifecycle/procedure 转换函数。`handlers_resource.go` 删除 plan/skill-lifecycle/procedure endpoint handler。

**Verification:** `go build ./internal/web && go test ./internal/web`

### Steps

- [ ] **2.1 重写 `routes.go`**:删除 `/mcp` route group 和 `registerRoutes` 中的 mcp 路由。保留 `/v1` + `/healthz`。
- [ ] **2.2 重写 `server.go`**:`Dependencies` 删除 `Skills`(如果 skill_service 删除)、`Capabilities`(如果 capability_service 删除)。`Server` 删除对应字段。
- [ ] **2.3 重写 `dto_run.go`**:`CreateRunRequest` 删除 `mode` 字段(或固定 direct_response)。`Run` DTO 删除 `orchestration_mode` / `skill_id` / `depth` / `parent_run_id`。`Run` 的 `mode` 字段固定返回 `direct_response`(不再有 `agent` 映射)。
- [ ] **2.4 重写 `dto_run_detail.go`**:删除 plan / context-boundary / provider_usage 相关字段。
- [ ] **2.5 重写 `dto_skill.go`**:删除 lifecycle / evidence / assess 相关字段。保留只读 list/detail/files。
- [ ] **2.6 重写 `dto_system.go`**:删除 capability snapshot 中的 plan/skill readiness 字段。简化为 `runtime_readiness` + `provider_readiness`。
- [ ] **2.7 重写 `converter_gen.go`**:删除 plan/skill-lifecycle/procedure/context-boundary 转换函数。
- [ ] **2.8 重写 `handlers_resource.go`**:删除 plan/skill-lifecycle/procedure endpoint handler。保留 thread/run/message/memory/skill/device/pending-action/inbox/system handler。
- [ ] **2.9 更新测试**:`openapi_test.go` 删除 plan/skill-lifecycle schema 断言。`client_handlers_test.go` 删除 mode/plan 测试。
- [ ] **2.10 运行验证**:`go build ./internal/web && go test ./internal/web`。修复编译错误直到通过。
- DTO_CONVERSION_DIR: `internal/web/converter.go` — 审查是否需要更新(如果引用已删除 DTO)。
- [ ] **2.11 Commit**:`refactor(web): remove /mcp + plan/skill-lifecycle/procedure DTO and handlers`

---

## Task 3: 精简 OpenAPI schema

**Files:**
- Modify: `docs/openapi.yaml`

**Why:** wire contract 必须与后端 DTO 同步。

**Impact/Compatibility:** 删除:`mode` 字段(create-run request)、`orchestration_mode` schema、plan/plan_evidence/skill.lifecycle/procedure.activation schema、context_boundary/context_pressure schema、`child_run`/`parent_run_id`/`depth` 字段。保留:threads/runs/events/inbox/pending-actions/memory/skills/system/devices schema。

**Verification:** `python3 mobile/tool/generate_openapi_client.py --check`(此时会 fail,因为 Flutter 还没重写——Phase 6 修复。本 task 只验证 yaml 语法正确)

### Steps

- [ ] **3.1 编辑 `docs/openapi.yaml`**:删除上述 schema 和字段。`CreateRunRequest` 删除 `mode` 字段或固定为 `direct_response`。`Run` schema 删除 `orchestration_mode`。保留 `RunEvent` schema(不变)。
- [ ] **3.2 验证 yaml 语法**:`python3 -c "import yaml; yaml.safe_load(open('docs/openapi.yaml'))"`。确认无语法错误。
- [ ] **3.3 Commit**:`refactor(openapi): remove plan/skill-lifecycle/procedure/context-boundary schema`

---

## Task 4: 更新架构守卫测试

**Files:**
- Modify: `tests/architecture/store_interface_count_test.go`
- Modify: `tests/architecture/store_boundary_test.go`
- Modify: `tests/architecture/structural_limits_test.go`
- Modify: `tests/architecture/client_projection_boundary_test.go`
- Delete: `tests/architecture/bleve_faiss_release_guard_test.go`(Phase 1 已删,确认)
- Delete: `tests/architecture/assembly_consolidation_test.go`(如果 Phase 2 已删,确认)
- Delete: `tests/architecture/action_round_sharing_test.go`(如果 Phase 2 已删,确认)

**Why:** 守卫测试需要匹配新架构。

**Verification:** `go test ./tests/architecture/...`

### Steps

- [ ] **4.1 确认 `store_interface_count_test.go`**:接口数 ≤4(删除 plan/skill snapshot store port 后)。
- [ ] **4.2 确认 `structural_limits_test.go`**:`refactorOwnedDirs` 已删除 `internal/runtime/plan`、`internal/contextplane/compaction`。从 `dirsEnforcingFuncLimits` 确认不需要添加新目录。
- [ ] **4.3 确认 `client_projection_boundary_test.go`**:已删除 plan/skill-lifecycle schema 断言。
- [ ] **4.4 确认已删除的守卫测试文件**:`bleve_faiss_release_guard_test.go`、`assembly_consolidation_test.go`、`action_round_sharing_test.go`。
- [ ] **4.5 运行验证**:`go test ./tests/architecture/...`。必须通过。
- [ ] **4.6 Commit**:`test(architecture): finalize guard updates for refactored architecture`

---

## Task 5: 全量验证

### Steps

- [ ] **5.1 运行**:`go build ./...`。必须通过。
- [ ] **5.2 运行**:`go test ./...`。必须通过。
- [ ] **5.3 运行**:`make lint && make format-check`。必须通过。
- [ ] **5.4 运行**:`make test-architecture`。必须通过。
- [ ] **5.5 运行**:`CGO_ENABLED=0 go build ./...`。确认无 CGO 依赖。
- [ ] **5.6 Commit**:`chore: phase 5 app+web+openapi cleanup complete`
