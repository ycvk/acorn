---
adr: 0006
title: agent 的工作机 — 在 VPS 上有一台能动手的隔离电脑
status: Proposed
date: 2026-10-12
supersedes: []
---

# ADR 0006 agent 的工作机

Acorn 已经跑在 owner 自己的 VPS 上，但 agent 用不了这台机器：能接触外部世界的只有网页搜索、抓取和一个每次用完即删配置的浏览器，没有命令和文件，也留不住登录。它能记住、能整理、能提醒，做不了需要动手的事，比如在某个网站上查订单、跑一段脚本处理数据、替 owner 填一张表。同类产品（OpenAI dots、xAI Grok Bot）的核心正是给 agent 一台自己的电脑。

## 决策

### 工作机

agent 有一台隔离的工作机：VPS 上一个由 Acorn 管理的 rootless Podman 容器，有自己的持久 home 卷、常用命令行工具和 Chromium，能联网。容器里看不到 Acorn 的 `storage_dir`、配置、env 和模型 key；agent 在容器里可以随意安装软件、写文件、跑命令，坏了可以由 owner 重置。工作机镜像随 release 发布，`acorn init` 和 installer 负责安装 Podman 并拉取镜像。

### 工具

- `shell`：在工作机里执行命令，有超时和输出上限，工作目录跨调用保持。
- `files`：读写工作机 home 下的文件，大文件只返回摘要并存为 artifact。
### 浏览器

浏览器统一由 [BrowserSkill](https://github.com/Tencent/BrowserSkill) 驱动：工作机里装固定版本的 `bsk` CLI 与 daemon，agent 经 `shell` 调用 `bsk`，用法由随附的技能说明提供。现有基于 chromedp 的 `browser` 工具、主机浏览器路径配置和临时配置目录随之删除。`bsk` 连两种浏览器：

- **owner 自己的浏览器。** daemon 以 server 模式运行，经现有反向代理暴露 WSS；owner 电脑上的扩展主动连入，配对、续期与吊销由 `bsk` 管理。agent 在独立的 Agent Window 里使用 owner 真实的登录状态，借用已打开的标签页需要显式借还。owner 的电脑在线时可用。
- **工作机里的 Chromium。** 装着同一个扩展，持久配置目录，24 小时可用，用于不需要 owner 登录状态的任务。

### owner 接手

遇到登录、二次验证、验证码、支付确认或同意页面，agent 调用 `bsk request-help`，同时用 `notify_owner` 推送到手机，说明在哪台浏览器上需要做什么。owner 在浏览器里完成后 agent 继续；取消或超时则如实结束。密码和验证码只在浏览器里输入，不进入模型上下文、消息记录或记忆。

### 审批

工作机内部是 agent 的空间，命令和浏览都直接执行。会产生对外后果的动作需要 owner 批准：发送、发布、购买、删除远端内容、修改账号设置。工具调用执行前经过一次有界的模型审查，结合 owner 的规则判断是否属于这类动作；属于时登记 `tool_approval` 并走现有的推送审批。owner 的规则分允许、审批、禁止三档，在 App 里编辑，`approval.require` 并入其中。

### 边界变化

本决定取代 ADR 0003「不做」中「需要登录态的网页代办」一条。自我迭代仍然不做：工作机里没有 Acorn 的源码、数据和部署权限。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| W1 | 工作机容器、`shell`、`files`，installer 与 doctor 支持 | 让 agent 下载一个 CSV、用脚本算出汇总并写进知识库笔记 |
| W2 | `bsk` 进工作机，连工作机 Chromium 与 owner 的浏览器；删除旧 `browser` 工具 | agent 在 owner 已登录的网站上查到一条只有登录后才能看到的信息 |
| W3 | `request-help` 与手机推送 | agent 遇到登录页时手机收到提醒，owner 在浏览器里登录后 agent 接着完成任务 |
| W4 | 动作前审查与三档规则 | 让 agent 在网站上提交表单，手机收到审批，批准后提交，拒绝则不提交 |

## 代价

- VPS 需要 Podman 和更多内存与磁盘；installer 变复杂，`acorn doctor` 要检查工作机状态。
- agent 连上 owner 的浏览器后，能用到这个浏览器配置里所有站点的登录状态；Agent Window 只隔离标签页，不隔离 cookie。工作机被攻破时，攻击者同样能经 `bsk` 使用这些登录状态。容器隔离只保护 Acorn 自己的数据。
- 依赖外部项目 BrowserSkill：版本在工作机镜像里固定并校验，升级随 Acorn release 走。owner 浏览器需要额外装扩展并与 `bsk` 单独配对。
- owner 不在电脑前时，需要其登录状态的任务只能等；手机上直接接手的实时画面不在本期范围。
- 动作前审查每次工具调用多一次模型调用，并且可能误判；禁止和审批规则以 owner 显式设置为准。
