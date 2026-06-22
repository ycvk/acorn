# Acorn 重构 - Phase 3: ContextPlane 清理 + Hybrid Context

Date: 2026-06-21
Parent Spec: `docs/aegis/specs/2026-06-21-acorn-refactor-design.md`
Depends on: Phase 1 + Phase 2

## Goal

砍掉 `CompactionEngine`、8 种 rehydration packet、`BudgetGovernor` 派生阈值、reactive compact、tool lifecycle pruning + durable ledger。实现 spec §3.6 的 hybrid context 方案:**observation masking + LLM auto-compact + 关键上下文 re-inject**。

## Architecture

```text
本阶段范围:
  contextplane/
    context_session.go          — 简化:Bootstrap + BeforeModelCall(masking+compact) + Record
    assembly.go                 — 简化:去掉 packet/checkpoint/skill_catalog 组装
    tool_lifecycle.go           — 简化:只保留 loaded/deferred,去掉 pruning/ledger
    budget_governor.go          — 删除(替换为简单阈值)
    compression_token_counter.go — 简化为统一 TokenCounter
    context_session_compact.go  — 删除
    context_overflow_error.go   — 删除
    skill_provider.go           — 删除
    skill_catalog_provider.go   — 删除
    selected_skill.go           — 删除
    memory_provider.go          — 删除
    run_context_snapshot.go     — 删除
    tool_lifecycle_runtime.go   — 删除
    envelope.go                 — 删除
    helpers.go                  — 删除
    compaction/                 — 整个目录删除

新增:
  contextplane/
    masking.go                  — observation masking 实现
    auto_compact.go             — LLM auto-compact + circuit breaker
```

## Baseline / Authority Refs

- Spec §3.6 上下文/压缩简化(hybrid 方案)
- Spec §3.8 工具系统简化(去掉 pruning/ledger)
- `internal/contextplane/context_session.go` — 当前 ContextSession
- `internal/contextplane/compaction/compaction_engine.go` — 当前 CompactionEngine(删除)

## Compatibility Boundary

- `ContextSession` 接口签名变化:`ReactiveCompact` 方法删除。`BeforeModelCall` 内部逻辑变化但签名保留。
- `ContextSession` 仍是 root-run model input 的唯一 owner。
- runtime Executor 调用 `Bootstrap` / `BeforeModelCall` / `RecordAssistant` / `RecordToolResults` 的方式不变。

## Verification

- `go build ./internal/contextplane && go test ./internal/contextplane` 通过
- `go build ./internal/runtime` 通过(runtime 引用 contextplane)
- `go test ./internal/runtime ./internal/contextplane` 通过

---

## Task 1: 删除 compaction/ 目录 + 简化相关文件

**Files:**
- Delete: `internal/contextplane/compaction/`(整个目录)
- Delete: `internal/contextplane/context_session_compact.go`
- Delete: `internal/contextplane/context_overflow_error.go`
- Delete: `internal/contextplane/budget_governor.go`
- Delete: `internal/contextplane/skill_provider.go`
- Delete: `internal/contextplane/skill_catalog_provider.go`
- Delete: `internal/contextplane/selected_skill.go`
- Delete: `internal/contextplane/memory_provider.go`
- Delete: `internal/contextplane/run_context_snapshot.go`
- Delete: `internal/contextplane/tool_lifecycle_runtime.go`
- Delete: `internal/contextplane/envelope.go`
- Delete: `internal/contextplane/helpers.go`
- Delete: `internal/contextplane/providers_test.go`
- Delete: `internal/contextplane/compression_state_test.go`
- Delete: `internal/contextplane/budget_governor_test.go`
- Delete: `internal/contextplane/run_context_snapshot_test.go`

**Why:** 这些文件全部是 CompactionEngine / BudgetGovernor / packet 系统的一部分。

**Verification:** `go build ./internal/contextplane`(会有编译错误,Task 2-4 修复)

### Steps

- [ ] **1.1 删除 `internal/contextplane/compaction/` 目录**
- [ ] **1.2 删除上述所有文件**
- [ ] **1.3 Commit**:`refactor(contextplane): delete compaction/budget-governor/packet files`

---

## Task 2: 简化 compression_token_counter + types

**Files:**
- Modify: `internal/contextplane/compression_token_counter.go`
- Modify: `internal/contextplane/types.go`
- Test: `internal/contextplane/test_counter_test.go`

**Why:** `CompressionTokenCounter` 简化为通用 `TokenCounter`。`types.go` 删除 packet/compact/pressure 相关类型。

