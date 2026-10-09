# P2: 知识库、Capture 与分享入口

**Goal:** 在手机上从任意 App 分享一个链接给 Acorn,几分钟内知识库里出现一篇整理好的笔记。知识库是 VPS 上的一个 markdown 目录(Obsidian 能直接打开),也是一个 git 仓库,agent 的每次改动都是一个 commit。App 能搜索和阅读这些笔记。技能加载迁到 Eino skill middleware。

**Architecture:** 新包 `internal/knowledge` 负责知识库:路径校验、frontmatter 读写、经注入的 `Git` 接口提交,以及把文件同步进 SQLite 全文索引。文件是知识的真相,SQLite 的 `knowledge_notes` 只是可重建的索引,每次搜索或列表前按 mtime 和 size 增量同步,所以 owner 在别处改的文件也会被索引到。agent 通过 `knowledge_write`、`knowledge_edit`、`knowledge_read`、`knowledge_search`、`knowledge_list` 五个工具操作知识库。`POST /v1/captures` 接收分享内容:图片先存进知识库的 `attachments/` 并提交,再为这次分享新建一个线程,以 `capture` 角色的输入立即起一个 run,由 agent 抓取链接、整理成笔记。App 注册系统分享入口,新增"知识库"页。技能改由 Eino `skill` middleware 提供 `skill` 工具,取代 `skill_list`、`skill_view` 和 Instruction 里的技能目录。

**Tech Stack:** Go 1.27、cloudwego/eino v0.9.21(`adk/middlewares/skill`)、modernc.org/sqlite(FTS5 trigram)、系统 `git` 命令、Android `ACTION_SEND` 分享、okhttp multipart、已有的 `compose-markdown`。

**Baseline / Authority Refs:**
- `docs/adr/0003-personal-agent-direction.md`(§记忆中的知识库、§感知中的 Capture、§Runtime、§App、P2 行)
- `AGENTS.md`(硬边界、验证要求)、`docs/architecture/INVARIANTS.md`
- `docs/openapi.yaml`(Message.role、新增 captures 与 knowledge 接口)
- P1 计划 `docs/aegis/plans/2026-10-02-p1-presence-and-wake.md` 留给 P2 的一项:技能迁到 Eino skill middleware

**Owner 决策(2026-10-02):**
- 知识库目录默认 `{storage_dir}/knowledge`,可由 `knowledge.dir` 指到已有的 Obsidian vault。
- 检索只做关键词(FTS5 trigram,不足 3 个字走 LIKE);向量检索不在 P2,ADR 同步注明。
- 分享进来的图片存为知识库附件,agent 只拿到路径、类型、大小和 owner 附带的文字,不做图像理解。
- git 用系统自带的 `git` 命令;找不到 `git` 时 serve 启动失败并给出安装提示。
- 每次分享新建一个线程,owner 可以在这个线程里接着交代这条内容怎么处理。

**Compatibility Boundary:**
- 保持不变:`/v1` 的线程、run、事件、待办、inbox、设备、skills 接口;约定、推送、审批语义;live RunEvent 的现有 kind 和 payload 形状。
- 有意破坏(hard cut,无兼容层):
  - 删除工具 `skill_list`、`skill_view`,由 middleware 提供的 `skill` 工具取代;Instruction 不再内嵌技能目录摘要。
  - Message.role 新增 `capture`。
  - 新增配置 `knowledge.dir`。
  - 新增工具 `knowledge_write`、`knowledge_edit`、`knowledge_read`、`knowledge_search`、`knowledge_list`。
  - 新增 `POST /v1/captures`、`GET /v1/knowledge/notes`、`GET /v1/knowledge/note`。
  - serve 依赖系统 `git`。
- 数据库:新表一律 `CREATE TABLE IF NOT EXISTS`;不改既有表的列,不需要迁移。索引表可随时清空重建。

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

