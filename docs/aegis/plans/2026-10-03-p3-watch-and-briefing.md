# P3: Watch 与早安卡

**Goal:** 每天早上 owner 收到一份简报:汇总 RSS、GitHub、网页(含价格)这些追踪项过去一天的变化,加上手上的事和今天的约定,写成一篇知识库笔记并推送到手机。追踪项由 agent 在对话里替 owner 建立和调整;标成"立即"的追踪项一有新条目就唤醒 agent。

**Architecture:** 新包 `internal/watch` 负责"确定性抓取 → 归一化 → 比对":四类信息源(`rss`、`github`、`web`、`web_rendered`)各自把抓到的内容变成条目,条目按 key 去重写进 SQLite,只有出现新条目才可能调用 LLM。抓取由 `wake.Scheduler` 在每次 tick 里驱动(仍是唯一的唤醒源):到期的追踪项被认领、抓取、比对;`immediate` 追踪项的新条目立即在建立它的线程里起一个 wake run,`digest` 追踪项的新条目留给早安卡。早安卡是调度器里按 `briefing.at`(owner 时区)每天触发一次的唤醒,在固定的 Briefings 线程里起 run,输入列出所有待简报的条目;agent 按种子技能 `morning_briefing` 写笔记 `briefings/YYYY-MM-DD.md` 并 `notify_owner`。agent 用 `watch_create`、`watch_update`、`watch_list` 管理追踪项。

**Tech Stack:** Go 1.27、`encoding/xml`(RSS 2.0 / Atom,不引入 feed 库)、GitHub REST API、`github.com/andybalholm/cascadia`(已在依赖树中,转为直接依赖)、现有 `webaccess` URL policy、现有 chromedp 浏览器服务。

**Baseline / Authority Refs:**
- `docs/adr/0003-personal-agent-direction.md`(§唤醒驱动、§感知 Watch、§输出与审批 早安卡、P3 行)
- `AGENTS.md`、`docs/architecture/INVARIANTS.md`
- P1 的约定调度(`internal/wake`)、推送(`internal/notify`);P2 的知识库(`internal/knowledge`)

**Owner 决策(2026-10-03,按推荐):**
- RSSHub 由 owner 自建(文档给出 Docker 一行命令);Acorn 只读配置 `watch.rsshub_base_url`,`rsshub:/route` 形式的 target 依赖它,未配置时 `watch_create` 拒绝并说明原因。
- 第一版支持 ADR 列出的四类信息源;`web_rendered` 只在配置了 `browser.executable_path` 时可用。
- 早安卡默认 `08:00`(owner 时区),`briefing.at` 为空则关闭。
- 每个追踪项可选 `immediate` 或 `digest`,默认 `digest`;`immediate` 的唤醒计入 `wake.daily_limit`,超出上限的新条目改进简报。
- App 的"此刻"页不在 P3;早安卡经推送和笔记送达。

**Compatibility Boundary:**
- 保持不变:`/v1` 全部现有接口与 OpenAPI;约定、推送、知识库、Capture 语义;`wake` 角色的存储与模型可见方式。
- 新增(无兼容层):
  - 配置 `watch.rsshub_base_url`、`watch.github_token`(环境变量展开)、`watch.max_checks_per_tick`、`briefing.at`。
  - 工具 `watch_create`、`watch_update`、`watch_list`;种子技能 `morning_briefing`。
  - 事件 `briefing.fired`(记在简报 run 上,不进 live 契约);`wake.fired` 的 payload 增加 `watch_id`。
  - App 把 `wake` 消息按输入前缀显示为"Woken by a commitment"、"New on a watch"或"Morning briefing"。
- 数据库:新表 `CREATE TABLE IF NOT EXISTS`;不改既有表的列,不需要迁移。

