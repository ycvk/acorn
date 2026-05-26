---
type: architecture_module
id: config
status: stable
path: internal/config
interfaces:
  - from: all
    contract: "configuration access"
owner_run: run_abc123
last_updated: 2026-05-26
---

# Config

## 职责

配置加载、验证与默认值管理。支持环境变量展开。

## 核心组件

- `configs/acorn.example.yaml`：可提交示例配置
- `configs/acorn.selfhosted.example.yaml`：自托管示例配置
- `configs/acorn.local.yaml`：本地配置（.gitignore）
- `~/.acorn/acorn.yaml`：二进制默认读取路径

## 当前状态

- Core logic: stable
- 已知问题: 无
- 最近改动: 2026-05-20 确认 public context config 只保留 `context.window_tokens`、`context.compact_margin_tokens`、`context.preserve_recent_turns`、`context.summary_max_tokens`

## 硬约束（不可违反）

1. `configs/acorn.local.yaml` 在 `.gitignore` 中，不要提交本地配置。
2. Provider `api_key` 支持环境变量展开；自托管 `systemd` path 通过 `~/.acorn/acorn.env` 的 `OPENAI_API_KEY` 注入。
3. `memory.semantic.embedding.api_key` 支持环境变量展开；它是 semantic retrieval 的独立 embedding key，不能从 chat provider key 静默复用。
4. 删除配置字段时不保留兼容读取，除非用户明确要求。旧本地配置 strict-load 失败是可接受的 hard cut 结果。
5. Public context config 只保留上述 4 个字段；reserved/static/warning/blocking/tokenizer/reduction 参数属于内部 derived policy。

## 关联决策

- [dec_001] Semantic retrieval 只使用 Bleve+FAISS，不接受 fallback
