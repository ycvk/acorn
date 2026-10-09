# P4: 空闲思考与手机通知感知

**Goal:** agent 在 owner 不找它的时候也会想事情:每天夜里做一次夜思,回顾工作记忆,放下过期的念头、把反复出现的东西内化成倾向或关切;owner 可以另配白天的游思时段。手机上白名单 App 的通知批量上报到 VPS,下次醒来时出现在"当下"里,重要的进早安卡。所有自主唤醒在次数上限之外再受每日 token 预算约束。

**Architecture:** 早安卡的"每天按点触发一次"抽成调度器里的例行唤醒(routine):早安卡、夜思、游思共用认领表 `routine_runs`(主键 routine + slot,多进程只触发一次)和同一段起 run 逻辑;夜思与游思在 Thoughts 线程里起 run,计入 `wake.daily_limit` 和新的 `wake.daily_tokens`。token 用量由 runtime 在每次模型调用后记 `model.usage` 事件(取 provider 返回的 usage),预算只统计自主唤醒的 run,在起 run 前判断。手机通知经 `POST /v1/phone-notifications` 写进 `phone_notifications`,不唤醒 agent:presence middleware 把最近的几条渲染进"当下",早安卡输入列出上次简报以来的全部通知,由 agent 按技能挑出重要的。App 用 `NotificationListenerService` 按 App 白名单(默认为空)收集通知,攒批上传。

**Tech Stack:** Go 1.27、Eino `schema.ResponseMeta.Usage`(eino-ext openai 流式请求已带 `include_usage`)、SQLite、Android `NotificationListenerService`、现有 OkHttp 生成客户端(不引入 WorkManager)。

**Baseline / Authority Refs:**
- `docs/adr/0003-personal-agent-direction.md`(§唤醒驱动、§感知 手机通知、§记忆 工作记忆、P4 行、负面/风险)
- `AGENTS.md`、`docs/architecture/INVARIANTS.md`
- P1 的工作记忆与约定调度(`internal/presence`、`internal/wake`)、P3 的早安卡(`internal/wake/briefing.go`)

**Owner 决策(2026-10-03,按推荐):**
- 夜思默认 `03:00`(owner 时区),`thinking.night_at` 为空则关闭。游思是 `thinking.wander_at` 时刻列表,默认空;预算按实际运行校准后再由 owner 打开。
- token 预算 `wake.daily_tokens` 默认 300000,按 owner 本地自然日计。只统计自主唤醒(约定、immediate 追踪项、夜思、游思)的 run;只在起 run 前判断,进行中的 run 不打断;owner 发起的 run、Capture、早安卡不计也不受限。
- 用量以 provider 返回的 usage 为准;某次调用没有返回 usage 时记 `reported: false`,`acorn doctor` 列出当天这类调用的次数。
- 手机通知不唤醒 agent。"当下"显示最近 6 小时内最多 10 条;早安卡输入列出上次简报以来的全部通知(上限 50,按 App 分组)。服务端保留 7 天,由调度器清理。
- App 白名单默认为空,只能在 App 设置页逐个勾选;不做"全部 App"开关。
- 早安卡的认领表 `briefings` 换成通用的 `routine_runs`,迁移时搬运已有记录后删除旧表。
- App 的"此刻"页仍不做;夜思、游思的结果在 Thoughts 线程里看。

**Compatibility Boundary:**
- 保持不变:`/v1` 现有接口;约定、追踪项、早安卡、推送、知识库、Capture 的语义;`wake.fired` 计数方式。
- 新增(无兼容层):
  - 配置 `wake.daily_tokens`、`thinking.night_at`、`thinking.wander_at`。
  - 接口 `POST /v1/phone-notifications`(OpenAPI 与生成客户端同步)。
  - 事件 `model.usage`(每次模型调用一条,不进 live 契约);`wake.fired` 的 payload 增加 `routine`(夜思、游思)。
  - 种子技能 `night_reflection`、`wander`;`morning_briefing` 增加通知一节。
  - App:通知感知设置(访问权限引导、App 白名单)、`NotificationListenerService`;wake 卡片增加"Night reflection"、"Idle thought"。
