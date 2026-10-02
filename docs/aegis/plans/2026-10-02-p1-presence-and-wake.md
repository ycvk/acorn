# P1: 工作记忆、约定唤醒、人格与推送

**Goal:** 对 agent 说"三天后提醒我看 X",它记下这个约定;到点后服务端自己醒来、在原对话里处理,并通过 FCM 推送到手机。中间服务重启不影响约定。旧记忆体系(facts、history、Active Memory、Periodic Review、WorldState、技能自动生成)整体删除。

**Architecture:** 工作记忆、约定、context 快照存在 SQLite(`memory_items`、`context_snapshots`),由 `internal/presence` 提供衰减与"当下"渲染这类纯逻辑。每次调用模型前,`presence` middleware 用 `WrapModel` 把渲染好的"当下"作为一条临时 system 消息附在输入末尾,不写回 agent 状态。快照按哈希去重落库,并记一条 `presence.snapshot` 事件。人格来自 `{storage_dir}/persona.md`,写进 agent Instruction。`internal/wake` 调度器每 30 秒做三件事:执行衰减、触发到期约定(在约定所属线程里起一个 run)、发送免打扰时段攒下的通知。`internal/notify` 负责 FCM HTTP v1 发送;`notify_owner` 工具带每小时上限和免打扰排队。经历检索用 FTS5(trigram)覆盖 runs 和 memory_items,由 `recall` 工具提供。

**Tech Stack:** Go 1.27、cloudwego/eino v0.9.21(`adk.ChatModelAgentMiddleware.WrapModel`、`ModelRetryConfig`、`WillRetryError`)、modernc.org/sqlite(FTS5 trigram,已验证可用)、`golang.org/x/oauth2/jwt`(已在 go.mod)、`time/tzdata`、Firebase Messaging(Android,BOM 方式引入)。

**Baseline / Authority Refs:**
- `docs/adr/0003-personal-agent-direction.md`(§唤醒驱动、§记忆、§输出与审批、§Runtime、P1 行)
- `AGENTS.md`(硬边界、验证要求)、`docs/architecture/INVARIANTS.md`
- `docs/openapi.yaml`(`/v1/memory/*`、`/v1/devices`、RunEvent 契约)
- P0 计划 `docs/aegis/plans/2026-10-02-p0-eino-runtime.md` 的遗留项(主模型重试、工具失败被记成成功)

**Owner 决策(2026-10-02):**
- FCM 推送提前到 P1(ADR 原排在 P3);P3 只剩 Watch 和早安卡。
- 旧记忆在 P1 一次性全部删除,不做迁移;P1 到 P2 之间,长期偏好只存在工作记忆的 `tendency` / `ruler` 里。
- 技能只删自动生成部分(`skill_create` 和 generated 目录);加载方式和资格判断不动,迁到 Eino skill middleware 的工作留到 P2。

**Compatibility Boundary:**
- 保持不变:`/v1` 的线程、run、事件、待办、inbox、skills 接口;审批与 `ask_operator` 的语义;live RunEvent 的现有 kind 和 payload 形状。
- 有意破坏(hard cut,无兼容层):
  - 删除配置 `memory.*`、`triggers.*`、`agent.system_prompt`。
  - 新增配置 `owner.timezone`、`presence.max_tokens`、`wake.daily_limit`、`notify.*`。
  - 人格改为必需文件 `{storage_dir}/persona.md`,缺失时 serve 启动失败并提示路径。
  - 删除 `/v1/memory/*`、CLI `acorn memory`、`acorn skills create|patch|delete`。
  - 删除工具 `remember`、`memory_*`、`worldstate_*`、`search_runs`、`skill_create`,以及四个文件工具。
  - 新增工具 `keep`、`think`、`schedule_wake`、`settle`、`recall`、`notify_owner`。
  - 新增 `PUT /v1/devices/self/push-token`。
  - `{storage_dir}` 下的 `facts/`、`history/`、`worldstate/`、`vectors.db`、`skills/generated/` 不再读取,也不自动删除。
- 数据库:新表一律 `CREATE TABLE IF NOT EXISTS`;不改既有表的列,也不需要迁移。

**TDD Route:**
- Mode: off
- Decision: skipped
- Strict authority: not applicable
- Test posture: 每个 Task 写针对性测试;修 bug 时先写能复现的测试(沿用 P0 的做法)
- Reason: 用户 TDD 模式为 off。
- Verification: 每个 Task 末尾的命令;全量门禁见 Task 15。

**Verification(全量):**
- `make format-check && make lint && go test -race ./... && make test-architecture`
- `deadcode ./...` 与基线一致
- `cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug`
- 模拟器验收(Task 15)

---

## Plan Basis

- **Requirement Ready Check:** ready。依据是 ADR-0003 的 P1 行加上 owner 的三项决策。验收标准:对 agent 说"三天后提醒我看 X",到点自动醒来处理并推送。
- **Change Necessity:** code-change。现状中没有约定、没有唤醒调度(cron 触发器只能由配置定义,agent 不能自己排),也没有推送通道;只能新增代码实现。旧记忆体系由 owner 决定删除。
- **Existence Check:**
  - `memory_items`:add-with-proof。工作记忆五类条目(said / thought / commitment / tendency / ruler)现在没有存储归属。被取代的 facts 文件、WorldState JSON 在本期删除。约定也放在这张表里,不另建表,原因是渲染"当下"和衰减都要统一遍历这五类条目。
  - `context_snapshots`:add-with-proof。ADR 要求"当下"能事后还原,现有表里没有合适的归属。
  - `push_tokens`、`notifications`:add-with-proof。FCM 需要设备 token;免打扰排队和每小时限额需要发送记录。没有放进 `devices` 表,因为那样要改既有表的列。
  - `runs_fts`、`memory_items_fts`:add-with-proof。取代 `search_runs` 的 LIKE 查询,同时覆盖工作记忆。
  - `internal/presence`:取代 `internal/memory`(后者在 Task 9 删除)。
  - `internal/wake`:取代 `internal/triggers`(Task 6 删除),cron 解析代码迁移过来。
  - `internal/notify`:新增,只负责 FCM 发送。
  - internal 包总数保持 14:删除 memory、triggers、workspace,新增 presence、wake、notify。
