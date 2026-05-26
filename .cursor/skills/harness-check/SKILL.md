---
name: harness-check
description: Acorn harness 的轻量级状态检查。由 afterAgentResponse hook 通过 agent_message 触发，判断是否需要更新 harness 状态或生成 reflexion。
---

# Harness Check

## 触发时机

1. **每次 agent 响应后**（由 `afterAgentResponse` hook 的 shell 脚本检测文件变更后输出 `agent_message` 触发）
2. **用户说"检查 harness"**

## 执行流程

1. **扫描当前 run 的变更**
   - 检查是否有文件被修改（增删改）
   - 检查是否有测试被执行
   - 检查是否有新的 error/warning 出现
   - 检查用户是否给了负面反馈

2. **判断是否需要 harness-update**
   - 如果有文件修改 → 需要更新
   - 如果测试被执行 → 需要更新
   - 如果用户给了反馈 → 需要更新
   - 如果 sprint 进度可感知变化 → 需要更新

3. **判断是否需要 reflexion**
   - run 涉及 >= 2 个模块 → 需要 reflexion
   - run 修改了 `internal/web/`、`docs/openapi.yaml`、`mobile/` → 需要 reflexion
   - 测试失败或部分通过 → 需要 reflexion
   - 用户负面反馈 → 需要 reflexion
   - 识别到与已有 RISK 匹配的问题 → 需要 reflexion

4. **执行动作**
   - 如果需要更新 → 调用 `harness-update`
   - 如果需要 reflexion → `harness-update` 会自动触发 reflexion 生成
   - 如果都不需要 → 汇报 "harness 检查完成：无状态变更"

## 轻量级检查规则

### 文件修改检测
- 对比 run 前后的文件树
- 记录修改的文件路径和模块归属
- 如果修改了 API 相关文件，标记为 "api_change"

### 测试执行检测
- 检查 run 中是否调用了 `go test`、`flutter test`、`make test`
- 记录测试结果（pass/fail/partial）

### 反馈检测
- 检测用户输入中的负面关键词："不对"、"错了"、"还要改"、"没解决"、"不行"
- 如果检测到，标记为 "negative_feedback"

## 输出格式

如果触发更新：
> harness-check：检测到 {N} 个文件修改，{M} 个模块涉及，触发 harness-update。

如果触发 reflexion：
> harness-check：检测到 API 变更 + 测试失败，触发 harness-update + reflexion。

如果无变更：
> harness-check：当前 run 无文件修改，跳过更新。