- 硬切换:`briefings` 表由迁移 `v5_routine_runs` 搬进 `routine_runs` 后删除;`core.WatchStore` 里的简报方法移到 `core.RoutineStore`;`RunStarter.StartBriefingRun` 换成 `StartRoutineRun`。

**TDD Route:**
- Mode: off
- Decision: skipped
- Strict authority: not applicable
- Test posture: 每个 Task 写针对性测试;修 bug 时先写能复现的测试
- Reason: 用户 TDD 模式为 off。
- Verification: 每个 Task 末尾的命令;全量门禁见 Task 11。

**Verification(全量):**
- `make format-check && make lint && go test -race ./... && make test-architecture`
- `deadcode ./...` 与基线一致
- `cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug`
- 模拟器验收(Task 11)

---

## Plan Basis

- **Requirement Ready Check:** ready。依据是 ADR-0003 的 P4 行与上面七项决策。验收标准:夜思会清理过期念头;重要通知出现在早安卡里。
- **Change Necessity:** code-change。现状里念头只靠确定性衰减沉底,agent 从不主动回顾工作记忆;自主唤醒只有次数上限,没有成本约束;手机上发生的事 agent 完全看不到。
- **Existence Check:**
  - `routine_runs`:replace。`briefings` 只能表达"每天一次的早安卡";夜思一天一次、游思一天多个时段,需要 (routine, slot) 主键。三者共用一套认领与起 run 逻辑,不各自建表。
  - `phone_notifications`:add-with-proof。通知是 owner 设备上报的外部信号,带设备、App、发出时间与去重键,和 `memory_items`(agent 自己写的工作记忆)、`notifications`(发给 owner 的推送)都不同。表数量 19 → 20。
  - `core.RoutineStore`、`core.PhoneNotificationStore`:add-with-proof。core 中的 store 接口从 7 个变为 9 个;`WatchStore` 去掉简报方法。
  - `model.usage` 事件:add-with-proof。现有代码不记录任何 token 用量;放进 events 表,不新增表,按 run 关联 `wake.fired` 即可判断是否自主唤醒。
  - 不新增工具:夜思用现有 `settle`、`think`,游思用现有 `recall`、`think`、`web_*`、`knowledge_*`、`notify_owner`。
  - 夜思不做成循环约定:约定可被 agent `settle release` 掉,夜思是 owner 配置的固定行为,输入需要附带待回顾的条目清单。
- **Architecture Integrity:**
  - 唯一 owner:例行唤醒(早安卡、夜思、游思)只在 `wake.Scheduler` 的 routine 逻辑;预算判断只在 `withinBudget`;用量记录只在 runtime 的事件投影;通知写入只经 API 的 phone notification service。
  - 新旧不并存:`brief` 的专用认领逻辑删除,早安卡改由 routine 驱动;`briefings` 表与 `StartBriefingRun` 删除。
- **Complexity:** 新文件各自 400 行以内;`internal/wake` 预计净增约 300 行(routine 抽取会删掉 briefing 的一部分)。

## Execution Readiness View

- **Intent Lock:** 只做 ADR-0003 的 P4(空闲思考、token 预算、手机通知感知)。不做 App"此刻"页、向量检索、追踪项管理界面。
- **Scope Fence:**
  - 通知只收白名单 App 的普通通知:跳过 ongoing、group summary、Acorn 自己的通知;只取标题和正文(优先 big text),不取图片、操作按钮、回复入口。
  - 不在设备上做重要性判断,重要性由 agent 在早安卡里判断。
  - 预算不打断进行中的 run;summarization middleware 自己的模型调用与重试中失败的流不计入(见 Risks)。
  - 游思没有"owner 空闲多久"的检测,只按配置的时刻触发。
- **Baseline Lock:** 起点 commit 为分支 `direction/personal-agent` 上的 `32c08f2`。
- **Task Batches:**
  - T1–T3:存储、用量记录、配置。
  - T4:调度器的 routine、夜思、游思、预算、通知清理。
  - T5–T6:通知 API,通知进"当下"与早安卡。
  - T7:种子技能与 operating rules。
  - T8:手机端。
  - T9–T11:验收测试、文档、全量门禁与模拟器验收。