- **Requirement Ready Check:** ready。依据是 ADR-0003 的 P2 行加上 owner 的五项决策。验收标准:从任意 App 分享一个链接,几分钟内知识库出现整理后的笔记。
- **Change Necessity:** code-change。现状里 agent 没有可长期写入的知识存储(工作记忆会衰减,artifact 只挂在 run 上),也没有从手机送内容进来的入口。
- **Existence Check:**
  - `internal/knowledge`:add-with-proof。知识库的文件操作、frontmatter、git 提交和索引同步没有现成归属;`internal/store` 的 ArtifactService 管的是 run 产物,按 run 分目录、不可编辑,语义不同。internal 包数量从 14 变为 15。
  - `knowledge_notes` 与 `knowledge_notes_fts`:add-with-proof。文件是真相,这两张表只做检索索引。`memory_items_fts` 属于工作记忆,不复用。
  - `core.KnowledgeStore`:add-with-proof。索引的读写接口,core 中的 store 接口从 5 个变为 6 个。
  - `capture` 角色:add-with-proof。沿用 P1 的 `wake` 做法,让 App 把分享内容画成卡片,模型仍当作用户消息读取。
  - 不新增 captures 表:分享的记录就是线程里的 `capture` 消息和对应的 run,图片在知识库里有 git 历史。
- **Architecture Integrity:**
  - 唯一 owner:知识库的写入只经 `knowledge.Vault`(工具和 Capture 都走它);索引同步只在 `Vault.Sync`;git 提交只经注入的 `knowledge.Git`。
  - 新旧不并存:`skill_list`、`skill_view`、`skillCatalogBrief`、`skills.BuildAgentTools` 在 Task 7 删除,只保留 middleware 一条技能路径。
- **Complexity:** 新文件各自控制在 400 行以内;`internal/knowledge` 预计约 700 行,Kotlin 新增约 800 行。

## Execution Readiness View

- **Intent Lock:** 只做 ADR-0003 的 P2(知识库、Capture、App 分享入口与知识库页面),外加 P1 留下的技能迁移。不做 Watch、早安卡、空闲思考、手机通知感知,也不做 App 的"此刻"和"经历"页面。
- **Scope Fence:**
  - agent 不能删除或移动笔记;整理只通过新建和修改完成。
  - App 的知识库页只读,不在手机上编辑笔记,也不显示附件图片(以占位文字代替)。
  - 不做向量检索、图像理解、知识库远程同步;Obsidian 访问方式只在文档里说明。
  - 技能不使用 fork 模式,不接 `AgentHub` 和 `ModelHub`。
- **Baseline Lock:** 起点 commit 为分支 `direction/personal-agent` 上的 `631f390`。
- **Task Batches:**
  - T1–T3:知识库存储、包、配置。
  - T4–T5:知识库工具与 Capture 接口。
  - T6:capture 角色与线程。
  - T7:技能迁到 Eino skill middleware。
  - T8–T9:手机端。
  - T10–T11:验收、文档、全量门禁。
- **Drift / Rewind Rules:**
  - Task 5 中,如果生成的 Kotlin client 处理不了 multipart 里的二进制字段,停下来,改为 App 手写这一个请求(其余接口仍用生成的 client),并在 `docs/openapi.yaml` 的该接口描述里保留 multipart 契约;不改成 base64 JSON。
  - Task 7 中,如果 skill middleware 加进来的工具绕过了 approval 或 tool errors middleware 的包装,停下来,改为把 `skill` 工具注册进 catalog(仍由 middleware 生成),让它走统一的工具链路。
  - Task 7 中,如果 presence 快照记下的 Instruction 不含 skill middleware 追加的部分,改为在 presence middleware 的 `BeforeAgent` 中取最终 Instruction。
- **Evidence Required Before Completion:**
  - Task 10 验收测试通过。
  - Task 11 全量门禁通过。
  - 模拟器验收:从模拟器里分享一个链接,知识库出现笔记、git log 有对应提交、App 知识库页能搜到并打开。

## Task 0: 记录基线

- [ ] 运行:

```bash
deadcode ./... > /tmp/acorn-p2-deadcode-before.txt; deadcode -test ./... >> /tmp/acorn-p2-deadcode-before.txt; cat /tmp/acorn-p2-deadcode-before.txt
go test ./... 2>&1 | tail -3
```

预期:deadcode 只有 `config/config_defaults.go DefaultConfig`;测试全部 ok。

---

## Task 1: 知识库索引的存储层

**Files:**
- Create `internal/core/knowledge.go`、`internal/store/store_knowledge.go` 和测试
- Modify `internal/store/store_schema_bootstrap.go`、`internal/store/store_schema.go`

- [ ] **Step 1:** 在 `storeBootstrapTables` 末尾追加(注释里的表数量同步更新):

