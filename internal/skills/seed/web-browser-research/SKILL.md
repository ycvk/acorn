---
name: web-browser-research
description: Research the public web with web_search and web_fetch, and open pages that need a real browser with the browser skill (查网页, 搜索, 联网).
metadata:
  acorn-requires-tools: web_fetch
---
# Web Browser Research

在用户要求查询当前网页信息、搜索公开资料、打开网站、读取需要 JavaScript 的页面，或检查网页交互状态时使用。

工作方式：

1. 先判断是否需要联网。如果用户给了明确 URL，通常先加载 `web_fetch`；如果页面需要 JavaScript、登录态、截图或按钮交互，改用 `browser` 技能，在工作机的浏览器里打开。
2. 如果用户只给主题，没有 URL，先加载 `web_search` 搜索候选来源。搜索 snippet 只用于发现来源，不当作最终证据。
3. 对需要引用或事实回答的内容，使用 `web_fetch` 产出 Markdown artifact，或在浏览器里 `observe` 读到的正文，再基于 URL/title/evidence 作答。
4. 每次回答都区分“搜索发现”和“已抓取证据”。不要把搜索结果摘要当成已验证事实。

推荐调用顺序：

1. 主题型问题：`tool_search({"query":"select:web_search,web_fetch"})` -> `web_search` -> 对可信候选逐个 `web_fetch`。
2. 明确 URL：`tool_search({"query":"select:web_fetch"})` -> `web_fetch`。
3. 动态页面或需要交互：加载 `browser` 技能，按它的说明用工作机的浏览器。
4. 需要登录的网站：按 `browser` 技能的说明请 owner 在 App 里登录工作机的浏览器。

工具未启用时的处理：

1. `web_search` 只在配置了 `web_access.search.api_key` 时启用；`tool_search` 加载不到它就说明配置缺失，不要反复重试。
2. 没有 `web_search` 时，请用户提供 URL，或对已知的可信来源直接 `web_fetch`。
3. `browser` 技能不可用（没有配置工作机）时，停止需要 JavaScript 或交互的部分，如实告诉用户需要在配置里设置 `workstation.url`。
