# Acorn 全面重构设计

Date: 2026-06-21
Status: Design Spec (pending user review)
Scope: backend + frontend architecture

## 1. 动机与问题诊断

### 1.1 当前问题

Acorn 当前架构边界清晰(有 INVARIANTS.md + 多个架构守卫测试),但作为**单用户自托管系统**背了大量企业级机制:

1. **三套编排模式**(`direct_response` / `plan_execute` / `single_agent`):plan_execute 贡献了 `internal/runtime/plan/` 整个子包、`ChildAgentExecutor`、`SubagentExecutor`、verifier child run、plan evidence ledger。单人 VPS 使用几乎不触发。
2. **过度复杂的上下文压缩**:`CompactionEngine` + 8 种 rehydration packet + `BudgetGovernor` effective window 派生阈值 + reactive compact 重试。现代模型 200K+ context window,单会话积满需要很长时间。
3. **Bleve + FAISS 重依赖链**:CGO + FAISS C 库 + release artifacts + 跨平台编译,全为了单用户语义检索。整个项目最重的依赖。
4. **skill 双轨制**:`internal/skills`(executable specs)+ `memorymodule/skills/`(procedure records with origin/status/verification)。两套系统的认知负担巨大。
5. **tool lifecycle pruning + durable ledger**:tool result 超 turn 窗口替换为 `tool_result_ref` marker,每次 tool result 走一遍 SQLite ledger。
6. **Flutter + OpenAPI 生成链路**:维护三个合约(OpenAPI yaml → generated Dart → Flutter UI)同步成本高。
7. **MCP server mode**:单用户不需要把工具暴露给外部 MCP 客户端。

### 1.2 目标

在保留核心能力(自托管 agent、工具执行、移动控制面、持久化、可中断恢复、审批)的前提下,大幅降低代码复杂度和维护成本。

## 2. 决策记录

基于实际使用情况确认的决策:

| 决策点 | 选择 | 理由 |
|---|---|---|
| 编排模式 | 只保留 `direct_response`,砍 plan_execute + single_agent | 单用户日常通用助手,模型自己分步即可 |
| Skill 系统 | 保留简化版(markdown + 简单匹配),去掉 lifecycle/evidence | 偶尔用 skill,不需要企业级知识管理 |
| 上下文压缩 | hybrid:observation masking + LLM auto-compact + re-inject(业界主流最佳实践) | 参考 Claude Code auto-compact + JetBrains 研究结论 |
| 语义检索 | 方案 A:OpenAI embedding + SQLite BLOB + 纯 Go 暴力余弦相似度 | 保留语义检索能力,零 CGO |
| 记忆工具 | 保留 agent 自主记忆(remember / memory_search) | 日常通用助手需要跨会话记忆 |
| 中断+审批 | 保留 pending_actions + operator_question / elicitation | 核心产品价值 |
| MCP | 只保留 client(连外部 server),砍 server mode | 单用户不暴露工具 |
| 移动端 | 保留 Flutter 但彻底重写,模块化 | 用户选择 |
| 旧数据 | 清空重来,不迁移历史 | 单用户可接受 |
| provider_usage | 不保留独立表,靠 events 投影 | 减少表数量 |
| Flutter 状态管理 | Riverpod(StateNotifier / AsyncNotifier) | 当前已用 ProviderScope,Riverpod 是演进版 |

## 3. 目标架构

### 3.1 总览

```text
operator CLI(仅运维命令)
  -> app.Container(组合根)
  -> /v1 API + device auth middleware
  -> runtime.Executor
  -> direct_response(model + tool loop)
     ├─ ContextSession(observation masking + auto-compact + re-inject)
     ├─ Tools(workspace · command · web · mcp-client · memory)
     └─ SQLite persistence
  -> Flutter mobile control surface
```

### 3.2 包结构(重构后)

