---
adr: 0011
title: 回复里的界面
status: Proposed
date: 2026-10-21
supersedes: []
---

# ADR 0011 回复里的界面

Acorn 的回复由 Markdown 正文、工具步骤和文件组成。比较 eino 几个版本的变化、列一份书单、给出几个下一步，都只能写成段落或 Markdown 表格；owner 在手机上横着滑表格，再打字说"就第二个"。对话里的结构化卡片只有审批、ask_operator 与 elicitation 三种，每种都由后端定义、App 写死。

让模型直接给出界面有两条路。ChatGPT 的 Intelligent UI 让模型写一段程序，服务端编译后放进浏览器里隔离的 iframe 与 Worker 运行，交互在本地即时重算；Google 牵头的 A2UI 让模型写一份组件清单，客户端从自己的组件目录里挑组件拼出界面，清单里没有可执行代码。Hark 的 Panels 把生成的界面常驻在首页，接着数据源刷新。

Acorn 走组件清单这条路。App 是原生 Compose，没有浏览器沙箱可用；App 不执行 run、不持有后端事实，清单由后端校验、App 只负责画；Acorn 用通用模型，写程序的路线需要很重的编译器容错，一份复刻用通用模型实测时大量依赖自动修补。

## 决策

### 视图

视图是 agent 在一次 run 里交给 owner 的一块界面：一份组件清单加一份数据。它属于产生它的对话，挂在那次 run 的回复上，与 `send_file` 发出的文件一样列在消息里；对话删除时一起删除。

清单沿用 A2UI v0.9 的组件模型：组件平铺成列表，每个组件有 `id`，容器用 `children` 列出子组件的 id，必须有一个 `root`；组件的值可以直接写，也可以用 `{"path": "/..."}` 指向数据里的 JSON Pointer，数据变了组件不用重发。组件只能来自 Acorn 自己的目录，视图记下它按哪个目录版本写成。A2UI 的传输层与 A2A 扩展不在范围内。

```json
{
  "title": "eino v0.9.17 → v0.9.21",
  "components": [
    {"id": "root", "component": "Column", "children": ["head", "table", "next"]},
    {"id": "head", "component": "Text", "variant": "title", "text": "五个版本的变化"},
    {"id": "table", "component": "Table", "columns": ["版本", "要点"], "rows": {"path": "/releases"}},
    {"id": "next", "component": "Row", "children": ["save", "skip"]},
    {"id": "save", "component": "Button", "label": "整理成笔记", "action": {"name": "save_note"}},
    {"id": "skip", "component": "Button", "label": "先不用", "variant": "quiet", "action": {"name": "skip"}}
  ],
  "data": {"releases": [["v0.9.21", "…"], ["v0.9.20", "…"]]}
}
```

### 组件目录

第一版目录：

- 布局：`Column`、`Row`、`Card`、`Divider`。
- 内容：`Text`（正文、标题、注释三种 variant，行内可带 Markdown 链接）、`Badge`、`Link`、`Image`、`Table`、`List`（按数据里的数组重复一个模板组件）。
- 图表：`Chart`（折线或柱状，数据来自数组）。
- 交互：`Button`、`ChoicePicker`（单选或多选）。

组件只描述内容和语义，颜色、字号与间距由 App 的主题决定，跟随深浅色。`Image` 只能引用这个对话里的文件或知识库附件，App 不按模型给的地址去外网取图。`Link` 的地址经 `webaccess` 的 URL policy 校验后才进入视图，owner 点开时在手机浏览器里打开。

### 工具

agent 用 `show_view` 交出一个新视图，用 `update_view` 按 id 替换这个对话里某个视图的数据或组件。两个工具与工作机工具一样经 toolsearch 按需加载；内置技能 `views` 写明目录、每种组件的字段和一份能直接用的完整例子，agent 要给界面时先加载它。

后端按目录严格校验：未知组件或字段、重复或缺失的 id、找不到的子组件、环、指向数据里不存在位置的 path、目录外的 variant、超过 200 个组件或 64 KiB 的清单都拒绝。错误是模型可见的 tool result，写明哪个组件的哪个字段不对，agent 改了再交。校验通过的视图存进 SQLite，记一个 `view.shown` 或 `view.updated` 事件，投影给 App。

视图不经审批：它只在 App 里显示，其中的按钮要 owner 点了才产生输入。

### 交互回到对话