**Impact/Compatibility:** `CompressionTokenCounter` 重命名为 `TokenCounter`(或保留原名但简化实现)。`types.go` 删除:`PipelineRequest`、`PipelineResult`、`CompressionPipeline`、`CompactTrigger`、`PreservePolicy`、`CompressionOutcome`、`CompressionBuildOptions`、`CompressionState`、`NewCompressionState`、`RecordCompression`。保留:`ContextPlane` 接口、`AssembleRequest`、`AssembleResult`、`ToolCallEvent`、`ToolResultEvent`、`DeferredLoadRequest`、`DeferredLoadResult`、`DefaultOptions`、`defaultContextPlane`、`ToolLifecycleState`、`LoadedToolRecord`、`DeferredToolRecord`、`ToolResultRecord`、`NewDefaultContextPlane`。`ContextSession` 接口和 `ModelInput` 保留(在 Task 3 修改)。

**Verification:** `go build ./internal/contextplane`

### Steps

- [ ] **2.1 重写 `compression_token_counter.go`**:简化为统一 token counter。保留 `tiktoken-go` 或 `len(content)/4` 实现。如果重命名为 `TokenCounter`,更新所有引用。
- [ ] **2.2 重写 `types.go`**:删除上述 packet/compact/pressure 类型。保留 `ContextPlane` 接口、`AssembleRequest`/`Result`、`ToolLifecycleState` 等。
- [ ] **2.3 更新 `test_counter_test.go`**:匹配新 token counter。
- [ ] **2.4 运行验证**:`go build ./internal/contextplane`(会有错误因为 context_session.go 引用已删除类型,Task 3 修复)
- [ ] **2.5 Commit**:`refactor(contextplane): simplify token counter and types`

---

## Task 3: 重写 ContextSession + Assembly

**Files:**
- Modify: `internal/contextplane/context_session.go`
- Modify: `internal/contextplane/assembly.go`
- Modify: `internal/contextplane/message_utils.go`
- Test: `internal/contextplane/context_session_test.go`

**Why:** ContextSession 是 root-run model input owner。需要去掉 ReactiveCompact / CompressionState / pressure 评估,加入 masking + auto-compact。

**Impact/Compatibility:** `ContextSession` 接口:

```go
type ContextSession interface {
    Bootstrap(ctx context.Context, req BootstrapRequest) (*ModelInput, error)
    BeforeModelCall(ctx context.Context, req ModelCallRequest) (*ModelInput, error)
    RecordAssistant(ctx context.Context, msg adk.Message) error
    RecordToolResults(ctx context.Context, results []adk.Message) error
}
```

删除 `ReactiveCompact`。`BeforeModelCall` 内部:1) apply observation masking → 2) check token count → 3) if over threshold, auto-compact → 4) return ModelInput。`ModelCallRequest` 简化:删除 `AllowCompact` 字段(auto-compact 总是允许)。`BootstrapRequest` 简化:删除 `ToolCatalog` 中的 profile 参数。

**Verification:** `go build ./internal/contextplane && go test ./internal/contextplane`

### Steps

- [ ] **3.1 重写 `context_session.go`**:`ContextSession` 接口删除 `ReactiveCompact`。`defaultContextSession` 删除 `compressionState` 字段。`Bootstrap` 简化:assembly + initial user messages,不触发 compact。`BeforeModelCall` 重写:调 `applyMasking` → `countTokens` → if over `effectiveWindow - compactMargin`,调 `autoCompact` → return ModelInput。`RecordAssistant` / `RecordToolResults` 保留(记录到 messages slice)。删除 `evaluatePressure` / `modelInput` / `currentInput` 中的 pressure 逻辑。
- [ ] **3.2 重写 `assembly.go`**:`Assemble` 简化:去掉 checkpoint section、skill catalog、packet 组装。保留:selected skill context + memory message + deferred tool messages。`budgetedContextMessages` 保留(超 budget fail)。删除 `runContextAssembler` 中的 checkpoint 逻辑。
- [ ] **3.3 审查 `message_utils.go`**:保留 `CloneMessages` / `CloneMessage` / `AnnotateMessageTurn`。删除引用已删除类型的函数。
- [ ] **3.4 更新 `context_session_test.go`**:删除 ReactiveCompact / pressure / packet 测试。新增 masking 测试 + auto-compact 触发测试。
- [ ] **3.5 运行验证**:`go build ./internal/contextplane && go test ./internal/contextplane`。修复编译错误直到通过。
- [ ] **3.6 Commit**:`refactor(contextplane): rewrite ContextSession with masking + auto-compact`

---

## Task 4: 新建 masking.go + auto_compact.go

**Files:**
- Create: `internal/contextplane/masking.go`
- Create: `internal/contextplane/auto_compact.go`
- Test: `internal/contextplane/masking_test.go`
- Test: `internal/contextplane/auto_compact_test.go`