```sql
CREATE TABLE IF NOT EXISTS knowledge_notes (
    path TEXT PRIMARY KEY,          -- relative to the knowledge dir, slash-separated, ends in .md
    title TEXT NOT NULL,
    tags TEXT NOT NULL DEFAULT '',  -- space-separated
    body TEXT NOT NULL,
    mtime_ns INTEGER NOT NULL,
    size INTEGER NOT NULL,
    updated_at TEXT NOT NULL        -- frontmatter updated, else file mtime; RFC3339 UTC
);
CREATE INDEX IF NOT EXISTS idx_knowledge_notes_updated ON knowledge_notes(updated_at);

CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_notes_fts USING fts5(path UNINDEXED, title, tags, body, tokenize='trigram');
```

  FTS 表由 store 方法显式维护(先删后插),不用 trigger,因为 `knowledge_notes` 只经 `UpsertKnowledgeNote` 和 `DeleteKnowledgeNote` 改动。

- [ ] **Step 2:** `schemaRequiredTables` 增加 `knowledge_notes` 的列;FTS 虚表不列入。

- [ ] **Step 3:** `internal/core/knowledge.go`:

```go
type KnowledgeNote struct {
	Path      string
	Title     string
	Tags      []string
	Body      string
	MTimeNS   int64
	Size      int64
	UpdatedAt time.Time
}

type KnowledgeFileStat struct {
	Path    string
	MTimeNS int64
	Size    int64
}

type KnowledgeHit struct {
	Path      string
	Title     string
	Tags      []string
	Snippet   string
	UpdatedAt time.Time
}

type KnowledgeStore interface {
	UpsertKnowledgeNote(ctx context.Context, note KnowledgeNote) error
	DeleteKnowledgeNote(ctx context.Context, path string) error
	ListKnowledgeFileStats(ctx context.Context) ([]KnowledgeFileStat, error)
	// SearchKnowledge ranks by bm25; queries shorter than 3 characters use LIKE.
	SearchKnowledge(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
	// RecentKnowledge lists notes by updated_at descending, optionally under prefix.
	RecentKnowledge(ctx context.Context, prefix string, limit int) ([]KnowledgeHit, error)
}
```

- [ ] **Step 4:** store 实现。`SearchKnowledge` 的 trigram 与 LIKE 分支沿用 `SearchExperience` 的写法和通配符转义;`snippet()` 取 body 列。`RecentKnowledge` 的 prefix 用 `path LIKE ? ESCAPE '\'`,snippet 取 body 前 160 个字符。

- [ ] **Step 5:** 测试:
  - upsert 后能被中文、英文、2 字查询命中;再次 upsert 后旧内容搜不到;
  - delete 后 FTS 中也消失;
  - `RecentKnowledge` 的排序和 prefix 过滤;
  - `ListKnowledgeFileStats` 返回全部路径。

```bash
go test ./internal/store ./internal/core -count=1
```

---

## Task 2: `internal/knowledge`:vault、frontmatter、git、同步

**Files:** Create `internal/knowledge/{vault.go,note.go,git.go,sync.go}` 和测试;Modify `tests/architecture/dependency_direction_test.go`(`knowledge` 为 Layer 2)、`tests/architecture/structural_limits_test.go`(加入 `internal/knowledge`)。

**Why:** 知识库的全部文件语义集中在一个包里,工具、Capture 和 API 共用。

- [ ] **Step 1:** `note.go`,纯函数:

```go
type Frontmatter struct {
	Title   string
	Tags    []string
	Source  string
	Created time.Time
	Updated time.Time
}

// ParseNote splits YAML frontmatter from the body. Notes without frontmatter
// (written in Obsidian, for example) take the first "# " heading or the file
// name as title and the file mtime as Updated.
func ParseNote(path string, raw []byte, mtime time.Time) (Frontmatter, string, error)
// RenderNote writes frontmatter (title, tags, source, created, updated in that
// order, times in the owner's timezone) followed by the body.
func RenderNote(fm Frontmatter, body string) []byte
// CleanPath validates a note or attachment path: relative, slash-separated, no
// "..", not under ".git/" or ".obsidian/", notes end in ".md".
func CleanPath(path string) (string, error)
```

  frontmatter 中未知的字段在改写时原样保留(owner 在 Obsidian 里加的 `aliases` 等),`Frontmatter` 另带一个 `Extra yaml.Node` 字段承载它们。

- [ ] **Step 2:** `git.go`:

```go
type Git interface {
	// Init makes dir a repository when it is not one, sets
	// receive.denyCurrentBranch=updateInstead, and commits nothing.
	Init(ctx context.Context, dir string) error
	// Commit stages exactly paths and commits them with message. A commit
	// with no changes is not an error and returns "".
	Commit(ctx context.Context, dir string, paths []string, message string) (sha string, err error)
}

// ExecGit runs the system git binary with a fixed author
// (Acorn <acorn@localhost>) so the owner's global identity is not required.
type ExecGit struct{ Binary string }

func LookupGit() (*ExecGit, error) // exec.LookPath("git"); error names the install command
```

  命令一律带 context;`git add -- <paths>` 后 `git commit -m <msg> -- <paths>`,只提交本次涉及的文件,owner 在工作区里其他未提交的改动不受影响。stderr 带进错误信息。

- [ ] **Step 3:** `vault.go`:

```go
type Vault struct {
	dir   string
	git   Git
	index core.KnowledgeStore
	clock func() time.Time
	loc   *time.Location
	mu    sync.Mutex // serializes writes, commits and sync
}

func Open(ctx context.Context, cfg VaultConfig) (*Vault, error) // mkdir + git.Init + Sync

type WriteNote struct {
	Path, Title, Source, Body string
	Tags []string
	RunID string // goes into the commit trailer
}
func (v *Vault) Write(ctx context.Context, n WriteNote) (Note, error)        // create or replace; keeps Created
func (v *Vault) Edit(ctx context.Context, path, old, new, runID string) (Note, error) // old must occur exactly once in the body
func (v *Vault) Read(ctx context.Context, path string) (Note, error)
func (v *Vault) Search(ctx context.Context, query string, limit int) ([]core.KnowledgeHit, error) // Sync first
func (v *Vault) Recent(ctx context.Context, prefix string, limit int) ([]core.KnowledgeHit, error) // Sync first
func (v *Vault) SaveAttachment(ctx context.Context, name, mime string, data []byte, reason string) (path string, err error)
```

  - `Note` 带 `Path`、`Frontmatter`、`Body`、`Commit`(本次写入的 sha,读取时为空)。
  - 写入流程:校验路径 → 渲染 → 写临时文件后 rename → `git.Commit` → 更新索引。提交说明为 `knowledge: write <path>` 或 `knowledge: edit <path>`,带 `Acorn-Run: <run_id>` trailer。
  - 单篇笔记 body 上限 256 KiB,超出返回错误。
  - 附件存到 `attachments/YYYY/MM/<随机 id>.<ext>`,扩展名只允许 jpg、jpeg、png、webp、gif,按 mime 决定,不信任文件名。

- [ ] **Step 4:** `sync.go`:`Sync` 遍历目录下所有 `.md`(跳过 `.git/`、`.obsidian/`、`attachments/`),与 `ListKnowledgeFileStats` 比对 mtime 和 size:新增或变化的解析后 upsert,已不存在的删除。读不了或解析失败的文件返回错误并带路径,不跳过。

- [ ] **Step 5:** 测试(用临时目录和真实 `git`;CI 的 ubuntu runner 自带 git):
  - `CleanPath` 拒绝 `../x.md`、`/abs.md`、`.git/config`、`a.txt`;
  - 写入后文件内容、frontmatter、`git log -1` 的说明和 trailer 正确;再写一次保留 `created`;
  - `Edit` 的 old 不存在或出现多次时报错;
  - 无 frontmatter 的笔记按标题规则解析,未知 frontmatter 字段改写后仍在;
  - 外部新建、修改、删除文件后 `Search` 能反映;
  - 工作区里 owner 未提交的其他文件不会被提交;
  - 附件的扩展名由 mime 决定,未知 mime 报错。

```bash
go test ./internal/knowledge -count=1 && make test-architecture
```

---

## Task 3: 配置与装配

**Files:** `internal/config/*`(struct、defaults、validation、tests)、`configs/acorn.example.yaml`、`configs/acorn.selfhosted.example.yaml`、`configs/acorn.minimal.yaml`、`internal/cli/acorn.init.yaml`、`internal/wire/{container.go,runtime.go}`、`internal/cli/doctor_output.go`、`scripts/install-release.sh`

- [ ] **Step 1:** 新增配置:

```yaml
knowledge:
  dir: ""   # markdown notes, also a git repository; empty = {storage_dir}/knowledge
```

  `cfg.KnowledgeDir()` 返回展开 `~` 后的绝对路径。校验:路径存在时必须是目录。

- [ ] **Step 2:** wire 中 `knowledge.LookupGit()` 后 `knowledge.Open`;任一失败则 container 构建失败,错误带路径或安装提示。`Vault` 注入工具依赖(Task 4)和 API 服务(Task 5)。

- [ ] **Step 3:** `acorn doctor` 输出知识库目录、git 版本和已索引的笔记数。安装脚本检查 `git`,缺失时提示 `apt install git` 并退出非零。

```bash
go test ./internal/config ./internal/cli ./internal/wire -count=1
```

---

## Task 4: 知识库工具与 operating rules

**Files:** Create `internal/tools/knowledge_tools.go` 和测试;Modify `internal/tools/builtin_registry.go`(`NativeToolDeps.Knowledge KnowledgeToolDeps`)、`internal/tools/configured.go`、`internal/runtime/runner.go`(`operatingRules`)、`internal/wire/runtime.go`、`internal/wire/run_catalog_test.go`;Create seed skill `skills/capture_to_note/SKILL.md`。

- [ ] **Step 1:** 工具(全部 eager、`ToolKindNative`):

| 工具 | 参数 | 行为 |
|---|---|---|
| `knowledge_write` | `path`、`title`、`body`,可选 `tags`、`source` | 新建或整体替换一篇笔记并提交。返回 path、commit。 |
| `knowledge_edit` | `path`、`old`、`new` | 在 body 中把唯一出现的 `old` 换成 `new` 并提交。 |
| `knowledge_read` | `path` | 返回 frontmatter 与 body。 |
| `knowledge_search` | `query`,可选 `limit`(默认 10,最大 50) | 返回 path、title、tags、snippet、updated。 |
| `knowledge_list` | 可选 `prefix`、`limit` | 按更新时间倒序列出笔记,用于决定新笔记放哪。 |

  `KnowledgeToolDeps{Vault, Context}`,`Context` 提供当前 run id 写进提交 trailer。缺任何一项时注册失败。

- [ ] **Step 2:** `operatingRules` 增加:
  - 篇幅长、需要以后查阅的内容写进知识库;owner 的一句原话仍用 `keep`。
  - 写新笔记前先 `knowledge_search`,已有相关笔记就 `knowledge_edit` 补充,不另起一篇。
  - 收到 owner 分享的内容(`[capture]` 开头的输入)时,按 `capture_to_note` 技能处理。
  - 删除原规则里 `skill_list`、`skill_view` 的说法(Task 7 改为 `skill` 工具)。

- [ ] **Step 3:** `skills/capture_to_note/SKILL.md`:链接先 `tool_search` 加载 `web_fetch` 再抓取;笔记放在 `inbox/` 下,按主题命名;frontmatter 的 `source` 填原链接;正文写摘要、要点和 owner 附带的话;图片附件用 `![[attachments/...]]` 引用;完成后用一两句话回复 owner 笔记路径。

- [ ] **Step 4:** 测试:每个工具的正常与错误路径(非法路径、old 不唯一、超限);提交 trailer 带 run id;`run_catalog_test` 中新工具出现在 eager 集合里。

```bash
go test ./internal/tools ./internal/runtime ./internal/wire ./internal/skills -count=1
```

---

## Task 5: Capture 与知识库接口

**Files:** Create `internal/api/{capture_service.go,handlers_capture.go,knowledge_service.go,handlers_knowledge.go}` 和测试;Modify `internal/api/{routes.go,server.go}`、`internal/wire/container.go`、`docs/openapi.yaml`、Kotlin client 重新生成。

- [ ] **Step 1:** OpenAPI:
  - `POST /v1/captures`,`multipart/form-data`:`text`(可选,分享的文字,常含链接)、`subject`(可选,分享来源给的标题)、`image`(可选,二进制)。`text` 和 `image` 至少一个。返回 202 `CaptureAccepted{thread_id, run_id}`。
  - `GET /v1/knowledge/notes?q=&prefix=&limit=`:有 `q` 时搜索,否则按更新时间列出。返回 `{notes: [KnowledgeNoteSummary{path, title, tags, snippet, updated_at}]}`。
  - `GET /v1/knowledge/note?path=`:返回 `KnowledgeNote{path, title, tags, source, created_at, updated_at, body}`;不存在返回 404,非法路径返回 400。
  - `Message.role` 枚举增加 `capture`。