```text
internal/
  app/                    # 组合根:唯一实例化具体实现处
    container.go          # 装配 store + executor + services + web deps
    container_*.go        # 分文件装配
  runtime/
    executor.go           # Executor 入口
    executor_run.go       # run lifecycle
    executor_finalize.go  # 终态收口
    runner.go             # RunnerFactory
    runner_build.go       # per-run assembly
    tool/
      tool.go             # 工具构建 + 调度(简化)
      tool_audit.go       # 审计包装
    toolset/
      memory_tools.go     # remember / memory_search
  contextplane/
    context_session.go    # ContextSession(简化:masking + auto-compact)
    assembly.go           # 上下文组装
    tool_lifecycle.go     # 简化(只保留 loaded/deferred,去掉 pruning/ledger)
  orchestration/
    direct_response.go    # direct_response builder + agent loop
    types.go
  memorymodule/
    service.go            # LocalService
    prepare.go            # Prepare
    search.go             # Search(embedding + FTS5 fallback)
    semantic.go           # embedding 调用 + SQLite 向量存储
    fact_learning.go      # CreateFact
    history.go            # AppendHistory
    frontmatter.go        # Record V2 frontmatter(简化)
  store/
    store.go              # shared records + sentinel errors
    sqlite/
      store_schema.go     # schema(~8 tables)
      store_*.go          # adapter
  web/
    server.go             # Dependencies + Server
    routes.go             # /v1 + /healthz
    handlers_*.go         # handlers
    dto_*.go              # DTO
  config/
    config.go             # Config struct(精简)
    config_load.go
    config_defaults.go
    config_validate.go
  clientevents/
    projector.go          # RunEvent 投影(mobile live subset)
  events/                 # RunEvent types
  tooling/                # ToolContract(简化)
  tools/                  # 具体工具实现
  skills/                 # skill loader(简化)
  workspace/              # workspace 工具
  providers/
    mcp/                  # MCP client(只保留 client)
  model/                  # session summary 等辅助
```

### 3.3 砍掉的包/文件清单

#### 完整删除的目录

- `internal/runtime/plan/` — plan_execute graph + dispatch + evidence
- `internal/contextplane/compaction/` — CompactionEngine + rehydration + packets
- `internal/store/sqlite/` 中以下文件:
  - `store_plan.go` / `store_plan_evidence.go`(如果有)
  - `store_context_boundary.go`
  - `store_tool_results.go`(如果有独立文件)
  - `store_run_archive.go`
  - `store_provider_usage.go`

#### 完整删除的文件

- `internal/runtime/subagent_executor.go` + `subagent_executor_*.go`(5 个文件)
- `internal/runtime/runner_childagent.go`
- `internal/runtime/runner_orchestration.go` 中 plan_execute / single_agent 分发
- `internal/orchestration/single_agent_builder.go`
- `internal/orchestration/child_agent.go`
- `internal/runtime/runner_toolset_skill.go`(skill lifecycle 相关)
- `internal/runtime/runner_toolset_emit.go`(如果只服务 plan evidence)
- `internal/runtime/plan/` 下所有文件
- `internal/app/mcp_wiring.go` 中 server mode 部分(保留 client wiring)
- `internal/app/skill_service.go`(如果 skill 只读不再需要独立 service)
- `internal/app/capability_service.go` / `capability_service_snapshot.go`(简化为系统状态检查)
- `internal/runtime/tool/safe_parallel_tools_node.go` 中的路径冲突检测(简化为 read-only 并行 / write 串行)
- `internal/contextplane/tool_lifecycle.go` 中的 `PruneToolMessages` / `UpdateToolResultRecord`
- `internal/contextplane/context_session_compact.go`
- `internal/contextplane/context_overflow_error.go`
- `internal/contextplane/compression_token_counter.go`(如果简化后不需要)
- `internal/contextplane/skill_provider.go` / `skill_catalog_provider.go` / `selected_skill.go`
- `internal/contextplane/memory_provider.go` / `providers_test.go`
- `internal/contextplane/run_context_snapshot.go`(如果只服务 compact resume)
- `internal/contextplane/tool_lifecycle_runtime.go`
- `internal/contextplane/envelope.go` / `helpers.go`
- `internal/contextplane/message_utils.go`(部分保留)
- `internal/contextplane/budget_governor.go`(简化为固定阈值)
- `internal/memorymodule/procedure_learning.go`
- `internal/memorymodule/mutation_plan.go` / `mutation_apply.go`(如果 memory 写入简化)
- `internal/memorymodule/bleve_index*.go` / `semantic_rebuild.go` / `semantic_search.go`(替换为新 embedding + SQLite 方案)
- `internal/memorymodule/selection.go`(简化 active selection)
- `internal/memorymodule/bleve_disabled.go`
- `internal/web/converter_gen.go` 中 plan/skill lifecycle 相关(如果简化后不需要)
- `internal/app/container_bleve_faiss_test.go` / `container_no_bleve_faiss_test.go`
- `internal/app/skill_eligibility.go`
- `internal/app/memory_lazy.go` / `memory_lazy_test.go`(不再需要 lazy wiring)
- `internal/app/memory_wiring.go`(简化)
- `internal/app/container_store_ports.go` 中 plan/skill snapshot store port
- `internal/app/pending_action_service_decision.go`(如果审批简化)

