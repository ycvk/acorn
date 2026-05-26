---
name: orchestrate
description: Acorn harness 的统一入口。由 Cursor hooks 通过 agent_message 触发，加载项目状态，评估上下文，再决定下一步动作。
---

# Orchestrate

## 自动触发方式

由 Cursor hooks 间接触发：

- `sessionStart` hook → `.cursor/hooks/harness-init.sh` → 输出 `agent_message` → AI 加载 harness 上下文 → 执行本 skill

用户也可以手动输入 `/orchestrate` 触发。

## 启动协议

每次对话开头，按以下顺序加载上下文：

1. **初始化 harness**
   - 检查目录完整性
   - 加载共识记忆 `memory/modules/*.md`
   - 加载项目状态 `state/current.md`
2. **评估用户输入**：判断用户意图属于以下哪类：
   - `specific_task` — 用户说了具体要做什么（"修这个 bug"、"加这个功能"）
   - `status_query` — 用户问项目状态
   - `vague` — 用户输入模糊（"看看项目"、"接下来做什么"）
   - `harness_meta` — 用户想更新 harness 本身

## 路由规则

### specific_task
- 识别涉及哪些模块
- 加载对应模块的 `memory/modules/{module}.md`
- 检查 `state/current.md` 中的 risk/blocker 是否相关
- 提示用户："这个任务涉及 contextplane 和 web，当前有一个阻塞风险 RISK-001（OpenAPI 未同步），是否先处理？"

### status_query / vague
- 直接呈现 `state/current.md` 的内容
- 主动建议下一步动作（基于 current.md 里的"下次建议动作"）

### harness_meta
- 进入 harness 维护模式（更新 memory、评估 skill 健康度等）

## 执行后自动链路

执行层 skill 完成后，自动触发以下链路：

```
[afterAgentResponse hook] → [.cursor/hooks/harness-check.sh] → 输出 agent_message → AI 执行 [harness-check] → [harness-update] → [reflexion-extract]（条件触发）
```

1. **harness-check**：检查是否需要更新
2. **harness-update**：更新 state/current.md + 记录 skill_usage.log
3. **reflexion-extract**：如果满足条件，生成 reflexion

## 跨 session 恢复

当检测到这是新 session（无历史消息或用户输入"继续"），由 harness-init hook 自动加载 `state/current.md` 并向用户呈现："上次我们做到这：..."
