# P0: Runtime 迁到 Eino ChatModelAgent + 可持久审批

**Goal:** 用 Eino `ChatModelAgent` + 官方 middleware 取代自研 `direct_response` 运行时;审批变成绑定具体调用参数、跨进程重启仍可恢复的工具级中断;owner 在手机上批准后服务端自动续跑。

**Architecture:** `RunnerFactory` 为每个 run 组装一个 `adk.Runner{Agent: ChatModelAgent, EnableStreaming: true, CheckPointStore: SQLite}`。Handlers 顺序:patchtoolcalls → summarization → reduction(clear-only) → toolsearch(有 deferred 工具时) → approval → 调用方附加。Executor 把 `AgentEvent` 投影成既有 RunEvent(`assistant.delta` / `run.*`)。`PendingActionService.Decide` 在该 run 没有剩余 pending 时后台调用 `RunResumeService`。

**Tech Stack:** Go 1.27、cloudwego/eino v0.9.21(`adk`、`adk/middlewares/{summarization,reduction,dynamictool/toolsearch,patchtoolcalls}`、`components/tool` interrupt API、`compose.GetToolCallID`)、modernc.org/sqlite、Kotlin generated client。

**Baseline / Authority Refs:**
- `docs/adr/0003-personal-agent-direction.md`(P0 行、§Runtime、§输出与审批)
- `AGENTS.md`(硬边界、验证要求)、`docs/architecture/INVARIANTS.md`
- `docs/openapi.yaml`(pending action kind enum、`/v1/runs/{run_id}:resume`)

**Compatibility Boundary:**
- 保持不变:`/v1` 其余路径与 DTO、live RunEvent kind 集合与 payload 形状(`run.started`、`assistant.delta`、`run.completed`、`run.failed`、`run.interrupted`、`run.resume_requested`、`operator_question.*`)、`ask_operator` 的 interrupt/resume 语义、SQLite 既有 10 张表的列。
- 有意破坏(hard cut,无兼容层):删除 `POST /v1/runs/{run_id}:resume`;删除配置 `context.preserve_recent_turns`、整个 `tools:` 块(`workspace` / `mutation` / `run_command`);新增配置 `approval.require`;pending action kind 新增 `tool_approval`。旧数据库无需迁移(新表 `CREATE TABLE IF NOT EXISTS`)。

**TDD Route:**
- Mode: off
- Decision: skipped
- Strict authority: not applicable
- Test posture: post-change regression(每个 Task 写针对性测试并跑通)
- Reason: 用户 TDD 模式为 off,未请求 strict。
- Verification: 每个 Task 末尾的命令;全量门禁见 Task 11。

**Verification(全量):** `make format-check && make lint && make test && go test -race ./internal/runtime ./internal/api ./internal/wire && make test-architecture`;`cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test assembleDebug`;模拟器或真机手动验收(Task 11)。

---

## Plan Basis

- **Requirement Ready Check:** ready。来源 ADR-0003 P0 行;验收 = "手机上触发一个需审批的调用,批准后执行;中途重启服务后批准依然生效"。
- **Change Necessity:** code-change。审批链路在当前代码里不可用(`run_command` 永久拦截、resume 时 checkpoint 丢失、mobile 从不调用 `:resume`),只能改代码修复;最小边界 = `internal/runtime`、`internal/store`、`internal/core`、`internal/api`、`internal/config`、`internal/tools`、`internal/wire`、`docs/openapi.yaml`、generated Kotlin client。
- **Existence Check:**
  - 新表 `agent_checkpoints`:add-with-proof。Eino `CheckPointStore` 需要跨进程持久化,现有表无合适归属。生命周期:run 进入 succeeded/failed 时删除对应行。
  - 新 middleware `approval`:add-with-proof。取代 `tools.ClassifyRisk` + `ApprovalRequiredError` 两处旧实现(Task 7 删除)。
  - 新 pending kind `tool_approval`:复用 elicitation 的 accept/decline 投影与决策构造,只新增 kind 常量与分支。
- **Architecture Integrity:** 上下文压缩、masking、延迟加载工具、工具调度的唯一 owner 移交给 Eino middleware 与 `ToolsNode`;`Session` / `dispatch` / `tool_lifecycle` 整体删除,不并存。工具串行执行(`ExecuteSequentially: true`):剩余工具里 `browser` 是单实例有状态服务,路径冲突调度失去存在理由。
- **Complexity:** `internal/runtime` 净删除约 3000 行;新文件 `agent.go`、`approval.go`、`events.go`、`checkpoint.go` 各 < 250 行,满足 `structural_limits_test.go` 的 800 行上限。

## Execution Readiness View

- **Intent Lock:** 只做 ADR-0003 P0;不碰 memory / WorldState / Periodic Review / skills 加载方式(P1 处理)。
- **Scope Fence:** 不改 mobile UI 代码(仅重新生成 client);不新增工具;`memory_*` 工具所依赖的文件工具实现与 `internal/workspace` 的 `Workspace` / mutation checkpoint 保留到 P1。
- **Baseline Lock:** 起点 commit = 分支 `direction/personal-agent` 上的 `c46764a`。
- **Task Batches:** T1–T3(新能力,旧路径仍在)→ T4–T6(切换)→ T7–T8(删除)→ T9–T11(验收 + 文档)。
- **Drift / Rewind Rules:** 如果 T3 证明 `WrapInvokableToolCall` 包装后的 endpoint 拿不到 interrupt state,停止并回到设计(改为在 `BuildAuditedTools` 层包装工具),不在 middleware 里绕路。
- **Evidence Required Before Completion:** Task 9 的 restart 验收测试通过;Task 11 全量门禁通过;模拟器手动验收有截图或明确说明未验证。

## Task 0: 记录基线

- [ ] 运行并保存基线,用于 Task 7/8 对比:

```bash
deadcode -test ./... > /tmp/acorn-deadcode-before.txt; wc -l /tmp/acorn-deadcode-before.txt
make test 2>&1 | tail -5
```

预期:`make test` 全部 ok。

---

## Task 1: SQLite 持久化 CheckPointStore

**Files:**
- Modify: `internal/store/store_schema_bootstrap.go`、`internal/store/store_schema.go`、`internal/core/store.go`、`internal/api/store_stub_test.go`
- Create: `internal/store/store_checkpoint.go`、`internal/store/store_checkpoint_test.go`、`internal/runtime/checkpoint.go`

**Why:** interrupt 后的 agent 状态必须跨 runner 实例、跨进程存活。

- [ ] **Step 1:** `store_schema_bootstrap.go` 在 `storeBootstrapTables` 末尾(`artifacts` 表之后)追加,并把文件顶部注释里的 "10 core tables" 改成 "11 core tables":