#### 简化而非删除的文件

- `internal/runtime/runner_orchestration.go` — 删除 plan_execute / single_agent 分发,保留 direct_response
- `internal/runtime/runner_build.go` — 删除 buildSingleAgentRun / buildPlanExecuteRun,保留 directResponseRequest
- `internal/runtime/runner_build_selection.go` — 删除 run selection 复杂逻辑,保留简单 skill 匹配
- `internal/runtime/runner_toolset.go` — 删除 skill lifecycle 相关,保留 tool catalog 构建
- `internal/runtime/executor_run.go` — 删除 mode 路由,固定 direct_response
- `internal/runtime/executor_finalize.go` — 删除 skill lifecycle / plan evidence 收口
- `internal/runtime/tool/tool.go` — 简化调度:read-only 并行 / write 串行,去掉路径冲突检测
- `internal/contextplane/context_session.go` — 去掉 ReactiveCompact / CompressionState / pressure 评估,加入 masking + auto-compact
- `internal/contextplane/assembly.go` — 去掉 checkpoint / skill catalog / packet 组装
- `internal/contextplane/tool_lifecycle.go` — 去掉 pruning / ledger / RecentResults
- `internal/orchestration/types.go` — 删除 SingleAgentRequest / PlanExecuteRequest / PlanStore 等
- `internal/orchestration/direct_response_builder.go` — 保留并简化
- `internal/config/config.go` — 删除 ContextConfig / MemorySemanticConfig / BleveSemanticConfig / ServeConfig 等
- `internal/config/config_defaults.go` — 精简默认值
- `internal/config/config_validate.go` — 删除 FAISS / semantic / plan 相关校验
- `internal/store/store.go` — 删除 plan / context_boundary / tool_result 相关 records
- `internal/store/sqlite/store_schema.go` — 删除砍掉表的 migration / required columns
- `internal/store/sqlite/store_schema_drops.go` — 主动 drop 旧表
- `internal/store/sqlite/store_schema_bootstrap.go` — 精简 schema
- `internal/store/sqlite/store_run.go` — 删除 orchestration_mode / skill_id / depth / parent_run_id
- `internal/events/` — 删除 plan / skill lifecycle / procedure activation 相关 event types
- `internal/clientevents/projector.go` — `liveRunEventKinds` 保持不变
- `internal/web/dto_*.go` — 删除 plan / skill lifecycle / procedure 相关 DTO
- `internal/tooling/contracts.go` — 简化 ToolContract
- `internal/tooling/specs.go` — 删除 PlanPolicy / EvidencePolicy / ResourceScope 复杂部分
- `internal/skills/` — 简化为 markdown loader + 简单匹配
- `internal/memorymodule/types.go` — 删除 ProcedureRecord / relation 复杂类型
- `internal/memorymodule/frontmatter.go` — 简化 Record V2 frontmatter(去掉 relation / procedure origin)
- `internal/memorymodule/service.go` / `prepare.go` / `search.go` — 适配新检索方案
- `internal/app/container.go` — 删除 mcpServer / serveToolset / runnerFactory 的 plan/skill 装配
- `internal/app/container_runtime_deps.go` — 删除 plan store / child agent executor factory
- `internal/app/container_app_services.go` — 删除 skill service / capability service 复杂部分

