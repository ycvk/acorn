# Acorn Harness

长期项目协作者 harness 的共识记忆层与状态层。

## 目录结构

```
.acorn/harness/
├── README.md                    # 本文件
├── memory/
│   ├── modules/                 # 核心模块架构记忆（Markdown + frontmatter）
│   │   ├── runtime.md
│   │   ├── contextplane.md
│   │   ├── orchestration.md
│   │   ├── memorymodule.md
│   │   ├── skills.md
│   │   ├── web.md
│   │   ├── config.md
│   │   └── decision.md
│   └── decisions/               # 关键架构决策记录
│       └── 001-semantic-retrieval-store.md
└── state/
    └── current.md              # 当前项目状态（高频更新，.gitignore）
```

## 格式约定

- **模块记忆**：Markdown + YAML frontmatter。正文自然语言叙事，frontmatter 承载结构化元数据。
- **决策记录**：Markdown + frontmatter，沿用 Acorn Memory Record V2 约定。
- **项目状态**：纯 Markdown，AI 可直接改写段落。
- **绝不使用**：深嵌套 JSON。

## 更新责任

- `memory/` 下的文件在架构发生重大变化时更新，由 harness 主动提示或人工触发。
- `state/current.md` 每次 run 结束后由 orchestration 层更新。