- **Architecture Integrity:**
  - 唯一 owner:"当下"的渲染只在 presence middleware;唤醒只在 `wake.Scheduler`;推送只经 `notify_owner` → `notify.Sender`。
  - 新旧不并存:ContextPlane 的记忆注入在 Task 9 整体删除,只保留技能目录。cron 触发器在 Task 6 被约定(可带 cron 循环)取代,不保留两套调度。
- **Complexity:** 新文件各自控制在 400 行以内,满足 800 行的结构上限;预计 `internal/runtime` 净删除约 1500 行,`internal/memory` 约 3500 行整体删除。

## Execution Readiness View

- **Intent Lock:** 只做 ADR-0003 的 P1 加上 owner 的三项决策,外加 P0 遗留的两项(主模型重试、工具失败的事件记录)和 P0 模拟器验收中发现的三个手机端小缺陷。不做知识库、Capture、Watch、早安卡、空闲思考、手机通知感知,也不做 App 的"此刻"页面。
- **Scope Fence:**
  - 不改技能的加载和资格判断。
  - 不改 `ask_operator`,包括它的名字。
  - 预算只做"每日自主唤醒次数"。token 预算要先记录用量,留到 P4 空闲思考时再做,本期在 ADR 中注明。
- **Baseline Lock:** 起点 commit 为分支 `direction/personal-agent` 上的 `3116802`。
- **Task Batches:**
  - T1–T5:新能力上线,旧记忆仍在。
  - T6–T8:唤醒、推送、手机端。
  - T9–T10:删除。
  - T11–T12:遗留项。
  - T13–T15:验收与文档。
- **Drift / Rewind Rules:**
  - Task 5 中,如果 provider 拒绝"末尾 system 消息",改为在第一条 system 消息之后插入,并在 Task 5 的测试里固定这个位置;不要退回到 `BeforeModelRewriteState`,那会把"当下"写进对话历史,被 summarization 压缩。
  - Task 6 中,如果 `RunService.CreateRun` 不能在没有 HTTP 请求的上下文里调用,停下来,把建 run 的部分从 api 抽到 wire 层的适配器里,不在 wake 包里直接调 runtime。
  - Task 8 中,如果 Firebase 手动初始化(`FirebaseOptions`)在 debug 包上取不到 token,改用 google-services 插件,并在 CI 中从 secret 写入 `google-services.json`。
- **Evidence Required Before Completion:**
  - Task 13 验收测试通过。
  - Task 15 全量门禁通过。
  - 模拟器验收:用 GMS 镜像加上 owner 的 Firebase 项目;如果条件不具备,明确写明推送这一环未经设备验证。

## Task 0: 记录基线

- [ ] 运行:

```bash
deadcode ./... > /tmp/acorn-p1-deadcode-before.txt; deadcode -test ./... >> /tmp/acorn-p1-deadcode-before.txt; cat /tmp/acorn-p1-deadcode-before.txt
go test ./... 2>&1 | tail -3
```

预期:deadcode 只有 `config/config_defaults.go DefaultConfig`;测试全部 ok。

---

## Task 1: 工作记忆、快照、推送的存储层

**Files:**
- Create: `internal/core/presence.go`、`internal/store/store_memory_items.go`、`internal/store/store_snapshots.go`、`internal/store/store_push.go`,以及对应的 `_test.go`
- Modify: `internal/store/store_schema_bootstrap.go`、`internal/store/store_schema.go`、`internal/api/store_stub_test.go`(只在需要满足新接口时)

**Why:** 后续所有任务都依赖这些表,先单独落地并测试。

- [ ] **Step 1:** 在 `storeBootstrapTables` 末尾追加(注释里的表数量同步更新):

```sql
CREATE TABLE IF NOT EXISTS memory_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,              -- said|thought|commitment|tendency|ruler
    content TEXT NOT NULL,
    status TEXT NOT NULL,            -- active|resting|sunk|woken|settled|internalized|released
    session_id TEXT NOT NULL DEFAULT '',
    source_run_id TEXT NOT NULL DEFAULT '',
    wake_at TEXT NOT NULL DEFAULT '',     -- commitment only, RFC3339 UTC
    recurrence TEXT NOT NULL DEFAULT '',  -- commitment only, 5-field cron in owner timezone
    expires_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_items_due ON memory_items(kind, status, wake_at);

CREATE VIRTUAL TABLE IF NOT EXISTS memory_items_fts USING fts5(content, content='memory_items', content_rowid='id', tokenize='trigram');
CREATE TRIGGER IF NOT EXISTS memory_items_ai AFTER INSERT ON memory_items BEGIN
  INSERT INTO memory_items_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS memory_items_au AFTER UPDATE OF content ON memory_items BEGIN
  INSERT INTO memory_items_fts(memory_items_fts, rowid, content) VALUES ('delete', old.id, old.content);
  INSERT INTO memory_items_fts(rowid, content) VALUES (new.id, new.content);
END;

CREATE VIRTUAL TABLE IF NOT EXISTS runs_fts USING fts5(run_id UNINDEXED, input_text, output_text, tokenize='trigram');
CREATE TRIGGER IF NOT EXISTS runs_fts_ai AFTER INSERT ON runs BEGIN
  INSERT INTO runs_fts(run_id, input_text, output_text) VALUES (new.run_id, new.input_text, new.output_text);
END;
CREATE TRIGGER IF NOT EXISTS runs_fts_au AFTER UPDATE OF output_text ON runs BEGIN
  DELETE FROM runs_fts WHERE run_id = new.run_id;
  INSERT INTO runs_fts(run_id, input_text, output_text) VALUES (new.run_id, new.input_text, new.output_text);
END;

CREATE TABLE IF NOT EXISTS context_snapshots (
    hash TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS push_tokens (
    device_id TEXT PRIMARY KEY,
    token TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    thread_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,            -- queued|sent|failed
    send_after TEXT NOT NULL,
    error_text TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    sent_at TEXT NOT NULL DEFAULT ''
);
```

  FTS 表上线前已存在的 runs 不会被 trigger 覆盖。`migrate()` 中补一个一次性回填:`runs_fts` 为空而 `runs` 非空时,`INSERT INTO runs_fts SELECT run_id, input_text, output_text FROM runs`,并记入 `schema_migrations`(key `v4_runs_fts_backfill`)。