### 3.4 SQLite Schema(重构后)

~8 张表(从 ~23 张):

| 表 | 用途 | 关键列 |
|---|---|---|
| `sessions` | 会话 | id, title, created_at |
| `messages` | 消息 | id, session_id, turn_index, role, content, content_parts, created_at |
| `runs` | run 记录 | id, session_id, turn_index, input, status, output, error, started_at, finished_at |
| `events` | run 事件流 | id, run_id, session_id, sequence, kind, payload_json, created_at |
| `pending_actions` | 审批动作 | id, run_id, interrupt_id, kind, subject, payload_json, status, mode, reason, rule, created_at, decided_at |
| `devices` | 设备认证 | device_id, name, platform, token_hash, created_at, last_seen_at, revoked_at |
| `pairing_codes` | 配对码 | code_hash, expires_at, used_at, created_at |
| `owner_profile` | owner | owner_id, created_at |
| `memory_vectors`(可选) | embedding 向量 | ref, kind, content_hash, vector_blob, model, dimensions, created_at |

migration 策略:**清空重来**。新 schema 在 `store_schema_bootstrap.go` 中定义。`store_schema_drops.go` 主动 drop 旧表:
- `plans` / `plan_evidence` / `plan_steps`
- `tool_results`
- `context_boundaries`
- `conversation_segments`
- `run_archives`
- `working_checkpoints`
- `provider_usage`
- `run_decisions`(legacy)

### 3.5 编排层简化

**砍掉**:
- 三模式路由(`resolveRootOrchestrationMode`)
- `plan_execute` graph(init → plan → executeDispatch → observe → closeout)
- `single_agent` graph(PlanNode → ActNode → Observe → Final)
- `ChildAgentExecutor` + `SubagentExecutor`
- verifier child run + plan evidence ledger
- worktree 隔离(child workspace)
- `orchestration_mode` / `skill_id` / `depth` / `parent_run_id` 列
- run selection by decision

**保留/简化**:
- `direct_response`:model → tool loop → record → 下一轮
- `AgentLoop.RunOneIteration`:每轮 `BeforeModelCall → ExecuteRound → RecordAssistant/RecordToolResults`
- `ExecuteRound`:模型流式调用 → 实时工具提交 → 终态结果收集
- run selection 简化为:有 explicit skill → 加载 skill context;否则无 skill context

### 3.6 上下文/压缩简化

采用业界主流最佳实践:**hybrid 方案(observation masking + LLM auto-compact + 关键上下文 re-inject)**。参考 Claude Code auto-compact 设计,但大幅简化当前 Acorn 的 8 种 packet + structured continuation summary + BudgetGovernor 派生阈值。

设计依据:
- JetBrains 研究表明 observation masking 单独使用常优于 LLM summarization,且快且便宜
- Claude Code 在 `effectiveWindow - 13000` 阈值触发 auto-compact,释放 40-60% context
- compact 后 re-inject system prompt / memory / skills 从 disk 重新加载
- circuit breaker:连续 3 次失败停止 auto-compact

**砍掉**:
- `CompactionEngine` 的 structured continuation summary + required sections 校验
- 8 种 rehydration packet(working checkpoint / selected skill / skill catalog / tool state / session summary / prepared memory / plan / recent files)
- `BudgetGovernor` effective window 派生阈值(改为简单阈值:`effectiveWindow - compactMargin`)
- `CompactTriggerReactive` + reactive compact 重试(provider overflow 不单独处理,auto-compact 覆盖)
- `CompressionState` / `CompressionOutcome` / `CompactTrigger` 类型
- `tool_results` durable ledger(tool result 随消息走,masking 时替换为占位符)
- `tool_result_ref` marker(改为简单占位符 `[tool result elided: tool=<name>, call_id=<id>]`)
- `context_boundaries` 表(compact 边界不持久化,改为内存状态)

