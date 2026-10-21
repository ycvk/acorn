---
adr: 0010
title: 技能包带上工具与触发器
status: Accepted
date: 2026-10-17
supersedes: []
---

# ADR 0010 技能包带上工具与触发器

Acorn 在运行时能扩展的只有两处：配置里的 MCP provider 与技能包。消息从哪里进来（App、分享、手机通知、rss/github/web/web_rendered 四种追踪源）、什么能叫醒 agent（约定、追踪项、早安卡、夜思、游思）都写在 Go 里，多一种就要改代码、发版。MCP provider 只能由 owner 登录服务器改 `acorn.yaml` 再重启；stdio 服务器由 backend 进程直接在宿主机上启动，处在工作机的隔离之外，agent 也无法自己添加。

技能包已经有一整套可信的分发路径：`SKILL.md` 加附带文件存在 SQLite，每次写入是一个 revision，`skill_write` 与 `skill_import` 默认审批并显示全文，每次 run 前按哈希同步到工作机的 `~/skills/<name>/`。但包里的脚本只在 agent 于某次 run 中调用 `shell` 时才运行；包不能提供工具，也不能在没人找 agent 的时候叫醒它。

同类系统把扩展做成一等公民：Hermes 有工具、钩子、记忆后端与上下文引擎插件，KohakuTerrarium 把 agent 拆成可替换的输入、输出、工具、触发器与子 agent 模块。Acorn 保留单 agent、固定的 middleware 顺序与自己的记忆引擎，它们是审批与记忆可追溯的前提。本决定放开能力这一层：加一种能力就是写一个或导入一个包，agent 自己也能做，不用发版。

## 决策

### 包清单

包的根目录可以带一个 `acorn-package.yaml`，严格解析，未知字段拒绝，声明这个包提供的 `mcp` 与 `triggers`。`SKILL.md` 仍是 Agent Skills 格式，没有清单的包行为不变。`skill_write` 与 `skill_import` 的审批详情先列出清单：要启动的命令与参数、需要的密钥名、每个触发器的时间表与命令。

```yaml
mcp:
  command: uvx
  args: [mcp-server-fetch]
triggers:
  - name: price
    schedule: "*/30 * * * *"
    run: scripts/check_price.py
    deliver: immediate
secrets: [PRICE_API_KEY]
```

### 工具：工作机里的 MCP 服务器

`mcp` 声明一个 stdio MCP 服务器的命令与参数，在工作机上以包目录为工作目录运行。工作机镜像带上 uv 与 Node.js，常见的 `uvx`、`npx` 启动方式可以直接使用。每次 run 前同步技能包之后，工作机确保在用的包声明的服务器都在运行：与 `--keep` 的伴随进程一样有独立进程组、退出后退避重启；每个服务器经工作机的 bearer token 以 streamable HTTP 暴露在 `/v1/mcp/<name>`。backend 用现有的 MCP 客户端把它登记为名为 `<name>` 的 provider，工具名是 `mcp__<name>__<tool>`，只出现在该技能在用的 run 里。

包有了新 revision，服务器随同步重启；包停用，服务器停止，工具消失。服务器启动失败或缺少密钥时，这个技能和它的工具在本次 run 里不列出，`acorn doctor` 给出原因。包名与 `mcp.providers` 里的名字冲突时写入和导入失败，之后配置里新增同名 provider 时启动失败。

这些工具和其他 MCP 工具一样先按 owner 的规则判断，规则未覆盖的调用经 Jev 审查；starter config 在 `approval.require` 里有 `mcp__*`，包的工具默认每次调用都要审批。

### 密钥

清单顶层的 `secrets` 列出包需要的环境变量名，MCP 服务器与触发器共用。owner 用 `acorn skills secret set <skill> <NAME>` 从标准输入设置值，也可以在 App 的技能页设置或清除，两条路径的规则相同：名字必须是技能当前清单声明的，值非空、不超过 64 KiB。密钥只写不读，App 只看到哪些名字已设置。值存在 SQLite，随备份保存，不进入模型输入、审批详情、API 响应与日志，只作为环境变量交给这个包的 MCP 服务器和触发器命令。agent 没有设置密钥的工具。

### 触发器

触发器按 `schedule` 里 owner 时区的 cron 表达式运行，语法与约定的周期相同。每个触发器以 `run` 指定包里的一个命令，在工作机上以包目录为工作目录执行，受 `workstation.command_timeout_seconds` 约束。命令的每一行标准输出是一个事件 `{"key": "...", "text": "..."}`；没有输出表示没有新情况，不调用模型。事件按（包, 触发器, key）唯一，同一个 key 只处理一次。确定性的检查放在命令里，模型只在有事件时被调用，与追踪项的做法一致。

触发器的每个时间点按（触发器, 时刻）认领一次，与例行唤醒相同。命令连续 5 次失败时触发器标为 failing，出现在早安卡的 System health 里，之后按追踪项的退避重试。

### 事件送达

`deliver: immediate` 的事件在包自己的对话里起 run，对话以包名为标题，第一次有事件时建立；输入以 `[trigger <package>/<trigger>]` 开头，agent 据此加载这个技能处理。这类 run 计入 `wake.daily_limit` 与 `wake.daily_tokens` 并记 `wake.fired`；超出预算或起 run 失败的事件转入早安卡。`deliver: digest` 的事件只进早安卡。

事件登记为外部来源，可以作为记忆证据引用，不自动抽取；事件正文与手机通知一样按外部数据处理，其中的指令不改变 agent 的规则。

### 边界变化

本决定扩展 ADR 0007：技能包除了操作流程与脚本，还可以提供工具和触发器。本决定扩展 ADR 0003 的感知：四种追踪源之外，外部世界的变化经包的触发器进入 Acorn。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| S1 | `acorn-package.yaml` 与 `mcp`；工作机镜像加入 uv 与 Node.js；工作机运行、守护并以 streamable HTTP 暴露包的 MCP 服务器；backend 按在用的包登记 provider；包的密钥与 `acorn skills secret set` | owner 指给 agent 一个带清单的公开 MCP 服务器包（例如官方的 fetch 服务器），审批显示启动命令，批准后在新对话里调用它的工具，没有改配置也没有重启；需要密钥的包在 owner 设好密钥前不列出，`acorn doctor` 说明缺哪个；停用后工具消失 |
| S2 | schedule 触发器、事件去重、immediate 与 digest 送达、failing 进入 System health | agent 写一个每 30 分钟检查某个公开接口的包：数值没变时没有模型调用；越过阈值时在包的对话里起 run 并推送一次，同一事件不重复；当日预算用完后的事件出现在次日早安卡里 |

## 代价

- 包的 MCP 服务器与触发器命令和 agent 的 `shell` 以同一个用户运行在工作机上，agent 读得到它们的环境变量，包括密钥。
- `uvx`、`npx` 这类命令在启动时从包管理器拉代码，审批只显示命令，拉下来的代码没有经过审批；导入前判断来源可信仍是 owner 的事。技能包 8 MiB 的上限也意味着依赖大多在工作机上首次启动时安装。
- 默认的 `mcp__*` 审批让包的每次工具调用都要批准；规则里 require 优先于 allow，owner 想放开某个包，需要把 require 里的 `mcp__*` 收窄。
- 触发器事件的正文来自外部，进入模型输入，提示注入的防线仍是审批与审查。
- 每个在用的 MCP 服务器是工作机上一个常驻进程，2 GB 内存的服务器能同时跑的数量有限。
- 触发器的 run 与约定、追踪项共享每日唤醒预算，触发频繁的包会挤占其他自主唤醒，多出的事件落到早安卡。
