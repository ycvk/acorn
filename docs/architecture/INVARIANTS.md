# Acorn 架构不变量

每条不变量标注执行它的测试文件。新增不变量须同步加测试。

## 核心层 (core)

- **core 有零内部导入**：`internal/core` 不导入任何 `github.com/ycvk/acorn/internal/*` 包；core 是 Layer 0，只依赖外部 SDK（Eino schema/adk）。
  - `tests/architecture/dependency_direction_test.go`
- **core 拥有 3 个 store 接口**：`SessionStore`/`IdentityStore`/`ArtifactStore` 是 core 定义的 consumer-owned 持久化接口，取代旧的 SessionRepo/MessageRepo/RunRepo/EventRepo/PendingActionRepo/DeviceRepo/ArtifactRepo/SummaryRepo/OAuthRepo。
  - `internal/core/store.go`
  - `internal/core/core_test.go`

## 运行时与编排

- **单一编排模式 direct_response**：`directResponseAgent.runFromState` 直接调用 `ExecuteRound` 执行模型回合（`AgentLoop` 中间层已内联删除）。plan_execute/single_agent 模式已删除。Session 在 BeforeModelCall 中执行 masking + auto-compact。runtime 包合并了 `agent`+`context`+`stream`，依赖方向无环。
  - `tests/architecture/structural_limits_test.go`
- **runtime 执行链自包含**：`internal/runtime` 拥有 executor、per-run assembly（7 个 struct）、direct_response、ExecuteRound、Plane、Session、masking、auto-compact、StreamItem 投影；不依赖 `internal/agent`/`internal/context`/`internal/stream`（已全部合并删除）。
  - `tests/architecture/structural_limits_test.go`
- **结构守卫覆盖全包**：`tests/architecture/structural_limits_test.go` 的 `refactorOwnedDirs` 覆盖所有 12 个 `internal/` 重构目录（core/runtime/store/tools/memory/mcp/api/wire/config/workspace/skills/webaccess）。所有目录强制文件 ≤800 行。generated files（`*_gen.go`）和 test 文件被守卫排除。
  - `tests/architecture/structural_limits_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/wire/container.go` 直接 import `internal/store`；其他包只依赖 consumer-owned ports（`core.SessionStore`/`core.IdentityStore`/`core.ArtifactStore`）或 `internal/store` shared records/errors。
  - `tests/architecture/dependency_direction_test.go`
- **Consumer-owned store 接口收敛**：`internal/runtime` + `internal/wire` 顶层定义的 consumer-owned store 接口（Store/Port/Repository/Ledger）≤4（ExecutorStore、RunnerFactoryStore、containerRuntimeStore）。
  - `tests/architecture/store_interface_count_test.go`

## 上下文与记忆

- **Hybrid context: masking + auto-compact**：Session 在 BeforeModelCall 中执行 observation masking（旧 tool result 替换为占位符）+ LLM auto-compact（token 超阈值时生成 summary，circuit breaker 3 次失败后停止）；public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`、`context.preserve_recent_turns`。
  - `internal/runtime/context_session_test.go`
  - `internal/runtime/masking_test.go`
  - `internal/runtime/auto_compact_test.go`
- **Memory Record V2 是长期记忆事实**：facts/history frontmatter 由 `internal/memory` 解析；semantic search 走 embedding + SQLite 暴力余弦相似度。
  - `internal/memory/service_test.go`

## Remote API 与 mobile

- **Remote client 必须设备认证**：除 `/healthz` 和 `POST /v1/devices:pair` 外，`/v1` 只接受 valid device bearer token；missing/malformed/unknown 返回 `unauthenticated`，revoked 返回 `device_revoked`。
  - `internal/store/store_schema_test.go`
- **OpenAPI 是 wire contract**：remote client DTO 只投影 core domain 类型；改 wire shape 须同步 `docs/openapi.yaml` + generated mobile client。`clientevents` 包已合并进 `internal/api`，投影逻辑在 `projection.go`/`projection_helpers.go` 中，不导入 `internal/runtime`。`thread_service.go`/`event_service.go` 合法导入 `internal/core`，不在 projection boundary 列表中。
  - `tests/architecture/client_projection_boundary_test.go`
- **Mobile 是 control surface 不是 runtime**：mobile 不执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle；context pressure/boundary/run status 都消费后端 projection。
  - `mobile-kotlin/app/src/test/...`（JUnit）

## 代码规范

- **Error 分两类**：Exported sentinel error（需要被 `errors.Is` 比对）必须是包级 `var ErrXxx`；precondition/internal-config error（不该发生的编程错误）用 inline `errors.New("...")` 直接返回。`.golangci.yml` 的 `errname` linter 强制导出 sentinel 命名。
  - `.golangci.yml`（errname linter）
