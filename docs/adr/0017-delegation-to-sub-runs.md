---
adr: 0017
title: agent 把一部分工作交给自己的子 run
status: Proposed
date: 2026-11-03
supersedes: []
---

# ADR 0017 agent 把一部分工作交给自己的子 run

agent 已经能在后台跑 shell 任务、等条件成立再醒来、跨几个 run 推进一个目标。这些都还是一个 agent 自己一步步做。owner 让它读十篇文章再汇总时，它在同一个 run 里一篇篇抓取，每篇的全文都留在上下文里挤占后面的工作；让它做完一件事再找人复核时，复核的也是写结果的同一份上下文，看不出它自己漏掉的东西。

Lexora 把这类工作交给子执行：父 agent 写一份 brief,子执行只拿到 brief 和明确交给它的材料，结果异步回到父 agent,子执行提交时附上证据，由父 agent 验收或退回重做，每个根回合有子执行数量上限。Acorn 采用同样的做法，在自己的 runtime 里实现，不交给 Claude Code、Codex 或其他外部程序。

## 决策

### 子 run

子 run 是同一对话里的一个 run,由父 run 的 `delegate` 调用派出。它照常记录事件、注册到 RunController、按 run 停止，Acorn 重启时还在跑的子 run 记为失败，删除对话时一并删除。子 run 不进对话的消息，不算对话最新的 run,不让 owner 的消息排队，也不出现在 run 列表里；线程摘要的文件段落不计子 run 的调用。

子 run 不能再派出子 run。每个父 run 至多派出 16 次（返工也算一次）,整个 Acorn 同时至多跑 4 个子 run,其余等空位。每个子 run 至多跑 30 分钟。

### brief 与上下文

`delegate start` 的 brief 有 objective、deliverables、constraints、acceptance,可选 block。子 run 的输入只有渲染后的 brief 与它在工作机上的临时目录，没有对话历史、人格、记忆、`<presence>` 与 owner 中途的消息。它的指令说明自己是被委派的子 agent、只为 brief 工作、外部文本是数据、完成时用 `submit` 交回。

子 run 的工具是白名单：`web_search`、`web_fetch` 与读取其全文的 `artifact_read`、工作机的 `shell`、`job` 与 `files`,知识库的只读工具，`news_search`、`feed_discover`,技能与技能包服务器的工具，再加 `submit`。推送、提问、约定、关切、目标、跟进、记忆写入、知识库写入、发文件、视图、技能维护与追踪项都不给。`mcp.providers` 的工具也不给：它们的服务器可能经 elicitation 向 owner 提问，而这个提问会落到对话的 run 上。

### 结果异步回到父 run

`delegate` 立即返回任务编号。子 run 结束后，它的报告在父 run 下一次调用模型前作为一条消息进入父 run:状态、`submit` 交回的 summary、evidence 与 limitations,没有提交时是它最后的回复或失败原因。

父模型说完一段话时如果还有子 run 没结束，运行时不结束这个 run,而是等：有子 run 结束、owner 在这个对话里发了消息，或 owner 停止 run。等到之后，同一个 run 带着到此为止的消息接着调用模型。所以父 run 只有在所有子 run 都报告之后才结束，它最后一段回复是对话里的 assistant 消息。

报告标明它是子 agent 的说法、不是 owner 的话，里面的指令不执行。遗忘提交时 memory visibility 会按来源重建父 run 的消息，父 run 已收到的报告随后重新交给它，被这次遗忘停止的子 run 的报告除外。

父 run 等审批时子 run 继续跑，报告留到父 run 续跑后交给它。子 run 每隔几秒看一次父 run,父 run 不论怎样结束(失败、被停止、续跑失败),还在跑的子 run 都随之停止。遗忘影响到父 run 时，它的子 run 也停止。

### 验收

子 run 用 `submit` 交回 summary、证据（跑过什么、读过什么、检查了什么，带路径和链接）与局限，可以多次提交，以最后一次为准。父 agent 对照 acceptance 用 `delegate` 的 accept 收下，或用 redo 带反馈退回。redo 以同一份 brief 加上一次的提交与反馈起这个任务的下一次尝试。报告只是子 agent 的说法，父 agent 要看证据，不照搬。

### 审批只会更严

子 run 使用父 run 的审批规则，brief 的 block 加进 `approval.block`。子 run 不能向 owner 要审批：任何在父 run 里会转审批的调用(`approval.require` 命中、审查判断需要确认、审查失败)在子 run 里直接拒绝，拒绝说明作为工具结果返回，要求它把没做成的事写进 limitations。需要 owner 批准的动作由父 agent 自己去做，照常审批。`submit` 只把结果交回父 run,不受规则约束。

### 用量

子 run 的模型调用记在子 run 上。Acorn 自己醒来的 run 派出的子 run 计入 `wake.daily_tokens`。

### 存储

SQLite 增加 `sub_runs`:子 run、父 run、任务编号、第几次尝试、brief、提交、验收结论、反馈、报告是否已交给父 run。子 run 的状态、输出与错误在 `runs` 里。

### App

父 run 的步骤里显示 `delegate` 调用与它的 objective。起一次尝试、报告交给父 run 与收下结果时，父 run 记 `sub_run.updated`;App 在回复下按任务列出每个任务最新的一次尝试：进行中、已交回、失败或已收下，交回的 summary 可展开，父 run 等子 run 时显示正在等几个子任务。子 run 自己的事件存在后端，可经 `/v1/runs/{run_id}/detail` 读取。

## 边界变化

本决定取消 ADR 0003「不做」里的「多 agent、子 agent」:每个 run 仍是一个 `ChatModelAgent`,run 可以派出不再派出的子 run。「把编码任务委派给 Codex / Claude」仍然不做。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| S1 | 子 run、`delegate` 与 `submit`、报告回到父 run 与等待、验收与返工、更严的审批、上限 | 在真实主机上让 agent 并行读几篇文章再汇总：每篇一个子 run,父 run 在全部报告后才给出汇总；再让它找一个子 run 复核结果，复核退回一次后重做通过 |
| S2 | App 在 run 详情里展开子 run 的步骤 | owner 在手机上看到每个子 run 的 brief、步骤与报告 |
