---
adr: 0019
title: Acorn 自己留意 owner 问过、答案会变的事
status: Proposed
date: 2026-11-14
supersedes: []
---

# ADR 0019 Acorn 自己留意 owner 问过、答案会变的事

测试机上 2026-11-10 到 11-13，owner 四天发了 34 条消息，其中有「Qwen 4 还没出吗」「这个以后会继续免费吗」「这个真的假的」这类答案以后会变的问题。Acorn 只在 owner 点「持续跟进」或开口交代时才跟进一件事，这个按钮 owner 自己没有用过，空闲思考在最近 7 个时段里只跑了 1 次。

唯一一条跟进是 10 月验收时留下的。夜思每晚检索它并把 review_at 往后推，改了 15 次，没有一次推送：夜思不调用 `notify_owner`，它推后的 review_at 又让会推送的空闲思考等不到到期。夜思还把上一晚自己加载过的技能全部列进当晚的复查，每晚重新加载十个技能；一次夜思读入约 32 万 token。

[Raven](https://github.com/evermind-ai/raven) 的主动性子系统把「该不该找用户」做成周期性的小判断：绝大多数周期不调用模型，推送受上限与反馈约束。这里借它的三点，便宜的批量检查、没有变化不出声、硬上限，落在 Acorn 已有的关切、空闲思考、约定与推送上，不另起一套循环。

## 决策

### 留意的问题是一种关切

owner 的问题答案会变、当下只能答「还没有」「未证实」「可能不长久」时，回答的 run 不先询问，直接用 `concern` 以 `question: true` 建一条关切：标题是问题，reason 是当下的答案，review_at 是下次看的时间，来源是 owner 这条消息。回复末尾说一句会留意。owner 说不用，agent 把它 released。

存储层保证四件事：留意的问题只能从 owner 自己的消息开出；不能同时有 outcome；同时至多 5 条；标记建立后不变。owner 没有设置空闲思考时间时 `concern` 拒绝开出并说明原因，agent 不作承诺。提问满 14 天仍没有新答案的，调度器在检查前直接 released，不调用模型。

### 到期的问题合成一次检查

空闲思考的每个时段先认领一次 `questions`。到期的问题合进一个 run，在它自己的对话 Watching 里运行，只看这份输入，不读任何对话的历史。输入列出每个问题、提问日期与目前的答案。agent 先 `news_search` 再 `web_search`，逐条用 `concern` 记录：有新答案记 resolved，reason 写新答案与链接；没有变化记 active，并给出下次 review_at。没有到期的问题时不起 run。这个 run 是自主唤醒，占唤醒额度。

### 新答案在 owner 提问的对话里告知

不在提问对话里的 run 把一个问题记为 resolved 时，存储层在同一事务里建一条立即到期的约定，落在提问的对话里。调度器下一个 tick 在那里唤醒 agent，输入带着检查记下的新答案；agent 在对话里写出答案，调用一次 `notify_owner`，了结约定。owner 自己又问了一次、由提问对话里的 run 把它 resolved 时不建约定。没有变化的检查不在 owner 的对话里留下消息，也不推送。

### 夜思不替检查做事

夜思的输入不再列留意的问题和持续跟进的新闻，它们由各自的检查查证并告知 owner。夜思复查的技能不计夜思自己加载的那些。

## 接口与存储

`memory_concerns` 增加 `question` 列（schema 40），`MemoryConcern` 增加 `Question`，`concern` 工具增加 `question` 参数。`/v1` 合同不变，此刻页照旧在关切里列出它。

## 边界变化

- agent 会不经询问为 owner 开出持续几天的检查。
- 空闲思考的时段可能多一个自主 run，Watching 对话出现在对话列表里。
- 持续跟进的新闻不再由夜思推进，由空闲思考每天检查并推送，这是 [ADR 0016](0016-briefing-story-follow.md) 原本的设计。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| P1 | 关切的 `question` 标记与四条约束；`questions` 检查；resolved 时的约定与唤醒输入；夜思输入与技能加载的两处改动；运行规则与夜思技能的说明 | 测试机上问一个答案会变的问题，回复末尾有一句会留意，此刻页的关切里出现这个问题；下一个空闲思考时段 Watching 对话里有一次检查，提问的对话里没有新消息；答案变化后提问的对话里出现新答案并收到一次推送；说「不用」后关切 released；夜思的输入里没有它，也没有夜思自己加载过的技能 |

## 代价

- 有问题到期的时段多一次 run，估计读入约 8 万 token；出现新答案时再加一次在提问对话里的 run。
- 「答案会不会变」由模型判断，会多开或漏开，靠 5 条上限与 14 天期限兜底。
- 检查的频率不高于空闲思考的时段，答案变了最迟要到下一个时段才知道。
- 停止留意只能在对话里说。
- 没有设置空闲思考时间的部署没有这项能力。

## 参考

- [ADR 0009](0009-background-work-on-concerns.md)：关切与空闲思考。
- [ADR 0016](0016-briefing-story-follow.md)：持续跟进早安卡的事件。
- `internal/store/memory_context.go`：`SaveConcern` 的约束与 resolved 时的约定。
- `internal/wake/thinking.go`：`questionsInput`、`nightInput` 与唤醒输入。