**保留/替换**:

`ContextSession` 接口简化:
```go
type ContextSession interface {
    Bootstrap(ctx, req) (*ModelInput, error)        // 首轮:assembly + initial user messages
    BeforeModelCall(ctx, req) (*ModelInput, error)  // 每轮:masking + auto-compact 检查
    RecordAssistant(ctx, msg) error
    RecordToolResults(ctx, results) error
}
```

**三层 context 策略**(替代当前 8 种 packet):

1. **Observation masking(默认防线)**:
   - tool result 超过 `mask_after_turns`(默认 2)轮后,用占位符替换完整 output
   - 占位符格式:`[tool result elided: tool=<name>, call_id=<id>]`
   - 不写 SQLite,不持久化,纯内存操作
   - 保留最近 `mask_after_turns` 轮的完整 tool result

2. **LLM auto-compact(接近 limit 时触发)**:
   - 阈值:`effectiveWindow - compact_margin_tokens`(默认 margin 13000,参考 Claude Code)
   - `effectiveWindow` = provider `max_completion_tokens` 或 context window(取较小值)
   - 触发时:用一次 model 调用生成 conversation summary(不带 tools),替换旧消息
   - summary prompt 简单:`"Summarize the conversation so far, preserving key decisions, facts, and pending work. Be concise."`
   - 释放后 context = `[system prompt] + [summary] + [recent N turns]`
   - **circuit breaker**:连续 3 次 compact 失败 → 停止 auto-compact,run 继续用当前消息(可能在后续 model call 溢出,但这是显式失败而非无限重试)

3. **关键上下文 re-inject(compact 后)**:
   - compact 后从 disk/memory 重新注入:System prompt(stable instruction)、Memory context(prepared memory)、Skill context(selected skill)
   - 只有这 3 种,不是 8 种 packet
   - re-inject 失败不阻塞 run(跳过该 context,继续)

**配置**(精简):
```yaml
context:
  effective_window: 200000        # provider context window(或从 provider 配置自动推断)
  compact_margin_tokens: 13000     # auto-compact 触发阈值 margin
  mask_after_turns: 2              # tool result masking 轮数
  preserve_recent_turns: 3         # compact 后保留的最近完整轮数
```

**统一 token counter**:保留单一 `TokenCounter` 接口实现。可选 `tiktoken-go`(精确)或 `len(content)/4`(粗略,零依赖)。不再有 `CompressionTokenCounter` 独立类型。

**不再有的概念**:
- context pressure state(`ok` / `auto_compact` / `warning` / `blocking` 多元状态)
- reactive compact(provider overflow 不单独处理)
- context boundary 持久化(compact 边界是内存状态)
- post-compact rehydration planner(简单 re-inject,无 packet 规划)
- tool result pruning ledger(pruning 是内存 masking,不持久化)

### 3.7 记忆系统简化

**砍掉**:
- Bleve + FAISS + CGO + build-faiss-artifacts + bleve_disabled
- `memorymodule/skills/` procedure record 双轨制
- `ProcedureRecord` schema(origin / status / verification)
- `skill_assess` + `evidence_refs` + lifecycle
- `source_ref_backlink` boost + relation boost
- relation types(supports / derived_from / supersedes / contradicts)
- `memorymodule/mutation_plan.go` / `mutation_apply.go`(如果 memory 写入简化为直接文件写)

