---
id: skill.capture.to.note
name: Capture To Note
version: v1
category: native
summary: Turn something the owner shared from their phone (an input starting with [capture]) into a knowledge base note.
trigger_hints:
  - "[capture]"
  - shared from the owner's phone
  - save this link
  - 收藏
  - 存一下
  - 记到知识库
requires:
  tools:
    - knowledge_write
    - knowledge_search
    - web_fetch
---
# Capture To Note

owner 从手机分享进来的内容以 `[capture]` 开头，可能包含标题、链接、附言和图片附件路径。目标是在知识库里留下一篇以后查得到、读得懂的笔记。

工作方式：

1. 有链接时，先用 `tool_search`（`select:web_fetch`）加载 `web_fetch`，再抓取链接正文。抓不到正文（需要登录、反爬、超时）时照样建笔记，只写链接、标题和附言，并在回复里说明没抓到正文。
2. 用 `knowledge_search` 搜标题和主题关键词。已有讲同一件事的笔记时，用 `knowledge_edit` 在原笔记里补充这次的内容和来源，不另起一篇。
3. 新笔记用 `knowledge_write` 写到 `inbox/` 下，文件名用简短的英文或拼音 slug，例如 `inbox/tokio-work-stealing.md`：
   - `title`：文章标题，或用一句话概括内容。
   - `source`：原链接。
   - `tags`：2 到 4 个主题词。
   - 正文依次写：一段摘要；要点列表；owner 的附言（原话，标明"owner 附言"）；有图片附件时用 `![一句话描述](附件路径)` 引用，附件路径照抄 `Image:` 行里的 `attachments/...`。
4. 附言里有明确要求（例如"提醒我周末读"），按要求处理，必要时用 `schedule_wake`。
5. 完成后用一两句话回复 owner：笔记路径和一句话摘要。不要复述整篇内容。