```sql
CREATE TABLE IF NOT EXISTS agent_checkpoints (
    checkpoint_id TEXT PRIMARY KEY,
    data BLOB NOT NULL,
    updated_at TEXT NOT NULL
);
```

- [ ] **Step 2:** `store_schema.go` 的 `schemaRequiredTables` 增加一项:

```go
	"agent_checkpoints": {"checkpoint_id", "data", "updated_at"},
```

- [ ] **Step 3:** 新建 `internal/store/store_checkpoint.go`:

```go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (s *Store) LoadCheckpoint(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT data FROM agent_checkpoints WHERE checkpoint_id = ?`, checkpointID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load checkpoint %s: %w", checkpointID, err)
	}
	return data, true, nil
}

func (s *Store) SaveCheckpoint(ctx context.Context, checkpointID string, data []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_checkpoints(checkpoint_id, data, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(checkpoint_id) DO UPDATE SET data = excluded.data, updated_at = excluded.updated_at`,
		checkpointID, data, formatTimestamp(time.Now().UTC()),
	)
	if err != nil {
		return fmt.Errorf("save checkpoint %s: %w", checkpointID, err)
	}
	return nil
}

func (s *Store) DeleteCheckpoint(ctx context.Context, checkpointID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_checkpoints WHERE checkpoint_id = ?`, checkpointID); err != nil {
		return fmt.Errorf("delete checkpoint %s: %w", checkpointID, err)
	}
	return nil
}
```

- [ ] **Step 4:** `internal/core/store.go` 的 `SessionStore` 末尾追加(`ListPendingActionsByRun` 已在 `*store.Store` 上实现,这里只上提到接口,供 Task 6 使用):

```go
	ListPendingActionsByRun(ctx context.Context, runID string) ([]PendingActionRecord, error)

	// --- Agent checkpoints ---
	LoadCheckpoint(ctx context.Context, checkpointID string) ([]byte, bool, error)
	SaveCheckpoint(ctx context.Context, checkpointID string, data []byte) error
	DeleteCheckpoint(ctx context.Context, checkpointID string) error
```

- [ ] **Step 5:** `internal/api/store_stub_test.go` 的 `unimplementedStore` 追加四个方法,风格与已有方法一致(返回 `errors.New("not implemented")`)。然后运行 `go vet ./...`,确认没有其他 `SessionStore` 实现缺方法。

- [ ] **Step 6:** 新建 `internal/runtime/checkpoint.go`:

```go
package runtime

import (
	"context"

	"github.com/ycvk/acorn/internal/core"
)

// storeCheckpointStore persists adk checkpoints through the session store so
// an interrupted run can resume in a fresh runner or a restarted process.
type storeCheckpointStore struct {
	store core.SessionStore
}

func (s storeCheckpointStore) Get(ctx context.Context, checkpointID string) ([]byte, bool, error) {
	return s.store.LoadCheckpoint(ctx, checkpointID)
}