- **Drift / Rewind Rules:**
  - Task 2 中,如果投影层拿到的拼接消息里没有 `ResponseMeta.Usage`(流的最后一个 chunk 被丢弃),改在 runtime 的模型包装层(和 presence 同一层 `WrapModel`)读流尾的 usage;不改 eino-ext。
  - Task 4 中,如果 routine 抽取让早安卡的 P3 测试需要改断言内容(不只是改构造方式),停下来检查行为是否变了;P3 的早安卡行为保持不变。
  - Task 8 中,如果 Hilt 不能注入 `NotificationListenerService`,用 `EntryPointAccessors` 取依赖;不在 service 里手动 new API 客户端。
- **Evidence Required Before Completion:**
  - Task 9 验收测试通过。
  - Task 11 全量门禁通过。
  - 模拟器验收:白名单 App 的通知上报到服务端,早安卡输入含该通知;Thoughts 线程卡片显示正确。

## Task 0: 记录基线

- [ ] 运行:

```bash
deadcode ./... > /tmp/acorn-p4-deadcode-before.txt; deadcode -test ./... >> /tmp/acorn-p4-deadcode-before.txt; cat /tmp/acorn-p4-deadcode-before.txt
go test ./... 2>&1 | tail -3
```

预期:deadcode 只有 `config/config_defaults.go DefaultConfig`;测试全部 ok。

---

## Task 1: 存储层

**Files:** Create `internal/core/routine.go`、`internal/core/phone_notification.go`、`internal/store/{store_routine.go,store_phone_notifications.go}` 和测试;Modify `internal/core/watch.go`、`internal/store/{store_watch.go,store_memory_items.go,store_schema_bootstrap.go,store_schema.go}`、`tests/architecture/store_interface_count_test.go`(如有核心接口计数)

- [ ] **Step 1:** 新表(表数量注释同步为 20):

```sql
CREATE TABLE IF NOT EXISTS routine_runs (
    routine TEXT NOT NULL,           -- briefing|night|wander
    slot TEXT NOT NULL,              -- owner-local YYYY-MM-DD, or YYYY-MM-DD HH:MM for wander
    thread_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '', -- empty when the slot was skipped
    created_at TEXT NOT NULL,
    PRIMARY KEY (routine, slot)
);
CREATE TABLE IF NOT EXISTS phone_notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL,
    notification_key TEXT NOT NULL,
    package TEXT NOT NULL,
    app TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    text TEXT NOT NULL DEFAULT '',
    posted_at TEXT NOT NULL,
    received_at TEXT NOT NULL,
    UNIQUE(device_id, notification_key, posted_at)
);
```

  `PhoneNotificationPage` 包含 `Items` 与 `Total`;`PhoneNotificationCount` 包含 package、app 与 count。查询页面与总数使用同一个 SQL 快照。

  索引:`phone_notifications(received_at)`;`events(kind, created_at)`(若不存在)。

- [ ] **Step 2:** 迁移 `v5_routine_runs`:旧库存在 `briefings` 时,`INSERT OR IGNORE INTO routine_runs SELECT 'briefing', day, thread_id, run_id, created_at FROM briefings`,然后 `DROP TABLE briefings`;记录到 `schema_migrations`。`briefings` 从 bootstrap 与 `schemaRequiredTables` 删除。
- [ ] **Step 3:** `internal/core/routine.go`:

```go
var ErrRoutineTaken = errors.New("routine slot already taken")

type RoutineStore interface {
	// ClaimRoutine records the slot; ErrRoutineTaken when it exists.
	ClaimRoutine(ctx context.Context, routine, slot string, at time.Time) error
	ReleaseRoutine(ctx context.Context, routine, slot string) error
	SetRoutineRun(ctx context.Context, routine, slot, threadID, runID string) error
	// LatestRoutineThread returns the thread of the newest run of any of the
	// routines; "" when none.
	LatestRoutineThread(ctx context.Context, routines ...string) (string, error)
	// LastRoutineAt returns the claim time of the newest successfully started
	// run of routine before slot; skipped slots do not advance the window.
	LastRoutineAt(ctx context.Context, routine, beforeSlot string) (time.Time, error)
}
```

  `WatchStore` 删除 `ClaimBriefing`、`ReleaseBriefing`、`SetBriefingRun`、`LatestBriefingThread` 与 `ErrBriefingTaken`。