**保留/替换**:
- File-backed memory:`facts/` + `history/`
- Canonical Memory Record V2 frontmatter(简化:status / tags / created / updated / source_run / source_refs,去掉 evidence_refs / relations / validity window)
- `Prepare`:扫描 facts/ 目录,按 tag 或简单匹配注入 model context
- `Search`:
  - **方案 A 实现**:OpenAI embedding 调用 → 向量存 SQLite `memory_vectors` 表 BLOB 列 → 纯 Go 暴力余弦相似度检索
  - 复用现有 `embedder_openai.go` 的 HTTP 调用逻辑
  - 新增 `vector_store_sqlite.go`:实现 `VectorStore` 接口(`Store` / `Search` / `Delete`)
  - 单用户几千条记录,暴力检索 <10ms
  - rebuild:遍历 facts/ → 调 embedding API → 存 SQLite
  - 零 CGO
- `remember` 工具:`CreateFact` 写入 facts/ + 更新 embedding 向量
- `memory_search` 工具:调 `Search` 返回结果
- `AppendHistory`:run 结束时追加历史记录
- `BuildMemoryInstruction`:生成 memory instruction system message
- Skill 简化为 markdown 文件 + 简单关键词匹配:
  - `./skills/` seed pack
  - `~/.acorn/skills/` user skills
  - `{storage_dir}/skills/generated/` generated skills
  - 匹配逻辑:input 关键词 → skill trigger 关键词,简单评分
  - 无 lifecycle / evidence / assess

### 3.8 工具系统简化

**砍掉**:
- `ToolLifecycleState` pruning(`PruneToolMessages`)
- `tool_result_ref` marker + durable ledger
- `SafeParallelToolsNode` 路径冲突检测(`pathsOverlap`)
- `toolExecutionScheduler` 复杂调度
- `PlanPolicy` / `EvidencePolicy` / `ResourceScope`(从 ToolContract)
- `ToolProfile`(`run` / `serve`)
- MCP server mode 工具构建

**保留/简化**:
- `ToolContract` 简化:
  ```go
  type ToolContract struct {
      Name          string
      Kind          ToolKind          // native/memory/mcp
      Category      ToolCategory      // read/write/execute/inspect/memory
      ParallelPolicy ParallelPolicy   // read_only / serial
      Loading       ToolLoadingPolicy  // eager / deferred / hidden
  }
  ```
- 工具执行调度简化:
  - `read_only`:可并行
  - `serial`:串行(所有 write/execute)
  - 去掉路径冲突检测,write 工具统一串行
- `load_tools` deferred loading 保留(减少初始 tool schema 体积)
- 工具审计:保留工具调用事件写入 `events` 表
- workspace mutation tools:保留 `workspace.ResolveWritePath` 路径校验(安全边界)
- `run_command`:保留 workspace cwd + timeout + `pause_before_exec`
- Web access tools:保留(fetch / web_search / browser)
- MCP client tools:保留,连外部 MCP server

### 3.9 API 契约简化

**砍掉**:
- `/mcp` server mode + `ServeConfig` / `ServeToolsConfig`
- plan / plan_evidence 相关 API
- skill lifecycle / skill_assess 相关 API
- procedure activation 相关 API
- context boundary / context pressure 相关 API
- provider_usage 相关 API(简化为事件投影)

**保留**:
- `/healthz`
- `/v1/devices:pair` / `GET /v1/devices` / `DELETE /v1/devices/{id}`
- `/v1/threads` + `/v1/threads/{id}/messages` + `/v1/threads/{id}/runs`
- `/v1/runs/{id}` / `/v1/runs/{id}/events` / `/v1/runs/{id}:resume`
- `/v1/inbox`
- `/v1/pending-actions` / `GET /v1/pending-actions/{id}` / `POST /v1/pending-actions/{id}:decide`
- `/v1/memory/facts` / `/v1/memory/history` / `/v1/memory/search`
- `/v1/skills`(只读 list/detail/files)
- `/v1/system/status`

