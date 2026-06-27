---
adr: 0001
title: 从 reactive agent 转向 ambient agent
status: Proposed
date: 2026-06-27
supersedes: []
---

# ADR-0001: 从 reactive agent 转向 ambient agent

## Context

Acorn 当前形态:手机 inbox 发 message → 起 run → `direct_response` 执行 → owner 看结果。agent 纯 reactive,只在 owner 派活时工作。工具集(file/git/browser/web/command)容易让人误判 Acorn 是 code cli。

三条路线评估:

### 1. another code cli — 否决

Claude Code / Cursor / Aider 在本地终端实时写代码上是同步交互,Acorn 的"手机派活 + VPS 长跑"形态用自己弱项打别人主场。`internal/tools` 的 file/git 能力是底座不是定位。

### 2. 普通 loop agent — 否决

`direct_response` 这个 model→tool loop 本身没价值。价值在 loop 之上挂了什么:持久身份、审批门、远程控制面、记忆。拆掉这些 Acorn 就是个 Eino demo。

### 3. owner 的常驻 ambient 代理 — 采纳

三个差异化事实叠加:
- 手机是控制面,不是桌面终端(owner 不在电脑前也能驱动 agent)
- agent 长跑 + 审批门(需要授权才执行:花钱、发消息、改部署、动 SaaS)
- 持久身份(memory + skills,越用越懂 owner)

这三者咬合,场景是"我出门了,让它把这事办了,该我签字时找我"。Claude Code 服务不了这个场景。

## 借鉴

### KohakuTerrarium(https://github.com/Kohaku-Lab/KohakuTerrarium)

定位正交:KT 是 agent substrate **框架**(creature 六模块 + terrarium 多 agent 图运行时)。Acorn 是 **产品**(单 owner 单 agent 形态),不该框架化。但 KT 的概念拆解给了镜子:

- **Trigger 作为一等原语** — KT 定义:"任何不用用户输入就能唤醒 controller 的东西"(timer / context watcher / channel / custom)。Acorn 现在纯 reactive,没有 trigger 概念,这是变 ambient 的最大缺口。ambient 场景(盯邮箱、盯日志、盯价格)需要 agent 自己观察世界、自己决定何时唤醒。
- **非阻塞 compaction** — KT:`asyncio.Task` 后台跑 compaction,turn 间原子 splice,controller 不冻住。原文:"对 coding-agent 可容忍;对 monitoring/conversational creature 是产品缺陷。" ambient agent 长跑,同步 compaction 冻住期间 trigger 事件堆积,是硬伤。
- **session 同时服务 resume + 人搜 + agent-side RAG** — KT:一个 store 三个消费者。Acorn 现在分两套(run_events 在 SQLite,长记忆在 facts/history,中间缺"agent 回忆自己 run 历史"的能力)。

### Spice(https://github.com/Dyalwayshappy/Spice)

定位正交:Spice 是 executor **之上**的决策层(WorldState + WorldDelta + Decision Card + SDEP)。Acorn 自己是 executor(`direct_response`),不能再盖一个完整 Spice 决策层。但有一个洞察直接命中 Acorn 的可读性命门:

 - **Decision Card** — Spice 在执行前生成结构化卡片:考虑了哪些选项、各有什么证据、为什么选 A、拒绝了什么、是否需审批。Acorn 现有 `ask_operator` 工具已支持 `options`(id/label/description)+ `allow_freeform`,不是纯二元 yes/no,但 payload 只有 question+options,缺"为什么这么选"的依据。变 ambient 后 owner 更需要 agent 的决策依据才敢放手批。Decision Card 是给 `ask_operator` 补 evidence/rationale/risk/recommendation 维度,不是新建审批系统。
- **WorldState 作为决策相关投影** — Spice:WorldState 是"决策相关投影",不是 raw log,只有 `apply_delta(state, delta)` 一条变更路径。Acorn 跨 run 缺这层:run 内是 Session(临时),跨 run 是 facts(显式 remember),中间缺"agent 跨多个 trigger 周期维持的世界现在长什么样"。

## Decision