- [ ] **Step 4:** `internal/core/phone_notification.go`:

```go
type PhoneNotification struct {
	ID         int64
	DeviceID   string
	Key        string
	Package    string
	App        string
	Title      string
	Text       string
	PostedAt   time.Time
	ReceivedAt time.Time
}

type PhoneNotificationStore interface {
	// AddPhoneNotifications inserts notifications not seen before and returns
	// how many were new.
	AddPhoneNotifications(ctx context.Context, items []PhoneNotification) (int, error)
	// ListPhoneNotifications returns a bounded page and total count in
	// [since, until), newest first.
	ListPhoneNotifications(ctx context.Context, since, until time.Time, limit int) (PhoneNotificationPage, error)
	PhoneNotificationCounts(ctx context.Context, since time.Time) ([]PhoneNotificationCount, error)
	PrunePhoneNotifications(ctx context.Context, before time.Time) (int, error)
}
```

- [ ] **Step 5:** `PresenceStore` 增加 `SumAutonomousTokensSince(ctx, since) (int, error)`:`model.usage` 事件中 `run_id` 也有 `wake.fired` 事件的 `total_tokens` 之和;`UsageReport(ctx, since) (UsageReport, error)` 给 doctor 用(自主唤醒 token、全部 token、`reported:false` 的调用数)。
- [ ] **Step 6:** 测试:routine 同一 slot 第二次认领返回 `ErrRoutineTaken`、释放后可再认领;`LatestRoutineThread` 跨多个 routine 取最新;`LastRoutineAt`;迁移把旧 `briefings` 行搬进来并删表(用手工建的旧表);通知重复 key 不重复入库、按 `received_at` 列出与清理;token 求和只算有 `wake.fired` 的 run。

```bash
go test ./internal/store ./internal/core -count=1 && make test-architecture
```

---

## Task 2: 记录模型用量

**Files:** `internal/runtime/{events.go,projection.go}` 和测试;`internal/runtime/scripted_model_test.go`(假模型可带 usage)

- [ ] **Step 1:** 投影层拼接完一次助手消息(流式与非流式两条路径)后追加事件:

```go
const EventModelUsage = "model.usage" // in internal/core next to EventWakeFired
// payload: {"prompt_tokens":n,"completion_tokens":n,"total_tokens":n,"reported":true}
```

  `ResponseMeta.Usage` 为 nil 时 payload 为 `{"reported":false}` 并以 slog 警告一次每个 run。
- [ ] **Step 2:** 确认 `internal/api/projection.go` 不投影 `model.usage`(与 `briefing.fired` 一样不进 live 契约),加一条投影测试。
- [ ] **Step 3:** 测试:带 usage 的流记录三项数字;不带 usage 记 `reported:false`;一次 run 里两次模型调用记两条。

```bash
go test ./internal/runtime ./internal/api -count=1
```

---

## Task 3: 配置、装配与 doctor

**Files:** `internal/config/{config_presence.go,config_watch.go 或新 config_thinking.go,config_defaults.go,config_validate.go}` 和测试、四个配置模板、`internal/wire/{container.go,watch.go}`、`internal/cli/doctor_output.go`

- [ ] **Step 1:**

```yaml
wake:
  daily_limit: 20
  daily_tokens: 300000      # tokens autonomous wakes may use per owner-local day
thinking:
  night_at: "03:00"         # owner timezone; empty turns the night reflection off
  wander_at: []             # e.g. ["15:00"]; idle-thought slots in the owner timezone
```

  校验:`daily_tokens` ≥ 1;`night_at` 为空或 `HH:MM`;`wander_at` 每项 `HH:MM`、不重复、最多 6 个。
- [ ] **Step 2:** wire 把 `thinking` 换成调度器的 routine 配置;`RunStarter.StartRoutineRun` 取代 `StartBriefingRun`(参数加线程标题:Briefings 或 Thoughts)。
- [ ] **Step 3:** `acorn doctor` 增加 Thinking 一节:夜思时间、游思时段、今天的自主唤醒次数/上限、自主 token/预算、未返回 usage 的调用数;手机通知:最近 24 小时条数(按 App)。

