---
adr: 0003
title: 个人代理方向 — 唤醒驱动、工作记忆、知识库与信息追踪
status: Proposed
date: 2026-10-02
supersedes: [0001, 0002]
---

# ADR-0003: 个人代理方向 — 唤醒驱动、工作记忆、知识库与信息追踪

## Context

Acorn 的使用者只有一个:owner 自己。它跑在 owner 的 VPS 上,数据、账号和模型 key 都在 owner 手里。2026 年下半年同类产品集中出现(xAI Grok Bot、OpenAI Dots、开源的 OpenClaw),共同形态是:一个有身份和记忆的常驻 agent,接入用户的信息和应用,在用户习惯的界面里主动找人,高风险动作有审批边界。

对照这个形态,现状有几处根本问题:

- **定位是用排除法写的。** ADR-0001 说明了 Acorn 不做什么(code cli、普通 loop agent),但没有落到具体的日用场景上。工具集是 file/git/command/browser/web,没有任何一个工具能接触到 owner 关心的信息。
- **审批链路不通。** `run_command` 被风险闸门按工具名永久拦截,批准没有和具体调用绑定,重试依旧被拦。resume 时会重建 runner,而每个 runner 都会新建一个空的进程内 checkpoint store(`internal/runtime/direct_response.go` 的 `checkpointStore`),导致 `ResumeWithParams` 找不到 checkpoint。
- **Eino 只用到了外壳。** 用的是 `adk.Runner` 和 `schema` 类型,model→tool 循环、工具调度、上下文压缩、observation masking、延迟加载工具、技能加载都是自己实现的。Eino v0.9 里这些都有对应实现(`ChatModelAgent`,以及 summarization、reduction、toolsearch、patchtoolcalls、skill 这几个 middleware,还有工具级 interrupt)。
- **记忆机制重叠,而且依赖 LLM 抽取。** facts、history、Active Memory、Periodic Review、WorldState、技能自动生成,一共六套机制,事实主要靠模型从对话里抽取和蒸馏,长期运行后对错没有保证。

owner 确定的第一批日用场景有两个:**信息追踪 + 简报**、**随手记录 + 知识整理**。交互面保留自研 Android App,推送走 FCM,知识以 markdown 形式存在 VPS 上。

## Decision

Acorn 是一个有连续人格的个人代理。它盯着 owner 关心的信息源,接住 owner 随手丢过来的东西并整理进知识库,自己约时间醒来,空闲时自己思考,该找 owner 时推送到手机。

### 唤醒驱动

每次运行都由一个 Wake 触发。所有来源共用一个调度器,每次唤醒对应一次 `ChatModelAgent` 运行:

| 来源 | 触发方式 |
|---|---|
| owner 发消息 | 立即 |
| Capture(owner 从系统分享进来的内容) | 立即 |
| Watch 抓到新条目 | 按追踪项配置:立即唤醒,或攒进简报 |
| 约定(agent 用 `schedule_wake` 自己排的唤醒) | 到点 |
| 空闲思考(游思 / 夜思) | 固定时段 |
| 手机通知 | 进入信号缓冲,下次唤醒时一并呈现 |

自主唤醒(Watch、约定、空闲思考)受每日 token 预算和次数上限约束;owner 发起的唤醒不受限。P1 只有次数上限(`wake.daily_limit`,按 owner 时区的自然日计),token 预算随 P4 的空闲思考一起落地。

约定醒来时,本次运行在立约定的线程里进行,输入是 `[commitment #<id>, made <立约时间>] <要做的事>`;线程已删除时进入名为 Reminders 的线程。

一次运行的输入由三部分组成:人格(`{storage_dir}/persona.md`,owner 可编辑)、渲染后的"当下"、本次唤醒的内容。"当下"连同哈希写入 `context_snapshots`,用于事后完整还原当时的输入。

### 记忆:三层,各有唯一归属

- **工作记忆**(SQLite)。条目类型:
  - `said`:owner 的原话,由 `keep` 写入,按序号排列;
  - `thought`:agent 自己的念头,由 `think` 写入;
  - `commitment`:约定,包含醒来时间和醒来要做的事;
  - `tendency`:长期倾向;
  - `ruler`:agent 自己总结的关切和假设。

  每个条目有状态和 `expires_at`。到期未续期,由确定性代码执行衰减:活跃 → 暂歇 → 沉底。内化或放下由 agent 通过工具决定。渲染"当下"时有 token 上限。
- **知识库**:一个 markdown 目录(frontmatter 格式,兼容 Obsidian),同时也是一个 git 仓库。agent 可以自由创建和修改笔记,每次修改自动 commit,历史可审计、可回滚。检索用 keyword + vector 混合检索。
- **经历**:以 runs/events 表为唯一来源,加全文检索,用来回答涉及过去对话和行动的问题。

facts、history、Active Memory、Periodic Review、WorldState、技能自动生成全部删除,不做迁移。

### 感知

- **Watch**:流程是"确定性抓取 → 归一化 → 哈希比对",只有出现新条目时才调用 LLM。信息源类型:
  - `rss`:X、微博等通过自托管的 RSSHub 转成 RSS;
  - `github`:releases、issues;
  - `web`:readability 正文抽取或 CSS 选择器;
  - `web_rendered`:chromedp 渲染后用选择器抓取,用于价格、库存这类页面。

  agent 可以通过 `watch_create` / `watch_update` 自己增改追踪项。
