---
doc_type: user-guide
slug: agent-plan-loop
component: agent-plan-loop
status: current
summary: 说明 Acorn 如何先生成计划、按步骤执行，并通过 mobile 和 authenticated /v1 后端事实展示计划状态
tags: [runtime, plan, execution]
last_reviewed: 2026-05-16
---

# 执行前先计划再行动

## 功能简介

Acorn 现在每次执行任务都会先形成一个可追踪计划，再按步骤行动。计划会记录每一步的状态、依赖和验证证据；如果某一步失败，Acorn 会把失败暴露出来并决定是否重规划，而不是悄悄跳过。

这适用于 Flutter mobile 和其他 authenticated `/v1` remote client 发起的 run。CLI 只保留 operator/admin 命令，不再作为用户执行客户端。

## 前置条件

- 已配置可执行的模型 provider。
- 先用 `doctor` 确认后端配置和 runtime readiness。
- 启动后端服务，并完成 device pairing。

```bash
go run ./cmd/acorn doctor -c configs/acorn.example.yaml
go run ./cmd/acorn serve -c configs/acorn.example.yaml
```

## 如何使用

1. 启动后端并生成一次性 pairing payload：

```bash
go run ./cmd/acorn serve -c configs/acorn.example.yaml
go run ./cmd/acorn pair -c configs/acorn.example.yaml --server-url http://127.0.0.1:8080 --qr
```

2. 在 mobile 中扫码/手输 pairing code，或者用自定义客户端调用 `POST /v1/devices:pair` 换取 bearer token。后续请求都要带：

```http
Authorization: Bearer <access_token>
```

3. 通过 `/v1` 创建 thread、追加 user message，再启动 run：

```bash
curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Plan loop check"}' \
  http://127.0.0.1:8080/v1/threads

curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"content":{"type":"text","text":"请帮我检查这个仓库的测试入口。"}}' \
  http://127.0.0.1:8080/v1/threads/THREAD_ID/messages

curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode":"plan_execute"}' \
  http://127.0.0.1:8080/v1/threads/THREAD_ID/runs
```

下一轮继续对话时，Acorn 会优先复用还可继续的计划；只有观察结果要求重规划时才生成新计划。

4. 在 mobile control surface 打开对应 run 的 detail / activity 视图；如果使用 API 调试，读取 `/v1/runs/{run_id}/detail`。后端 RunDetail 会投影每个 step 的状态、依赖和验证证据。常见状态含义如下：

| 符号 | 状态 |
|---|---|
| `·` | pending |
| `→` | in_progress |
| `✓` | completed |
| `✗` | failed |
| `⊘` | skipped |

5. 需要离线核对计划时，可以查询标准 `/v1` run detail 或 persisted run events。除 `/healthz` 和 pairing exchange 外，`/v1` 端点需要 device bearer token：

```bash
curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" http://127.0.0.1:8080/v1/runs/RUN_ID/detail
curl -H "Authorization: Bearer $ACORN_DEVICE_TOKEN" 'http://127.0.0.1:8080/v1/runs/RUN_ID/events?follow=false'
```

## 常见问题

Q: 简单任务也会先计划吗？

A: 不一定。未显式指定 mode 的 root run 会走 `direct_response`，不生成 plan；带 `skill_id` 的 run 或显式选择 `mode=plan_execute` 的入口才会生成可追踪计划。

Q: 我还能让 agent 自己调用 `plan_write` 或 `plan_read` 吗？

A: 不能。这两个工具已经移除。计划是运行时执行循环的一部分，不再是模型可选调用的普通工具。

Q: `step.failed` 是否等于整次 run failed？

A: 不一定。普通工具错误会让当前 step 标记为 failed，然后交给观察节点决定是否重规划。只有模型调用、计划格式、计划持久化、SQLite、graph 装配等 Acorn 自身运行时错误才会让 run failed。

Q: 为什么有时会出现 `plan.updated`？

A: 当某一步失败或结果与预期不一致时，Acorn 可能选择 replan。此时它会保留 session/run 线索并更新计划。

Q: 计划里的 verification 是什么？

A: verification 是某个 step 留下的执行证据，例如工具名、命令摘要、工具输出摘要、verifier verdict、mutation checkpoint、rollback result 和来源 run。它用于解释“这一步为什么算执行过”，也方便后续调试。

Q: Acorn 会自动回滚修改吗？

A: 不会。会修改 workspace 的工具会留下 mutation checkpoint；只有显式调用 rollback 动作时才会按 checkpoint 尝试恢复。若当前工作区有非本次 checkpoint 的 dirty/conflicting 文件，rollback 会失败并列出冲突，不会覆盖你的其他改动。

Q: Plan 面板显示 No active plan 怎么办？

A: 当前 run 可能是普通问答，也可能还没有生成执行计划。先确认任务是否属于需要工具执行的类型；如果已经执行过，再用 `/v1/runs/{run_id}/detail` 或 `/v1/runs/{run_id}/events` 核对后端是否有 plan。

## 相关功能

- `POST /v1/threads/{thread_id}/messages`：追加用户消息。
- `POST /v1/threads/{thread_id}/runs`：启动任务执行。
- `/v1/runs/{run_id}/detail`：查看某次 run 的 detail 聚合，包括 workbench/plan/trace。
- `/v1/runs/{run_id}/events`：查看 persisted RunEvent 事件流。