**Acorn 从 reactive agent 转向 ambient agent。** agent 自己盯世界(trigger),自己维持世界模型(WorldState),自己做结构化决策(Decision Card),高风险动作升级到 owner 手机审批,低风险自动执行。Mobile 是 Decision Card 的阅读器和审批面。

核心循环:

```
Trigger(观察世界) → WorldState(决策投影) → Decision Card(结构化决策)
  → 审批门(低风险自动 / 高风险升 mobile) → direct_response 执行
  → Outcome → 更新 WorldState
```

### 实施顺序(依赖链)

| # | 项 | 依赖 | 借鉴 |
|---|----|------|------|
| 1 | 非阻塞 compaction | — | KT `non-blocking-compaction.md` | ✅ 已完成(2026-06-27) |
| 2 | Trigger 抽象 + webhook trigger | #1(长跑前置) | KT `trigger.md` | ✅ 已完成(2026-06-27) |
| 3 | WorldState 投影层 | #2(有 trigger 才有世界状态可读) | Spice `world_model.md` | ✅ 已完成(2026-06-27) |
| 4 | Decision Card + 风险分级审批 | #3(有 WorldState 决策才有依据) | Spice `decision.md` | ✅ 已完成(2026-06-27) |
| 5 | search_runs 工具 | 独立 | KT `session-persistence.md` | ✅ 已完成(2026-06-27) |

### 锁定的选择

- Trigger 第一类:**webhook**。ambient 第一性场景是"外部世界事件进来"(邮件到达、CI 失败、价格变动),webhook 最通用。cron / file-watch 作为第二、三类,挂在同一 trigger 抽象。
- 非阻塞 compaction:**现在就做**。ambient 长跑前置,改 Session 核心循环。
- 审批形态:**Decision Card + 风险分级**。`ask_operator` 已是结构化审批(options+freeform),Decision Card 是在其 payload 上增 considered_options/evidence/rationale/risk/recommendation 维度。低风险自动放行,高风险升 mobile 审批。
- 跨 run 记忆:**WorldState 投影层**。`internal/memory` 加 delta-only 更新的结构化投影,填补 Session(临时)和 facts(显式)之间空白。

### 已锁定的实施决定

以下决定原为 Open Questions,经代码核实后按推荐最佳实践锁定:

1. **Trigger scheduler 住 `serve` 进程内,不独立 daemon。** 单 owner 不需进程间通信复杂度。新增 `internal/triggers` 包,Container 装配 `TriggerScheduler`,与 `Executor` 平级;`serve` 进程内常驻监听,fire 时调 `Executor.ExecuteMessages` 起新 run。独立 daemon 是过度设计。
2. **Trigger fire 起新短命 run,不续 session。** `RunTimeoutSeconds`(默认 900s)+ `direct_response` 同步 loop 决定了长 run 不可行。trigger fire 走现有 `Executor.ExecuteMessages` 起新 run,从持久化 WorldState 加载投影注入首轮 message;WorldState 是跨 run 唯一状态,session 是 per-run 临时态。
3. **WorldState 走 file-backed,不进 SQLite。** `sqlite_store.go` `SetMaxOpenConns(1)` 串行化所有 DB 操作,ambient 多 trigger 并发 fire 会排队等 DB。WorldState 投影用 file-backed(与 `internal/memory` facts/history 一致)+ 内存层,批量落盘,避开单连接瓶颈。SQLite 查询能力强但单 owner 场景下 file-backed + 内存索引够用;不足时再迁移。
4. **Trigger 配置走 file-backed。** trigger 定义、webhook 签名密钥、debounce 窗口是持久状态,serve 重启不能丢。与 skills 同模式(file-backed loader),不进 SQLite(同理避开单连接)、不进 in-memory(checkpoint 是 volatile)。
5. **审批动作广播到所有 active 设备。** 单 owner 语义下任意设备可批。`pending_actions` 现无 device 字段、广播轮询,保持现状;后续若需设备定向再加 `target_device`。
6. **安全策略:硬编码高风险白名单 + owner 可调中风险阈值。** 安全基线不可由用户降低,但可调高:
   - 风险判定用规则(非 LLM),判定器本身不可被 agent 绕过
   - 低风险(自动放行):只读操作(file_read、web_fetch、web_search、inbox 查看)
   - 高风险(必须审批,硬编码不可降级):任何花钱、任何对外发送(邮件/消息/commit push)、任何删除、任何部署变更、任何 SaaS 账户操作
   - 中风险(可配):白名单与高风险之间,owner 可调阈值

