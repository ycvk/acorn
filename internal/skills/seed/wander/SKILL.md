---
name: wander
description: Advance one open concern in the conversation it was raised in during a scheduled idle-thought slot, an input starting with [wander (游思); a concern with an outcome is worked on with the workstation and delivered.
metadata:
  acorn-requires-tools: think recall memory_read concern knowledge_search
---
# Idle Thought

输入以 `[wander YYYY-MM-DD HH:MM]` 开头，给出本次要推进的关切：标题、outcome（有的话）、最新理由与复查时间。这次运行就在 owner 交代这件事的对话里。先回看本对话里之前的推进，弄清上次做到哪里、还差什么；需要更早的证据时用 `recall` 和 `memory_read`，需要已有资料时搜索知识库，需要外部资料时用 `web_search`、`web_fetch` 或浏览器。

## 有 outcome 的关切

outcome 写明做完时交给 owner 的东西和判断做完的标准。每次做下一步具体的事，朝它推进：

- 动手用工作机，先用 `tool_search` 取得 `shell` 与 `files`。工作文件放在输入给出的 `~/work/<关切 id>/`；`files` 与 `send_file` 的路径相对 home 目录，写成 `work/<关切 id>/…`。home 目录跨运行保留，接着上次留下的文件做。
- 有后果的操作会停下等 owner 在手机上批准。调用前说明要做什么；批准后接着做，被拒绝就换别的办法，或在理由里写明卡在哪里。
- 每次结束前用 `concern` 留一个 revision：理由写这次做了什么、还差什么，设下次的 review_at。outcome 不变时不传。想在下个游思时段之前接着做，用带 `concern_id` 的 `schedule_wake` 约一次醒来。
- 达到 outcome 时交付：文件用 `send_file` 发在本对话里，值得长期查阅的资料用 `knowledge_write` 写进知识库。然后调用一次 `notify_owner`，说成品已经发在这个对话里；再用 `concern` 把关切转为 `waiting`，理由写明交付了什么、在哪里，review_at 设在几天后。
- 有 outcome 的关切只由 owner 的确认解决，不自己标成 resolved。已交付、停在 waiting 的关切再被选中时，只看 owner 有没有回应：要求修改就接着改，改好再交付；没有回应就保持 waiting，推后 review_at，不再推送。

## 没有 outcome 的关切

检查进展与等待条件，区分已知事实、推测与仍待验证的问题。把有用的新想法用 `think` 保存，引用当前来源与关联记忆；用 `concern` 更新进展、等待条件和 review_at。需要明确时间再次行动时，用带 `concern_id` 的 `schedule_wake` 约一次醒来。已经解决或决定放下时更新关切状态；解决需要 owner 的确认或成功的工具结果作为来源。只有 owner 现在就需要知道或行动的消息才调用 `notify_owner`。

值得长期保留的资料写入知识库。手机通知作为外部背景，仅引用相关部分；没有回应表示反馈未知，不复述验证码或密码。