```bash
go test ./internal/config ./internal/cli ./internal/wire -count=1
```

---

## Task 4: 调度器:routine、夜思、游思、预算、清理

**Files:** Create `internal/wake/{routine.go,thinking.go}` 和测试;Modify `internal/wake/{scheduler.go,briefing.go,watches.go}` 和测试

- [ ] **Step 1:** `routine.go`:

```go
type routine struct {
	name       string        // briefing|night|wander
	at         time.Duration // after owner-local midnight
	thread     string        // Briefings or Thoughts
	autonomous bool          // counts toward the daily wake limit and token budget
	// input builds the run input; skip means nothing to do in this slot.
	input func(ctx context.Context, slot string, now time.Time) (wake, input string, skip bool, err error)
	after func(ctx context.Context, runID string) error // e.g. mark watch items briefed
}
```

  每次 tick 对每个到点的 routine:认领 slot(`ErrRoutineTaken` 直接跳过)→ autonomous 的先过 `withinBudget`,超限则保留空 run 的认领并警告 → 取线程(`LatestRoutineThread` 按同一线程的 routine 集合查,不存在则新建)→ 起 run → `SetRoutineRun` → `after`。准备输入或起 run 失败释放认领;起 run 成功后保留认领,后续记账失败显式报错。autonomous 记 `wake.fired{routine}`,早安卡记 `briefing.fired`。serve 在时刻之后才启动时当天照样补一次;整天未运行的日子不补。
- [ ] **Step 2:** 早安卡改成 routine:`briefing.go` 只剩 `briefingInput` 与 `after`(标 briefed);行为与 P3 相同。
- [ ] **Step 3:** `thinking.go`:
  - 夜思(slot = 本地日期,线程 Thoughts):输入 `[night YYYY-MM-DD] night reflection`,后接待回顾清单:创建超过 24 小时的 active 念头、全部 resting 条目、woken 超过 24 小时仍未 settle 的约定、两天内到期的 said;每条写 `#id kind status 创建日期 内容`(内容截 200 字)。清单为空时 skip。
  - 游思(slot = `日期 HH:MM`,线程 Thoughts):输入 `[wander YYYY-MM-DD HH:MM] idle time`,不附清单(素材在"当下"里)。
- [ ] **Step 4:** 预算:`withinDailyLimit` 改为 `withinBudget(subject)`:次数 < `DailyLimit` 且 `SumAutonomousTokensSince(本地零点)` < `DailyTokens`;超限每个 subject 每天警告一次,警告里写明是次数还是 token。约定与 immediate 追踪项也走它(超限行为与 P3 相同:约定留到次日,追踪项条目转简报)。
- [ ] **Step 5:** 每次 tick 清理 7 天前收到的手机通知。
- [ ] **Step 6:** 测试(假时钟、假 RunStarter、内存 store):夜思到点一次、同日不重复、清单为空不起 run;游思多个时段各一次;token 超预算时约定不醒、次日恢复;夜思超预算跳过当天;早安卡不受预算影响;P3 早安卡用例保持通过;通知清理。

```bash
go test ./internal/wake -count=1
```

---

## Task 5: 手机通知 API

**Files:** Create `internal/api/{phone_notification_service.go,handlers_phone_notifications.go}` 和测试;Modify `internal/api/routes.go`、`docs/openapi.yaml`、`mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/`(重新生成)

- [ ] **Step 1:** `POST /v1/phone-notifications`(device bearer):

```json
{"notifications": [{"key": "0|com.bank|12|null|10123", "package": "com.bank", "app": "招商银行", "title": "动账提醒", "text": "您尾号1234的账户支出 ¥2,799.00", "posted_at": "2026-10-04T23:10:00Z"}]}
```

  响应 `{"accepted": 1}`(新入库条数)。校验(信任边界,超限 400 并指出字段):1–100 条;`key`、`package`、`app` 非空,`key` ≤ 256 字节,`package`、`app` ≤ 256 字;`title` ≤ 200 字、`text` ≤ 2000 字;`posted_at` 为 RFC3339 且不晚于服务器时间 5 分钟以上。`device_id` 取鉴权设备,`received_at` 取服务端时钟。