- [ ] **Step 2:** `CaptureService.Capture(ctx, CaptureInput) (CaptureAccepted, error)`:
  1. 校验:请求体上限 12 MiB,图片上限 10 MiB,mime 由内容嗅探(`http.DetectContentType`)决定,只接受 jpeg、png、webp、gif;`text` 上限 16 KiB。
  2. 有图片时 `Vault.SaveAttachment`,提交说明 `knowledge: capture image`。
  3. 新建线程,标题取 `subject`,否则取 `text` 中第一个链接的 host,否则为 "Shared image" 或 text 前 40 个字符。
  4. 组装输入:`[capture] shared from the owner's phone`,依次列出 subject、链接、其余文字、附件路径与大小。
  5. `RunService.CreateCaptureRun(ctx, threadID, input)` 起 run(Task 6),wake 说明为 `owner shared something`。
  - Capture 由 owner 发起,不计入 `wake.daily_limit`。

- [ ] **Step 3:** `KnowledgeService` 包装 `Vault.Search`、`Vault.Recent`、`Vault.Read`,投影成 DTO。

- [ ] **Step 4:** 测试:
  - multipart 正常路径:链接、纯文字、图片各一次,返回 202 且线程和 run 存在;
  - 400:都为空、图片超限、mime 不支持、text 超限;401:未认证;
  - 知识库接口:搜索、列表、读取、404、400(`../x.md`)。

```bash
go test ./internal/api ./internal/wire -count=1
cd mobile-kotlin && ./tool/generate_openapi_client.sh && ./tool/generate_openapi_client.sh --check
```

---

## Task 6: `capture` 角色

**Files:** `internal/core/store_types.go`、`internal/api/{run_service.go,projection_helpers.go,thread_service.go}`、`internal/store/store_session_messages.go`(`BindUserMessageRunIDByID`)、相关测试

- [ ] **Step 1:** `core.MessageRoleCapture = "capture"`。`createRun` 改为接收显式的 `role`;`CreateRun` 传 `user`,`CreateWakeRun` 传 `wake`,新增 `CreateCaptureRun(ctx, threadID, input)` 传 `capture` 并带 wake 说明 `owner shared something`。

- [ ] **Step 2:** `buildChatMessages` 把 `capture` 映射为 `UserMessage`;`projectMessage` 接受 `capture`;`BindUserMessageRunIDByID` 匹配 `role IN ('user','wake','capture')`。

- [ ] **Step 3:** 测试:capture run 的输入消息角色为 `capture`、绑定到该 run、进入下一轮的历史时为用户消息;INVARIANTS 中 wake 那一条扩展为 wake 与 capture。

```bash
go test ./internal/api ./internal/store ./internal/runtime -count=1
```

---

## Task 7: 技能迁到 Eino skill middleware

**Files:** Create `internal/runtime/skill_backend.go` 和测试;Modify `internal/runtime/{agent.go,runner.go,capability_assembler.go,context_helpers.go}`、`internal/tools/builtin_registry.go`、`skills/capability_recall/SKILL.md`、`skills/web_browser_research/SKILL.md`(如提到旧工具);Delete `internal/skills/tools.go`、`skillCatalogBrief` 及相关测试。

- [ ] **Step 1:** `skillBackend` 实现 `skill.Backend`,数据来自本次 run 的 `skills.Snapshot`(已按工具可用性过滤):`List` 返回 eligible 技能的 `FrontMatter{Name: id, Description}`;`Get` 返回 `Skill{FrontMatter, Content: SKILL.md 正文, BaseDirectory}`。`Context` 恒为空,即 inline 模式。

- [ ] **Step 2:** `skill.NewMiddleware(ctx, &skill.Config{Backend, CustomSystemPrompt})`,工具名保持默认 `skill`;system prompt 用我们自己的一段说明(遇到匹配技能描述的任务先加载技能再动手)。handler 顺序变为:patchtoolcalls → summarization → reduction → toolsearch(可选)→ skill → presence → approval → tool errors。`agent_test.go` 的顺序断言同步更新。

- [ ] **Step 3:** 删除 `skill_list`、`skill_view`(`builtinToolOrder` 只剩 `ask_operator`)、`skills.BuildAgentTools`、`skillCatalogBrief`;`buildAgentInstruction` 只拼 persona 和 operating rules。`/v1/skills` 和 `acorn skills` 不变。