**TDD Route:**
- Mode: off
- Decision: skipped
- Strict authority: not applicable
- Test posture: 每个 Task 写针对性测试;修 bug 时先写能复现的测试
- Reason: 用户 TDD 模式为 off。
- Verification: 每个 Task 末尾的命令;全量门禁见 Task 10。

**Verification(全量):**
- `make format-check && make lint && go test -race ./... && make test-architecture`
- `deadcode ./...` 与基线一致
- `cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug`
- 模拟器验收(Task 10)

---

## Plan Basis

- **Requirement Ready Check:** ready。依据是 ADR-0003 的 P3 行与 owner 的五项决策。验收标准:每天早上收到汇总 RSS、GitHub、价格变化的简报。
- **Change Necessity:** code-change。现状里 agent 只能在被唤醒时临时 `web_fetch`,没有持续追踪、去重和按时汇总的机制;约定能定时醒来,但不知道"自上次以来有什么新东西"。
- **Existence Check:**
  - `internal/watch`:add-with-proof。四类信息源的抓取、解析与比对没有归属;`webaccess` 只做单次抓取和正文抽取,不认识条目与去重。internal 包数量从 15 变为 16。
  - `watches`、`watch_items`:add-with-proof。追踪项配置与已见条目需要持久化,和 `memory_items`(会衰减的工作记忆)语义不同。
  - `briefings`:add-with-proof。按 owner 本地日期做主键,保证多进程下每天只触发一次简报,并记住 Briefings 线程。
  - `core.WatchStore`:add-with-proof。core 中的 store 接口从 6 个变为 7 个。
  - 早安卡不建成 `memory_items` 里的循环约定:约定可被 agent `settle release` 掉,而早安卡是 owner 配置的固定行为;它也需要附带待简报的条目,约定输入里没有这个位置。
  - 不新增 App 页面或 `/v1` 接口:追踪项通过对话管理,简报通过推送和知识库笔记阅读。
- **Architecture Integrity:**
  - 唯一 owner:唤醒(约定、追踪项、早安卡)只在 `wake.Scheduler`;抓取与比对只在 `watch.Checker`;外网访问统一过 `webaccess` 的 URL policy。
  - 新旧不并存:不保留"让 agent 用约定 + web_fetch 模拟追踪"的写法;`operatingRules` 改为指向 watch 工具。
- **Complexity:** 新文件各自 400 行以内;`internal/watch` 预计约 900 行。

## Execution Readiness View

- **Intent Lock:** 只做 ADR-0003 的 P3(Watch、早安卡)。不做空闲思考、token 预算、手机通知感知,也不做 App 的"此刻"页。
- **Scope Fence:**
  - 不做登录态抓取、cookie 注入、代理切换;需要登录的页面在 `watch_create` 首次抓取时失败并给出原因。
  - GitHub 只做 `releases` 与 `issues`(新开的 issue,不含 PR 评论、状态变化)。
  - `web` / `web_rendered` 比较的是"选中内容的快照",不做结构化 diff;价格追踪靠 CSS 选择器选中价格元素。
  - 不做追踪项的 App 管理界面。
- **Baseline Lock:** 起点 commit 为分支 `direction/personal-agent` 上的 `b235071`。
- **Task Batches:**
  - T1–T3:存储、`internal/watch`、配置。
  - T4–T5:调度器接入 Watch 与早安卡。
  - T6:工具与 operating rules、种子技能。
  - T7:手机端 wake 卡片区分来源。
  - T8–T10:验收测试、文档、全量门禁与模拟器验收。
- **Drift / Rewind Rules:**
  - Task 2 中,如果 `webaccess.FetchService` 拒绝 XML/JSON 内容类型,新增 `FetchRaw`(同一 URL policy、超时与大小上限,不做正文抽取)供 watch 使用;不在 watch 里另建 HTTP client 绕过 policy。
  - Task 2 中,如果 chromedp 浏览器服务不能在 run 之外独立创建,停下来,把"渲染页面取 HTML"抽成 `tools` 里的独立函数,由 wire 注入 `watch.Renderer`;不在 watch 包里直接 import chromedp。
  - Task 4 中,如果抓取耗时让 tick 明显拖慢约定唤醒(单次 tick 超过 interval),把追踪项检查移到调度器里独立的 goroutine 循环(仍归 `wake.Scheduler` 所有,共用认领逻辑);不新建第二个调度器类型。