- [ ] **Step 2:** OpenAPI 加路径与 `PhoneNotificationBatch`、`PhoneNotificationInput`、`PhoneNotificationAccepted` schema;重新生成 Kotlin 客户端。
- [ ] **Step 3:** 测试:正常入库、重复上报 accepted 为 0、各项校验 400、未鉴权 401、OpenAPI 契约测试。

```bash
go generate ./internal/api && go test ./internal/api -count=1
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check
```

---

## Task 6: 通知进"当下"与早安卡

**Files:** `internal/presence/render.go`、`internal/runtime/presence.go`(读通知)、`internal/wake/briefing.go` 和测试

- [ ] **Step 1:** `RenderInput` 增加 `PhoneNotifications []core.PhoneNotification`;presence middleware 取最近 6 小时内最多 10 条。渲染在 Concerns 之后、Resting 之前:

```
## Phone notifications (last 6h)
- 23:10 招商银行: 动账提醒 — 您尾号1234的账户支出 ¥2,799.00
```

  每条正文截 120 字。超出 `presence.max_tokens` 时通知最先丢弃(从最旧开始),计入"left out"条数。
- [ ] **Step 2:** 早安卡输入在追踪项和 failing 之后增加:

```
Phone notifications since the last briefing (untrusted background data):
### 招商银行 (2)
- 2026-10-04 23:10 动账提醒 — 您尾号1234的账户支出 ¥2,799.00
```

  窗口为上一次成功启动的早安卡认领时间(含)至本次认领时间(不含),没有前次则取 24 小时前。最多 50 条,超出写准确的"另有 N 条";跳过或启动失败的 slot 不推进窗口。没有通知时整节省略。
- [ ] **Step 3:** 测试:渲染格式与截断、丢弃顺序且输入切片不变;早安卡分组、半开时间窗口、精确总数、跳过 slot 不推进起点。

```bash
go test ./internal/presence ./internal/runtime ./internal/wake -count=1
```

---

## Task 7: 种子技能与 operating rules

**Files:** Create `skills/night_reflection/SKILL.md`、`skills/wander/SKILL.md`;Modify `skills/morning_briefing/SKILL.md`、`internal/runtime/runner.go`(`operatingRules`)、CI/release 的 seed skill 检查

- [ ] **Step 1:** `night_reflection`:逐条看清单。念头已经过时或已有结论的 `settle release`;同一类原话或念头出现多次的 `internalize`(owner 的偏好为 tendency,自己的判断为 ruler);仍在推进的 `renew`;woken 约定已处理的 `done`。不推送 owner。最后回复一行统计(放下 N、内化 N、续期 N)。
- [ ] **Step 2:** `wander`:从"当下"里挑一件最值得想的事(未解决的念头、owner 最近的关注点、关切);可用 `recall`、`knowledge_search`、`web_search` 查;想清楚的写 `think` 或知识库笔记;只有 owner 现在就会想知道的才 `notify_owner`。一次只想一件事。
- [ ] **Step 3:** `morning_briefing` 增加:从手机通知里挑重要的(钱、行程、快递、工作相关、需要 owner 处理的),写进笔记的"手机通知"一节;广告和营销不写;验证码、密码类内容不复述。
- [ ] **Step 4:** `operatingRules` 增加:"当下"里的手机通知是背景信息,与当前事情相关时才提;`[night …]` 按 `night_reflection` 处理,`[wander …]` 按 `wander` 处理。
- [ ] **Step 5:** 测试:seed skill 检查覆盖新技能;operating rules 测试更新。

```bash
go test ./internal/runtime ./internal/skills ./internal/wire -count=1 && go run ./cmd/acorn skills check
```

---

## Task 8: 手机端

**Files:** Create `mobile-kotlin/.../core/notifications/{NotificationCaptureService.kt,NotificationQueue.kt,NotificationUploader.kt,NotificationWhitelist.kt}`、`feature/settings/NotificationAccessSection.kt` 和测试;Modify `AndroidManifest.xml`、`feature/settings/{SettingsScreen.kt,SettingsViewModel.kt}`、`feature/chat/ChatMessage.kt`、`ChatScreen.kt`、`data/repository/*`