- [ ] **Step 4:** 测试:
  - 脚本化模型调用 `skill{skill: "capture_to_note"}`,工具结果是该技能正文;
  - 不 eligible 的技能不出现在 `skill` 工具描述里;
  - `skill` 工具的调用也经过 approval 与 tool errors 包装(工具不存在的技能返回模型可见的错误,run 不失败);
  - presence 快照的 Instruction 包含 skill middleware 追加的说明。
  - deadcode 无新增。

```bash
go test ./internal/runtime ./internal/skills ./internal/tools ./internal/wire -count=1 && deadcode ./...
```

---

## Task 8: 手机端分享入口与 capture 卡片

**Files:**
- `AndroidManifest.xml`:新增 `ShareActivity`,`intent-filter` 接 `ACTION_SEND` 的 `text/plain` 与 `image/*`
- Create `feature/share/{ShareActivity.kt,ShareViewModel.kt,ShareSheet.kt}`、`data/repository/CaptureRepository.kt`
- `feature/chat/{ChatMessage.kt,ChatScreen.kt}`:`ChatMessage.Capture` 与卡片
- 相关单元测试

- [ ] **Step 1:** `ShareActivity` 以底部面板样式打开:显示链接或文字预览、图片缩略图、可选的附言输入框和"发送"按钮。附言拼在 `text` 末尾。未配对时显示"请先在 Acorn 中配对"并提供打开主界面的按钮。

- [ ] **Step 2:** 图片从 content URI 读入,写到 cache 目录的临时文件后作为 multipart 上传,上传结束删除临时文件;大于 10 MiB 时在面板里直接提示,不发请求。请求在 IO 线程执行。

- [ ] **Step 3:** 发送成功后面板显示"已交给 Acorn"和"打开对话"按钮(经 `DeepLinks.openThread` 进入新线程),2 秒后自动关闭;失败时显示服务端返回的错误并可重试。

- [ ] **Step 4:** 聊天页把 `capture` 消息画成"Shared from your phone"卡片:分享图标、链接、文字,附件以文件名占位。`chatMessageFrom` 增加 `capture` 分支。

- [ ] **Step 5:** 单元测试:分享 intent 解析(text、subject、单张图片、不支持的类型);`chatMessageFrom("capture", ...)`;超限图片不发请求。

```bash
cd mobile-kotlin && JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home ./gradlew test assembleDebug
```

---

## Task 9: 手机端知识库页

**Files:** Create `feature/knowledge/{KnowledgeScreen.kt,KnowledgeViewModel.kt,NoteScreen.kt}`、`data/repository/KnowledgeRepository.kt`;Modify `feature/shell/{AcornShell.kt,ShellViewModel.kt}`(新增 Knowledge tab,位于 Threads 与 Settings 之间)。

- [ ] **Step 1:** 列表页:顶部搜索框(输入停止 300ms 后搜索),空搜索时显示最近更新的笔记;每行显示标题、路径、更新时间、片段。加载中、加载失败(带重试)、空结果分别有状态,沿用 P1 线程列表的 `Load`/`Content` 写法。

- [ ] **Step 2:** 笔记页:标题、标签、来源链接(可点开浏览器)、更新时间,正文用 `compose-markdown` 渲染;`![[...]]` 附件引用替换成"附件:<文件名>"占位文字。

- [ ] **Step 3:** 单元测试:ViewModel 的搜索去抖与状态切换;附件引用替换。

```bash
cd mobile-kotlin && ./gradlew test assembleDebug
```

---

## Task 10: 验收测试与文档

**Files:** Create `internal/wire/capture_acceptance_e2e_test.go`;Modify `docs/adr/0003-personal-agent-direction.md`、`AGENTS.md`、`docs/architecture/INVARIANTS.md`、`docs/user/self-hosted-onboarding.md`、README

