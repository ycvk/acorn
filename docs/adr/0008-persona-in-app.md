---
adr: 0008
title: 人格存进 SQLite，owner 在 App 里编辑
status: Accepted
date: 2026-10-15
supersedes: []
---

# ADR 0008 人格存进 SQLite，owner 在 App 里编辑

人格是 `{storage_dir}/persona.md`，每次 run 读取，只有 owner 能改。这个归属是对的：人格说明 agent 是谁、怎么说话，工具用法在 runtime 的内置规则里，agent 能写的记忆和技能都碰不到它。问题在入口。记忆、笔记和技能都在 SQLite 里，有 revision，App 能看；人格是服务器上的一个文件，只能 ssh 上去改，没有历史。Acorn 的日常入口是手机，结果人格成了 owner 最难调的东西。

## 决策

### 存储

人格存进 SQLite 的 `persona_revisions`：每次保存写一个 revision，记录内容、哈希、作者（`release` 或 `owner`）、经由的设备与时间。当前人格是最新的 revision。内容去掉首尾空白后不能为空，上限 8 KiB。保存基于读到的 revision，期间被改过则失败。

启动时还没有任何 revision，就以 release 身份写入默认人格作为 revision 1。之后的版本即使改了默认人格，也不动已有的人格。

每个 run 开始时从 SQLite 读当前人格，保存后的下一个 run 生效。人格随数据库进 `acorn backup` 与 `restore`。

`{storage_dir}/persona.md`、`acorn init --persona-only`、installer 写人格文件的步骤，以及备份恢复里复制人格文件的代码随之删除。

### owner 的入口

App 设置页有「人格」：显示当前内容，可以编辑保存；历史列出每个 revision 的时间、作者和设备，可以看它与当前版本的差异，也可以恢复到它。恢复是在当前 revision 之上写一个内容相同的新 revision，历史不改写。保存时遇到冲突，App 显示服务器上较新的内容，owner 在它之上重新编辑。

服务器上用 `acorn persona show` 输出当前人格，用 `acorn persona set <文件>` 以 owner 身份写一个新 revision。

接口：

- `GET /v1/persona`：当前人格与 revision。
- `PUT /v1/persona`：带 `content` 与 `base_revision` 保存；base 不是当前 revision 时返回 409，App 重新读取当前人格。
- `GET /v1/persona/revisions`：revision 列表，不含正文。
- `GET /v1/persona/revisions/{revision}`：某个 revision 的全文。

### agent

agent 没有写人格的工具。owner 在对话里说的说话方式偏好照旧进入记忆，由记忆影响之后的回答。人格不是记忆来源，遗忘请求和删除对话不改动人格。

### 与技能页

ADR 0007 S3 的技能页复用同一套历史、差异与恢复的界面。人格只有一份正文、没有附带文件，先做人格，再做技能页。

### 边界变化

本决定取代 ADR 0003 中「人格来自 owner 可编辑的 `{storage_dir}/persona.md`」。人格只由 owner 修改，这一点不变。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| P1 | `persona_revisions`、默认人格写入、run 从 SQLite 读人格、`/v1/persona` 接口、`acorn persona show/set`；删除人格文件与相关步骤 | 在测试机上 `acorn persona set` 一份新人格，新对话里的语气随之改变；基于同一 revision 的两次保存，后一次失败 |
| P2 | App 设置页的人格编辑、历史、差异与恢复 | owner 在手机上改人格，下一次对话按新人格回答；恢复到 revision 1 后，下一次 run 用回默认人格 |

## 代价

- 升级后不再读取 `persona.md`。改过人格的 owner 升级后用 `acorn persona set` 把文件写回，再删掉它。恢复本变更之前的备份快照时也一样，快照里的 `persona.md` 要手动写回。
- 在服务器上不能再直接用编辑器改人格，要经过 CLI 或 App。
- 人格进入每次 run 的 Instruction，写得越长，记忆和对话能用的预算越少；每次修改还会让模型侧的前缀缓存失效一次。
- 新版本的默认人格不会到达已有安装，想要新写法得 owner 自己改。
