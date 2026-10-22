---
adr: 0014
title: owner 的设置存进 SQLite，在 App 里修改
status: Proposed
date: 2026-10-22
supersedes: []
---

# ADR 0014 owner 的设置存进 SQLite，在 App 里修改

owner 的设置都在 `~/.acorn/acorn.yaml` 里，后端启动时读一次，改了要重启 `acorn.service`。starter config 有 56 个字段，混着三类东西：部署时定一次的地址、密钥和上限；owner 的作息、预算和审批规则；只用于调参的内部数值。第一类本该在服务器上。第二类会随 owner 的生活调整，现在也只能 ssh 上去改。第三类 owner 不知道有什么用，也不该由 owner 来调。

2026-10-22 验收 ADR 0013 时，为了试 `news.daily_tokens`，要 SSH 进主机改 YAML 再重启。owner 的反馈是：这些配置应该能在 App 上设置，否则要配的东西太多，很多项也不知道有什么用。

人格与 owner 的称呼已经走过这条路（ADR 0008）：存进 SQLite，owner 经 App 或 CLI 修改，下一个 run 生效。

## 决策

### 留在配置文件

部署时定一次、带密钥、或改了要重启或重新部署的项留在 `acorn.yaml`：

- `runtime.storage_dir`
- `providers` 各项
- `review`
- `memory.embedding`
- `workstation.url`、`workstation.token`
- `web.listen_addr`、`web.allowed_origins`
- `web_access.allow_private_networks`、`web_access.search.provider`、`web_access.search.api_key`
- `notify.fcm`
- `backup.dir`、`backup.keep`
- `mcp.providers`
- `watch.rsshub_base_url`、`watch.github_token`
- `files.max_upload_mib`、`files.max_send_mib`：要和反向代理的请求体上限一起改

### 进 App 的设置

owner 的作息、预算和审批规则挪进 SQLite，称为设置：

| 设置 | 管什么 | 默认 |
|---|---|---|
| `owner.timezone` | 显示时间、按本地日计算预算、理解"早上八点"这类说法所用的时区 | UTC |
| `briefing.at` | 早安卡的时间，空表示不写早安卡 | 08:00 |
| `thinking.night_at` | 夜思的时间，空表示不夜思 | 03:00 |
| `thinking.wander_at` | 游思的时段，至多 6 个 | 无 |
| `notify.quiet_hours` | 免打扰时段，期间的推送等时段结束再发 | 23:00–08:00 |
| `notify.max_per_hour` | 每小时至多推送几条 | 6 |
| `wake.daily_limit` | agent 每天自主唤醒的次数上限，0 表示关掉自主唤醒 | 20 |
| `wake.daily_tokens` | 自主唤醒每天能用的 token，0 表示不限 | 0 |
| `memory.daily_tokens` | 后台整理记忆每天能用的 token，0 表示不限 | 100000 |
| `news.daily_tokens` | 后台整理新闻每天能用的 token，0 表示不限 | 100000 |
| `approval.block` | 这些工具直接拒绝 | 空 |
| `approval.require` | 这些工具每次等 owner 批准 | `mcp__*`、`skill_write`、`skill_import`、`skill_retire` |
| `approval.allow` | 这些工具直接执行，不经审查 | 空 |

审批规则的优先级不变：block 优先于 require，require 优先于 allow，规则没覆盖的 `shell` 与 MCP 调用交给审查。

### 删掉的调参项

只用于调参的内部数值删掉，改成代码常量，取现在的默认值：

- `memory.batch_tokens`、`memory.context_tokens`、`memory.history_tokens`；`history_tokens` 取测试机在用的 65536
- `presence.max_tokens`
- `context.compact_margin_tokens`、`context.mask_after_turns`
- `watch.max_checks_per_tick`
- `agent.name`、`agent.description`、`agent.max_iterations`
- `workstation.command_timeout_seconds`、`workstation.max_output_bytes`
- `web_access.user_agent`、`web_access.timeout_seconds`、`web_access.max_response_bytes`、`web_access.search.timeout_seconds`、`web_access.search.max_results`
- `runtime.run_timeout_seconds`：run 不设总时限，owner 在 App 里随时可以停止

`context.window_tokens` 跟着模型走：已知模型取模型表里的上下文窗口；其他模型在 provider 里写 `context_window_tokens`，没写时启动失败。

starter config 从 56 个字段减到 28 个。

### 存储与校验

设置存在 SQLite 的 `owner_settings`：每项一行，记录键、值、保存时间和经由的设备。键沿用原来的配置名。没有保存过的项用代码里的默认值。把某项恢复默认就是删掉它那一行。

每次保存只带改动的项，整批按现在配置的校验规则检查，有一项不合法就整批不存。时区是 IANA 名；时间是 `HH:MM`；游思时段不重复；免打扰的开始和结束同时给或同时不给；每小时推送上限至少 1；次数与 token 预算不小于 0；审批规则是合法的 glob，block 与 require 不能命中 `ask_operator`。

设置没有历史，以最后一次保存为准。

### 生效

用到设置的地方每次都从 SQLite 读当前值，不在进程里缓存；CLI 从另一个进程写的值同样在下一次读取时生效。

- 每个 run 开始时读审批规则，保存后的下一个 run 生效。
- wake 调度器每次检查时读早安卡、夜思与游思的时间和唤醒预算。改了时间，当天已经跑过的不重跑；新时间已过、还没跑过的，在下一次检查时跑。
- 记忆与新闻每次预留用量时读各自的 token 预算。某项预算调高或改为不限时，当天因它推迟到明天的记忆任务与新闻批次立即重新排上。
- 推送每次发送时读免打扰时段与每小时上限；已经排队的通知按排队时算出的时刻发出。
- 按本地日或本地时间计算的地方都读当前时区，包括预算的日界、presence 里的当前时间与技能触发器的 cron。