- [ ] **Step 2:** `schemaRequiredTables` 增加四张普通表的列;FTS 虚表不列入。

- [ ] **Step 3:** `internal/core/presence.go` 定义领域类型和接口(core 不 import 其他内部包):

```go
type MemoryKind string // said, thought, commitment, tendency, ruler
type MemoryStatus string // active, resting, sunk, woken, settled, internalized, released

type MemoryItem struct {
	ID          int64
	Kind        MemoryKind
	Content     string
	Status      MemoryStatus
	SessionID   string
	SourceRunID string
	WakeAt      time.Time // zero unless commitment
	Recurrence  string
	ExpiresAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PresenceStore interface {
	AddMemoryItem(ctx context.Context, item MemoryItem) (MemoryItem, error)
	ListMemoryItems(ctx context.Context, statuses []MemoryStatus) ([]MemoryItem, error)
	LoadMemoryItem(ctx context.Context, id int64) (*MemoryItem, error)
	// UpdateMemoryItem writes status/expires_at/wake_at/content; fails with ErrMemoryItemNotFound.
	UpdateMemoryItem(ctx context.Context, item MemoryItem) error
	// ClaimDueCommitment flips one active commitment with wake_at <= now to woken.
	// Returns ErrMemoryItemNotDue when another claimer won.
	ClaimDueCommitment(ctx context.Context, id int64, now time.Time) error
	ListDueCommitments(ctx context.Context, now time.Time) ([]MemoryItem, error)
	SearchExperience(ctx context.Context, query string, limit int) ([]ExperienceHit, error)
	SaveContextSnapshot(ctx context.Context, hash, content string) error
}

type NotificationStore interface {
	SetPushToken(ctx context.Context, deviceID, token string) error
	ListPushTokens(ctx context.Context) ([]PushToken, error) // excludes revoked devices
	DeletePushToken(ctx context.Context, deviceID string) error
	QueueNotification(ctx context.Context, n Notification) (Notification, error)
	ListDueNotifications(ctx context.Context, now time.Time) ([]Notification, error)
	FinishNotification(ctx context.Context, id int64, status NotificationStatus, errText string, at time.Time) error
	CountNotificationsSince(ctx context.Context, since time.Time) (int, error)
}
```

  `ExperienceHit{Source: "run"|"memory", RunID, MemoryID, Kind, Snippet, CreatedAt}`。sentinel error 放在 `core` 的 errors 文件:`ErrMemoryItemNotFound`、`ErrMemoryItemNotDue`。

- [ ] **Step 4:** store 实现。`SearchExperience` 对两个 FTS 表分别 `MATCH ?`,用 `snippet()` 取片段,合并后按 `bm25` 排序。少于 3 个字符的查询不能走 trigram,对 `content` / `input_text` / `output_text` 改用 `LIKE` 并转义通配符(沿用现有 `search_runs` 的转义写法)。`RevokeDevice` 同时删除该设备的 push token。

- [ ] **Step 5:** 测试:
  - CRUD 和状态更新;
  - 两个并发 `ClaimDueCommitment` 只有一个成功;
  - `SearchExperience` 中文、英文、2 字符查询都能命中;
  - 旧库(只有 runs 数据)打开后 `runs_fts` 被回填;
  - 吊销设备后 token 消失。

```bash
go test ./internal/store ./internal/core -count=1
```

---

## Task 2: `internal/presence`:衰减、渲染、人格

**Files:** Create `internal/presence/{decay.go,render.go,persona.go}` 和对应测试;Modify `tests/architecture/dependency_direction_test.go`(`presence` 定为 Layer 2)、`tests/architecture/structural_limits_test.go`。

**Why:** 衰减和渲染是确定性纯逻辑,单独成包测试;runtime 和 wake 都会用到。

- [ ] **Step 1:** `decay.go`。默认参数为包级常量,不进配置:

```go
// activeTTL is how long an untouched item stays active; restingTTL how long it then rests.
var activeTTL = map[core.MemoryKind]time.Duration{
	core.MemorySaid: 7 * 24 * time.Hour, core.MemoryThought: 48 * time.Hour,
	core.MemoryTendency: 30 * 24 * time.Hour, core.MemoryRuler: 30 * 24 * time.Hour,
}
const restingTTL = 14 * 24 * time.Hour

// Decay returns the items whose status must change at now. Commitments never decay.
func Decay(items []core.MemoryItem, now time.Time) []core.MemoryItem
func NewExpiry(kind core.MemoryKind, now time.Time) time.Time
```

  规则:
  - `active` 且 `now > expires_at` → `resting`,`expires_at = now + restingTTL`;
  - `resting` 且 `now > expires_at` → `sunk`;
  - 其它状态不变。
  - 函数只返回需要变更的条目副本,不修改入参。