**OpenAPI 简化**:
- 删除 `mode` 字段(create-run request 固定 direct_response,或移除该字段)
- 删除 `orchestration_mode` 相关 DTO
- 删除 plan / plan_evidence / skill.lifecycle / procedure.activation 相关 schema
- 删除 context_boundary / context_pressure 相关 schema
- 删除 `child_run` / `parent_run_id` / `depth` 相关字段
- `RunDetail` 保留但精简:run/thread facts + mobile live events + artifacts
- `RunEvent` live subset 不变(当前已足够精简)

### 3.10 配置简化

**砍掉**:
- `ContextConfig`(window_tokens / compact_margin_tokens / preserve_recent_turns / summary_max_tokens)中过度派生部分
  - 替换为简单字段(见 3.6 配置)
- `MemorySemanticConfig` / `BleveSemanticConfig` / `EmbeddingProviderConfig` 中的 bleve 配置
- `ServeConfig` / `ServeToolsConfig`(MCP server)
- `AgentConfig.MaxSubagentDepth`(无子代理)
- `ToolsConfig` 中 serve profile 相关
- `MCPConfig` 中 server 相关

**保留**:
- `Providers`(LLM provider,单一 enabled)
- `RuntimeConfig`(storage_dir / run_timeout_seconds)
- `WebConfig`(listen_addr / allowed_origins)
- `MemoryConfig`:
  ```yaml
  memory:
    search:
      memory_context_token_budget: 2000
    embedding:
      provider: openai_compatible
      model: text-embedding-3-small
      base_url: https://api.openai.com/v1
      api_key: ${OPENAI_API_KEY}
      dimensions: 1536
      timeout_seconds: 30
      batch_size: 64
  ```
- `WebAccessConfig` / `BrowserConfig`
- `ToolsConfig`(workspace / mutation / run_command)
- `AgentConfig`(name / description / max_iterations)

### 3.11 Flutter 重写

**设计原则**:模块化、feature 独立、状态集中管理、减少合约同步。

**结构**:
```text
mobile/lib/
  main.dart
  app.dart                        # MaterialApp + 路由
  src/
    core/
      api/
        acorn_api.dart            # generated from openapi
        sse_client.dart          # 独立 SSE 模块(独立测试)
      auth/
        auth_controller.dart      # pairing + token storage
        secure_store.dart         # flutter_secure_storage
      state/
        app_providers.dart        # Riverpod global providers
      theme/
        acorn_theme.dart          # FlexColorScheme + 状态色
    features/                     # 每个 feature 自包含
      chat/
        chat_controller.dart      # Riverpod state
        chat_repository.dart       # API 调用
        chat_screen.dart          # UI
        widgets/                   # chat 专属 widget
      inbox/
        inbox_controller.dart
        inbox_repository.dart
        inbox_screen.dart
      approvals/
        approvals_controller.dart
        approvals_repository.dart
        approvals_screen.dart
      settings/
        settings_controller.dart
        settings_repository.dart
        settings_screen.dart
    ui/
      widgets/                    # 共享 widget
        status_pill.dart
        empty_state.dart
        message_widgets.dart
```

**状态管理**:Riverpod(`StateNotifier` / `AsyncNotifier`)
- 每个 feature 有独立 controller(StateNotifier)
- controller 通过 repository 调 API
- controller 通过 global provider 共享 connection state
- chat 的 streaming delta 只在 chat_controller 内,不触发其他 feature rebuild

**SSE**:
- 独立模块 `sse_client.dart`,独立测试
- 消费 `RunEvent` live subset
- 投影 assistant delta / message / run status / resume / approval 信号
- 不混在 controller 里

**OpenAPI 合约同步**:
- `docs/openapi.yaml` 仍是唯一 wire contract
- `generate_openapi_client.py --check` 仍是 CI 门禁
- Flutter 端直接消费 generated client,不手写 DTO
- SSE 仍手写(OpenAPI 不建模 SSE transport)

### 3.12 Release 简化

**砍掉**:
- FAISS C 库 build artifacts(`build-faiss-artifacts.sh`)
- CGO_ENABLED=1(无 CGO 依赖)
- `-tags "bleve_faiss vectors"` build tags
- bleve_faiss release guard test
- FAISS fork pinned checkpoint
- `deploy/faiss.version`
- `scripts/build-faiss-artifacts.sh` / `scripts/run-with-faiss-artifacts.sh`

