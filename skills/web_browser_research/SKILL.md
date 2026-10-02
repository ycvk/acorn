---
id: skill.web.browser.research
name: Web Browser Research
version: v1
category: native
summary: Use Acorn's deferred web_search, web_fetch, and browser tools for public web research and interactive web pages.
trigger_hints:
  - web search
  - browser
  - browse website
  - open webpage
  - research online
  - search the web
  - search online
  - can you access the internet
  - can you browse the web
  - 查网页
  - 查一下网页
  - 帮我查一下
  - 搜索网页
  - 搜索一下
  - 打开网站
  - 会联网吗
  - 能联网吗
  - 联网
requires:
  tools:
    - web_fetch
---
# Web Browser Research

在用户要求查询当前网页信息、搜索公开资料、打开网站、读取需要 JavaScript 的页面，或检查网页交互状态时使用。

工作方式：

1. 先判断是否需要联网。如果用户给了明确 URL，通常先加载 `web_fetch`；如果页面需要 JavaScript、登录态、截图、按钮交互、console 或 network，再加载 `browser`。
2. 如果用户只给主题，没有 URL，先加载 `web_search` 搜索候选来源。搜索 snippet 只用于发现来源，不当作最终证据。
3. 对需要引用或事实回答的内容，使用 `web_fetch` 或 `browser` 的 `scan` 产出 Markdown artifact，再基于 URL/title/artifact evidence 作答。
4. 对动态页面使用 `browser`：
   - `open` 打开 URL。
   - `scan` 读取渲染后的正文。
   - `snapshot` 获取可操作元素 refs。
   - `click`、`fill`、`select` 优先使用 snapshot ref；CSS selector 只在 ref 不够用时使用。
   - `screenshot` 只在用户需要视觉证据或调试布局时使用。
   - `console` / `network` 必须显式 start 后再 list/stop，避免无意义噪声。
5. 每次回答都区分“搜索发现”和“已抓取证据”。不要把搜索结果摘要当成已验证事实。

推荐调用顺序：

1. 主题型问题：`tool_search({"query":"select:web_search,web_fetch"})` -> `web_search` -> 对可信候选逐个 `web_fetch`。
2. 明确 URL：`tool_search({"query":"select:web_fetch"})` -> `web_fetch`。
3. 动态页面：`tool_search({"query":"select:browser"})` -> `browser.open` -> `browser.scan`；需要操作时再 `browser.snapshot` -> action。
4. 需要同时搜索和动态页面：先 `web_search` / `web_fetch` 缩小范围，再加载 `browser`。

工具未启用时的处理：

1. `web_search` 只在配置了 `web_access.search.api_key` 时启用；`browser` 只在配置了 `browser.executable_path` 时启用。`tool_search` 加载不到它们就说明对应配置缺失，不要反复重试。
2. 没有 `web_search` 时，请用户提供 URL，或对已知的可信来源直接 `web_fetch`。
3. 没有 `browser` 时，停止需要 JavaScript 或交互的部分，不要改用 shell/curl 假装完成动态页面任务。告诉用户需要在 VPS 上安装 Chrome/Chromium，把可执行文件路径（常见为 `/usr/bin/chromium`）写进 `~/.acorn/acorn.yaml` 的 `browser.executable_path`，再重启 Acorn 服务。
4. `browser` 已启用但运行时报错（例如可执行文件不可访问）时，同样如实报告错误，不重试。

硬规则：

- 只访问 `http` / `https`，并尊重 Acorn 的 URL policy；不要尝试 file、localhost、metadata、私网绕过。
- 不暴露或请求 raw JavaScript、raw CDP、cookie 读写或持久浏览器 profile。
- 不使用 shell、curl、外部 CLI 或 MCP 来替代这些 native tools，除非用户明确要求。
- 工具未启用或页面抓取失败时，直接把失败事实和缺少的配置告诉用户，不伪造结果。
- 不在普通回答里倾倒 raw network/console 列表；只引用与问题直接相关的条目。

输出至少应覆盖：

- 使用了哪些来源或页面。
- 哪些内容来自抓取/扫描证据，哪些只是搜索发现。
- 如果失败，说明失败工具、目标 URL、错误边界和下一步需要的配置。
