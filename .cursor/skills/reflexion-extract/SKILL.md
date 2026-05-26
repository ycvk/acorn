---
name: reflexion-extract
description: Acorn harness 的单次 run 模式提取。从 run 的执行结果中提取可复用模式、错误根因和预防措施。
---

# Reflexion Extract

## 触发时机

1. **harness-update 判定需要 reflexion 时自动调用**
2. **用户说"分析这次 run"**

## 输入

- run 的完整执行记录（文件修改、测试输出、用户反馈）
- 修改前的 `state/current.md`（作为 baseline）
- 涉及模块的 `memory/modules/*.md`

## 提取维度

### 1. 模式识别（Pattern）
- 识别可复用的问题模式，格式：`pattern_{kebab-case-description}`
- 记录：触发条件、表现、根因、建议沉淀位置

### 2. 错误记录（Mistake）
- 记录本次 run 中犯的错误
- 格式：`[MISTAKE-NNN] 描述`
- 包含：影响、避免方法

### 3. 验证缺口（Verification Gap）
- 记录本次 run 遗漏的验证步骤
- 例如：未跑 `flutter analyze`、未检查 OpenAPI 同步

### 4. 下次注意（Next Time）
- 基于本次教训，给出下次执行的注意事项
- 格式：编号列表，每条具体可操作

## 输出格式

生成 `.acorn/harness/reflexions/{YYYY-MM-DD}_{run_id}.md`：

```markdown
---
type: reflexion
run_id: {run_id}
date: {YYYY-MM-DD}
triggered_by: harness-auto | user-request
severity: error | warning | info | pattern
affected_modules: [module1, module2]
---

# Reflexion: {run_id}

## 执行摘要
{一句话总结本次 run 做了什么}

## 发现的模式/坑
- **pattern_{id}**（第 {N} 次发生）
  - 触发条件：{conditions}
  - 表现：{manifestation}
  - 根因：{root_cause}
  - 建议沉淀：{suggested_action}

## 犯的错误
- [MISTAKE-NNN] {description}
  - 影响：{impact}
  - 避免方法：{prevention}

## 验证缺口
- {verification_step} 未执行
- {expected_result} 未验证

## 下次注意
1. {actionable_item}
2. {actionable_item}

## 关联更新
- 已触发 harness-update 更新 `state/current.md`（新增/更新 RISK-XXX）
- 建议后续 meta-review 关注 pattern_{id}
```

## 模式计数规则

- 新 pattern → 计数 = 1，状态 = "new"
- 已有 pattern（在 reflexions/index.md 中存在）→ 计数 +1，状态 = "recurring"
- 计数 >= 2 → 建议 meta-review 升级

## 更新索引

生成 reflexion 后，更新 `reflexions/index.md`：
- 如果是新 pattern → 添加到"活跃模式"表
- 如果是已有 pattern → 更新"最近发生"和"次数"