**简化**:
- `build-release.sh`:纯 Go 交叉编译
  ```bash
  GOOS=linux GOARCH=arm64 go build -o acorn-linux-arm64 ./cmd/acorn
  GOOS=linux GOARCH=amd64 go build -o acorn-linux-amd64 ./cmd/acorn
  ```
- GitHub Release:Linux binary + signed Android APK + systemd unit
- installer:`install-release.sh` 安装 binary + skills seed pack + systemd
- Android APK 仍走 Flutter build(不涉及 CGO)

## 4. 验收标准

### 4.1 功能验收

- [ ] `direct_response` 模式:发任务 → 模型回复 / 工具调用 → 最终 assistant message
- [ ] 工具执行:workspace 读写、run_command、web fetch/search、MCP client 工具
- [ ] 记忆:`remember` 写 fact + `memory_search` 检索(embedding + FTS5 fallback)
- [ ] 中断 + 审批:`operator_question` / `elicitation` → pending_action → `:decide` → resume
- [ ] Flutter app:pair → inbox → chat → approvals → settings
- [ ] SSE streaming:assistant delta 实时投影
- [ ] device auth:pairing code → bearer token → revoked
- [ ] Resume:中断后恢复 run
- [ ] Context masking:tool result 超 N 轮后自动 mask
- [ ] Auto-compact:接近 context limit 时自动生成 summary

### 4.2 架构验收

- [ ] `make test` 通过(更新后的测试)
- [ ] `make lint` + `make format-check` 通过
- [ ] `make test-architecture` 通过(更新守卫后)
- [ ] 砍掉的包/文件全部删除,无遗留引用
- [ ] SQLite schema 从 ~23 表降到 ~8 表
- [ ] orchestration mode 路由从三模式降到单模式
- [ ] `plan_execute` / `single_agent` 代码全删
- [ ] CompactionEngine / rehydration packet / BudgetGovernor 代码全删
- [ ] Bleve + FAISS 代码全删,无 CGO 依赖
- [ ] Flutter 模块化重写,feature 独立

### 4.3 重构收益指标
- 代码行数:预计减少 60-70%
- SQLite 表:~23 → ~8
- 依赖链:去掉 CGO + FAISS + Bleve
- release 流程:纯 Go build,无 cross-compile FAISS
- 编译时间:去掉 CGO 后大幅降低
- Flutter:feature 模块化,状态集中,合约同步成本降低

## 5. 非目标

- 不做迁移工具:旧数据清空,不写 migration
- 不引入新框架:后端继续 Go + Eino ADK(核心 model + tool loop),前端继续 Flutter
- 不做 PWA 或 web 前端:保留 Flutter
- 不引入新存储:继续 SQLite + file-backed memory
- 不做跨用户/多租户:单用户
- 不做本地 run 执行:mobile 仍是纯 control surface

## 6. ADR 信号

本 spec 触发的架构决策需要后续 ADR 记录:
- 砍掉 plan_execute / single_agent 编排模式
- 砍掉 CompactionEngine + 8 种 rehydration packet,改为 hybrid masking + auto-compact
- 砍掉 Bleve + FAISS,改为 embedding + SQLite 暴力检索
- 砍掉 MCP server mode
- Flutter 模块化重写
- SQLite schema 从 ~23 表降到 ~8 表

## 7. 实现策略

- **hard cutover**:新代码在新分支,旧代码不动,验证通过后清空 VPS SQLite + 重部署
- **不写迁移工具**:旧数据清空,重新 pair 手机
- **subagent-driven-development 适用**:独立任务可并行实现(后端各模块、Flutter 各 feature)
- **writing-plans 作为下一步**:spec 通过后写实现计划
- **anti-entropy-governance review**:大量删除旧路径,确认 delete-first 策略
