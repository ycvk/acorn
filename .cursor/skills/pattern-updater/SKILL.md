---
name: pattern-updater
description: Acorn harness 的模式自动升级。将 meta-review 识别的高频模式自动升级为 RISK、约束或硬规则。
---

# Pattern Updater

## 触发时机

1. **meta-review 扫描完成后自动调用**
2. **用户说"升级这个模式"**

## 输入

- meta-review 报告中的"待治理模式"列表
- 当前 `state/current.md`
- 相关 `memory/modules/{module}.md`

## 升级策略

### 自动升级（无需确认）

**升级 RISK：**
- 如果 pattern 计数 >= 2 且 `state/current.md` 中无对应 RISK
- 自动新增 RISK 条目：
  ```
  - [RISK-XXX] {pattern_description}（已发生 {count} 次）
  ```

**升级当前状态：**
- 在 `state/current.md` 的"已知风险"小节追加
- 更新"下次建议动作"（增加预防措施）

**更新模式索引：**
- 在 `reflexions/index.md` 中将 pattern 状态改为"已治理"

### 人工确认后升级

**升级硬约束：**
- 如果 pattern 涉及架构/契约层面的系统性问题
- 生成建议文本，等待用户确认：
  > pattern-updater 建议：在 `memory/modules/web.md` 的"硬约束"中新增条目：
  > "修改 internal/web/ 中 API handler 后，必须同步 docs/openapi.yaml 并重新生成 mobile client"
  > 确认升级？

**升级决策：**
- 如果 pattern 涉及技术选型或架构决策
- 建议新建 `memory/decisions/{NNN}-{topic}.md`
- 等待用户确认

## 升级流程

```
读取 meta-review 报告
    ↓
对每个待治理模式：
  ├─→ 检查是否已有 RISK
  │   ├─→ 无 → 自动新增 RISK
  │   └─→ 有 → 更新计数
  ├─→ 检查是否已有约束
  │   ├─→ 无 → 生成建议，等确认
  │   └─→ 有 → 跳过
  └─→ 更新索引
    ↓
汇报升级结果
```

## 汇报格式

> pattern-updater 自动升级：
> - 新增 RISK-003（integration test 缺失，发生 2 次）
> - 更新 RISK-001 计数（openapi/mobile sync，发生 3 次）
> - 1 条硬约束建议待确认：web.md 新增 API 同步条目