### 接口与 CLI

- `GET /v1/settings`：每项的当前值、默认值、是否在用默认值。预算项同时给出当天已用多少：自主唤醒次数、自主唤醒 token、记忆 token、新闻 token，这些数字后端已经在记。
- `PATCH /v1/settings`：`set` 带要改的项，`reset` 列出要恢复默认的项；不合法时返回 400 并指出是哪一项；返回保存后的全部设置。接口里的名字是键把 `.` 换成 `_`，例如 `owner_timezone`。

服务器上用 `acorn settings show` 列出全部设置、是否默认与预算的当天用量；`acorn settings set <键> <值>...` 保存一项，列表项给多个值；`acorn settings reset <键>` 恢复默认。`acorn doctor` 里的时间与预算改读设置。

### App

设置页的「Acorn」分组里加「作息、预算与审批」，里面分四组：作息（时区、早安卡、夜思、游思）、通知（免打扰、每小时上限）、每日预算（四项预算，下面是当天已用多少）、审批规则（block、require、allow 三个列表）。每项下面一句说明，内容同上表的「管什么」。时区从列表里选，首项是手机当前的时区。审批规则可以从 `/v1/tools` 的工具名里选，也可以手写 glob。保存失败时 App 标出后端指出的那一项。

### agent

agent 没有改设置的工具。预算和审批规则约束的就是 agent 自己，交给它改就失去了约束。owner 在对话里让 agent 改设置时，agent 请 owner 到 App 设置页去改。

### 边界变化

ADR 0004 里的 `memory.batch_tokens`、`memory.context_tokens`、`memory.history_tokens` 改为常量，`memory.daily_tokens` 改为设置。ADR 0008 之后 `owner` 一节只剩 `timezone`，它挪进设置后配置里不再有 `owner` 一节。ADR 0009 与 0013 用到的 `wake.*`、`briefing.at`、`news.daily_tokens` 改在 App 或 CLI 里设置，含义不变。

### 升级

配置照旧以 KnownFields 严格解析，挪走和删掉的字段没有兼容读取。新版本遇到它们拒绝启动，报错按行列出全部未知字段；CLI 读同一份配置，同样拒绝。安装器原样保留已有配置，升级后服务起不来，owner 按下面的步骤处理：

1. 记下 `acorn.yaml` 里改过默认值的设置。
2. 删掉 `owner`、`briefing`、`thinking`、`wake`、`news`、`approval`、`presence`、`context`、`agent` 整节，以及 `runtime.run_timeout_seconds`、`memory` 下 `embedding` 以外的行、`notify.max_per_hour` 与 `notify.quiet_hours`、`watch.max_checks_per_tick`、`workstation.command_timeout_seconds` 与 `workstation.max_output_bytes`、`web_access` 下的调参行。用的是模型表以外的模型时，在 provider 里加 `context_window_tokens`。
3. 用 `acorn settings set` 写回第 1 步记下的值，例如 `acorn settings set owner.timezone Asia/Singapore`。
4. 启动 `acorn.service`。

没改过默认值的项删掉即可。用户指南的配置迁移一节列出同样的步骤。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| P1 | `owner_settings`；设置的读写与校验；各使用处改读当前值；预算调高时放回推迟的任务；`/v1/settings`；`acorn settings show/set/reset`，安装器的 wrapper 认识 `acorn settings`；doctor 改读设置；调参项改为常量，`context.window_tokens` 按模型推出；配置结构、默认值、校验、starter config 与用户指南同步 | 在测试机上按升级步骤处理现有 `acorn.yaml`，写回时区、早安卡时间、游思时段与新闻预算后启动，`acorn settings show` 显示这四项与其余默认值；不重启，把 `news.daily_tokens` 设成 1000，当天的新闻整理因预算推迟，改回 50000 后推迟的批次当天处理完；不重启，把 `briefing.at` 改成 08:30，次日早安卡在 08:30 生成；`approval.require` 加上 `web_fetch` 后，下一个 run 里的 `web_fetch` 转审批 |
| P2 | App 设置页的作息、通知、预算与审批规则 | owner 在手机上改免打扰时段，之后的推送按新时段排队；预算旁的当天用量与 `acorn settings show` 一致；保存不合法的时间时 App 标出是哪一项 |

## 代价

- 升级要手动改 `acorn.yaml` 并写回改过的值；改完之前后端和 CLI 都起不来。
- 看 `acorn.yaml` 不再知道全部行为，设置要看 App 或 `acorn settings show`。设置随数据库进 `acorn backup`，恢复旧快照会回到快照时的设置。
- 默认值写在代码里。以后版本改了默认值，owner 没改过的项跟着变，预算也一样。
- 调参项成了常量，想改要发版本；已知模型不能再把上下文窗口设得比模型小。
- 改时区的当天，按本地日计算的预算与例行任务可能多跑或少跑一次。
- 设置没有历史，两台设备先后保存同一项，后保存的为准。
- 手机能改审批规则，没吊销的丢失设备能放开审批。补救和批准待审批项一样，是吊销设备。

## 参考

- [ADR 0004](0004-personal-memory.md)：记忆预算与上下文预算的划分。
- [ADR 0008](0008-persona-in-app.md)：人格与 owner 称呼存进 SQLite，由 owner 经 App 与 CLI 修改。
- [ADR 0009](0009-background-work-on-concerns.md)：游思与约定醒来计入的唤醒预算。
- [ADR 0013](0013-news-desk.md)：`news.daily_tokens` 与早安卡时间。
- `internal/cli/acorn.init.yaml`：starter config。
- `internal/config/`：配置结构、默认值与校验。