- **Evidence Required Before Completion:**
  - Task 8 验收测试通过。
  - Task 10 全量门禁通过。
  - 模拟器验收:简报线程与卡片显示正确;推送因本机网络无法送达设备时如实写明。

## Task 0: 记录基线

- [ ] 运行:

```bash
deadcode ./... > /tmp/acorn-p3-deadcode-before.txt; deadcode -test ./... >> /tmp/acorn-p3-deadcode-before.txt; cat /tmp/acorn-p3-deadcode-before.txt
go test ./... 2>&1 | tail -3
```

预期:deadcode 只有 `config/config_defaults.go DefaultConfig`;测试全部 ok。

---

## Task 1: 追踪项与简报的存储层

**Files:** Create `internal/core/watch.go`、`internal/store/store_watch.go` 和测试;Modify `internal/store/store_schema_bootstrap.go`、`internal/store/store_schema.go`

- [ ] **Step 1:** 新表(表数量注释同步更新):

```sql
CREATE TABLE IF NOT EXISTS watches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,              -- rss|github|web|web_rendered
    target TEXT NOT NULL,            -- URL, rsshub:/route, or owner/repo
    selector TEXT NOT NULL DEFAULT '',  -- CSS selector (web*), releases|issues (github)
    mode TEXT NOT NULL,              -- immediate|digest
    interval_seconds INTEGER NOT NULL,
    status TEXT NOT NULL,            -- active|paused|failing
    session_id TEXT NOT NULL DEFAULT '',
    next_check_at TEXT NOT NULL,
    last_checked_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    failures INTEGER NOT NULL DEFAULT 0,
    snapshot TEXT NOT NULL DEFAULT '',   -- last selected content (web*)
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS watch_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    watch_id INTEGER NOT NULL,
    item_key TEXT NOT NULL,          -- guid/link, release id, issue number, or content hash
    title TEXT NOT NULL,
    url TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,            -- baseline|pending|woken|briefed
    run_id TEXT NOT NULL DEFAULT '',
    seen_at TEXT NOT NULL,
    UNIQUE(watch_id, item_key)
);
CREATE TABLE IF NOT EXISTS briefings (
    day TEXT PRIMARY KEY,            -- owner-local YYYY-MM-DD
    thread_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
```

  索引:`watches(status, next_check_at)`、`watch_items(status, seen_at)`。

- [ ] **Step 2:** `internal/core/watch.go`:`Watch`、`WatchItem` 类型,`WatchKind`、`WatchMode`、`WatchStatus`、`WatchItemStatus` 常量,以及:

```go
type WatchStore interface {
	AddWatch(ctx context.Context, w Watch) (Watch, error)
	LoadWatch(ctx context.Context, id int64) (*Watch, error)       // ErrWatchNotFound
	ListWatches(ctx context.Context) ([]Watch, error)
	UpdateWatch(ctx context.Context, w Watch) error
	// ClaimDueWatch moves next_check_at of an active or failing watch due at now
	// to now+lease; exactly one concurrent caller succeeds (ErrWatchNotDue otherwise).
	ClaimDueWatch(ctx context.Context, id int64, now time.Time, lease time.Duration) error
	ListDueWatches(ctx context.Context, now time.Time, limit int) ([]Watch, error)
	// AddWatchItems inserts items not seen before and returns the inserted ones.
	AddWatchItems(ctx context.Context, items []WatchItem) ([]WatchItem, error)
	ListWatchItems(ctx context.Context, status WatchItemStatus, limit int) ([]WatchItem, error)
	MarkWatchItems(ctx context.Context, ids []int64, status WatchItemStatus, runID string) error
	// ClaimBriefing records day's briefing; ErrBriefingTaken when it exists.
	ClaimBriefing(ctx context.Context, day, threadID string, at time.Time) error
	SetBriefingRun(ctx context.Context, day, runID string) error
	LatestBriefingThread(ctx context.Context) (string, error) // "" when none
}
```

  时间一律由调用方给出。