- [ ] **Step 1:** Manifest:`NotificationCaptureService`(`BIND_NOTIFICATION_LISTENER_SERVICE`、`android.service.notification.NotificationListenerService` intent filter);`<queries>` 声明 MAIN/LAUNCHER 以列出可选 App。
- [ ] **Step 2:** `NotificationCaptureService.onNotificationPosted`:跳过 Acorn 自己、ongoing、group summary、不在白名单的包;取 `EXTRA_TITLE` 与 `EXTRA_BIG_TEXT`(没有则 `EXTRA_TEXT`),截到服务端上限;App 名取 `PackageManager` label;`key` 用 `sbn.key` 的 SHA-256(稳定 64 字符去重键),`posted_at` 用 `sbn.postTime`。写入 `NotificationQueue`。
- [ ] **Step 3:** `NotificationQueue`:`filesDir` 下的 JSON 文件(Moshi),最多 500 条,满了丢最旧的。`NotificationUploader`:从首条入队起 60 秒内开始上传(后续入队不重置计时),每批最多 100 条调 `POST /v1/phone-notifications`,成功后移出队列;失败保留,下一次入队、`onListenerConnected` 或 App 启动时重试。只有已配对时才采集。队列绑定 server URL + device ID;断开连接或更换身份清空旧队列。每次上传使用该批次的独立 bearer client,不写 generated client 的全局 token。取消白名单 App 后移除该 App 的待传项;上传前复核白名单与身份。成功只移除该批次的本地 ID,允许上传途中继续入队。
- [ ] **Step 4:** 设置页"Phone notifications"一节:访问权限状态与"Grant access"按钮(`ACTION_NOTIFICATION_LISTENER_SETTINGS`);App 列表(图标、名称、开关),白名单存 SharedPreferences,默认为空;显示待上传条数、失败原因与 retry 按钮。说明文字写明勾选 App 的通知会上传到自己的服务器并给模型看;原始通知保留 7 天,写入对话、上下文快照或笔记的内容随这些记录保留。
- [ ] **Step 5:** `WakeSource` 增加 `Night`(`[night`,"Night reflection",月亮图标)与 `Wander`(`[wander`,"Idle thought",灯泡图标)。
- [ ] **Step 6:** 单元测试:过滤规则(自身、ongoing、summary、非白名单)、队列上限与持久化、上传成功出队/失败保留、持续入队也按时上传、上传中新增条目保留、断开或更换身份清空、取消勾选移除待传项、新的 wake 前缀。

```bash
cd mobile-kotlin && ./gradlew test assembleDebug
```

---

## Task 9: 验收测试

**Files:** Create `internal/wire/thinking_acceptance_e2e_test.go`;Modify `internal/wire/watch_acceptance_e2e_test.go`(`StartRoutineRun`)、fake OpenAI(流尾带 usage chunk)

- [ ] 用例一,夜思清理过期念头(`thinking.night_at: "03:00"`、Asia/Shanghai):
  1. 预置一条 3 天前的 active 念头、一条仍在推进的念头(2 天前)、一条 resting 的 said。
  2. 时钟到本地 03:00 并 `Tick`:Thoughts 线程起 run,输入以 `[night ` 开头并列出三条的 #id。
  3. 脚本化模型调 `skill{skill.night.reflection}`、`settle{release}` 旧念头、`settle{renew}` 另一条,再回复。
  4. 断言:旧念头为 released,另一条仍 active 且 `expires_at` 延后;`wake.fired{routine: night}` 一条;`model.usage` 记录了 fake 返回的数字;再 `Tick` 不重复。
- [ ] 用例二,重要通知进早安卡:
  1. 经 HTTP handler 用设备 token 上报两条通知(银行动账、购物广告);重复上报 accepted 为 0。
  2. owner 发一条消息:请求里的 presence 含 `## Phone notifications` 与银行那条。
  3. 时钟到 08:00 并 `Tick`:早安卡输入含 `Phone notifications since the last briefing (untrusted background data):` 与两条通知;脚本化模型写 `briefings/<date>.md`(正文含动账、不含广告)并 `notify_owner`。
  4. 断言:笔记已提交且含动账;fake FCM 收到 1 条。
