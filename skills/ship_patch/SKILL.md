---
id: skill.ship.patch
name: Ship Patch
version: v1
category: native
summary: Implement a focused code change, keep scope tight, and verify the patch before closing the task.
lifecycle_status: verified
evidence_refs:
  - builtin:acorn-native-skill-seed-pack
trigger_hints:
  - implement the plan
  - apply patch
  - ship patch
  - 修复这个问题
  - 继续推进
requires:
  tools:
    - read_file
    - create_file
    - replace_span
    - apply_unified_patch
    - run_command
---
# Ship Patch

在方向已经收口、需要直接改代码并验证结果时使用。

工作方式：

1. 先确认本轮只改什么，不顺手扩成大重构。
2. 修改前先看现有入口、测试和相关边界，确保补丁贴在主路径上。
3. 改完立刻做格式化、测试和最短可复现验证。
4. 结尾明确说明改了什么、验证了什么、还没覆盖什么。

硬规则：

- 补丁要贴在主路径，不要绕到旁支。
- 能删掉的旧残留就一起删掉，不要留下双轨逻辑。
- 不要只停在“理论上应该可以”，要给出真实验证结果。
- 如果发现当前计划本身前提不成立，先停下来修正方向。

输出至少应覆盖：

- Scope of patch
- Files changed
- Verification
- Remaining risk