**Why:** 实现 spec §3.6 的三层 context 策略的前两层(observation masking + LLM auto-compact)。第三层 re-inject 在 `BeforeModelCall` 中内联实现。

**Impact/Compatibility:** 新文件,不破坏现有接口。

**Verification:** `go test ./internal/contextplane -run TestMasking -run TestAutoCompact`

### Steps

- [ ] **4.1 写 `masking.go`**:
  ```go
  // masking.go
  package contextplane

  // applyMasking replaces tool result messages older than maskAfterTurns with
  // a compact placeholder. This is the first-line defense against context bloat.
  func applyMasking(messages []adk.Message, currentTurn int, maskAfterTurns int) []adk.Message {
      if maskAfterTurns <= 0 || len(messages) == 0 {
          return messages
      }
      result := make([]adk.Message, len(messages))
      copy(result, messages)
      for i, msg := range result {
          if msg.Role != schema.ToolRole {
              continue
          }
          callID := strings.TrimSpace(msg.ToolCallID)
          if callID == "" {
              continue
          }
          msgTurn := turnIndexFromMessage(msg)
          if currentTurn - msgTurn <= maskAfterTurns {
              continue // keep recent tool results
          }
          result[i] = maskToolMessage(msg)
      }
      return result
  }

  func maskToolMessage(msg adk.Message) adk.Message {
      clone := msg
      clone.Content = fmt.Sprintf("[tool result elided: call_id=%s]", msg.ToolCallID)
      return clone
  }

  func turnIndexFromMessage(msg adk.Message) int {
      // extract turn index from message annotation (set by AnnotateMessageTurn)
      if msg.Metadata == nil {
          return 0
      }
      if v, ok := msg.Metadata["turn_index"]; ok {
          if t, ok := v.(int); ok {
              return t
          }
      }
      return 0
  }
  ```
- [ ] **4.2 写 `masking_test.go`**:测试:1) recent tool result not masked; 2) old tool result masked; 3) non-tool messages unchanged; 4) maskAfterTurns=0 disables masking。
- [ ] **4.3 写 `auto_compact.go`**:
  ```go
  // auto_compact.go
  package contextplane

  // autoCompact generates a conversation summary using a model call, then
  // replaces old messages with [system + summary + recent turns].
  // Circuit breaker: after 3 consecutive failures, stops attempting compact.
  type autoCompactor struct {
      model        einomodel.BaseChatModel
      tokenCounter TokenCounter
      maxFailures  int // default 3
      failures    int
  }

  func newAutoCompactor(model einomodel.BaseChatModel, counter TokenCounter) *autoCompactor {
      return &autoCompactor{model: model, tokenCounter: counter, maxFailures: 3}
  }

  func (c *autoCompactor) compact(ctx context.Context, messages []adk.Message, preserveRecentTurns int) ([]adk.Message, error) {
      if c.failures >= c.maxFailures {
          return messages, nil // circuit breaker tripped
      }
      // split: old messages to summarize vs recent turns to keep
      summaryEnd := len(messages) - preserveRecentTurns*2 // approx (user+assistant per turn)
      if summaryEnd <= 0 {
          return messages, nil // nothing to compact
      }
      oldMessages := messages[:summaryEnd]
      recentMessages := messages[summaryEnd:]
      // generate summary
      summary, err := c.generateSummary(ctx, oldMessages)
      if err != nil {
          c.failures++
          return messages, err
      }
      c.failures = 0 // reset on success
      // result: [summary message] + recent messages
      summaryMsg := adk.Message{Role: schema.System, Content: "Conversation summary:\n" + summary}
      return append([]adk.Message{summaryMsg}, recentMessages...), nil
  }

  func (c *autoCompactor) generateSummary(ctx context.Context, messages []adk.Message) (string, error) {
      prompt := "Summarize the conversation so far, preserving key decisions, facts, and pending work. Be concise."
      req := &schema.Message{Role: schema.User, Content: prompt + "\n\n---\n\n" + serializeMessages(messages)}
      resp, err := c.model.Generate(ctx, []*schema.Message{req})
      if err != nil {
          return "", err
      }
      return resp.Content, nil
  }

  func serializeMessages(messages []adk.Message) string {
      var b strings.Builder
      for _, m := range messages {
          b.WriteString(string(m.Role))
          b.WriteString(": ")
          b.WriteString(m.Content)
          b.WriteString("\n")
      }
      return b.String()
  }
  ```
