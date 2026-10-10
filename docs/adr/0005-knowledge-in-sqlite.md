---
adr: 0005
title: 知识库笔记存进 SQLite
status: Accepted
date: 2026-10-10
supersedes: []
---

# ADR 0005 知识库笔记存进 SQLite

ADR 0003 让知识库以 Markdown 文件和 Git 版本为真相，设想 owner 把目录 clone 到本地、用 Obsidian 编辑后推回。实际使用中知识库只由 agent 写入，owner 通过 App 阅读，没有双向同步的需求；Git 带来的系统依赖、命令行调用、文件与索引同步，以及无提交仓库这类边界情况，都没有换来对应的价值。

## 决策

笔记存进 SQLite：`knowledge_notes` 保存当前 revision，`knowledge_revisions` 保存每次写入的正文、哈希与写入它的 run，全文索引沿用 FTS5。一次写入在同一事务里产生新 revision 并登记为记忆来源，来源版本是 revision 号，展开原文读取对应 revision。编辑基于读到的 revision，期间被改过则失败。分享的图片仍存为 `{storage_dir}/attachments/` 下的文件。

`knowledge.dir` 配置、frontmatter 管理、文件与索引同步、服务器上的 `git` 依赖随之删除。笔记只能经 App 和 agent 读写。

这项决定取代 ADR 0003 中知识库以文件和 Git 为真相的部分，以及 ADR 0004 中知识来源按 Git 提交游标登记的部分。

## 迁移

已有安装在首次启动时把 `{storage_dir}/knowledge` 中的笔记导入为 revision 1，记忆来源的版本从提交号改为 1，来源 ID 不变；附件目录移到 `{storage_dir}/attachments`，旧目录删除。一个路径有多个已登记提交版本时导入失败并说明原因。