- [ ] **Step 1:** 验收测试,沿用 `wake_acceptance_e2e_test.go` 的 harness(fake OpenAI、可控时钟、`writeTestConfig`),另起一个 `httptest` 页面服务作为被分享的链接,配置 `web_access.allow_private_networks: true`:
  1. 以 multipart 调 `POST /v1/captures`,`text` 为该页面链接,`subject` 为页面标题。
  2. 脚本化模型依次调用 `skill{capture_to_note}`、`tool_search{select:web_fetch}`、`web_fetch`、`knowledge_search`、`knowledge_write{path: "inbox/<slug>.md", source: <链接>}`,最后回复。
  3. 断言:新线程的消息角色依次为 `capture, assistant`;知识库目录里出现该文件,frontmatter 的 `source` 正确;`git log -1` 的说明为 `knowledge: write inbox/<slug>.md` 且带该 run 的 trailer;`GET /v1/knowledge/notes?q=<页面里的词>` 能搜到;`wake.fired` 事件为 0 条。
  - 另一个用例:图片 capture,附件文件存在并已提交,run 输入里含附件路径。
  - 另一个用例:在知识库目录里直接新建一个无 frontmatter 的笔记,`GET /v1/knowledge/notes` 能列出,标题取自第一个标题行。

- [ ] **Step 2:** 文档:
  - ADR-0003:P2 行注明检索只有关键词,向量检索待定;§Runtime 的技能写法改为 skill middleware 已落地;§App 注明 P2 的知识库页只读。
  - `AGENTS.md`:项目概览、关键包(15 个,加 knowledge)、两套真相(知识库文件是知识的真相,`knowledge_notes` 是索引)、表数量与表名、middleware 顺序、工具 & 技能一节(`skill` 工具)、Remote API 一节(captures 与 knowledge 接口)、自托管发布(依赖 git)。
  - INVARIANTS:知识写入必有一个 git 提交、只提交本次涉及的文件、索引可由文件重建且反映外部改动、路径不能逃出知识库、capture 不计入每日上限、middleware 顺序。每条对应 Task 2、5、6、7、10 的测试。
  - onboarding:`knowledge.dir`;服务器需要 git;用 `git clone ssh://<vps>/<dir>` 在本地 Obsidian 打开、改完 push 回去(`updateInstead` 已配置);App 分享入口的用法。

```bash
go test ./internal/wire -run 'Capture|Wake|Approval' -count=1 -race && make test-architecture
```

---

## Task 11: 全量门禁 + 模拟器验收

- [ ] 全量门禁:

```bash
make format-check && make lint && go test -race ./... && make test-architecture
deadcode ./... && deadcode -test ./...
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug
```

- [ ] 模拟器验收(`acorn-gms` AVD,`/tmp/acorn-verify` 的 serve 配置加上 `knowledge.dir`,fake 模型脚本按 Task 10 的流程扩展):
  1. 在模拟器的浏览器里打开一个页面,点分享,系统分享面板里出现 Acorn;选 Acorn,面板显示预览,加一句附言后发送,显示"已交给 Acorn"。
  2. 点"打开对话",线程里第一条是"Shared from your phone"卡片,随后是 agent 的回复。
  3. VPS 侧(本机)知识库目录出现笔记,`git log` 有对应提交。
  4. App 的知识库页搜到这篇笔记,打开后正文正常渲染。
  5. 从相册分享一张图片,附件出现在 `attachments/` 并已提交。
- [ ] 真实模型验收:用 owner 现有 provider 配置跑一次第 1–4 步,确认真实模型能按技能完成整理(会消耗少量 token)。

---

## Risks

- **系统 git 依赖。** 精简的 VPS 镜像可能没装 git;安装脚本和 serve 启动都会明确报错并给出安装命令。
- **索引同步的代价。** 每次搜索都遍历知识库目录做 stat;几千篇笔记在毫秒级,数万篇后需要改成文件监听或定时同步。
- **owner 与 agent 同时改同一文件。** owner push 时如果 agent 刚好在写,`updateInstead` 会因工作区有改动而拒绝 push,owner 重试即可;agent 只提交自己写的文件,不会覆盖 owner 未提交的改动,但会覆盖同一文件的内容,git 历史可找回。
- **链接抓取失败。** 需要登录或有反爬的页面抓不到正文;按技能规则,agent 仍然建一篇只含链接、标题和附言的笔记,并在回复里说明没抓到正文。
- **图片不被理解。** 图片笔记只有 owner 的附言;以后接入多模态时,capture 输入格式不变,只需让 run 把附件作为图片输入。

## Retirement

- `skill_list`、`skill_view`、`skills.BuildAgentTools`、`skillCatalogBrief` 删除。
- Instruction 中的技能目录摘要删除,由 skill middleware 的说明和 `skill` 工具描述取代。