- [ ] **Step 2:** `render.go`:

```go
type RenderInput struct {
	Now       time.Time
	Location  *time.Location
	Wake      string // "owner message" or "commitment #12: ..."
	Items     []core.MemoryItem // active + resting + woken commitments
	MaxTokens int
	Count     func(string) int
}
// Render returns the <presence> block. Sections in order: now/wake, commitments
// (woken first, then upcoming by wake_at), thoughts, said (by id), tendencies and
// rulers, resting (one line each). Drops resting, then oldest said/thought, until
// the block fits MaxTokens, and states how many items were left out.
func Render(in RenderInput) string
```

  每个条目以 `#id` 开头,方便 agent 在 `settle` 中引用;时间按 owner 时区显示,并带星期。

- [ ] **Step 3:** `persona.go`:`LoadPersona(path string) (string, error)`。文件不存在或为空时返回包含路径的错误。另提供 `DefaultPersona` 常量,供 `acorn init` 和安装脚本写出初始文件:身份、语气、与 owner 的关系,不含工具规则。

- [ ] **Step 4:** 测试:
  - 衰减边界(刚好到期、未到期、commitment 不衰减);
  - 渲染顺序和截断说明;
  - 跨时区显示;
  - 人格缺失时报错。

```bash
go test ./internal/presence -count=1 && make test-architecture
```

---

## Task 3: 配置

**Files:** `internal/config/*`(struct、defaults、validation、tests)、`configs/acorn.example.yaml`、`configs/acorn.selfhosted.example.yaml`、`configs/acorn.minimal.yaml`、`internal/cli/acorn.init.yaml`、`scripts/install-release.sh`、`cmd/acorn/main.go`

- [ ] **Step 1:** 新增配置:

```yaml
owner:
  timezone: Asia/Shanghai     # IANA name; validated with time.LoadLocation
presence:
  max_tokens: 4000            # cap for the rendered <presence> block
wake:
  daily_limit: 20             # autonomous wakes per owner-local day; 0 disables autonomous wakes
notify:
  max_per_hour: 6
  quiet_hours: {start: "23:00", end: "08:00"}   # owner timezone; equal start/end disables
  fcm:
    service_account_file: ""  # empty = notify_owner disabled with this reason
```

  校验:
  - timezone 必须能被 `time.LoadLocation` 加载;
  - `max_tokens` 在 500 到 `context.window_tokens / 4` 之间;
  - `daily_limit` 大于等于 0;
  - `quiet_hours` 符合 `HH:MM` 格式;
  - `service_account_file` 非空时,文件必须存在且能解析出 `project_id`、`client_email`、`private_key`。

- [ ] **Step 2:** `cmd/acorn/main.go` 加 `import _ "time/tzdata"`。release 构建是 `CGO_ENABLED=0`,VPS 上不一定有 zoneinfo。

- [ ] **Step 3:** 本 Task 只新增配置,删除放在 Task 6 和 Task 9 做。模板同步新增上述配置块。安装脚本在 `~/.acorn/persona.md` 不存在时写出 `DefaultPersona`;`acorn init` 同样处理,并在输出里打印 persona 路径。

```bash
go test ./internal/config ./internal/cli -count=1
```

---

## Task 4: 工作记忆工具与经历检索

**Files:** Create `internal/tools/presence_tools.go` 和测试;Modify `internal/tools/builtin_registry.go`(`NativeToolDeps` 增加 `Presence core.PresenceStore`、`Clock func() time.Time`、`Location *time.Location`)、`internal/tools/configured.go`、`internal/wire/runtime.go`、`internal/wire/run_catalog_test.go`;Delete `internal/tools/search_runs.go`、`internal/store` 中的 `SearchRuns` 及其测试、`RunSearchStore` 端口。

- [ ] **Step 1:** 工具定义(全部 eager、`ToolKindNative`,参数用 `jsonschema` tag):

| 工具 | 参数 | 行为 |
|---|---|---|
| `keep` | `content` | 记一条 `said`:owner 的原话,不改写。 |
| `think` | `content` | 记一条 `thought`。 |
| `schedule_wake` | `task`,`at`(RFC3339 或 owner 时区下的 `YYYY-MM-DD HH:MM`)或 `in`(Go duration,如 `72h`),可选 `recurrence`(5 段 cron) | 记一条 `commitment`,`session_id` 取当前线程。`at` 和 `in` 必须二选一;过去的时间报错;有 `recurrence` 时 `wake_at` 取下一个触发点。 |
| `settle` | `id`,`action`:`renew` / `internalize` / `release` / `done`,`internalize` 时需要 `as`(tendency 或 ruler)和 `content` | `renew`:重置到期时间并恢复为 active。`internalize`:新建 tendency 或 ruler,原条目标记 internalized。`release`:标记 released。`done`:只用于 woken 状态的 commitment,标记 settled。 |
| `recall` | `query`,`limit` | 调 `SearchExperience`,返回 `source`、`run_id`、`memory_id`、`snippet`、`created_at`。 |

  参数错误和状态不对(例如对 thought 调用 `done`)一律返回 error,由 tool error middleware 转成模型可见的结果。

- [ ] **Step 2:** `NativeToolDeps` 缺 `Presence`、`Clock`、`Location` 中任何一项时注册失败(沿用 `nativeToolFactory` 的依赖检查)。

- [ ] **Step 3:** 测试:每个工具的正常路径和错误路径;`schedule_wake` 的时区换算(owner 时区 `09:00` 存为对应的 UTC);`recurrence` 计算下一次触发。`approval_restart_e2e_test.go` 中被审批的工具从 `search_runs` 改为 `recall`,脚本化模型的工具调用参数同步修改。

