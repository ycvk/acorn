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

agent 有一台隔离的工作机：VPS 上一个 Podman 容器，由单独的 `acorn-workstation.service` 运行，有持久的 home 卷和常用命令行工具，能联网。容器里以只读方式挂载 Acorn 二进制，运行 `acorn workstation serve`，只在主机回环端口上提供命令与文件接口，每个请求带 env 里的 token。Acorn 的 serve 只经这个 HTTP 接口使用工作机，不接触容器引擎，原有的 systemd 加固保持不变；工作机里的程序随 Acorn 升级自动同版本。

容器里看不到 Acorn 的 `storage_dir`、配置、env 和模型 key。agent 以有 sudo 的普通用户运行，可以随意安装软件、写文件、跑命令；home 下的文件跨重建保留，系统包随镜像重建丢失。镜像定义随 release 包发布，installer 在服务器上用 Podman 构建。

### 工具

- `shell`：在工作机里执行 bash 命令，超时杀掉整个进程组，输出超出上限时保留开头和结尾。
- `files`：读写工作机里的 UTF-8 文本文件，读取超出上限时截断并标明。

### 浏览器

浏览器统一由 [BrowserSkill](https://github.com/Tencent/BrowserSkill) 驱动：工作机里装固定版本的 `bsk` CLI 与 daemon，agent 经 `shell` 调用 `bsk`，用法由随附的技能说明提供。现有基于 chromedp 的 `browser` 工具、主机浏览器路径配置和临时配置目录随之删除。`bsk` 连两种浏览器：

- **owner 自己的浏览器。** 一个 server 模式的 daemon 在工作机的 52801 端口用自签证书监听 TLS，反向代理把 `ACORN_BROWSER_PUBLIC_URL` 的 WSS 路径转发过来；owner 电脑上的扩展主动连入，配对、续期与吊销由 `bsk` 管理。agent 在独立的 Agent Window 里使用 owner 真实的登录状态，借用已打开的标签页需要显式借还。owner 的电脑在线时可用。
- **工作机里的 Chromium。** 无头运行，装着同一个扩展，连本地模式的 daemon，持久配置目录，24 小时可用，用于不需要 owner 登录状态的任务。

两个 daemon 和工作机的 Chromium 是 `acorn workstation serve` 守护的伴随进程。`web_rendered` 追踪经工作机的 `/v1/render` 用一次性的无头浏览器渲染。agent 的 `browser` 技能说明两个浏览器的用法；BrowserSkill 自带的详细说明随镜像放在工作机上，agent 用 `files` 读取。

### owner 接手

遇到登录、二次验证、验证码、支付确认或同意页面，agent 调用 `bsk request-help`，同时用 `notify_owner` 推送到手机，说明在哪台浏览器上需要做什么。owner 在浏览器里完成后 agent 继续；取消或超时则如实结束。密码和验证码只在浏览器里输入，不进入模型上下文、消息记录或记忆。

### 审批

工作机内部是 agent 的空间，命令和浏览都直接执行。会产生对外后果的动作需要 owner 批准：发送、发布、购买、删除远端内容、修改账号设置。

工具调用先按 owner 在配置里写的规则：`approval.block` 禁止、`approval.require` 审批、`approval.allow` 放行。规则没覆盖的 `shell` 与 MCP 调用由 TypeSafe 的 Jev 审查：它只返回带概率的判断，代码按阈值决定放行或登记 `tool_approval` 走现有的推送审批。只读命令由代码识别后直接放行，不调用审查；`bsk` 的点击、填写、按键和选择带着 agent 本次观察到的元素名和页面送审。付款页上的点击、试图说服审查放行的页面文字、没观察过的元素和审查失败都转审批。

用 Jev 而不用主模型审查，是因为它专做这类判断：一次审查约半秒、成本可以忽略，概率经过校准，阈值写在代码里可测。问题与阈值在 `internal/review/testdata` 的标注调用上评测确定，换模型版本时重跑。

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
- 动作前审查让需要审查的调用多一次 Jev 请求，负载高时会慢到数秒，并且可能误判；禁止和审批规则以 owner 显式设置为准。被审查的调用、页面信息和 owner 的请求会发给 TypeSafe。
