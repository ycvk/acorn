# Acorn 架构不变量

每条不变量标注执行它的测试文件。新增不变量须同步加测试。

## 运行时与编排

- **单一编排模式 direct_response**：`AgentLoop.RunOneIteration` 通过 `orchestration.RunActionRound`（ExecuteRound）执行模型回合。plan_execute/single_agent 模式已删除。ContextSession 在 BeforeModelCall 中执行 masking + auto-compact。
  - `tests/architecture/runtime_split_test.go`
- **结构守卫覆盖全包**：`tests/architecture/structural_limits_test.go` 的 `refactorOwnedDirs` 覆盖 `internal/runtime`、`internal/runtime/toolset`、`internal/tools`、`internal/contextplane`、`internal/store/sqlite`、`internal/memorymodule`、`internal/app`、`internal/orchestration`、`internal/providers/mcp`、`internal/web`、`internal/config`、`internal/workspace`、`internal/skills`、`internal/webaccess`。所有目录强制文件 ≤400 行；`internal/runtime` 和 `internal/runtime/toolset` 额外强制函数 ≤30 行、嵌套 ≤3 层。generated files（`*_gen.go`）被守卫排除。
  - `tests/architecture/structural_limits_test.go`
  - `tests/architecture/runtime_split_test.go`

## 持久化与 store 边界

- **SQLite adapter 不跨层泄漏**：production 代码只允许 `internal/app/container.go` 直接 import `internal/store/sqlite`；其他包只依赖 consumer-owned ports 或 `internal/store` shared records/errors。
  - `tests/architecture/store_boundary_test.go`
- **Consumer-owned store 接口收敛**：`internal/runtime` + `internal/app` 顶层定义的 consumer-owned store 接口（Store/Port/Repository/Ledger）≤6（ExecutorStore、RunnerFactoryStore、containerRuntimeStore、containerAppStore、PendingActionCreateStore、skillSnapshotStore）。
  - `tests/architecture/store_interface_count_test.go`

## 上下文与记忆

- **Hybrid context: masking + auto-compact**：ContextSession 在 BeforeModelCall 中执行 observation masking（旧 tool result 替换为占位符）+ LLM auto-compact（token 超阈值时生成 summary，circuit breaker 3 次失败后停止）；public YAML 只暴露 `context.window_tokens`、`context.compact_margin_tokens`、`context.mask_after_turns`、`context.preserve_recent_turns`。
  - `internal/contextplane/context_session_test.go`
  - `internal/contextplane/masking_test.go`
  - `internal/contextplane/auto_compact_test.go`
- **Memory Record V2 是长期记忆事实**：facts/history frontmatter 由 `internal/memorymodule` 解析；semantic search 走 embedding + SQLite 暴力余弦相似度。
  - `internal/memorymodule/fact_learning_test.go`

## Remote API 与 mobile

- **Remote client 必须设备认证**：除 `/healthz` 和 `POST /v1/devices:pair` 外，`/v1` 只接受 valid device bearer token；missing/malformed/unknown 返回 `unauthenticated`，revoked 返回 `device_revoked`。
  - `internal/app/device_auth_service_test.go`
- **OpenAPI 是 wire contract**：remote client DTO 只投影 app/runtime domain；改 wire shape 须同步 `docs/openapi.yaml` + generated mobile client。
  - `tests/architecture/client_projection_boundary_test.go`
- **Mobile 是 control surface 不是 runtime**：mobile 不执行 run、不持 runtime truth、不做 offline-first run execution、不维护第二套 message lifecycle；context pressure/boundary/run status 都消费后端 projection。
  - `mobile-kotlin/app/src/test/...`（JUnit）

## 代码规范

- **Error 分两类**：Exported sentinel error（需要被 `errors.Is` 比对）必须是包级 `var ErrXxx`；precondition/internal-config error（不该发生的编程错误）用 inline `errors.New("...")` 直接返回。`.golangci.yml` 的 `errname` linter 强制导出 sentinel 命名。
  - `.golangci.yml`（errname linter）
- **Consumer-owned port 接口重复是故意的**：`tools.DelegateTaskContext`、`tools.OperatorQuestionContext`、`tools.ArtifactContext`、`skills.RunContextBridge`、`skills.LifecycleEventAppender` 与 `runtime/api.RunContextBridge`、`runtime/api.EventAppender` 结构相同但不可合并——合并会创建 import cycle（`runtime → tools/skills → runtime/api`）。`orchestration.PlanStore` 空标记接口同理（`orchestration → runtime/api` 禁止）。
  - `tests/architecture/store_boundary_test.go`（import direction 不可破坏）