- [ ] **Step 3:** 测试:CRUD;`AddWatchItems` 对重复 key 不插入、只返回新条目;两个并发 `ClaimDueWatch` 只有一个成功;`ClaimBriefing` 同一天第二次返回 `ErrBriefingTaken`。

```bash
go test ./internal/store ./internal/core -count=1
```

---

## Task 2: `internal/watch`:信息源、比对、检查

**Files:** Create `internal/watch/{source.go,rss.go,github.go,web.go,checker.go}` 和测试;Modify `internal/webaccess/fetcher.go`(按 Drift Rule 增加 `FetchRaw`)、`tests/architecture/*`(`watch` 为 Layer 2,加入 structural limits)

- [ ] **Step 1:** `source.go`:

```go
type Fetched struct {
	Items    []core.WatchItem // rss/github: entries; web*: at most one "changed" item
	Snapshot string           // web*: selected content, normalized
}
type Source interface {
	Fetch(ctx context.Context, w core.Watch) (Fetched, error)
}
```

- [ ] **Step 2:** 各类信息源:
  - `rss`:`FetchRaw` 取回后用 `encoding/xml` 解析 RSS 2.0 与 Atom;key 依次取 guid/id、link、title+published 的哈希;summary 去 HTML 标签后截 300 字。`rsshub:/route` 展开为 `watch.rsshub_base_url + route`。
  - `github`:`selector` 为 `releases` 或 `issues`;调 `GET /repos/{owner}/{repo}/releases?per_page=20` 与 `GET /repos/{owner}/{repo}/issues?state=open&sort=created&per_page=20`(过滤掉 PR);有 `watch.github_token` 时带 `Authorization`;API base URL 可注入(测试用)。key 为 release id / issue number。
  - `web`:`FetchRaw` 取 HTML;有 selector 时用 cascadia 选中元素,取文本(折叠空白)按顺序拼接为快照;无 selector 时用 readability 正文。快照与上次不同则产出一个条目,key 为新快照的哈希,summary 写"之前:… / 现在:…"(各截 200 字)。首次检查只记快照。
  - `web_rendered`:经注入的 `Renderer.RenderHTML(ctx, url)` 取渲染后的 HTML,其余同 `web`。
- [ ] **Step 3:** `checker.go`:`Checker.Check(ctx, w) (newItems []core.WatchItem, err error)`:抓取 → 新条目写入(首次检查的条目状态记 `baseline`,不触发任何东西)→ 更新快照、`last_checked_at`、`next_check_at = now + interval`;失败时 `failures+1`,`next_check_at` 按 interval × 2^failures 退避(上限 24h),连续 5 次失败状态改为 `failing`,成功后恢复 `active`。检查结果记在 `watches` 行上(`last_checked_at`、`last_error`、`failures`),失败另以 slog 记录;检查不属于任何 run,不写 events 表。
- [ ] **Step 4:** 测试(全部 httptest):RSS 与 Atom 解析、guid 缺失时的 key;GitHub releases/issues、PR 被过滤、token 头;web 选择器快照变化产出条目、未变化不产出;首次检查为 baseline;失败退避与 failing 状态;URL policy 拒绝 loopback 时返回错误。

```bash
go test ./internal/watch ./internal/webaccess -count=1 && make test-architecture
```

---

## Task 3: 配置与装配

**Files:** `internal/config/*`(struct、defaults、validation、tests)、四个配置模板、`internal/wire/{container.go,runtime.go}`、`internal/cli/doctor_output.go`