func (s storeCheckpointStore) Set(ctx context.Context, checkpointID string, data []byte) error {
	return s.store.SaveCheckpoint(ctx, checkpointID, data)
}
```

- [ ] **Step 7:** 新建 `internal/store/store_checkpoint_test.go`。覆盖四种情况:不存在 → `ok=false`;保存后读回;覆盖写;删除后不存在;关闭再 `Open` 同一目录后仍能读回:

```go
package store

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestCheckpointRoundTripSurvivesReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	ctx := context.Background()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, ok, err := s.LoadCheckpoint(ctx, "run_1"); err != nil || ok {
		t.Fatalf("missing checkpoint: ok=%v err=%v", ok, err)
	}
	if err := s.SaveCheckpoint(ctx, "run_1", []byte("v1")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := s.SaveCheckpoint(ctx, "run_1", []byte("v2")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	data, ok, err := s.LoadCheckpoint(ctx, "run_1")
	if err != nil || !ok || !bytes.Equal(data, []byte("v2")) {
		t.Fatalf("after reopen: data=%q ok=%v err=%v", data, ok, err)
	}
	if err := s.DeleteCheckpoint(ctx, "run_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := s.LoadCheckpoint(ctx, "run_1"); ok {
		t.Fatal("checkpoint still present after delete")
	}
}
```

- [ ] **Verify:** `go test ./internal/store ./internal/api ./internal/core && go build ./...`
- [ ] **Commit:** `feat(store): persist agent checkpoints in SQLite`

---

## Task 2: `tool_approval` pending kind + `approval.require` 配置 + API 决策/续跑目标

**Files:**
- Modify: `internal/core/domain.go`、`internal/config/config.go`、`internal/config/config_defaults.go`、`internal/config/config_validate.go`、`internal/api/inbox_service.go`、`internal/api/pending_action_service.go`、`internal/api/pending_action_service_decision.go`、`internal/api/run_resume_service.go`、`docs/openapi.yaml`、`configs/*.yaml`
- Regenerate: `mobile-kotlin/app/src/main/java/io/ycvk/acorn/api/`
- Test: `internal/api/client_pending_actions_test.go`、`internal/config/*_test.go`

- [ ] **Step 1:** `core/domain.go`:

```go
	PendingActionKindToolApproval     PendingActionKind = "tool_approval"
```

- [ ] **Step 2:** `config.go` 在 `Config` 中加入 `Approval ApprovalConfig \`yaml:"approval"\``:

```go
// ApprovalConfig lists tool-name glob patterns (path.Match syntax) whose calls
// pause for owner approval before executing.
type ApprovalConfig struct {
	Require []string `yaml:"require"`
}
```

  `config_defaults.go` 设默认值 `Approval: ApprovalConfig{Require: []string{"browser", "mcp__*"}}`。`config_validate.go` 的 `ValidateBase` 里逐条校验 `path.Match(p, "")` 不报错,报错信息写成 `approval.require[%d] %q: %w`。

- [ ] **Step 3:** `inbox_service.go`:把 `buildElicitationPendingActionSummary` 改名为 `buildAcceptDeclinePendingActionSummary`,并让 `buildPendingActionSummary` 的 `case core.PendingActionKindElicitation, core.PendingActionKindToolApproval:` 都走它。

- [ ] **Step 4:** `pending_action_service_decision.go`:把 `buildElicitationDecision` 改为 `buildAcceptDeclineDecision(record, input, eventKind string)`,事件名由参数传入。`pending_action_service.go` 的 `buildPendingActionDecision` 分别以 `"elicitation.decided"` 和 `"tool_approval.decided"` 调用它。

- [ ] **Step 5:** `run_resume_service.go` 的 `resumeTargetsForContext`:删除 `"run_command_pause"` 分支,新增 `case "tool_approval": return s.toolApprovalTargets(ctx, runID, interrupt)`。

```go
func (s *RunResumeService) toolApprovalTargets(ctx context.Context, runID string, interrupt resumeInterruptContext) (map[string]any, error) {
	actionID := interruptInfoField(interrupt.Info, "action_id")
	if actionID == "" {
		return nil, fmt.Errorf("run %s interrupt %s tool_approval is missing action_id", runID, interrupt.ID)
	}
	record, err := s.store.LoadPendingAction(ctx, actionID)
	if err != nil {
		return nil, err
	}
	if record.RunID != runID || record.Kind != core.PendingActionKindToolApproval {
		return nil, fmt.Errorf("run %s interrupt %s: action %s is %s of run %s", runID, interrupt.ID, actionID, record.Kind, record.RunID)
	}
	if record.Status != core.PendingActionStatusApproved && record.Status != core.PendingActionStatusRejected {
		return nil, fmt.Errorf("run %s interrupt %s: tool_approval action %s has status %q", runID, interrupt.ID, actionID, record.Status)
	}
	var decision map[string]any
	if err := json.Unmarshal([]byte(record.DecisionJSON), &decision); err != nil {
		return nil, fmt.Errorf("run %s interrupt %s: tool_approval action %s decision_json: %w", runID, interrupt.ID, actionID, err)
	}
	decision["action_id"] = actionID
	return map[string]any{interrupt.ID: decision}, nil
}
```

- [ ] **Step 6:** `docs/openapi.yaml` 第 1616 行附近 `enum: [elicitation, operator_question]` 改为 `enum: [elicitation, operator_question, tool_approval]`。然后重新生成客户端:

```bash
cd mobile-kotlin && ./tool/generate_openapi_client.sh && ./tool/generate_openapi_client.sh --check
```

- [ ] **Step 7:** 三份配置模板(`acorn.example.yaml`、`acorn.selfhosted.example.yaml`、`acorn.minimal.yaml`)加入带注释的 `approval:` 块:

```yaml
approval:
  # Tool-name glob patterns that pause for owner approval on the phone.
  require:
    - browser
    - "mcp__*"
```

- [ ] **Step 8:** 测试(在 `client_pending_actions_test.go` 沿用现有的 stub store 写法):
  - `tool_approval` 记录投影出 `accept` / `decline` 两个 option,`Body` 等于 payload 里的 `message`;
  - `Decide(accept)` 写入 `{"action":"accept"}`,并追加 `tool_approval.decided` 事件;
  - 配置测试:非法 pattern `"["` 使 `ValidateBase` 返回包含 `approval.require[0]` 的错误。

- [ ] **Verify:** `go test ./internal/api ./internal/config ./internal/core && (cd mobile-kotlin && ./tool/generate_openapi_client.sh --check)`
- [ ] **Commit:** `feat(api): add tool_approval pending actions and approval.require config`

---

## Task 3: approval middleware

**Files:**
- Create: `internal/runtime/approval.go`、`internal/runtime/approval_test.go`、`internal/runtime/scripted_model_test.go`
- Modify: `internal/runtime/types.go`(`RegisterTypes`)

- [ ] **Step 1:** 新建 `internal/runtime/approval.go`:

```go
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
)

const toolApprovalInterruptKind = "tool_approval"

// toolApprovalState is checkpointed with the interrupt so the resumed call can
// prove it executes exactly the arguments the owner approved.
type toolApprovalState struct {
	ActionID  string
	ToolName  string
	Arguments string
}

type approvalStore interface {
	CreatePendingAction(ctx context.Context, input core.PendingActionInput) (*core.PendingActionRecord, error)
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

// approvalMiddleware pauses tool calls whose names match an approval pattern.
// The first execution records a tool_approval pending action and interrupts;
// on resume an accepted call runs with the recorded arguments and a declined
// call returns a notice to the model instead of executing.
type approvalMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	patterns []string
	store    approvalStore
}

func newApprovalMiddleware(patterns []string, store approvalStore) (*approvalMiddleware, error) {
	if store == nil {
		return nil, errors.New("approval middleware requires a store")
	}
	for i, p := range patterns {
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("approval pattern [%d] %q: %w", i, p, err)
		}
	}
	return &approvalMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		patterns:                     slices.Clone(patterns),
		store:                        store,
	}, nil
}

func (m *approvalMiddleware) requiresApproval(toolName string) bool {
	for _, p := range m.patterns {
		if ok, _ := path.Match(p, toolName); ok {
			return true
		}
	}
	return false
}

func (m *approvalMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	if !m.requiresApproval(tCtx.Name) {
		return endpoint, nil
	}
	return func(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
		wasInterrupted, hasState, state := einotool.GetInterruptState[toolApprovalState](ctx)
		if !wasInterrupted {
			return "", m.requestApproval(ctx, tCtx, args)
		}
		if !hasState {
			return "", fmt.Errorf("tool approval resume for %s has no saved state", tCtx.Name)
		}
		if state.Arguments != args {
			return "", fmt.Errorf("tool approval %s: arguments changed between interrupt and resume", state.ActionID)
		}
		isTarget, hasData, data := einotool.GetResumeContext[map[string]any](ctx)
		if !isTarget {
			return "", einotool.StatefulInterrupt(ctx, approvalInterruptInfo(state), state)
		}
		if !hasData {
			return "", fmt.Errorf("tool approval %s resumed without a decision", state.ActionID)
		}
		switch decision, _ := data["action"].(string); decision {
		case "accept":
			return endpoint(ctx, args, opts...)
		case "decline":
			return fmt.Sprintf("The owner declined this %s call. Do not retry it; continue without it or ask the owner what to do instead.", tCtx.Name), nil
		default:
			return "", fmt.Errorf("tool approval %s: unsupported decision %q", state.ActionID, decision)
		}
	}, nil
}

func (m *approvalMiddleware) requestApproval(ctx context.Context, tCtx *adk.ToolContext, args string) error {
	runID := core.CurrentRunID(ctx)
	if runID == "" {
		return errors.New("tool approval requires a run id in context")
	}
	state := toolApprovalState{
		ActionID:  "tool_approval:" + runID + ":" + tCtx.CallID,
		ToolName:  tCtx.Name,
		Arguments: args,
	}
	payloadJSON, err := json.Marshal(map[string]any{
		"message":   fmt.Sprintf("%s wants to run with arguments:\n%s", tCtx.Name, args),
		"tool_name": tCtx.Name,
		"arguments": args,
	})
	if err != nil {
		return fmt.Errorf("marshal tool approval payload: %w", err)
	}
	if _, err := m.store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    state.ActionID,
		RunID:       runID,
		Kind:        core.PendingActionKindToolApproval,
		Subject:     "Approve " + tCtx.Name,
		PayloadJSON: string(payloadJSON),
		Status:      core.PendingActionStatusPending,
		Reason:      "approval.require",
	}); err != nil {
		return err
	}
	if _, err := m.store.AppendEvent(ctx, runID, "tool_approval.pending", map[string]any{
		"action_id": state.ActionID,
		"tool_name": tCtx.Name,
		"arguments": args,
	}); err != nil {
		return fmt.Errorf("append tool_approval.pending event: %w", err)
	}
	return einotool.StatefulInterrupt(ctx, approvalInterruptInfo(state), state)
}

func approvalInterruptInfo(state toolApprovalState) map[string]any {
	return map[string]any{
		"kind":      toolApprovalInterruptKind,
		"action_id": state.ActionID,
		"tool_name": state.ToolName,
	}
}
```

- [ ] **Step 2:** `types.go` 的 `RegisterTypes` 里加 `gob.Register(toolApprovalState{})`,同时删掉 `gob.Register(&DirectResponseInterruptData{})`(Task 7 删除该类型时会被一并清理,这里先删,避免两步改同一行)。

- [ ] **Step 3:** 新建 `internal/runtime/scripted_model_test.go`(Task 3/5/9 共用):

```go
package runtime

import (
	"context"
	"fmt"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// scriptedModel replays fixed assistant replies and records every input.
type scriptedModel struct {
	mu      sync.Mutex
	replies []*schema.Message
	inputs  [][]*schema.Message
}

func (m *scriptedModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs = append(m.inputs, input)
	if len(m.inputs) > len(m.replies) {
		return nil, fmt.Errorf("scripted model exhausted after %d replies", len(m.replies))
	}
	return m.replies[len(m.inputs)-1], nil
}

func (m *scriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func toolCallReply(callID, name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{ID: callID, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
}
```

- [ ] **Step 4:** 新建 `internal/runtime/approval_test.go`。用一个测试内 map 实现 `adk.CheckPointStore`,两个独立的 `adk.Runner` 共享它,模拟重启:
  - runner A:模型先调用 `echo_tool({"v":1})`。断言事件流里出现 `Action.Interrupted`,其 `InterruptContexts[0].Info` 的 `kind == "tool_approval"`;fake store 收到一条 `tool_approval` pending action;工具调用计数 = 0。
  - runner B:新建 agent、新建 scriptedModel(回复 `"done"`),调用 `ResumeWithParams(ctx, runID, &adk.ResumeParams{Targets: {interruptID: {"action":"accept"}}})`。断言工具被调用 1 次,参数为 `{"v":1}`;模型第二次输入里有一条 tool message,内容等于工具输出。
  - decline 用例:工具调用 0 次,tool message 包含 `"The owner declined"`。
  - `core.WithRunID(ctx, runID)` 绑定到 Run / Resume 的 ctx 上。
  - fake store:`type approvalTestStore struct{ actions []core.PendingActionInput; events []string }`,实现 `approvalStore` 的两个方法。

  agent 构造:

```go
agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
	Name:        "approval_test",
	Description: "approval test agent",
	Model:       model,
	ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []einotool.BaseTool{echo}, ExecuteSequentially: true}},
	Handlers:    []adk.ChatModelAgentMiddleware{mw},
	MaxIterations: 5,
})
runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true, CheckPointStore: cps})
```

- [ ] **Stop condition(Drift Rule):** 如果 runner B 的 resume 在 wrapper 里得到 `wasInterrupted == false`,停止执行本计划,回报设计变更需求。

- [ ] **Verify:** `go test ./internal/runtime -run 'TestApproval' -race -count=1`
- [ ] **Commit:** `feat(runtime): approval middleware with tool-level interrupts`

---

## Task 4: Agent 组装切到 ChatModelAgent

**Files:**
- Create: `internal/runtime/agent.go`
- Modify: `internal/runtime/runner.go`、`internal/runtime/types.go`、`internal/runtime/plane.go`、`internal/runtime/capability_assembler.go`、`internal/runtime/model_builder.go`、`internal/config/config.go`、`internal/config/config_defaults.go`、`internal/config/config_validate.go`、`configs/*.yaml`

- [ ] **Step 1:** 新建 `internal/runtime/agent.go`:

```go
package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/dynamictool/toolsearch"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/tools"
)

type agentRunnerRequest struct {
	RunID             string
	ChatModel         einomodel.BaseChatModel
	Catalog           *tools.Catalog
	AllowedToolNames  []string
	ExcludedToolNames []string
	InstructionSuffix string
}

func buildAgentRunner(ctx context.Context, deps RuntimeDeps, req agentRunnerRequest) (*adk.Runner, error) {
	built, err := BuildAuditedTools(ctx, deps.Store, req.Catalog.EnabledSpecs(), req.ExcludedToolNames, req.AllowedToolNames, req.RunID)
	if err != nil {
		return nil, err
	}
	eager, deferred, err := splitToolsByLoading(ctx, req.Catalog, built)
	if err != nil {
		return nil, err
	}
	handlers, err := buildAgentHandlers(ctx, deps, req.ChatModel, deferred)
	if err != nil {
		return nil, err
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        deps.Config.Agent.Name,
		Description: deps.Config.Agent.Description,
		Instruction: buildStableInstruction(deps.Config.Agent.SystemPrompt, req.InstructionSuffix),
		Model:       req.ChatModel,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools:               eager,
			ExecuteSequentially: true,
		}},
		MaxIterations: deps.Config.Agent.MaxIterations,
		Handlers:      handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("build chat model agent: %w", err)
	}
	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: storeCheckpointStore{store: deps.Store},
	}), nil
}

// splitToolsByLoading separates eager tools from deferred ones; deferred tools
// are only reachable through the tool_search middleware.
func splitToolsByLoading(ctx context.Context, catalog *tools.Catalog, built []einotool.BaseTool) (eager, deferred []einotool.BaseTool, err error) {
	modes := make(map[string]core.ToolLoadingMode)
	for _, spec := range catalog.EnabledSpecs() {
		modes[strings.TrimSpace(spec.Name)] = spec.Loading.Mode
	}
	for _, t := range built {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read tool info: %w", err)
		}
		switch modes[info.Name] {
		case core.ToolLoadingModeDeferred:
			deferred = append(deferred, t)
		case core.ToolLoadingModeHidden:
		default:
			eager = append(eager, t)
		}
	}
	return eager, deferred, nil
}

func buildAgentHandlers(ctx context.Context, deps RuntimeDeps, chatModel einomodel.BaseChatModel, deferred []einotool.BaseTool) ([]adk.ChatModelAgentMiddleware, error) {
	window := deps.Config.Context.WindowTokens
	patch, err := patchtoolcalls.New(ctx, &patchtoolcalls.Config{})
	if err != nil {
		return nil, fmt.Errorf("patchtoolcalls middleware: %w", err)
	}
	summarize, err := summarization.New(ctx, &summarization.Config{
		Model:   chatModel,
		Trigger: &summarization.TriggerCondition{ContextTokens: window - deps.Config.Context.CompactMarginTokens},
	})
	if err != nil {
		return nil, fmt.Errorf("summarization middleware: %w", err)
	}
	reduce, err := reduction.New(ctx, &reduction.Config{
		SkipTruncation:            true,
		MaxTokensForClear:         int64(window / 2),
		ClearRetentionSuffixLimit: deps.Config.Context.MaskAfterTurns,
	})
	if err != nil {
		return nil, fmt.Errorf("reduction middleware: %w", err)
	}
	approval, err := newApprovalMiddleware(deps.Config.Approval.Require, deps.Store)
	if err != nil {
		return nil, err
	}
	handlers := []adk.ChatModelAgentMiddleware{patch, summarize, reduce}
	if len(deferred) > 0 {
		search, err := toolsearch.New(ctx, &toolsearch.Config{DynamicTools: deferred})
		if err != nil {
			return nil, fmt.Errorf("toolsearch middleware: %w", err)
		}
		handlers = append(handlers, search)
	}
	handlers = append(handlers, approval)
	return append(handlers, deps.Handlers...), nil
}
```

  执行时先用 `go doc` 核对 `summarization.Config`、`reduction.Config`、`patchtoolcalls.Config` 的字段名。如果 `patchtoolcalls.New` 接受 nil config,就传 nil。

- [ ] **Step 2:** `runner.go`:
  - `newDirectResponseRunner` 改名为 `newAgentRunner`,内部调用 `buildAgentRunner`;
  - `ActiveRunner` 改为 `{Mcp, Runner, ChatModel, ContextMessages []adk.Message, RunID, ToolCatalog, CloseRunTools}`,删除 `SelectedSkill`、`Instruction`、`ContextResult`、`ContextSession`;
  - `ContextMessages` 取自 `assembleContext(...).Messages`;
  - 删除 `ambientAgentInstruction` 常量,`buildStableInstruction` 只拼 system prompt、`capabilityDiscoveryInstruction` 和 suffix;
  - 删除 `inMemoryCheckpointStore`、`newInMemoryCheckpointStore`。

- [ ] **Step 3:** `plane.go`:`AssembleResult` 只保留 `Messages []*schema.Message`;`ContextPlane.Assemble` 删除 `newToolLifecycleState` 调用和 `LifecycleState`、`EagerToolNames`、`DeferredToolNames` 字段的赋值。

- [ ] **Step 4:** `capability_assembler.go` 的 `artifactToolBridge.CurrentToolCallID` 改为 `return compose.GetToolCallID(ctx)`,并删除 `dispatch` import。

- [ ] **Step 5:** `types.go`:从 `RuntimeDeps` 删除 `ToolBuilder`、`ToolNodeFactory`、`CheckpointStore`;删除 `DirectResponseRequest`、`RunAssembly`、`bootstrapContextSessionMessages`、`validateBootstrapDeps`、`buildContextSession`、`prepareInitialMessages`。`model_builder.go` 删除 `buildRunnerAgentHandlers`。

- [ ] **Step 6:** 配置:
  - `ContextConfig` 删除 `PreserveRecentTurns`;
  - `MaskAfterTurns` 的注释改为"最近 N 轮工具调用原样保留,更早的工具结果在超阈值时被清理";
  - 默认值和校验同步删除;
  - 三份 YAML 模板与 `cli init` 生成的模板删除 `preserve_recent_turns`(用 `grep -rn preserve_recent_turns configs internal` 确认清零)。

- [ ] **Verify:** `go build ./... && go test ./internal/config ./internal/runtime -run 'TestApproval|TestSplitTools' -count=1`。在 `agent_test.go` 新增 `TestSplitToolsByLoading`:构造一个 deferred spec、一个 eager spec,断言分组正确。Executor 在 Task 5 切换,这一步允许 executor 相关测试暂时编译失败,但 Task 4 与 Task 5 必须作为一个提交落地。

---

## Task 5: Executor 事件投影与 run / resume 路径

**Files:**
- Create: `internal/runtime/events.go`、`internal/runtime/events_test.go`
- Modify: `internal/runtime/executor.go`

- [ ] **Step 1:** 新建 `internal/runtime/events.go`。从 `assistant_stream.go` 原样搬入 `StreamMessageFromSchema`、`streamInterruptFromInfo`、`streamPlannedToolCalls`、`streamMessageMeta`、`activeProviderName`,再加入投影器:

```go
// agentEventProjector converts adk events into persisted StreamItems. It owns
// the per-run assistant message counter used for delta message ids.
type agentEventProjector struct {
	runID          string
	provider       string
	assistantCount int
}

func (p *agentEventProjector) project(event *adk.AgentEvent, emit func(core.StreamItem) error) error {
	now := time.Now().UTC()
	if event.Err != nil {
		return emit(core.StreamItem{Kind: core.StreamKindRunFailed, CreatedAt: now, Payload: map[string]any{"error": event.Err.Error()}})
	}
	if event.Output != nil && event.Output.MessageOutput != nil {
		mo := event.Output.MessageOutput
		switch mo.Role {
		case schema.Assistant:
			if err := p.projectAssistant(mo, emit); err != nil {
				return err
			}
		case schema.Tool:
			msg, err := mo.GetMessage()
			if err != nil {
				return fmt.Errorf("read tool result message: %w", err)
			}
			if err := emit(core.StreamItem{Kind: core.StreamKindToolCallSucceeded, CreatedAt: now, Payload: map[string]any{
				"tool_call_id": msg.ToolCallID,
				"tool_name":    msg.ToolName,
				"output":       msg.Content,
			}}); err != nil {
				return err
			}
		}
	}
	if event.Action != nil && event.Action.Interrupted != nil {
		return emit(core.StreamItem{Kind: core.StreamKindRunInterrupted, CreatedAt: now, Payload: map[string]any{
			"interrupt": streamInterruptFromInfo(event.Action.Interrupted),
		}})
	}
	return nil
}

func (p *agentEventProjector) projectAssistant(mo *adk.MessageVariant, emit func(core.StreamItem) error) error {
	p.assistantCount++
	messageID := fmt.Sprintf("%s:assistant:%d", p.runID, p.assistantCount)
	final := mo.Message
	if mo.IsStreaming {
		defer mo.MessageStream.Close()
		frames := make([]*schema.Message, 0, 16)
		seq := 0
		for {
			frame, err := mo.MessageStream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return fmt.Errorf("read assistant stream: %w", err)
			}
			frames = append(frames, frame)
			if frame.Content == "" && frame.ReasoningContent == "" && len(frame.ToolCalls) == 0 {
				continue
			}
			seq++
			if err := emit(core.StreamItem{Kind: core.StreamKindAssistantDelta, CreatedAt: time.Now().UTC(), Payload: map[string]any{
				"assistant_delta": &core.StreamAssistantDelta{
					Role:      string(schema.Assistant),
					Delta:     frame.Content,
					Reasoning: frame.ReasoningContent,
					Sequence:  seq,
					MessageID: messageID,
					ToolCalls: streamPlannedToolCalls(frame.ToolCalls),
					Meta:      streamMessageMeta(frame),
				},
			}}); err != nil {
				return err
			}
		}
		if len(frames) == 0 {
			return errors.New("assistant stream returned no frames")
		}
		concat, err := schema.ConcatMessages(frames)
		if err != nil {
			return fmt.Errorf("concat assistant stream: %w", err)
		}
		final = concat
	}
	if final == nil {
		return errors.New("assistant event carried no message")
	}
	return emit(core.StreamItem{Kind: core.StreamKindAssistantMessage, CreatedAt: time.Now().UTC(), Payload: map[string]any{
		"message": StreamMessageFromSchema(final, p.provider),
	}})
}
```

- [ ] **Step 2:** `executor.go`:
  - `ExecuteMessages`:建好 runner 后,`messages := append(append([]adk.Message{}, active.ContextMessages...), req.Messages...)`,随后 `active.Runner.Run(execCtx, messages, adk.WithCheckPointID(runID))`;
  - `executionContext` 删掉 `WithSession` 分支;
  - `executeResume`:删除 `bootstrapResumeContextSession`,直接 `ResumeWithParams`;
  - `collectRunState`:持有一个 `agentEventProjector{runID, provider: activeProviderName(chatModel)}`,每个 event 调 `project`,`emit` 闭包里 `item.RunID = runID` → `AppendStreamItem` → `state.applyStreamItem`;
  - `finishSucceededRun` 和 `finishFailedRun` 在 `FinishRun` 之后调用 `e.store.DeleteCheckpoint(durableCtx, runID)`,出错直接返回;
  - 删除 `SelectedSkill` 参数链、`verifyAndRecordSkill`、`failureReasonForStatus`、`bootstrapResumeContextSession`。

- [ ] **Step 3:** `events_test.go`:用 `adk.EventFromMessage` 构造三类事件(流式 assistant、tool、带 interrupt 的事件,以及 Err 事件),断言产出的 kind 顺序和 payload:delta 的 `MessageID` 形如 `run_x:assistant:1`,`Sequence` 从 1 递增;最终 `assistant_message` 的 content 等于各帧拼接。

- [ ] **Verify:** `go build ./... && go test ./internal/runtime -count=1 -race`(此时旧的 direct_response 测试会编译失败,直接把 Task 7 Step 1 列出的测试文件一并删除,让包重新编译通过)
- [ ] **Commit(Task 4 + 5 一起):** `refactor(runtime): run agents on Eino ChatModelAgent with persistent checkpoints`

---

## Task 6: 决策后服务端自动续跑;删除 `:resume` 端点

**Files:**
- Modify: `internal/api/pending_action_service.go`、`internal/api/run_resume_service.go`、`internal/api/handlers_run.go`、`internal/api/routes.go`、`internal/api/server.go`(如有 runResume 字段)、`internal/wire/container.go`、`internal/cli/serve.go`、`docs/openapi.yaml`
- Regenerate: Kotlin client
- Test: `internal/api/client_pending_actions_test.go`、删除 `:resume` 相关测试

- [ ] **Step 1:** `RunResumeService` 新增方法:

```go
// ResumeInBackground resumes an interrupted run whose pending actions are all
// decided. Failures before the runtime takes over are recorded on the run so
// it does not stay interrupted silently.
func (s *RunResumeService) ResumeInBackground(ctx context.Context, runID string) {
	go func() {
		ctx := context.WithoutCancel(ctx)
		if _, err := s.Resume(ctx, runID); err != nil {
			if _, appendErr := s.store.AppendEvent(ctx, runID, "run.failed", map[string]any{"error": err.Error()}); appendErr != nil {
				reportClientBackgroundError(ctx, runID, appendErr)
			}
			if finishErr := s.store.FinishRun(ctx, runID, core.RunStatusFailed, "", err.Error()); finishErr != nil {
				reportClientBackgroundError(ctx, runID, finishErr)
			}
		}
	}()
}
```

- [ ] **Step 2:** `PendingActionService` 增加字段 `resumer interface{ ResumeInBackground(context.Context, string) }` 和 `WithResumer(...)`。`Decide` 在追加事件之后执行:

```go
	if s.resumer != nil {
		pending, err := s.store.ListPendingActionsByRun(ctx, record.RunID)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(pending, func(r core.PendingActionRecord) bool { return r.Status == core.PendingActionStatusPending }) {
			s.resumer.ResumeInBackground(ctx, record.RunID)
		}
	}
```

  在 `wire/container.go` 中,先构造 `runResume`,再把它作为 resumer 注入 pending action service。

- [ ] **Step 3:** 删除 `handlers_run.go` 中的 resume handler、`routes.go` 中对应路由、`serve.go` 里的 `RunResume:` 字段(只在 `ServerDeps` 用于该 handler 时才删)、`openapi.yaml` 中的 `/v1/runs/{run_id}:resume` path 及只被它引用的 schema(`interrupt_resume`、`resume_run` 枚举等,用 `grep -n "resume" docs/openapi.yaml` 逐一核对)。重新生成 Kotlin client。

- [ ] **Step 4:** 测试:同一 run 有两个 pending,决定第一个时 resumer 不被调用,决定第二个时被调用 1 次(fake resumer 计数);`internal/api/openapi_test.go` 仍通过。

- [ ] **Verify:** `go test ./internal/api ./internal/wire ./internal/cli && (cd mobile-kotlin && ./tool/generate_openapi_client.sh --check)`
- [ ] **Commit:** `feat(api): resume interrupted runs when their pending actions are decided`

---

## Task 7: 删除旧 runtime

**Files(删除):**
- `internal/runtime/`:`direct_response.go`、`direct_response_test.go`、`agent_loop.go`、`agent_loop_test.go`、`agent_loop_partial_rejection_test.go`、`assistant_stream.go`、`assistant_stream_test.go`、`streaming_assistant.go`、`masking.go`、`masking_test.go`、`auto_compact.go`、`auto_compact_test.go`、`auto_compact_nonblocking_test.go`、`tool_lifecycle.go`、`tool_lifecycle_test.go`、`risk_gate.go`、`risk_gate_test.go`、`context_session_test.go`
- `internal/tools/`:`risk_gate.go`、`risk_gate_test.go`、整个 `dispatch/` 目录
- `session.go`:先把 `TokenCounter`、`NewTokenCounter`、`tiktokenCounter`、`ensureTokenLoader`、`normalizeMessage`、`normalizeTool` 移到新文件 `internal/runtime/tokens.go`(memory 预算仍在用),其余整文件删除

**Files(修改):**
- `internal/runtime/catalog.go`:删除 `NewLoadToolsTool`;`load_tools` 从 `builtinToolOrder`、`builtinToolContract` 和注册处删除
- `internal/core`:删除 `AssistantStreamer`、`AssistantStreamRequest`、`InterleavedStream`、`AssistantStreamResult`、`AssistantStopReason*`、`CallSiteAssistant`(若无其他引用)、`ExecutionPolicyResolver`、`ParallelPolicy*`、`ToolExecutionPolicy` 及 `ToolContract.Execution`、`StreamKindDecisionBlocked`、`StreamKindSkillFailed` 等不再产生的 kind
- MCP 并行策略:删除 `MCPToolParallelPolicy` 以及它读取的 provider 配置字段,三份 YAML 模板同步删除
- `internal/api/projection.go`:从 `liveRunEventKinds` 删除从未产生的 `agent.message`、`decision_blocked`,同时删除对应的投影函数、`core` DTO 和 `openapi.yaml` 里的事件 schema,并重新生成 Kotlin client

- [ ] **Step 1:** 按上面的清单删除文件,`go build ./...` 逐个处理编译错误。凡是只服务于已删代码的符号,一律删除。
- [ ] **Step 2:** 用 `grep -rn "ParallelPolicy\|ExecutionPolicy\|load_tools\|ClassifyRisk\|IsHighRisk\|direct_response\|DirectResponse" internal configs docs/openapi.yaml` 确认结果为 0(ADR 文档除外)。
- [ ] **Step 3:** `deadcode -test ./... > /tmp/acorn-deadcode-after.txt`,然后 `diff /tmp/acorn-deadcode-before.txt /tmp/acorn-deadcode-after.txt`,确认没有新增条目。如果有,删除对应代码。

- [ ] **Verify:** `go build ./... && go vet ./... && make test && make test-architecture && (cd mobile-kotlin && ./tool/generate_openapi_client.sh --check)`
- [ ] **Commit:** `refactor: delete direct_response runtime, dispatch scheduler and risk gate`

---

## Task 8: 删除 git / command 工具与 agent 侧 workspace

**Files(删除):** `internal/tools/command.go`、`internal/tools/run_verification.go`、`internal/tools/processgroup_unix.go`、`internal/tools/processgroup_other.go`、`internal/tools/processgroup_test.go`、`internal/workspace/git_status.go`、`internal/workspace/git_status_test.go`、`internal/workspace/worktree.go`、`internal/workspace/worktree_test.go`、`internal/config/config_workspace.go`

**Files(修改):**
- `internal/tools/catalog_builders.go`:删除 `buildGitTools`、`buildRunCommandTools`、rollback 工具构建;`BuildCatalog` 中的 workspace / mutation 分组只在 `cfg.Workspace != nil` 时构建(只剩 memory 根目录这一个调用方)。
- `internal/tools/file_mutate.go`:删除 `rollback_workspace_checkpoint` 工具及其 git 状态展示代码。
- `internal/tools/configured.go`:`localToolDefs` 删除 `read_file`、`list_files`、`search_text`、`inspect_git_status`、`inspect_git_diff`、`git_summary`、`create_file`、`replace_span`、`apply_unified_patch`、`multi_edit`、`rollback_workspace_checkpoint`、`run_command`、`run_verification`。
- `internal/tools/ports.go`:删除 `InspectGitStatus`。
- `internal/tools/builtin_registry.go`:`CatalogConfig` 删除 `MutationEnabled`、`RunCommandEnabled` 的赋值来源;`RegisterNativeTools` 不再传 workspace。
- `internal/config/config.go`:删除 `ToolsConfig`、`WorkspaceToolConfig`、`MutationToolConfig`、`RunCommandToolConfig` 以及 `Config.Tools`;`config_load.go` 和 `config_validate.go` 同步删除相关行。
- `internal/runtime`:`RuntimeDeps.Workspace`、`RunnerFactoryOptions.Workspace`、`resolveWorkspace`、`validateToolsetDeps` 里的 workspace 检查;`buildLocalCatalog` 不再传 workspace。
- `internal/wire/runtime.go`:删除 `cfg.Workspace()` 及其 `ws` 字段。
- `internal/runtime/types.go`:删除 `ElicitationInterruptInfo`、`ElicitationInterruptState`(先 `grep` 确认 MCP elicitation 没有使用)。
- `internal/runtime/dispatch` 已在 Task 7 删除;`side_effects` 也随之消失。
- `configs/*.yaml`、`internal/cli` 的 init 模板、`scripts/install-release.sh`:删除 `tools:` 块(`workspace.root_dir`、`run_command`、`mutation`),删除 system prompt 里关于 `run_command` 的指引。
- 删除 webhook trigger:`internal/triggers/webhook.go`、`webhook_test.go`、`internal/api/handlers_trigger.go`、对应路由、`config` 的 `triggers.webhooks`、`openapi.yaml` 的 `/v1/triggers/{trigger_id}`(如果存在)、相关 INVARIANTS 条目。

- [ ] **Step 1:** 按清单逐项修改或删除,并保证 `go build ./...` 通过。
- [ ] **Step 2:** `grep -rn "run_command\|git_summary\|inspect_git\|rollback_workspace\|root_dir\|webhook" internal configs scripts docs/openapi.yaml` 结果只剩 memory 根目录和 ADR 文本。
- [ ] **Step 3:** 跑 deadcode,对比 Task 0 的基线,确认没有新增条目。

- [ ] **Verify:** `make test && make lint && make test-architecture && (cd mobile-kotlin && ./tool/generate_openapi_client.sh --check)`
- [ ] **Commit:** `refactor: drop git, command and workspace tools and webhook triggers`

---

## Task 9: 重启验收测试(P0 验收)

**Files:** Create `internal/wire/approval_restart_e2e_test.go`

整条链路走真实的 `wire.NewContainer` + SQLite;provider 指向一个用 `httptest` 起的 OpenAI 兼容 SSE 服务,生产代码不加任何测试钩子。

- [ ] **Step 1:** 编写 fake provider:`POST /chat/completions` 按请求序号回放 SSE。
  - 第 1 次:一个 chunk `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_runs","arguments":"{\"query\":\"probe\"}"}}]},"finish_reason":null}]}`,再发一个 `finish_reason":"tool_calls"` 的空 delta chunk,最后 `data: [DONE]`;
  - 第 2 次:content `"done"`,`finish_reason":"stop"`;
  - 把每次请求体保存下来,供断言使用。

- [ ] **Step 2:** 测试配置写到 `t.TempDir()/acorn.yaml`:`runtime.storage_dir` 指向临时目录;`providers[0]` 的 `base_url` 为 httptest URL,`api_key: test`,`model: fake`;`approval.require: ["search_runs"]`;memory embedding 保持关闭。用 `config.Load` 加载。

- [ ] **Step 3:** 测试流程:
  1. `c1 := wire.NewContainer(ctx, cfg)`;`CreateThread` → `Runs().CreateRun(ctx, threadID, "", "find probe")`;
  2. 轮询 `Runs().GetRun`,10 秒内状态应变为 `interrupted`;`PendingAction().List` 恰好返回 1 条,kind 为 `tool_approval`;
  3. `c1.Close()`,模拟进程重启;
  4. `c2 := wire.NewContainer(ctx, cfg)`;`c2.PendingAction().Decide(ctx, actionID, {Decision: "accept", SelectedOptionID: "accept"})`;
  5. 轮询直到 run 变为 `completed`(10 秒超时);
  6. 断言:fake provider 第 2 次请求体中有 `"role":"tool"` 且 `tool_call_id` 为 `call_1`;run 输出为 `"done"`;`agent_checkpoints` 中已无该 run 的记录(通过 `c2` 内部 store 的 `LoadCheckpoint` 断言,测试与 container 同包,可直接访问)。
- [ ] **Step 4:** 增加 decline 用例:第 2 次请求的 tool message 内容包含 `"The owner declined"`,`search_runs` 没有被真正执行(events 中没有该工具的 `tool_call_succeeded` 事件)。

- [ ] **Verify:** `go test ./internal/wire -run TestApprovalSurvivesRestart -race -count=3`
- [ ] **Commit:** `test(wire): approval survives a restart end to end`

---

## Task 10: 文档与不变量

**Files:** `AGENTS.md`、`docs/architecture/INVARIANTS.md`、`README.md`、`docs/user/self-hosted-onboarding.md`、`configs/*.yaml`

- [ ] **Step 1:** `AGENTS.md`:
  - "运行时 & 编排"、"上下文 & 压缩"两节改写为 ChatModelAgent + middleware 链,单一编排方式描述为"每个 run 一个 ChatModelAgent";
  - 工具执行改为串行(`ExecuteSequentially`);
  - 审批改为 `approval.require` + approval middleware + `tool_approval` pending + 决策后自动续跑;
  - SQLite 表数改为 11,并列出 `agent_checkpoints`;
  - context 配置只保留 `window_tokens`、`compact_margin_tokens`、`mask_after_turns`;
  - 删除 `run_command`、workspace mutation、webhook、ambient instruction、Decision Card 风险闸门的描述;
  - 关键包列表删除 `internal/tools/dispatch`。
- [ ] **Step 2:** `INVARIANTS.md`:
  - 删除 direct_response、partial rejection、hybrid context、webhook、ambient 指令、`ClassifyRisk` 等条目;
  - 新增两条:
    - **审批绑定具体调用并可跨重启恢复**:approval middleware 记录参数、resume 时比对参数,checkpoint 落 SQLite。对应测试:`internal/runtime/approval_test.go`、`internal/wire/approval_restart_e2e_test.go`、`internal/store/store_checkpoint_test.go`。
    - **上下文管理由 Eino middleware 承担**:summarization + reduction(clear-only),配置只暴露三个键。对应测试:`internal/runtime/agent_test.go`。
- [ ] **Step 3:** `README.md` 的 API 表删除 `:resume`;在配置一节加入 `approval.require` 的说明。onboarding 文档删除 workspace 和 run_command 的说明。

- [ ] **Verify:** `go test ./tests/architecture/ && make format-check`
- [ ] **Commit:** `docs: describe the ChatModelAgent runtime and persistent approvals`

---

## Task 11: 全量门禁 + 模拟器验收

- [ ] **Step 1:** 跑全量门禁:

```bash
make format-check && make lint && make test && go test -race ./... && make test-architecture
cd mobile-kotlin && ./tool/generate_openapi_client.sh --check && ./gradlew test lint assembleDebug
```

- [ ] **Step 2:** 手动验收(按 AGENTS.md 要求,mobile 相关改动必须在模拟器或真机上验证):
  1. 本地 `make serve`,配置里 `approval.require: ["search_runs"]`,用真实 provider;
  2. `acorn pair --server-url http://10.0.2.2:8080 --qr`,在模拟器 App 里完成配对;
  3. 在对话页发送"用 search_runs 搜索 test"→ 审批页出现 `Approve search_runs`,body 显示参数 → 点 Accept → 对话页出现最终回复;
  4. 再触发一次,在待审批状态下重启 `make serve`,然后点 Accept → run 正常完成;
  5. 截图保存到 PR 描述。无法连接设备时,在交付说明里写明"未在设备上验证"。

- [ ] **Step 3:** 用 `aegis:requesting-code-review` 对整条分支做一次审查,修完问题后再交付。

## Risks

- **Eino middleware 字段名与预期不符**:Task 4 Step 1 要求先用 `go doc` 核对;编译失败会立即暴露。
- **流式 tool call 的 SSE 分块**:Task 9 的 fake provider 使用 OpenAI 标准分块格式;如果 eino-ext openai 对 `index` 有额外要求,以它的解析代码为准修正 fake provider,生产代码不改。
- **summarization 改为同步执行**:超阈值的那一轮会多一次模型调用,延迟上升。P0 接受这一点,P1 的工作记忆会显著缩短上下文。
- **`browser`、`mcp__*` 默认需审批,可能打扰日常使用**:owner 可以在配置里收窄。

## Retirement

本计划完成后,以下内容不复存在:`direct_response` / `ExecuteRound` / `Session` / masking / 非阻塞 auto-compact / `dispatch` 调度器 / `ClassifyRisk` 风险闸门 / `load_tools` / 进程内 checkpoint / `:resume` 端点 / git 与 command 工具 / agent 侧 workspace 配置 / webhook trigger / ambient instruction / 从未产生的 `agent.message`、`decision_blocked` live kind。

文件工具实现、`internal/workspace` 的 `Workspace` 与 mutation checkpoint 仅供 `memory_*` 工具使用,在 P1 随记忆重写一并删除。
