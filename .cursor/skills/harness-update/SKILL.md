---
name: harness-update
description: Acorn harness 的状态更新出口。由 harness-check 自动调用，更新 harness 记忆、项目状态、生成 reflexion 并记录 skill 使用日志。
---

# Harness Update

## 调用链路

```
[afterAgentResponse hook] → [harness-check] → [harness-update] → [reflexion-extract]（条件触发）
```

## 何时触发

1. **由 harness-check 判定需要更新时自动调用**
2. **用户说"更新 harness"**

## 执行顺序

每次触发时按以下顺序执行：

1. **记录 skill 使用** — 向 `.acorn/harness/skill_usage.log` 追加一行
2. **更新 state/current.md**（最高优先级）
3. **评估是否触发 reflexion**
   - 如果满足条件 → 调用 `reflexion-extract`
4. **执行增量更新**
5. **汇报更新摘要**

## Skill 使用日志

每次调用本 skill 时，向 `.acorn/harness/skill_usage.log` 追加一行：

```
date|skill_id|triggered_by|result|notes
```

例如：
```
2026-05-26|harness-update|auto|success|updated state/current.md, no reflexion triggered
2026-05-26|harness-update|auto|success|updated state/current.md + reflexion generated
```

## Reflexion 触发评估

在更新 state/current.md 前，评估本次 run 是否需要生成 reflexion：

**触发条件（满足任一即调用 reflexion-extract）：**
- run 涉及 >= 2 个模块的文件修改
- run 修改了 `internal/web/`、`docs/openapi.yaml`、`mobile/` 中任一文件
- run 执行了测试且结果非全绿
- run 结束后用户给了负面反馈（"不对"、"还要改"、"没解决"）
- run 识别到与 `state/current.md` 中已有 RISK 描述匹配的问题
- run 中使用了新 skill 且效果不理想

**不触发的情况：**
- 只修改了单文件的注释/格式
- 纯查询类操作（没改代码）
- run 完全成功且用户明确满意

**Reflexion 文件命名：**
`.acorn/harness/reflexions/{YYYY-MM-DD}_{run_id}.md`

生成 reflexion 后，更新 `reflexions/index.md` 的活跃模式表。

## 更新范围与优先级

### 最高优先级：state/current.md
**每次 run 结束后必更新。**

允许的操作：
- 勾选/取消勾选任务 `[x]` / `[ ]`
- 更新进度百分比
- 追加/删除阻塞项
- 追加/更新风险（格式：`[RISK-XXX] 描述（已发生 N 次）`）
- 更新"下次建议动作"列表
- 更新"最后更新"日期

禁止的操作：
- 删除历史 sprint 记录（应归档到 `state/archive/` 而非删除）
- 重构整个文件格式
- 添加与本次 run 无关的内容

### 中优先级：memory/modules/{module}.md
**当该模块的架构/约束/接口发生变更时更新。**

允许的操作：
- 更新 frontmatter 中的 `last_updated`、`status`
- 在"当前状态"小节追加新条目
- 在"硬约束"小节追加/删除条目（需说明原因）
- 更新"最近改动"字段

禁止的操作：
- 删除已有约束条目（除非明确被新决策取代）
- 修改接口契约而不更新关联模块文件
- 改变 `id` 或 `path` 等标识字段

### 低优先级：memory/decisions/*.md
**仅当做出新决策时创建/更新。**

允许的操作：
- 新建决策文件（命名格式：`{NNN}-{kebab-case-topic}.md`）
- 更新已有决策的 `status`（active → superseded）
- 在 `supersedes` 字段追加被取代的决策 ID

禁止的操作：
- 编辑已有决策的 `decision` 字段（决策内容不可篡改，只能被新决策取代）
- 删除决策文件（历史必须保留）

## 更新流程

1. **识别变更类型**：本次 run 产生了哪类变更？
2. **评估 reflexion 触发**：是否满足触发条件？
3. **读取目标文件**：先读当前文件内容，确认格式
4. **执行增量修改**：只改必要字段，保持其余内容不变
5. **自验证**：重新读取文件，确认格式未被破坏
6. **汇报更新摘要**：向用户汇报改了什么、为什么

## 格式保护规则

- Markdown frontmatter 用 `---` 包围，YAML 缩进用 2 空格
- 列表项保持统一前缀（`- ` 或 `1. `）
- 日期格式统一 `YYYY-MM-DD`
- 风险 ID 保持 `RISK-XXX` 顺序递增
- 决策 ID 保持 `dec_NNN` 顺序递增
- skill_usage.log 每行以日期开头，用 `|` 分隔字段