- [ ] **Step 1:**

```yaml
watch:
  rsshub_base_url: ""        # e.g. http://127.0.0.1:1200; needed for rsshub:/ targets
  github_token: ""           # optional; ${GITHUB_TOKEN} works; raises the API rate limit
  max_checks_per_tick: 5
briefing:
  at: "08:00"                # owner timezone; empty turns the morning briefing off
```

  校验:`rsshub_base_url` 非空时必须是 http(s) URL;`max_checks_per_tick` 1–50;`briefing.at` 为空或 `HH:MM`。`github_token` 走环境变量展开。

- [ ] **Step 2:** wire 构建 `watch.Checker`(FetchService、GitHub client、可选 Renderer)注入工具与调度器;`web_rendered` 的 Renderer 只在配置浏览器时存在。
- [ ] **Step 3:** `acorn doctor` 增加 Watch 一节:追踪项数量(按状态)、RSSHub 地址、早安卡时间。

```bash
go test ./internal/config ./internal/cli ./internal/wire -count=1
```

---

## Task 4: 调度器接入追踪项

**Files:** `internal/wake/{scheduler.go,watches.go}` 和测试

- [ ] **Step 1:** `Config` 增加 `Watches WatchChecker`(接口:`ListDue`、`Claim`、`Check`)与 `WatchStore`、`MaxChecksPerTick`。`Tick` 顺序:衰减 → 到期约定 → 到期追踪项 → 早安卡(Task 5)→ 排队通知。
- [ ] **Step 2:** 每个到期追踪项:`ClaimDueWatch`(lease 10 分钟,防止崩溃后卡死)→ `Check`(单项超时 60s)→ 新条目:
  - `digest`:状态 `pending`,等早安卡。
  - `immediate`:未超每日上限时,经 `StartWakeRun` 在追踪项的线程起 run,输入为 `[watch #id <name>] N new` 加条目列表(每条标题、链接、summary;最多 20 条,其余写"另有 M 条"),条目标 `woken` 并记 run id,记 `wake.fired{watch_id}`;超上限时条目转 `pending` 进简报。
  - 起 run 失败:条目转 `pending`,返回错误。
- [ ] **Step 3:** 测试(假 Checker、假 RunStarter、假时钟):digest 条目只入库不起 run;immediate 起 run 且计入每日上限;超上限转 pending;两个调度器并发只检查一次;单项失败不影响其他追踪项与约定。

```bash
go test ./internal/wake -count=1
```

---

## Task 5: 早安卡

**Files:** `internal/wake/briefing.go` 和测试;`internal/wire/container.go`(`StartWakeRun` 支持"Briefings 线程")

- [ ] **Step 1:** 每次 tick:若 `briefing.at` 已配置、owner 本地时间已过当天 `at`、且当天未简报:
  1. 线程:`LatestBriefingThread` 存在且未删除则复用,否则新建标题为 "Briefings" 的线程。
  2. `ClaimBriefing(day, thread)`;`ErrBriefingTaken` 直接返回(别的进程已做)。
  3. 取全部 `pending` 条目(上限 100,按追踪项分组;另附 `failing` 追踪项名单和最近错误)。
  4. 起 wake run,wake 说明 `morning briefing YYYY-MM-DD`,输入 `[briefing YYYY-MM-DD]` 开头,依次列出各追踪项的新条目、失败的追踪项;条目为空时写明"no watch changes"。
  5. 条目标 `briefed` 并记 run id;`SetBriefingRun`。
  - 早安卡不计入 `wake.daily_limit`,不记 `wake.fired`,记 `briefing.fired`。
  - serve 在 `at` 之后才启动时当天照样补发一次;跨过整天未运行的日子不补。
- [ ] **Step 2:** 测试:到点触发一次、同日不重复;条目分组与截断;无条目也触发;Briefings 线程被删后重建;并发两个调度器只有一个触发。