`Button` 与 `ChoicePicker` 各带一个 `action`。owner 点按钮或提交选择时，App 调用 `POST /v1/threads/{thread_id}/views/{view_id}/actions`，只带组件 id、它在 List 里的位置和选中的选项；后端从存着的视图找出这个动作，核对选项来自组件声明的选项，再把它作为 owner 在这个对话里的一条消息起 run，规则与 owner 发消息相同。消息正文是 owner 看得懂的一句话，例如"整理成笔记"或"选了：A、C"；模型另外看到视图 id、动作名与动作声明要带上的数据。

每个动作只能提交一次。提交后视图记下 owner 的选择，按钮显示为已选，对话里留下这条记录；重复提交返回 409，不会因为网络重试多起 run。

动作产生的消息是 owner 的输入，照常进入记忆抽取。视图里的文字是 agent 写的，`show_view` 的调用与其他工具结果一样登记为来源，不会成为 owner 的事实。

ask_operator 留给 run 必须拿到回答才能继续的情况，run 暂停等待；视图的按钮在 run 结束后随时可点，起一个新 run。两者并存。

### App 渲染

目录里的每种组件对应 App 设计系统里的一个 Compose 组件。视图显示在回复正文下方，与文件并列，历史消息里同样显示。App 版本落后于后端时，不认识的组件显示为一行"这个版本的 App 显示不了这部分"，同一视图的其余部分照常显示。

### 面板

owner 可以把一个视图固定到今天页，成为面板；点面板回到它所在的对话，取消固定不影响对话里的视图。面板的数据有两种更新方式：agent 在任意 run 里用 `update_view` 改它；或者视图在 `show_view` 时声明 `source: <skill>/<trigger>`，这个技能包触发器（ADR 0010）的命令每输出一行 `{"view_data": {...}}`，后端就用它替换面板的数据，不调用模型。

### 边界变化

`/v1` 合同增加：RunEvent 的 `view.shown` 与 `view.updated`，Message 的 `views`，`POST /v1/threads/{thread_id}/views/{view_id}/actions`，今天页的 `panels`，以及固定与取消固定的端点。本决定扩展 ADR 0010 的触发器输出：除事件行外，带 `source` 的视图还接受 `view_data` 行。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| S1 | 视图的存储、校验与投影；`show_view`；Message 的 `views`；App 渲染第一版目录里的展示组件；`views` 技能 | 让 agent 比较 eino 最近五个版本：回复下方出现一张表和一段说明，深浅色都正常，重开对话仍在；故意交一个引用不存在子组件的清单，agent 收到具体错误后改正；删除对话后视图随之消失 |
| S2 | `Button` 与 `ChoicePicker`；动作端点与转成 owner 消息；`update_view` | agent 读完一篇长文后给出三个下一步按钮，owner 点"整理成笔记"，对话里出现这句话并起 run 写好笔记；同一按钮第二次提交返回 409；伪造视图里没有的动作返回 400 |
| S3 | 固定到今天页；面板；触发器输出 `view_data` | owner 把"追踪的仓库最新版本"视图固定到今天页；包的触发器发现新版本后面板数据更新，这次更新没有模型调用；点面板回到原对话 |

## 代价

- 组件目录是后端与 App 之间的新合同：加一种组件要同时改校验、openapi、App 渲染与 `views` 技能，并一起发版；旧 App 遇到新组件只显示占位。
- 清单计入模型输出，一张几十个组件的视图要上千 token；agent 要先加载 `views` 技能，多一次工具调用；通用模型写错清单时还要多一轮改正。
- 计算都在模型那里：owner 改了选择要等一轮 run 才看到结果，做不了本地即时重算的计算器或模拟器。
- 每个动作只能提交一次，想换个选择要 agent 给出新视图。
- 面板只在 agent 或触发器更新时变化，App 不自己轮询数据源。

## 参考

- [A2UI v0.9 规范](https://github.com/google/A2UI/tree/main/specification/v0_9_1)：平铺组件、数据绑定、组件目录与客户端动作。
- [Intelligent UI 实现详解](https://blog.pdjjq.org/post/detailed-explanation-of-intelligent-ui-implementation-4rp54)：ChatGPT 的写程序路线、与 A2UI、AG-UI 的对照，以及用通用模型复刻时的容错经验。
- [ADR 0010](0010-package-tools-and-triggers.md)：技能包触发器的输出约定。
