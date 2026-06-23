---
doc_type: architecture
status: current
last_reviewed: 2026-06-23
slug: runtime-orchestration
---

# Runtime Orchestration

## direct_response

`internal/runtime` 是编排唯一入口。`buildDirectResponse`（`direct_response.go`）构建 `directResponseAgent`，执行 model → tool loop → record 循环。没有 strategy pattern、没有 DefaultPlane 抽象——所有依赖直接内联。

执行时按 `ContextSession.BeforeModelCall(masking + auto-compact) → ExecuteRound → ContextSession.RecordAssistant/RecordToolResults` 的 session-owned loop 推进，直到模型返回无 tool call 的最终 assistant message。`AssistantStreamer` 负责把模型 stream chunk 持久化为 `assistant.delta`，再保留最终完整 assistant message。

`direct_response` 是 Acorn-specific ADK agent，不是 Eino `adk.NewChatModelAgent` 的薄封装。保留自定义 loop 的原因是：普通问答必须产出 Acorn persisted event truth，tool lifecycle 必须和 ContextPlane 的 loaded/deferred state 绑定，普通 tool failure 必须继续作为模型可见 failed tool result。

ContextSession 拥有 root-run 的首轮 model input。缺少 root ContextSession binding 时 direct_response 直接失败，不回退到 `input.Messages`。

## Hybrid Context

ContextSession 在 `BeforeModelCall` 中执行三层 context 策略：

1. **Observation masking**：tool result 超 `mask_after_turns` 轮后用占位符替换。纯内存操作。
2. **LLM auto-compact**：token 超 `window_tokens - compact_margin` 阈值时用一次 model 调用生成 summary，替换旧消息。circuit breaker：连续 3 次失败停止。
3. **关键上下文 re-inject**：compact 后从 assembly 重新注入 system prompt + memory context + skill context。

不再有 CompactionEngine、BudgetGovernor、reactive compact、context boundary 持久化、rehydration packet 系统。

## 工具调度

`SafeParallelToolsNode`（`internal/runtime/tooldispatch/node.go`）是 Acorn-specific tool dispatch adapter，通过 `StreamingExecutor` 暴露实时提交接口。它从 `toolkit.ExecutionPolicyResolver` 读取 `ToolContract.Execution`：

- `read_only`：可并行执行
- `serial`：串行执行（所有 write/execute/integration 工具）

已加载工具没有 execution policy 是 runtime wiring failure。模型调用 unknown/deferred tool 仍是模型可见 failed tool result。