```bash
go test ./internal/wake ./internal/wire -count=1
```

---

## Task 6: 工具、operating rules、种子技能

**Files:** Create `internal/tools/watch_tools.go` 和测试、`skills/morning_briefing/SKILL.md`;Modify `internal/tools/{builtin_registry.go,configured.go}`、`internal/runtime/runner.go`(`operatingRules`)、CI/release 的 seed skill 检查

- [ ] **Step 1:** 工具(eager、`ToolKindNative`):

| 工具 | 参数 | 行为 |
|---|---|---|
| `watch_create` | `name`、`kind`、`target`,可选 `selector`、`mode`(默认 digest)、`every`(Go duration,默认 1h,最小 15m) | 校验后立即做首次抓取(baseline),失败则不建立并返回原因;成功返回条目数与前 3 条标题或快照摘要。`session_id` 取当前线程。 |
| `watch_update` | `id`,可选 `name`、`selector`、`mode`、`every`、`status`(active/paused) | 修改后若 selector 变化,重新做 baseline。 |
| `watch_list` | 无 | 列出全部追踪项:状态、模式、间隔、上次检查、最近错误、待简报条目数。 |

  `kind` 不可用时(`rsshub:` 未配置、`web_rendered` 未配置浏览器)返回具体原因。

- [ ] **Step 2:** `operatingRules` 增加:owner 想持续关注某个来源时用 `watch_create`,不要用约定反复 `web_fetch`;醒来处理 `[watch …]` 输入时只挑值得说的告诉 owner;`[briefing …]` 输入按 `morning_briefing` 技能处理。
- [ ] **Step 3:** `skills/morning_briefing/SKILL.md`:先读"当下"里今天的约定与手上的事;按追踪项归纳变化(价格变化写前后值);写 `briefings/YYYY-MM-DD.md`(标题"早安 YYYY-MM-DD",tags `briefing`);`notify_owner` 标题"早安",正文三行以内;失败的追踪项单列并建议修复;最后回复笔记路径。
- [ ] **Step 4:** 测试:三个工具的正常与错误路径(kind 不可用、首次抓取失败、every 过小、paused 不被调度)。

```bash
go test ./internal/tools ./internal/runtime ./internal/wire -count=1
```

---

## Task 7: 手机端 wake 卡片区分来源

**Files:** `mobile-kotlin/.../feature/chat/{ChatMessage.kt,ChatScreen.kt}` 和测试

- [ ] `ChatMessage.Wake` 增加 `source`(commitment/watch/briefing),由输入前缀 `[commitment`、`[watch`、`[briefing` 判断;卡片标签与图标分别为"Woken by a commitment"(铃铛)、"New on a watch"(眼睛)、"Morning briefing"(太阳)。
- [ ] 单元测试:三种前缀与未知前缀(按 commitment 显示)。

```bash
cd mobile-kotlin && ./gradlew test assembleDebug
```

---

## Task 8: 验收测试

**Files:** Create `internal/wire/watch_acceptance_e2e_test.go`

- [ ] 沿用 P1/P2 的 harness(fake OpenAI、fake FCM、可控时钟、私网地址的 httptest 页面),`briefing.at: "08:00"`、`owner.timezone: Asia/Shanghai`:
  1. owner 消息"帮我盯一下这个 feed";脚本化模型调 `watch_create{kind: rss, target: <feed>, mode: digest}`,返回 baseline 条目数;再调 `watch_create{kind: web, target: <price page>, selector: ".price", mode: immediate}`。
  2. feed 增加一条,价格页从 ¥2999 变为 ¥2799;时钟推进 1 小时并 `Tick`。
  3. 断言:价格追踪项在建立它的线程起了 wake run,输入含 `[watch #2` 与"之前:¥2999 / 现在:¥2799",`wake.fired` 记 1 条且带 `watch_id`;RSS 新条目为 `pending`,没有起 run。
  4. 时钟推进到次日 08:00 并 `Tick`:Briefings 线程起 run,输入以 `[briefing ` 开头并含 RSS 新条目;脚本化模型依次调 `skill{skill.morning.briefing}`、`knowledge_write{path: briefings/<date>.md}`、`notify_owner`,再回复。
  5. 断言:笔记已提交;fake FCM 收到 1 条,`thread_id` 为 Briefings 线程;条目状态为 `briefed`;再 `Tick` 一次不会重复简报。