```bash
go test ./internal/tools ./internal/wire -count=1
```

---

## Task 5: presence middleware、人格进 Instruction、context 快照

**Files:** Create `internal/runtime/presence.go` 和测试;Modify `internal/runtime/agent.go`(handler 顺序)、`internal/runtime/runner.go`(Instruction 组装)、`internal/runtime/types.go`(`RuntimeDeps` 增加 `Presence core.PresenceStore`、`Clock`、`Location`、`PersonaPath`)、`internal/wire/runtime.go`、`internal/runtime/agent_test.go`

- [ ] **Step 1:** `presenceMiddleware` 嵌入 `adk.BaseChatModelAgentMiddleware`,只实现 `WrapModel`。每次 Generate 或 Stream:
  1. 读 `ListMemoryItems(active, resting, woken)`。
  2. 用 `presence.Decay` 算出需要变更的条目并写回(衰减在 wake 调度器里也会执行,这里保证渲染前状态正确)。
  3. 调 `presence.Render`。
  4. 把 `schema.SystemMessage(rendered)` 追加到本次输入的末尾,不修改传入的切片。
  5. 计算 `sha256(instruction + "\n" + rendered)`;与本 run 上一次的哈希不同时,调用 `SaveContextSnapshot`,并 `AppendEvent(run, "presence.snapshot", {"hash": h})`。

  本次唤醒的说明(`Wake`)从 context 中读取:`core.WithWake(ctx, "...")`,由 Executor 写入。owner 发消息时为 `owner message`;约定唤醒时为 `commitment #id: <task>`。

- [ ] **Step 2:** handler 顺序变为:patchtoolcalls → summarization → reduction → toolsearch(可选)→ presence → approval → tool errors。`agent_test.go` 的顺序断言同步更新。presence 放在 summarization 之后,是为了让 summarization 计算 token 时不把"当下"算进去;"当下"的大小由 `presence.max_tokens` 单独控制,配置校验保证它远小于 `compact_margin_tokens`。

- [ ] **Step 3:** Instruction 组装为:`persona.md` 内容 → 内置的 `operatingRules` 常量 → 技能目录。`operatingRules` 由现在的 `capabilityDiscoveryInstruction` 扩展而来,增加以下规则:
  - 何时用 `keep`、`think`、`schedule_wake`、`settle`;
  - 醒来处理约定后要么 `settle done`,要么重新 `schedule_wake`;
  - 需要找 owner 时用 `notify_owner`。

  persona 每个 run 读取一次,读取失败则 run 失败。

- [ ] **Step 4:** 测试:
  - 用脚本化模型断言收到的最后一条消息是 `<presence>` system 消息,且 agent 状态里不包含它(第二次调用模型时,历史中没有上一次的"当下");
  - 同一 run 内"当下"不变时只记一条快照事件,变化后再记一条;
  - persona 缺失时 run 失败,错误中包含路径。

```bash
go test ./internal/runtime ./internal/wire -count=1
```

---

## Task 6: `internal/wake` 调度器,取代 cron 触发器

**Files:**
- Create `internal/wake/{scheduler.go,cron.go}` 和测试(`cron.go` 由 `internal/triggers/cron_schedule.go` 迁移而来)
- Delete `internal/triggers/`、`internal/wire/container.go` 中的 `triggerRunCreator`、WorldState 注入、去重和 quota 代码,以及 `trigger_*_test.go`
- Modify `internal/cli/serve.go`、`internal/wire/container.go`、`internal/config`(删除 `triggers.*`)、配置模板、dependency_direction 层级表(`wake` 为 Layer 2,通过接口拿到"起 run"的能力)

- [ ] **Step 1:**

```go
type RunStarter interface {
	// StartWakeRun starts a run in threadID (creating a "Reminders" thread when it no longer exists).
	StartWakeRun(ctx context.Context, threadID, wake, input string) (runID string, err error)
}

type Scheduler struct {
	store    core.PresenceStore
	runs     RunStarter
	clock    func() time.Time
	loc      *time.Location
	daily    int
	interval time.Duration // 30s
}

// Tick runs one pass: decay, then due commitments (Task 7 adds queued
// notifications). Errors are joined and returned; one failing commitment does
// not stop the others.
func (s *Scheduler) Tick(ctx context.Context) error
func (s *Scheduler) Run(ctx context.Context) // Tick now, then every interval; logs Tick errors
```

  到期约定的处理步骤:
  1. 数一下 owner 时区"今天"已有多少条 `wake.fired` 事件,达到 `daily` 则跳过,并对每个跳过的约定每天只 warn 一次。
  2. `ClaimDueCommitment`。
  3. 调 `StartWakeRun`,input 为 `到点了:<task>(约定 #id,建立于 <created_at 本地时间>)`。
  4. 追加 `wake.fired` 事件。
  5. 有 `recurrence` 时插入下一条 active commitment,内容不变,`wake_at` 为下一次触发点。原条目保持 woken,等 agent `settle`。
  6. `StartWakeRun` 失败时,条目改回 active,`wake_at` 推后 5 分钟,并返回错误。

- [ ] **Step 2:** wire 层实现 `RunStarter`:
  - 线程存在就直接用;
  - 不存在就新建标题为"Reminders"的线程;
  - 通过 `api.RunService.CreateRun` 起 run;
  - 用 `core.WithWake` 带上唤醒说明,让 presence 能读到。
  - 如果 `CreateRun` 无法携带 context 值,按 Drift Rule 处理。

- [ ] **Step 3:** `serve.go` 中 `go scheduler.Run(ctx)` 替换原 trigger scheduler 的启动。`resumeReadyRunsLoop` 不动。

