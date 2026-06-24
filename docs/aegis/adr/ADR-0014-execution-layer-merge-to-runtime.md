# ADR-0014: 执行层 3→1 合并 (agent/context/stream → runtime)

Date: 2026-06-24
Status: Accepted

## Context

一次 run 的执行链跨 6 个包(wire → agent → context → stream → store),7 个 per-run assembly struct 只搬运代码不简化核心流。greenfield refactor 把 runtime 拆成 agent+context+stream 三包,执行链跨包数不减反增。

## Decision

将 agent/context/stream 全部合并为 `internal/runtime/` 单一包:
- Executor + RunnerFactory + direct_response 从 agent 移入
- Session + Plane + masking + auto_compact + tool_lifecycle 从 context 移入
- projection + assistant_stream + streaming_assistant 从 stream 移入
- factextract 内联为 memtools.go
- 7 个 assembly struct 全部删除,内联为函数(newChatModel/buildToolset/assembleContext 等)
- tool_lifecycle.go + tool_lifecycle_runtime.go 合并

## Consequences

- **正面**:执行链在一个包内闭合;7 个 struct→函数,减少装配间接;tool_lifecycle 从 2 文件合 1
- **负面**:runtime 包 24 文件 ~5900 行,需用多文件组织(最大 596 行)
- **风险**:无——全量测试 + race 检测通过

## Baseline Sync

- `internal/runtime/` 创建(commit `ce021f3`)
- `internal/agent/`、`internal/context/`、`internal/stream/` 删除(commit `6453d5c`)
- 7 个 assembly struct 内联(commit `3a5ce5f`)
- 测试迁移(commit `e367811`)