- [ ] 另一个用例:GitHub releases(fake API base URL)新 release 进入简报;连续失败的追踪项在简报输入里列为 failing。

```bash
go test ./internal/wire -run 'Watch|Briefing|Capture|Wake|Approval' -count=1 -race
```

---

## Task 9: 文档

- [ ] ADR-0003:P3 行注明 GitHub 只做 releases/issues、`web*` 比较快照;§感知注明 RSSHub 由 owner 自建;早安卡为调度器的固定每日唤醒,不是可被 settle 的约定。
- [ ] `AGENTS.md`:概览、关键包(16 个,加 watch)、表数量与表名、`wake.Scheduler` 职责(约定、追踪项、早安卡)、新增"Watch & 早安卡"硬边界一节、工具清单。
- [ ] INVARIANTS:追踪项首次检查只建基线;条目按 (watch, key) 只入库一次;只有 immediate 追踪项起 run 且计入每日上限,超限转简报;每个本地日只有一次简报;外网抓取都过 URL policy。每条对应 Task 1、2、4、5、8 的测试。
- [ ] onboarding:新增"Watches and the Morning Briefing"一节:RSSHub 自建命令(`docker run -d --name rsshub --restart always -p 127.0.0.1:1200:1200 diygod/rsshub`)、`watch.rsshub_base_url`、`watch.github_token`、`briefing.at`、怎么对 agent 说"帮我盯着某个页面的价格";Current Limits 补充不支持登录态页面。
- [ ] README 功能列表。

---

## Task 10: 全量门禁 + 模拟器验收

- [ ] 全量门禁:

```bash
make format-check && make lint && go test -race ./... && make test-architecture
deadcode ./... && deadcode -test ./...
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug
```

- [ ] 模拟器验收(`acorn-gms`、`/tmp/acorn-verify` 的 serve 配置加 `briefing.at` 为当前时间后两分钟,fake 模型脚本扩展):
  1. 在 App 里对 agent 说"盯一下 <本机私网 RSS 地址>,每天早上告诉我";确认回复里有 baseline 条目数。
  2. 给 feed 加一条;等到 `briefing.at`,Briefings 线程出现,卡片显示"Morning briefing",随后是 agent 回复;知识库页出现 `briefings/<date>.md`。
  3. 一个 immediate 价格追踪项变化后,对应线程出现"New on a watch"卡片。
  4. 推送:本机 DNS 无法连到 FCM,设备收不到时如实记录,服务端以 fake/真实 FCM 返回为准。

---

## Risks

- **抓取被限流或封禁。** GitHub 未带 token 每小时 60 次;公共 RSSHub 实例常被上游风控。最小间隔 15 分钟、失败退避与 failing 状态让问题可见,简报里会列出失败的追踪项。
- **网页结构变化。** CSS 选择器失效时快照变为空或无关内容;空快照按失败处理并进入 failing,不当作"价格变为空"。
- **简报成本。** 每天一次 run 加 immediate 唤醒;token 预算仍要等 P4。条目数在输入里截断,控制单次上下文。
- **tick 变慢。** 抓取是网络 I/O;每个 tick 检查数受 `max_checks_per_tick` 与单项 60s 超时约束,必要时按 Drift Rule 拆出独立循环。

## Retirement

- 无代码删除;`operatingRules` 中"用约定定期查看某个网页"的做法由 watch 工具取代。