- [ ] **Step 4:** 测试(假时钟,假 `RunStarter`):
  - 到期触发一次,不会重复触发;
  - 两个调度器并发时只有一个能 claim 成功;
  - 超过每日上限时跳过,第二天恢复;
  - 循环约定生成下一条;
  - `StartWakeRun` 失败后回滚;
  - 衰减被执行。

```bash
go test ./internal/wake ./internal/wire ./internal/cli -count=1 && make test-architecture
```

---

## Task 7: `internal/notify` + `notify_owner` + push token 接口

**Files:**
- Create `internal/notify/{fcm.go,sender.go}` 和测试、`internal/tools/notify_tool.go` 和测试
- Modify:
  - `internal/wake/scheduler.go`:新增必需依赖 `NotificationFlusher`(`FlushDue`),`Tick` 在处理完约定后调用它;构造时为 nil 直接返回错误
  - `internal/api` 设备认证中间件:把 `DeviceAuthContext` 放进请求 context,新增 `DeviceFromContext`;
  - `handlers_device.go`:新增 `PUT /v1/devices/self/push-token`;
  - `routes.go`、`docs/openapi.yaml`、`internal/wire`

- [ ] **Step 1:** `fcm.go`:

```go
type FCMClient struct {
	projectID string
	http      *http.Client // from jwt.Config{Email, PrivateKey, TokenURL: <token_uri from the service account file>,
	                       // Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"}}.Client(ctx)
	endpoint  string       // https://fcm.googleapis.com/v1/projects/%s/messages:send; tests override
}
// Send posts one message. A 404 or UNREGISTERED status returns ErrTokenUnregistered.
func (c *FCMClient) Send(ctx context.Context, token string, msg Message) error
```

  `Message{Title, Body, ThreadID, RunID}`。发送时同时带 `notification` 块(App 在后台时由系统显示)和 `data` 块(`thread_id`、`run_id`,用于点击跳转)。HTTP 请求带 context,并关闭响应 body。对 429 和 5xx 做最多 3 次指数退避重试;最终失败要返回错误。`TokenURL` 直接使用服务账号文件中的 `token_uri` 字段,不引入 `oauth2/google` 包(它会带进 `cloud.google.com/go/compute/metadata` 依赖)。

- [ ] **Step 2:** `sender.go`:

```go
type Sender struct {
	store      core.NotificationStore
	fcm        *FCMClient
	clock      func() time.Time
	loc        *time.Location
	maxPerHour int
	quiet      QuietHours
}
// Notify enforces the hourly cap (returns ErrRateLimited), queues inside quiet hours
// (send_after = quiet end), otherwise sends to every registered token.
func (s *Sender) Notify(ctx context.Context, n core.Notification) (core.Notification, error)
// FlushDue sends queued notifications whose send_after has passed.
func (s *Sender) FlushDue(ctx context.Context) error
```

  - 没有已注册的 token:返回 `ErrNoPushToken`。
  - token 失效:删除该 token,继续发给其他设备。
  - 所有设备都发送失败:记为 `failed`,并返回错误。

- [ ] **Step 3:** `notify_owner` 工具,参数 `title`、`body`,`thread_id` 和 `run_id` 取自当前 run。返回结果写明 `sent`、`queued until HH:MM` 或具体错误。没有配置 `service_account_file` 时,工具注册为 disabled,原因是 `notify.fcm.service_account_file is not configured`(与 `web_search` 的处理方式一致)。

- [ ] **Step 4:** API:`PUT /v1/devices/self/push-token`,请求体 `{"token": "..."}`,返回 204;token 为空返回 400。OpenAPI 同步更新,并重新生成 Kotlin client。

- [ ] **Step 5:** 测试:
  - `httptest` 模拟 FCM:验证请求体和鉴权头、401 后刷新、429 重试、UNREGISTERED 时删除 token;
  - 每小时上限;
  - 免打扰时段排队,结束后由 `FlushDue` 发出;
  - 没有 token 时报错;
  - API 的 204、400、未认证。

```bash
go test ./internal/notify ./internal/tools ./internal/api ./internal/wake -count=1
cd mobile-kotlin && ./tool/generate_openapi_client.sh && ./tool/generate_openapi_client.sh --check
```

---

## Task 8: 手机端 FCM

**Files:**
- `mobile-kotlin/build.gradle.kts`、`app/build.gradle.kts`(Firebase BOM 加 `firebase-messaging`;从 `local.properties` 或环境变量读取 `acorn.firebase.projectId`、`appId`、`apiKey`、`senderId`,写入 `BuildConfig`)
- `AndroidManifest.xml`(`POST_NOTIFICATIONS` 权限、`FirebaseMessagingService`)
- 新建 `core/push/{PushInitializer.kt,AcornMessagingService.kt,PushTokenRegistrar.kt}`
- `MainActivity.kt`(点击通知后的 intent → 打开对应线程)
- `feature/settings`(显示推送状态)

- [ ] **Step 1:** 在 `Application.onCreate` 中,当 `BuildConfig` 的 Firebase 字段齐全时用 `FirebaseOptions` 手动初始化;字段缺失时不初始化,Settings 显示"推送未配置(构建时缺少 Firebase 配置)"。这样 CI 在没有 secret 的情况下也能构建,不需要 google-services 插件和 json 文件。

- [ ] **Step 2:** 配对成功和 `onNewToken` 时,把 token `PUT` 到 `/v1/devices/self/push-token`,在 IO 线程上执行。失败时在 Settings 显示错误并提供重试;不静默吞掉。

- [ ] **Step 3:** Android 13 及以上,在首次进入 ConnectedShell 时请求通知权限;被拒绝时,Settings 显示状态和"去系统设置"的入口。

