# Pattern Version History

本目录保存 harness 模式的完整版本历史。

## 目录结构

```
.acorn/harness/memory/patterns/
├── README.md
└── {pattern-id}/
    ├── v0_discovery.md
    ├── v1_risk_created.md
    ├── v2_constraint_added.md
    └── v2_rollback.md（如有）
```

## 版本定义

- **v0**: 发现（首次记录在 reflexion）
- **v1**: 治理（升级为 RISK 或约束）
- **v2**: 完善（硬约束落地）
- **v3+**: 迭代优化

## 文件格式

```markdown
---
type: pattern_version
pattern_id: pattern_openapi_mobile_sync
version: v1
date: 2026-05-26
action: risk_created
---

# Pattern Version: pattern_openapi_mobile_sync v1

## 变更内容
在 `state/current.md` 中新增 RISK-001。

## 变更原因
meta-review 发现该模式在 3 个 run 中重复出现。

## 关联文件
- `state/current.md`
- `reflexions/index.md`
```

## 回滚记录

```markdown
---
type: pattern_rollback
pattern_id: pattern_openapi_mobile_sync
from_version: v2
to_version: v1
date: 2026-06-01
---

# Rollback: pattern_openapi_mobile_sync v2 → v1

## 回滚原因
新增的硬约束阻碍了正常开发（具体原因）。

## 操作
1. 标记约束为 `status: suspended`
2. 降级 pattern 版本
3. 在 `state/current.md` 中记录回滚原因
```