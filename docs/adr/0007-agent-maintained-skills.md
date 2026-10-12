---
adr: 0007
title: agent 维护自己的技能
status: Proposed
date: 2026-10-12
supersedes: []
---

# ADR 0007 agent 维护自己的技能

技能现在是 `skills.dir` 下的 `SKILL.md` 文件，只来自 release 的 seed pack，agent 只能读。agent 做成一件多步骤的事之后，下次遇到同类任务还得从头摸索；它学到的东西只能写成 insight，insight 适合表达判断和偏好，装不下一套操作流程。有了工作机和浏览器之后，这类可复用的流程（在某个网站上查订单、按固定口径处理一份数据）正是 agent 最该积累的东西。installer 每次升级都用 seed pack 覆盖技能目录，技能也不在备份范围里，现在的存放方式本身就留不住任何本地积累。

## 决策

### 存储

技能存进 SQLite：`skills` 保存当前 revision，`skill_revisions` 保存每次写入的正文、哈希、作者与写入它的 run。一个技能是一份带 frontmatter 的 markdown，字段沿用现在的 id、name、summary、trigger_hints、requires，frontmatter 仍按现有规则严格解析。编辑基于读到的 revision，期间被改过则失败。技能纳入 `acorn backup` 与 `restore`。

seed pack 编进二进制，作为内置技能。启动时缺少的内置技能以 release 身份写入 revision 1；某个内置技能的内容在新版本里变了，启动时以 release 身份在当前 revision 之上写一个新 revision，agent 之前的修改留在历史里。内置技能和其他技能一样可以被修改和停用。

`skills.dir` 配置、installer 复制技能目录的步骤与从文件系统扫描技能的代码随之删除。技能只经 App、agent 和内置 seed 写入。

### 工具

- `skill_write`：创建技能，或基于读到的 revision 修改技能。
- `skill_retire`：停用技能，写一个停用 revision，可以从历史恢复。

内置规则要求技能只写可复用的操作流程与适用条件；关于 owner 的事实写进记忆，不写进技能。夜思在回顾时检查最近用过和新写的技能，合并重复、修正过时步骤、停用不再适用的技能。

### 审批

技能正文会进入之后每一次 run 的 Instruction，一段被网页、邮件或通知诱导写进去的文字会长期影响所有后续运行。starter config 默认把 `skill_write` 与 `skill_retire` 放进 `approval.require`：写入登记为 `tool_approval`，手机上的审批显示技能全文或与当前 revision 的差异，批准后写入。owner 把它们移到 `approval.allow` 即放开审批，revision 历史照常保留。

### owner 的入口

App 的技能页显示每个技能的当前内容、revision 历史和每次修改出自哪个 run，owner 可以回滚到任一 revision、直接编辑或新建技能。

### 跟着 owner 学

owner 在「Agent 的浏览器」里接手操作时，工作机记录这段操作：经过的页面地址和标题、点击和选择的元素名、导航与滚动。输入只记录输入到了哪个元素，文字本身不记录，密码和验证码因此不会进入记录。owner 结束接手后可以让 agent 把这段操作写成技能，agent 读取记录、补全适用条件与判断，经 `skill_write` 写入。

### 边界变化

本决定取代 ADR 0003 中「技能经只读发布流程提供」与 ADR 0004 中「技能保留现有只读发布流程」两处。insight 仍承担行为经验中的判断与偏好，技能承担可执行的流程。

## 分期

| 期 | 内容 | 验收 |
|---|---|---|
| S1 | 技能进 SQLite、内置 seed、`skill_write` 与 `skill_retire`、默认审批、夜思整理技能；删除 `skills.dir` | 让 agent 完成一件多步骤的事后记下流程，手机收到含技能全文的审批，批准后在新对话里交代同类任务，agent 加载并照着做 |
| S2 | App 技能页：历史、差异、回滚、编辑与新建 | owner 在 App 回滚 agent 的一次修改，下一次 run 用回滚后的内容 |
| S3 | 记录 owner 接手浏览器的操作，agent 据此写技能 | owner 在 App 里亲手完成一次网站查询，让 agent 存成技能，之后 agent 独自完成同样的查询 |

## 代价

- 技能质量取决于 agent 写得好不好，会出现重复和过时；靠审批、夜思整理和 owner 的回滚兜住，没有自动的对错保证。
- 每个技能的摘要都列在 `skill` 工具的描述里，技能变多会挤占上下文；摘要要短，重复的技能要合并。
- 内置技能和代码的输入格式有耦合（比如简报的 `[briefing` 输入）。release 改了内置技能时会盖过 agent 对它的修改，修改只留在历史里，需要 agent 或 owner 重新取舍。
- 技能不是记忆来源，遗忘请求不会顺带改动技能；技能里出现了不该留的内容，由 owner 在 App 里编辑或停用。
- 默认审批让每次写技能都要 owner 批一次；放开审批后，持久化注入的风险由 owner 自己承担。
