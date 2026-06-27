---
id: skill.inspect.repo
name: Inspect Repo
version: v1
category: native
summary: Map the current repository shape, entrypoints, and main call chain before proposing changes.
trigger_hints:
  - inspect repo
  - codebase map
  - 项目现状
  - 仓库结构
  - read the repo
requires:
  tools:
    - list_files
    - read_file
    - search_text
    - inspect_git_status
---
# Inspect Repo

在准备重构、修 bug、评估进度，或者用户让你先“看一下当前项目代码和架构设计”时使用。

工作方式：

1. 先用 `list_files` 和 `search_text` 看当前仓库的目录形状、入口文件和高信号符号，再确认真实入口和可运行表面，例如 `cmd/`、CLI 路由、server bootstrap、主要 runtime 入口。
2. 用少量高信号文件建立代码地图，不要一上来就把整个仓库扫平。
3. 确认真正的主调用链、状态持久化边界、工具边界、关键测试入口。
4. 先说当前真相，再说问题和下一步；不要脱离现有代码讲抽象方案。

推荐优先顺序：

1. `cmd/acorn/main.go`
2. `internal/cli/cli.go`
3. `internal/runtime/executor.go`
4. `internal/runtime/runner_factory.go`
5. `internal/store/sqlite/store.go`
6. 如果需要快速确认入口分布，可再看 `scripts/quick_map.sh`

如果 `list_files`、`search_text` 和上面几处已经足够建立代码地图，就直接收口，不要继续无边界漫游。

硬规则：

- 先确认真实入口，再谈重构。
- 优先看代码和测试，不要先相信旧文档。
- 结论要说人话，明确“现在是什么”“主路径在哪里”“最该先动哪一刀”。
- 如果发现方向已经跑偏，先指出偏差和原因，不要顺着错误前提继续扩写。
- 不要为了补充信息去重读 `skills/` 目录或 `SKILL.md`；skill 内容已经在上下文里。
- 默认先用 `list_files`、`read_file`、`search_text`、`inspect_git_status` 建图；不要把 `run_command` 当成常规入口。

输出至少应覆盖：

- Current shape
- Main execution path
- Primary hotspots
- Recommended next step