- [ ] **Step 4:** App 在前台时 `onMessageReceived` 自己发通知;在后台时由系统显示。点击通知带 `thread_id` extra,`MainActivity` 读到后调用 `shellViewModel.openThread(threadId)`。

- [ ] **Step 5:** 单元测试:
  - Firebase 字段缺失时推送状态为"未配置";
  - intent 中的 `thread_id` 被解析并路由到对应线程。

```bash
cd mobile-kotlin && JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home ./gradlew test assembleDebug
```

---

## Task 9: 删除旧记忆体系

**Files(删除):**
- `internal/memory/`
- `internal/workspace/`
- `internal/runtime/{memtools.go,memory_context.go,context_assembler.go,reviewer.go}` 及测试,`plane.go` 中的记忆部分
- `internal/tools` 中的四个文件工具、`worldstate_tool.go`、`file_mutate.go`、`ports.go` 中相关的端口
- `internal/api` 的 memory service、handlers、DTO,以及 `/v1/memory` 路由
- `internal/cli/memory.go` 及测试
- `internal/config` 的 `memory.*`

**Files(修改):** `internal/wire`、`docs/openapi.yaml`(删除 `/v1/memory/*` 和只被它们引用的 schema)、Kotlin client 重新生成、`doctor` 输出、`tests/architecture`(删除 memory 和 workspace 的条目)、配置模板。

- [ ] **Step 1:** ContextPlane 只保留技能目录这一段,并直接并入 Instruction 组装:`assembleContext` 和 `ContextPlane` 如果只剩这一个用途,就删掉这个类型,改为在 `runner.go` 中调用 `buildSkillCatalogMessage`。

- [ ] **Step 2:** 删除 `Executor.SetReviewer` 和 run 结束后的 review 调用;删除 embedding client、sqlite-vec 相关代码;删除 `WorldState`、`worldStateAdapter`。

- [ ] **Step 3:** 用 `grep -rn "memory\.\|MemoryModule\|WorldState\|worldstate\|remember\|memory_search\|memory_read_file" internal cmd skills docs configs` 逐项清理。seed skills 中提到 `memory_*` 或 `remember` 的段落改成工作记忆工具的写法(`skills/capability_recall` 等)。

- [ ] **Step 4:** deadcode 与 Task 0 的基线对比,不能有新增条目。

```bash
go build ./... && go test ./... -count=1 && make test-architecture && deadcode ./... && deadcode -test ./...
cd mobile-kotlin && ./tool/generate_openapi_client.sh && ./tool/generate_openapi_client.sh --check
```

---

## Task 10: 删除技能自动生成

**Files:** `internal/skills/{writer.go,writer_lifecycle.go}` 和测试、`internal/tools/builtin_registry.go`(删除 `skill_create`)、`internal/cli/skills.go`(删除 `create`、`patch`、`delete` 子命令)、`internal/skills/loader.go`(不再扫描 `{storage_dir}/skills/generated`)、`skills/skill_creator/`(seed skill 整个删除)、AGENTS.md 中"generated skill"的相关段落。

- [ ] 删除后 `skill_list` 和 `skill_view` 照常工作;`acorn skills list|inspect|check` 照常工作。

```bash
go test ./internal/skills ./internal/tools ./internal/cli -count=1 && deadcode ./...
```

---

## Task 11: P0 遗留:工具失败事件与主模型重试

**Files:** `internal/runtime/tool_errors.go`、`events.go`、`agent.go`、`runner.go` 和测试

- [ ] **Step 1:** 每个 run 新建一个 `failedCalls`(以 call ID 为 key 的 set,并发安全)。tool error middleware 把错误转换成结果文本时,同时记录该 call ID。projector 处理工具消息时,如果 call ID 在 set 里,发出 `tool_call_failed{tool_call_id, tool_name, error}`,否则发出 `tool_call_succeeded`。这两个都是诊断事件,不进入 live 契约。

- [ ] **Step 2:** `ChatModelAgentConfig.ModelRetryConfig = &adk.ModelRetryConfig{MaxRetries: 3}`。projector 读流时,遇到 `*adk.WillRetryError` 不记 `streamErr`,丢弃本轮已拼接的 assistant 文本,等下一轮。客户端已经收到的 delta 不撤回,`run.completed` 中的最终消息会覆盖流式气泡,这个行为在测试中固定。

- [ ] **Step 3:** 测试:
  - 失败的工具只产生 `tool.call.failed` 事件;
  - 第一次流在中途以可重试错误结束、第二次成功时,run 成功,最终输出只包含第二次的文本;
  - 重试 3 次都失败时 run 失败。

```bash
go test ./internal/runtime -count=1
```

---

## Task 12: 手机端已知缺陷(P0 模拟器验收中发现)

**Files:** `feature/threads/ThreadsViewModel.kt` 和 `ThreadsScreen.kt`、`feature/chat/ChatScreen.kt`、`core/sse/RunEventProjection.kt` 和测试

- [ ] 线程列表增加加载状态:加载中显示进度,加载失败显示错误和重试;只有加载成功且列表为空时才显示 "No threads yet"。
- [ ] 聊天页标题按 `runStatus` 显示:running、waiting(Interrupted 且有 tool approval 活动)、idle。
- [ ] `RunResumeRequested` 的活动行在后续出现 `assistant.delta` 或终止事件时移除。
- [ ] 为上述三项各写一个 reducer 或 ViewModel 单元测试。

```bash
cd mobile-kotlin && ./gradlew test assembleDebug
```

---

## Task 13: 验收测试

**Files:** Create `internal/wire/wake_acceptance_e2e_test.go`

