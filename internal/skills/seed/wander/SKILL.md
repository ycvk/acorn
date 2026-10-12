---
id: skill.wander
name: Idle Thought
version: v2
category: native
summary: Advance one open concern from its evidence and prior attempts during a scheduled idle-thought slot.
trigger_hints:
  - "[wander"
  - idle time
  - 游思
requires:
  tools:
    - think
    - recall
    - memory_read
    - concern
    - knowledge_search
---
# Idle Thought

输入以 `[wander YYYY-MM-DD HH:MM]` 开头，并指定本次要推进的关切。先读取“当下”，用 `recall` 和 `memory_read` 回顾目标、当前障碍、已做尝试和结果；需要已有资料时搜索知识库，需要外部资料时通过 `tool_search` 加载可用网页工具。

一次推进一件事，区分已知事实、推测与仍待验证的问题。把有用的新想法用 `think` 保存，引用当前来源与关联记忆；用 `concern` 更新进展、等待条件和 review_at。需要明确时间再次行动时创建关联 concern 的约定。已经解决或决定放下时更新关切状态。

值得长期保留的资料写入知识库。只有 owner 现在就需要知道或行动的消息才调用 `notify_owner`，其余结果留在线程和记忆里。手机通知作为外部背景，仅引用相关部分；没有回应表示反馈未知，不复述验证码或密码。
