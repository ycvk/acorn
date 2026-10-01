---
doc_type: architecture
status: current
last_reviewed: 2026-10-01
slug: runtime-orchestration
---

# Runtime Orchestration

## direct_response

`internal/runtime` 是编排唯一入口。`buildDirectResponse`（`direct_response.go`）构建 `directResponseAgent`，执行 model → tool loop → record 循环。

执行时按 `Session.BeforeModelCall → ExecuteRound → Session.RecordAssistant/RecordToolResults` 的 session-owned loop 推进，直到模型返回无 tool call 的最终 assistant message。`AssistantStreamer` 把模型 stream chunk 持久化为 `assistant.delta`，再保留最终完整 assistant message。

`direct_response` 是 Acorn-specific ADK agent，不是 Eino `adk.NewChatModelAgent` 的薄封装：普通问答必须产出 Acorn persisted event truth，tool lifecycle 必须和 Plane 的 loaded/deferred state 绑定，普通 tool failure 必须继续作为模型可见 failed tool result。

Session 拥有 root-run 的首轮 model input。缺少 root Session binding 时 direct_response 直接失败。

高风险工具调用（`tools.ClassifyRisk`）在执行前被拦截：已提交的其他调用结果照常记录，被拦截的调用以 approval-required tool message 回给模型，由模型通过 `ask_operator` 申请批准。

## Hybrid Context

Session 把消息分为两段：Bootstrap 时的 **prefix**（assembled context + stable instruction）和之后的 **conversation**。`BeforeModelCall` 依次执行：

1. **Observation masking**：tool result 超 `mask_after_turns` 轮后用占位符替换。纯内存操作。
2. **Apply pending compact**：若上一轮启动的后台 summary 已完成，用一条 summary system message 替换 conversation 中被总结的前段，prefix 原样保留。未完成则本轮照常继续。
3. **非阻塞 LLM auto-compact**：token 超 `window_tokens - compact_margin_tokens` 时 `maybeStartCompact` 启后台 goroutine 总结 conversation 的前段（保留最近 `preserve_recent_turns` 轮，切分点不会让 live 区以 tool result 开头），立即返回。circuit breaker：连续 3 次失败后停止。

后台 goroutine 只读自己的快照副本并写 pending 状态；splice 只在 Session 自己的 goroutine 中、两轮之间发生，所以消息列表保持单写者。

## 工具调度

`SafeParallelToolsNode`（`internal/tools/dispatch/node.go`）是 Acorn-specific tool dispatch adapter，通过 `StreamingExecutor` 暴露实时提交接口，从 `core.ExecutionPolicyResolver` 读取 `ToolContract.Execution`。调度规则见 [tools.md](tools.md)。

已加载工具没有 execution policy 是 runtime wiring failure。模型调用 unknown/deferred tool 是模型可见 failed tool result。
