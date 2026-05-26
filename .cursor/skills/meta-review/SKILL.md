---
name: meta-review
description: Acorn harness 的跨 run 模式识别与规则升级。定期扫描 reflexions，将高频模式升级为持久化规则。
---

# Meta-Review

## 触发条件

1. **数量触发**：每积累 5 个 reflexion 自动触发一次
2. **时间触发**：每 3 天自动扫描一次（即使 reflexion 很少）
3. **严重触发**：出现 `severity: error` 的 reflexion 后立即触发
4. **用户触发**：用户说"评估一下最近的问题模式"或"meta-review"

## 扫描逻辑

```
扫描 .acorn/harness/reflexions/*.md
    ↓
提取所有 `pattern_*` 条目
    ↓
按 pattern ID 聚合计数
    ↓
计数 >= 2 的 pattern → 升级为"待治理模式"
    ↓
对每个待治理模式：
  ├─→ 检查是否已在 module memory 的"硬约束"或"已知坑"中存在
  ├─→ 如果不存在 → 建议新增约束条目
  ├─→ 检查是否已在 state/current.md 的 RISK 中存在
  └─→ 如果不存在 → 建议新增 RISK
    ↓
生成 meta-review 报告
```

## 自动升级策略

### 自动升级（无需确认）
- 在 `state/current.md` 中新增/升级 RISK
- 在 `memory/modules/{module}.md` 的"当前状态"小节追加条目
- 在 `reflexions/index.md` 中更新模式状态（待治理 → 已治理）
- 更新 `memory/modules/{module}.md` 的 `meta_reviewed_at` frontmatter

### 人工确认后升级
- 在 `memory/modules/{module}.md` 的"硬约束"小节新增条目
- 创建新的 `memory/decisions/*.md`
- 修改已有决策的 `status`（需要确认 supersession 链）

## 报告格式

生成 `.acorn/harness/meta-reviews/{YYYY-MM-DD}.md`：

```markdown
---
type: meta_review
date: 2026-05-26
reflexions_scanned: 5
patterns_found: 2
auto_upgraded: 2
pending_confirm: 1
---

# Meta-Review: 2026-05-20 ~ 2026-05-26

## 高频模式（>= 2 次）

### pattern_openapi_mobile_sync
- **发生次数**: 3
- **涉及模块**: web, mobile
- **根因**: 执行流程缺少 API 变更后的 mobile 同步检查点
- **自动升级**: 已在 state/current.md 中新增 RISK-001
- **待确认**: 建议在 web.md 的"硬约束"中新增条目

### pattern_integration_test_missing
- **发生次数**: 2
- **涉及模块**: mobile
- **根因**: mobile 功能开发流程缺少 integration test 步骤
- **自动升级**: 已在 state/current.md 中新增 RISK-003
- **待确认**: 无

## 已治理模式（本轮处理）

| Pattern ID | 治理方式 | 关联文件 |
|---|---|---|
| pattern_openapi_mobile_sync | 新增 RISK-001 | state/current.md |

## 待确认升级（需人工 review）

| Pattern ID | 建议动作 | 关联文件 |
|---|---|---|
| pattern_openapi_mobile_sync | 在 web.md 硬约束中新增 API 同步条目 | memory/modules/web.md |

## Skill 改进建议

- `flutter-add-widget-test` skill 目前仅在用户明确要求时触发，建议将其作为 mobile 改动的默认验证步骤
```

## 汇报给用户

自动升级完成后汇报一行：
> meta-review 自动升级：新增 RISK-001（openapi/mobile sync）、RISK-003（integration test 缺失），web.md 有一条硬约束建议待确认。

如果全部自动升级无待确认项：
> meta-review 完成：扫描 5 个 reflexion，治理 2 个模式，无待确认项。