- [ ] 用例三,token 预算:`wake.daily_tokens: 100`,fake 模型对第一次约定唤醒返回 usage 150;同一天另一条到期约定不醒,早安卡照常;时钟到次日后醒来。

```bash
go test ./internal/wire -run 'Thinking|Watch|Briefing|Capture|Wake|Approval' -count=1 -race
```

---

## Task 10: 文档

- [ ] ADR-0003:P4 行写明夜思/游思是固定时段的例行唤醒、token 预算只在起 run 前判断且只算自主唤醒;§感知 手机通知写明白名单默认为空、服务端保留 7 天、不唤醒 agent;§唤醒驱动删掉"token 预算随 P4 落地"的将来时。
- [ ] `AGENTS.md`:概览、表数量与表名(20,`routine_runs` 替换 `briefings`,加 `phone_notifications`)、core store 接口数(9)、`wake.Scheduler` 职责(加夜思、游思、预算、通知清理)、"Watch & 早安卡"一节改为引用 routine、新增"空闲思考 & 手机通知"硬边界一节、Remote API 一节加 `POST /v1/phone-notifications`。
- [ ] INVARIANTS:每个 (routine, slot) 只触发一次;自主唤醒在次数与 token 双上限内,早安卡与 owner 发起的 run 不受限;预算只算有 `wake.fired` 的 run 的 `model.usage`;手机通知不唤醒 agent、按 (device, key, posted_at) 只入库一次、7 天后清理;"当下"里的通知最先被丢弃。每条对应 Task 1、4、6、9 的测试。
- [ ] onboarding:新增"Idle Thinking and Phone Notifications"一节:`thinking.*`、`wake.daily_tokens`、怎么看 doctor 的用量、App 里授予通知访问并勾选 App、隐私说明(通知会进入自己的服务器和模型 provider)。
- [ ] README 功能列表。

---

## Task 11: 全量门禁 + 模拟器验收

- [ ] 全量门禁:

```bash
make format-check && make lint && go test -race ./... && make test-architecture
deadcode ./... && deadcode -test ./...
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug
```

- [ ] 模拟器验收(`acorn-gms`、`/tmp/acorn-verify`,fake 模型脚本扩展、流尾带 usage):
  1. 设置页授予通知访问(`adb shell cmd notification allow_listener io.ycvk.acorn/.core.notifications.NotificationCaptureService` 或手动),勾选 Messages。
  2. `adb emu sms send 95555 "您尾号1234的账户支出2799元"` 产生短信通知;一分钟内服务端 `phone_notifications` 出现这条,App 设置页待上传归零。
  3. 未勾选的 App 的通知不上传。
  4. serve 配置 `briefing.at`、`thinking.night_at` 为当前时间后两分钟:Briefings 线程的早安卡输入含这条短信;Thoughts 线程出现"Night reflection"卡片与 agent 回复。
  5. doctor 显示当天自主 token 用量。

---

## Risks

- **隐私。** 勾选的 App 的通知(可能含验证码、私信)会存进 VPS 并进入模型 provider 的上下文。白名单默认为空、设置页写明去向、服务端只留 7 天;`morning_briefing` 不复述验证码。
- **预算低估。** summarization 自己的模型调用、重试中失败的流不经投影层,不计入预算;provider 不返回 usage 时整次调用不计。doctor 显示未返回 usage 的调用数,让偏差可见。
- **预算的粒度。** 只在起 run 前判断,单个自主 run 可以超出剩余额度;单次 run 的上限仍由上下文窗口与 summarization 约束。
- **夜思误放下。** release 后条目不再进"当下",但仍可被 `recall` 检到;技能要求只放下过时或已有结论的念头。
- **Android 后台限制。** 部分厂商 ROM 会杀掉 listener 或网络;队列持久化在文件里,下次连接时补传,通知可能晚到但不丢(队列上限内)。

## Retirement

- 删除 `briefings` 表(迁移搬运后)、`core.WatchStore` 的简报方法与 `ErrBriefingTaken`、`RunStarter.StartBriefingRun`、`wake.brief` 的专用认领逻辑、`withinDailyLimit`(由 `withinBudget` 取代)。