- [ ] 使用 P0 的 `fakeOpenAI` 和 `writeApprovalTestConfig` 的写法(新增 `persona.md`、FCM 服务账号文件,`notify` 指向 `httptest` 模拟的 FCM,配置一个可控时钟)。脚本流程:
  1. owner 发消息"三天后提醒我看 X"。脚本化模型调用 `schedule_wake{in: "72h", task: "提醒 owner 看 X"}`,然后回复"好的"。
  2. 关闭 container,用同一份配置重新打开(验证重启后约定仍然存在)。
  3. 时钟推进 72 小时,调用 `scheduler.Tick`。
  4. 断言:同一线程里出现新的 run;模型收到的输入包含 `到点了:提醒 owner 看 X`,且最后一条 system 消息是包含该约定(状态为 woken)的 `<presence>`。
  5. 脚本化模型在第 2 轮调用 `notify_owner{title: "提醒", body: "该看 X 了"}`,第 3 轮调用 `settle{id, action: done}`,第 4 轮回复。
  6. 断言:模拟的 FCM 收到 1 条消息,`data.thread_id` 正确;约定状态为 settled;`wake.fired` 只有 1 条。
  7. 再执行一次 `Tick`,不会再次触发。

- [ ] 另加一个用例:时钟推进到免打扰时段内,`notify_owner` 返回 queued;推进到免打扰结束后 `Tick`,FCM 收到消息。

```bash
go test ./internal/wire -run 'Wake|Approval' -count=1 -race
```

---

## Task 14: 文档

- [ ] `docs/adr/0003-personal-agent-direction.md`:
  - 分期表:P1 加上"FCM 推送与 `notify_owner`";P3 改为"Watch、早安卡"。
  - §Runtime:presence 改为 `WrapModel`,并写明原因(`BeforeModelRewriteState` 会把"当下"写进对话历史)。
  - 预算一节:写明 P1 只有次数上限,token 预算随 P4 落地。
- [ ] `AGENTS.md`:
  - 项目概览;
  - 关键包(presence、wake、notify,删除 memory、triggers、workspace);
  - 两套真相改为:SQLite 中的 runtime、工作记忆、经历,以及 persona 文件;表数量和表名;
  - 硬边界中的"记忆 & 检索"一节重写;
  - 删除 generated skill 的相关描述;
  - 常用命令中删除 `memory` 和 `skills create`。
- [ ] `docs/architecture/INVARIANTS.md`:
  - 新增不变量:"当下"不写入 agent 状态、约定只触发一次、每日上限、免打扰排队、push token 随设备吊销删除。每条都对应 Task 5、6、7、13 的测试。
  - 删除记忆、WorldState、trigger 的旧条目。
- [ ] `docs/user/self-hosted-onboarding.md`:
  - persona.md;
  - `owner.timezone`;
  - Firebase 项目的创建步骤:服务账号 JSON 放到 VPS 并配置 `notify.fcm.service_account_file`;App 构建所需的 `acorn.firebase.*` 四个值写入 `local.properties`;
  - 配置迁移清单:删除的键和新增的键。
- [ ] README 中涉及记忆和触发器的段落。
- [ ] `make test-architecture`:`docs_structure_test` 通过。

---

## Task 15: 全量门禁 + 模拟器验收

- [ ] 全量门禁:

```bash
make format-check && make lint && go test -race ./... && make test-architecture
deadcode ./... && deadcode -test ./...
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug
```

- [ ] 模拟器验收(需要 owner 提供 Firebase 项目,以及带 GMS 的 system image:`system-images;android-35;google_apis;arm64-v8a`,下载前征得同意):
  1. 配对,允许通知权限;Settings 显示推送已注册。
  2. 对 agent 说"两分钟后提醒我看 X"(用 `in: 2m` 缩短验收时间)。
  3. 把 App 切到后台。两分钟内手机收到推送;点击推送进入对应线程,能看到醒来后的回复。
  4. 重复第 2 步,在约定到期前重启 `acorn serve`,推送照常到达。
- [ ] 不具备 Firebase 或 GMS 条件时,第 3、4 步不执行,在交付说明中写明"推送未经设备验证";Task 13 的模拟 FCM 测试仍然必须通过。

---

## Risks

- **Firebase 依赖 owner 操作。** 项目创建、服务账号和 App 配置都需要 owner 手工完成。缺少这些时,`notify_owner` 以 disabled 状态注册,原因可见,不会假装发送成功。
- **trigram 的限制。** trigram 分词对 1 到 2 个字的查询无效,已用 LIKE 兜底;但 LIKE 不走索引,经历数据量达到数万条后需要重新评估。
- **推送数据离开 VPS。** 推送内容经过 Google 服务器。`notify_owner` 的规则要求正文只写摘要,细节留在线程里;这一点写进 `operatingRules`。
- **预算不完整。** 自主唤醒只有次数上限,agent 在每次醒来的 run 里仍可能用很多 token。P4 引入空闲思考之前,`schedule_wake` 只能由 owner 对话或已有约定触发,风险可控。
- **配置 hard cut。** 已部署的 `~/.acorn/acorn.yaml` 中含 `memory`、`triggers`、`agent.system_prompt` 时,serve 会启动失败;onboarding 文档给出逐项迁移清单。

## Retirement

- `internal/memory`、`internal/triggers`、`internal/workspace` 整个删除。
- `/v1/memory/*`、`acorn memory`、`acorn skills create|patch|delete` 删除。
- `remember`、`memory_*`、`worldstate_*`、`search_runs`、`skill_create`,以及四个文件工具删除。
- 配置 `memory.*`、`triggers.*`、`agent.system_prompt` 删除。
- `{storage_dir}` 下的 `facts/`、`history/`、`worldstate/`、`vectors.db`、`skills/generated/` 由 owner 自行删除;Acorn 不再读取它们。
