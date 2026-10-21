---
adr: 0012
title: 进行中的 run 在 App 外可见
status: Proposed
date: 2026-10-21
supersedes: []
---

# ADR 0012 进行中的 run 在 App 外可见

owner 在手机上丢一个链接或一张图给 Acorn，然后切去别的 App。run 要几十秒到几分钟，期间 owner 看不到它做到哪一步：App 只在对话页打开时跟着事件流画步骤，切走后连接很快被系统冻结，回来时再重连。完成后除非 agent 调用 `notify_owner`，手机上没有任何提示；run 停下来等审批时才会有一条审批推送。

iOS 的灵动岛把这类进行中的事放在屏幕顶端。社区里的 Android 复刻（例如 dynamic-island-mchack）在 App 自己的窗口里用 Compose 画出会形变的胶囊，只在那个 App 打开时可见。Android 16 的 Live Updates 是系统给出的对应物：满足条件的进行中通知被提升为状态栏胶囊、通知栏顶部和锁屏上不可折叠的卡片，三星把它放进 Now Bar。系统要求它只用于用户发起、有开始和结束、正在进行的事，owner 刚发出的请求正是这一类。

## 决策

### 范围

只显示 owner 从这台手机发起的 run：对话里发出的消息、视图的按钮与选择、分享面板发出的分享。约定、追踪项、技能包触发器、早安卡、夜思与游思这些 agent 自己醒来的 run 不显示，它们不是 owner 发起的，照旧靠推送；其他设备发起的 run 也不显示。

### 跟随

App 发起 run 时还在前台，这时启动一个 `dataSync` 类型的前台服务，由它持有这个 run 的事件流：现有的 `GET /v1/runs/{run_id}/events`，断线按现有的规则重连。对话页读同一份投影，不再自己另开一条连接。服务可以同时跟随几个 run，每个 run 一条通知；最后一个 run 结束时服务退出。

run 走到 `run.completed` 或 `run.failed`、停下来等 owner 决定，或者 owner 停止它时，服务不再跟随这个 run。owner 在 App 里或在审批通知上批准之后，服务端续跑的 run 由 App 接着跟随。

### 通知

前台服务的通知就是 Live Update：ongoing，请求提升（`setRequestPromotedOngoing`），`ProgressStyle` 不定进度，因为 run 没有总步数。标题是对话标题，正文是当前步骤，沿用对话页里工具步骤的说法，例如"在搜索网页""整理成视图"；状态栏胶囊用 `setShortCriticalText` 显示不超过 6 个字的状态，例如"思考中""搜索中""写笔记"。点通知回到对话；通知上有一个"停止"，与对话页的停止相同。通知走单独的"正在处理"频道，只在出现时提示一次，之后的更新不响铃不振动。

run 结束时 Live Update 消失。App 不在前台时：完成的 run 在"Acorn 消息"频道留一条普通通知，正文是回复开头，点开进入对话；失败的 run 留一条说明失败的通知；停下来等审批的 run 不另发通知，由现有的审批推送接手。

### 系统版本

Live Update 需要 Android 16 QPR1 起才会被提升为胶囊和锁屏卡片。更早的系统，或 owner 在系统设置里关掉了 Acorn 的实时更新，同一条通知显示为普通的进行中通知，跟随与结束通知照常。App 不检测是否被提升，也不为此另做界面。manifest 增加 `POST_PROMOTED_NOTIFICATIONS`、`FOREGROUND_SERVICE` 与 `FOREGROUND_SERVICE_DATA_SYNC`。

### 边界变化

后端与 `/v1` 合同不变，通知里的内容都来自已投影的 RunEvent。App 第一次在后台持有网络连接，只限它发起的 run 进行期间；它仍不执行 run，也不持有或猜测后端事实。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| S1 | 前台服务跟随对话里发起的 run（消息与视图动作），对话页改读服务的投影；Live Update、停止、结束后的通知 | 在真机上发一条需要查资料的消息后切到别的 App：状态栏出现 Acorn 的胶囊，随 run 调用工具变换文字，点它回到对话；完成后胶囊消失，收到一条带回复开头的通知；在通知上点停止能中止 run；run 停下来等审批时胶囊消失，审批推送照常出现 |
| S2 | 分享面板发出的分享；在 App 里或审批通知上批准后续跑的 run | 从浏览器分享一篇文章后留在浏览器里，胶囊显示正在处理这次分享，完成后消失并留下通知；在审批通知上直接批准，续跑的 run 同样显示 |

## 代价

- run 进行期间 App 保持一个前台服务和一条事件流连接，多耗电与流量；Android 15 起 `dataSync` 前台服务每天合计至多 6 小时，超过后系统停止服务，通知随之消失。
- 只覆盖这台手机发起的 run；自主唤醒和其他设备发起的 run 仍只靠推送。
- 胶囊与锁屏卡片依赖 Android 16 QPR1 和厂商的实现，各家显示的位置不同，厂商还可能加额外条件。
- run 没有总步数，进度只能是不定进度，状态胶囊只有几个字。

## 参考

- [Create live update notifications](https://developer.android.com/develop/ui/views/notifications/live-update)：提升条件、状态栏胶囊与适用场景。
- [Progress-centric notifications](https://developer.android.com/about/versions/16/features/progress-centric-notifications)：`ProgressStyle`。
- [dynamic-island-mchack](https://github.com/ng1145/dynamic-island-mchack)：App 窗口内的灵动岛复刻，只在该 App 打开时可见。
