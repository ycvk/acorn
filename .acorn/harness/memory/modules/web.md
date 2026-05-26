---
type: architecture_module
id: web
status: stable
path: internal/web
interfaces:
  - from: client
    contract: "HTTP /v1 API requests"
  - to: runtime
    contract: "run execution requests"
  - to: store
    contract: "SQLite runs/events/pending_actions"
owner_run: run_abc123
last_updated: 2026-05-26
---

# Web

## 职责

HTTP 服务层。暴露 `/v1` 远程客户端 API、`/healthz` 健康检查、serve-time `/mcp` mount。

## 核心组件

- `/v1/inbox`：mobile inbox aggregation
- `/v1/pending-actions`：pending approval list/detail/decide
- `/v1/devices:pair`：device auth pairing
- `/healthz`：服务健康检查

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认 pending action 消费 SQLite `pending_actions`，不从 RunEvent 反推

## 硬约束（不可违反）

1. Remote clients 只走 `/v1`、`/healthz` 和 serve-time `/mcp` mount。
2. 不新增 legacy `/api` alias、debug-only API、mobile fake type 或绕过 OpenAPI 的 wire shape。
3. 修改 mobile DTO、RunEvent 类型、stream payload 或 OpenAPI schema 时，必须同步 `docs/openapi.yaml`、generated mobile client 和相关 parser/projection tests。
4. Mobile client API/model 改动必须从 OpenAPI 重新生成 `mobile/lib/src/api/acorn_api.dart`，不得手写 parallel DTO。
5. Push notification 只是 wake-up signal；client 必须回拉 `/v1/inbox`、RunDetail 或 RunEvent cursor。
6. Self-hosted remote access 必须有显式 auth/device boundary；token 缺失、格式错误、未知或 revoked 必须显式失败。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