## 不做(边界)

- **不做框架化 / creature 六模块。** Acorn 是产品不是框架。内部模块化可以,不暴露 agent config 给用户造新 agent 形态。
- **不做多 agent terrarium / channels / composition algebra。** AGENTS.md 已删 plan_execute/single_agent/child_agent/verifier。一个 agent + trigger + 工具够用。KT 的 terrarium 图运行时是开倒车。
- **不做 SDEP / DomainPack / SimulationContext。** 单 owner 单进程不需要进程间握手协议。`internal/tools/dispatch` 已是 tool 执行边界。DomainPack 是给跨领域(软件/金融/机器人)用的,Acorn 只有一个领域:owner 的数字生活。Simulation(what-if 预演)对"发不发邮件"级别决策太重。
- **不新增 root mode。** ambient 不是新编排模式。trigger 是 `direct_response` 外层事件源,fire 时注入 message,direct_response 处理。保持 AGENTS.md "只有一个 root mode:direct_response" 硬约束。

## Alternatives Considered

- **继续 code cli 路线** — 否决,见 Context §1。
- **框架化变 KT 竞品** — 否决,必输且背叛 Acorn 产品定位。
- **完整 Spice 决策层** — 否决,过重。只取 Decision Card 结构 + WorldState 投影概念。

## Consequences

### 正面

- 定位与 code cli 彻底分开,差异化清晰
- mobile + 审批门 + 长跑三者咬合形成别人没有的场景
- WorldState 让 agent 跨周期有连续记忆,不是每次从零

### 负面 / 风险

- 触及 Session 核心循环(非阻塞 compaction)和 run 模型(trigger 注入),是结构改动非小修
- ambient 24/7 跑引入成本控制、安全(见 Open Questions)
- mobile 可能需要 push notification(现是 inbox 轮询)

## Open Questions

以下为实施期细节,方向已定(见 Decision §已锁定的实施决定):

1. **MCP manager 长间隔重连。** `mcpManagerCache` 跨 run 复用已就绪。风险:ambient 长 fire 间隔下 MCP 连接可能断。实施 #2 时验证 `ReconcileProviders` 是否够用,不够再加 health check + 自动重连。
2. **mobile push 通知机制。** ambient "该叫醒 owner 时叫醒他"需要主动通知,现是 pull(inbox 轮询)。SSE 已有基础但 mobile 后台不可靠。待评估 FCM/SSE/本地通知权衡。
3. **ambient 成本控制具体策略。** trigger 频繁 fire = 大量 LLM 调用。需 debounce、WorldState 变化检测(无实质变化跳过 LLM)、每日配额。实施 #2 时定具体阈值。
4. **Decision Card 生成时机。** 倾向:风险评估(规则)→ 低风险自动放行 → 高风险才生成 Decision Card 升审批。不做每次 tool call 都生成(贵)。实施 #4 时定。
5. **WorldState 具体 schema。** 不照搬 Spice 通用 entities/relations/goals。实施 #3 时设计 owner 数字代理专用 schema。
6. **webhook 认证细节。** HMAC 验签 + 速率限制。信任模型与现有 device auth 不同。实施 #2 时定签名方案。

## References

- KohakuTerrarium: https://github.com/Kohaku-Lab/KohakuTerrarium
  - `docs/en/concepts/modules/trigger.md`
  - `docs/en/concepts/impl-notes/non-blocking-compaction.md`
  - `docs/en/concepts/impl-notes/session-persistence.md`
- Spice: https://github.com/Dyalwayshappy/Spice
  - `docs/architecture.md`、`docs/world_model.md`、`docs/decision.md`、`docs/llm_boundaries.md`
- Acorn current-state: `docs/architecture/ARCHITECTURE.md`、`INVARIANTS.md`、`docs/architecture/runtime-context-memory-decision.md`