- **Capture**:App 注册系统分享入口,文本、链接、图片走 `POST /v1/captures`。
- **手机通知**:App 用 NotificationListener 按 App 白名单批量上报,后端存为信号。

### 输出与审批

- **推送**:`notify_owner` 工具经 FCM 推送,内置频率限制和免打扰时段。
- **早安卡 / 简报**:一个固定的约定。到点醒来后汇总 Watch 变化、手上的事和接下来的约定,写成知识库笔记并推送。
- **等你处理**:`approval` middleware 在 `WrapInvokableToolCall` 中按规则判断,需要审批的调用触发工具级 `StatefulInterrupt`。checkpoint 持久化在 SQLite,审批对象是这一次调用的具体参数。批准后按原参数执行;拒绝结果作为工具结果返回给 agent。服务重启不影响待审批的运行。

### Runtime

- 使用 Eino `ChatModelAgent`,挂载 summarization、reduction、toolsearch、patchtoolcalls、skill 几个 middleware,checkpoint store 落 SQLite,chat model 加重试。
- 自研两个 middleware:
  - `presence`:在 `WrapModel` 中把"当下"作为最后一条 system 消息追加到本次模型调用的输入,并写入快照。"当下"只属于这一次调用;`BeforeModelRewriteState` 会把它写进 agent 状态,进而进入对话历史和 checkpoint,所以不用;
  - `approval`:见上文。
- 工具集:`web_search`、`web_fetch`、`browser`、`knowledge_*`、`keep`、`think`、`schedule_wake`、`settle`、`recall`、`notify_owner`、`watch_*`、`ask_owner`,另加 MCP 工具。
- 删除:`direct_response`、`ExecuteRound`、`internal/tools/dispatch`、`Session`、masking、auto-compact、file/git/command 工具、workspace checkpoint、webhook trigger、ambient instruction。

### App

App 分五页:此刻(早安卡、接下来、手上的事)、等你处理、知识库、经历、对话。另加系统分享入口和通知白名单设置。

### 分期

每一期结束都处于可日用状态。

| 期 | 内容 | 验收 |
|---|---|---|
| P0 | runtime 迁到 Eino `ChatModelAgent`;审批 middleware 和 SQLite checkpoint;删除旧 runtime | 手机上触发一个需审批的调用,批准后执行;中途重启服务后批准依然生效 |
| P1 | 工作记忆、经历检索、约定调度、人格、context 快照、FCM 推送与 `notify_owner` | 对 agent 说"三天后提醒我看 X",到点自动醒来处理并推送 |
| P2 | 知识库、Capture、App 分享入口 | 从任意 App 分享一个链接,几分钟内知识库出现整理后的笔记 |
| P3 | Watch、早安卡 | 每天早上收到汇总 RSS、GitHub、价格变化的简报 |
| P4 | 空闲思考、手机通知感知 | 夜思会清理过期念头;重要通知出现在早安卡里 |

## 不做(边界)

- 自我迭代(agent 修改自身代码并上线)。
- 语音、摄像头等家庭环境感知。
- 多 agent、子 agent。
- 把编码任务委派给 Codex / Claude。
- 需要登录态的网页代办(下单、填表、2FA 交接)。
- 多用户。

## Alternatives Considered

- **新仓库从零写。** 结构最干净,但鉴权、发布、App 外壳都要重新接线,见效最慢,还容易重演"建了再拆"。
- **服务端用 OpenClaw,只写客户端和插件。** 见效最快,但服务端会变成别人的 TypeScript 项目,知识库和 Watch 的设计也要迁就它的插件模型。
- **交互面用 Telegram 或飞书 bot。** 分享和推送都是现成的,但 owner 选择保留自研 App,以获得完全可控的交互和数据路径。

## Consequences

### 正面

- 每个功能都对应 owner 的一个日用场景,取舍有明确的判断标准。
- Watch 的确定性抓取把 LLM 调用限制在"确实有变化"的时候,成本可控。
- 工作记忆不依赖 LLM 抽取事实,原话按序保存,状态会衰减,上下文规模稳定。
- Runtime 代码量大幅下降,上下文管理和审批交给 Eino 的成熟实现。

### 负面 / 风险

- P0 是大手术:删除旧 runtime、替换 Session 体系,期间 `AGENTS.md`、`INVARIANTS.md` 和架构测试都要同步重写。
- 空闲思考和自主唤醒会持续消耗 token,预算阈值需要实际运行后再调。
- 通知感知会把手机通知内容上传到 VPS,白名单必须默认为空。
- 社交平台依赖 RSSHub,可用性受上游风控影响。

## References

- Grok Bot: https://docs.x.ai/grok-bot/overview
- OpenAI Dots 发布报道: https://www.theverge.com/ai-artificial-intelligence/1002033/openai-dots-launch-muse-competitor
- OpenClaw: https://github.com/openclaw/openclaw
- 果核(个人 agent 助手,工作记忆与自我唤醒设计): https://linux.do/t/topic/2958614
- Eino ADK middlewares: `github.com/cloudwego/eino/adk/middlewares`
