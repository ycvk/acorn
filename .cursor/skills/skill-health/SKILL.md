---
name: skill-health
description: Acorn harness 的 skill 库健康度扫描与治理建议。
---

# Skill Health

## 触发条件

1. **定期触发**：每 7 天自动扫描一次
2. **事件触发**：meta-review 发现"某 skill 频繁失败"时立即扫描
3. **用户触发**：用户说"检查 skill 库健康度"或"skill-health"

## 健康度指标

扫描 `.acorn/harness/skill_usage.log` 和 `.cursor/skills/*.md` 评估：

| 指标 | 计算方式 | 阈值 |
|---|---|---|
| 调用频率 | 最近 30 天调用次数 | — |
| 成功率 | success / (success + failure) | < 0.7 标记 fragile |
| 最后调用 | 距今天数 | > 90 天标记 zombie |
| 关联 reflexion | 被 reflexion 提及为"改进建议"的次数 | >= 2 标记 needs_rework |

## 分类与处置

### healthy（健康）
- 条件：成功率 >= 0.7 且最近 30 天有调用
- 处置：无需行动

### fragile（脆弱）
- 条件：成功率 < 0.7 或关联 reflexion >= 2
- 处置：生成改进建议，标记为 needs_rework，列入下次 meta-review 关注

### zombie（僵尸）
- 条件：> 90 天无调用
- 处置：自动移动到 `.cursor/skills/deprecated/`，不删除文件

### gap（缺口）
- 条件：meta-review 发现重复模式但无 skill 覆盖
- 处置：在 `skill_usage.log` 末尾记录缺口，等人确认后创建新 skill

## 报告格式

生成 `.acorn/harness/skill_health_reports/{YYYY-MM-DD}.md`：

```markdown
---
type: skill_health_report
date: 2026-05-26
skills_scanned: 12
healthy: 8
fragile: 2
zombie: 1
gaps: 1
---

# Skill Health Report: 2026-05-26

## Healthy Skills

| Skill ID | 成功率 | 最近调用 | 备注 |
|---|---|---|---|
| orchestrate | 1.00 | 2026-05-26 | — |

## Fragile Skills（需改进）

| Skill ID | 成功率 | 最近调用 | 问题 | 建议 |
|---|---|---|---|---|
| cs-issue-fix | 0.60 | 2026-05-20 | 频繁遗漏 mobile 同步 | 在流程中增加 openapi.yaml 检查步骤 |

## Zombie Skills（已归档）

| Skill ID | 最后调用 | 归档日期 | 原因 |
|---|---|---|---|
| cs-feat-design | 2026-02-01 | 2026-05-26 | 90 天无调用，orchestrate 已取代 |

## Skill Gaps（待创建）

| Gap ID | 来源 | 优先级 | 建议功能 |
|---|---|---|---|
| integration-test-pattern | meta-review pattern_integration_test_missing | high | mobile 改动的默认 integration test 触发 |
| openapi-sync-check | meta-review pattern_openapi_mobile_sync | high | API 变更后的自动 mobile client 同步验证 |

## 自动处置

- 已自动归档 zombie skill：`cs-feat-design` → `.cursor/skills/deprecated/`
- 已标记 fragile skill：`cs-issue-fix` 增加 needs_rework 标签
```

## 汇报给用户

扫描完成后汇报一行：
> skill-health 扫描完成：8 healthy, 2 fragile, 1 zombie 已归档, 1 gap 待创建。

如果有 fragile 或 gap：
> 关注项：`cs-issue-fix` 成功率 60% 需改进；缺口 `openapi-sync-check` 建议创建新 skill。

## 缺口创建流程

1. skill-health 在报告中记录 gap
2. 用户确认"创建这个 skill"
3. 使用 `create-skill` 或手动编写 SKILL.md
4. 创建完成后在 skill_usage.log 中记录首次调用