- [ ] **4.4 写 `auto_compact_test.go`**:测试:1) compact replaces old messages with summary; 2) preserve recent turns; 3) circuit breaker after 3 failures; 4) compact failure increments failure count。
- [ ] **4.5 集成到 `context_session.go` 的 `BeforeModelCall`**:`defaultContextSession` 新增 `compactor *autoCompactor` 字段。`BeforeModelCall`:masking → count tokens → if over threshold → `compactor.compact` → re-inject system prompt + memory + skill context → return ModelInput。
- [ ] **4.6 运行验证**:`go build ./internal/contextplane && go test ./internal/contextplane`。
- [ ] **4.7 Commit**:`feat(contextplane): implement observation masking + auto-compact with circuit breaker`

---

## Task 5: 简化 tool_lifecycle.go

**Files:**
- Modify: `internal/contextplane/tool_lifecycle.go`
- Modify: `internal/contextplane/tool_lifecycle_test.go`
- Modify: `internal/contextplane/tool_lifecycle_runtime.go`(已删除在 Task 1,确认)
- Test: `internal/contextplane/tool_lifecycle_runtime_test.go`

**Why:** tool_lifecycle 当前包含 pruning + ledger 写入。砍掉后只保留 loaded/deferred 状态。

**Impact/Compatibility:** 删除 `PruneToolMessages`、`UpdateToolResultRecord`、`formatPrunedToolResult`、`ToolResultRecord` 中的 ledger 相关字段。保留 `ToolLifecycleContext`、`WithToolLifecycleContext`、`ToolLifecycleContextFromContext`、`LoadedToolInfosFromContext`、`newToolLifecycleState`、`splitToolDefinitions`、`sortedLoadedToolNames`、`sortedDeferredToolNames`。`ToolLifecycleState` 简化:删除 `RecentResults`、`MaxResultRefs` 字段。保留 `LoadedTools`、`DeferredTools`、`MaxAgeTurns`(masking 用)。

**Verification:** `go build ./internal/contextplane && go test ./internal/contextplane`

### Steps

- [ ] **5.1 重写 `tool_lifecycle.go`**:删除 `PruneToolMessages`、`UpdateToolResultRecord`、`formatPrunedToolResult`。`ToolLifecycleState` 删除 `RecentResults`、`MaxResultRefs`。保留 `LoadedTools`、`DeferredTools`、`MaxAgeTurns`。`ToolResultRecord` 删除或简化(masking 不需要 ledger record,只需要 callID + turnIndex)。
- [ ] **5.2 更新 `tool_lifecycle_test.go`**:删除 pruning / ledger 测试。保留 loaded/deferred 测试。
- [ ] **5.3 删除 `tool_lifecycle_runtime_test.go`**(如果 `tool_lifecycle_runtime.go` 已在 Task 1 删除)。
- [ ] **5.4 运行验证**:`go build ./internal/contextplane && go test ./internal/contextplane`。
- [ ] **5.5 Commit**:`refactor(contextplane): simplify tool_lifecycle - remove pruning and ledger`

---

## Task 6: 修复 runtime 引用 + 全量编译

**Files:**
- Modify: `internal/runtime/context_session_bridge.go`
- Modify: `internal/runtime/runcontext.go`
- Modify: `internal/runtime/run_build_helpers.go`
- Modify: `internal/runtime/helpers.go`
- Modify: `internal/runtime/runner_build.go`(如果引用了已删除的 contextplane 类型)

**Why:** runtime 引用了 contextplane 的已删除类型(如 `ReactiveCompact`、`BudgetGovernor`、`CompactionEngine`)。需要清理引用。

**Verification:** `go build ./internal/runtime ./internal/contextplane && go test ./internal/runtime ./internal/contextplane`

### Steps

- [ ] **6.1 搜索 runtime 中对已删除 contextplane 符号的引用**:`go build ./internal/runtime 2>&1` 查看编译错误。
- [ ] **6.2 修复 `context_session_bridge.go`**:删除 `ReactiveCompact` 引用。适配新 `ContextSession` 接口。
- [ ] **6.3 修复 `runcontext.go`**:删除 `BudgetGovernor` / `CompactionEngine` 引用。
- [ ] **6.4 修复其他 runtime 文件的编译错误**。
- [ ] **6.5 运行验证**:`go build ./internal/runtime ./internal/contextplane && go test ./internal/runtime ./internal/contextplane`。
- [ ] **6.6 Commit**:`fix(runtime): adapt to simplified ContextSession interface`

---

## Task 7: 全量编译检查

### Steps

- [ ] **7.1 运行**:`go build ./internal/contextplane ./internal/runtime ./internal/orchestration`。必须通过。
- [ ] **7.2 运行**:`go test ./internal/contextplane ./internal/runtime ./internal/orchestration`。必须通过。
- [ ] **7.3 记录未通过的包**:`go build ./... 2>&1 | head -50`。记录 memorymodule/app/web 的编译错误(Phase 4-5 修复)。
- [ ] **7.4 Commit**:`chore: phase 3 contextplane cleanup + hybrid context complete`
