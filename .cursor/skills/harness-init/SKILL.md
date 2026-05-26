---
name: harness-init
description: Acorn harness 的新 session 初始化。加载共识记忆、项目状态，准备 harness 上下文。
---

# Harness Init

## 触发时机

1. **新 session 开始**（由 `sessionStart` hook 的 shell 脚本输出 `agent_message` 触发）
2. **用户说"继续"或"恢复"**
3. **项目重新加载后**

## 执行流程

1. **检查 harness 目录完整性**
   - 确认 `.acorn/harness/` 目录存在
   - 确认 `memory/modules/*.md` 可读
   - 确认 `state/current.md` 可读
   - 如果缺失，提示用户"harness 尚未初始化，需要运行 setup 吗？"

2. **加载共识记忆**
   - 按优先级加载：
     1. `memory/modules/contextplane.md`（context 协议核心）
     2. `memory/modules/runtime.md`（执行链）
     3. `memory/modules/orchestration.md`（编排层）
     4. `memory/modules/web.md`（API 契约）
     5. `memory/modules/memorymodule.md`（记忆层）
     6. 其他模块按需加载

3. **加载项目状态**
   - 读取 `state/current.md`
   - 提取：
     - 当前 sprint 目标
     - 活跃任务列表
     - 阻塞项
     - 风险列表
     - 下次建议动作

4. **评估用户意图**
   - 如果用户输入为空或模糊 → 进入 `status_query` 模式
   - 如果用户输入了具体任务 → 进入 `specific_task` 模式
   - 如果用户说"继续" → 进入 `resume` 模式，加载上次上下文

5. **呈现上下文**
   - 向用户汇报当前 sprint 状态和已知风险
   - 格式："上次我们做到这：{sprint_summary}"

## 跨 session 恢复

当检测到这是新 session（无历史消息或用户输入"继续"），自动加载 `state/current.md` 并向用户呈现：

> 上次我们做到这：
> - Sprint：{current_sprint_goal}（{progress}%）
> - 活跃任务：{active_tasks}
> - 当前阻塞：{blockers}
> - 已知风险：{risks}
> - 建议下一步：{suggested_actions}

## 初始化检查清单

- [ ] `.acorn/harness/` 目录存在
- [ ] `memory/modules/` 有至少一个模块文件
- [ ] `state/current.md` 可读且格式正确
- [ ] `README.md` 存在（格式参考）
- [ ] `.gitignore` 已排除 `state/` 和运行时文件
