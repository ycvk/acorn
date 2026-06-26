package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	mcpprovider "github.com/ycvk/acorn/internal/mcp"
	mem "github.com/ycvk/acorn/internal/memory"
	"github.com/ycvk/acorn/internal/skills"
)

func TestClientResourceSurfaceHandlers(t *testing.T) {
	service := &clientHandlerStub{
		thread: Thread{
			ID:            "thread_1",
			Title:         "Inspect repo",
			WorkspaceRoot: "/repo",
			CreatedAt:     time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 5, 2, 10, 1, 0, 0, time.UTC),
			State:         "completed",
		},
		run: Run{
			ID:        "run_1",
			ThreadID:  "thread_1",
			Status:    "completed",
			CreatedAt: time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
		},
		events: []core.RunEvent{
			{
				EventID: "run_1:1",
				RunID:   "run_1",
				Seq:     1,
				TS:      time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
				Type:    "run.started",
				Data:    core.RunStartedData{Input: "hello"},
			},
		},
		artifacts: []ArtifactSummary{{
			ArtifactID:          "artifact_report",
			RunID:               "run_1",
			SessionID:           "thread_1",
			SourceToolResultRef: "tool_result:run_1:call_1",
			Kind:                "markdown",
			Title:               "Report",
			MIMEType:            "text/markdown",
			SizeBytes:           42,
			SHA256:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt:           time.Date(2026, 5, 2, 10, 4, 0, 0, time.UTC),
		}},
	}
	systemSnapshot := SystemCapabilities{
		RuntimeReadiness: &RuntimeReadiness{Status: RuntimeReadinessReady},
		ProviderReadiness: []ProviderReadinessSummary{{
			Scope:         "mcp",
			Provider:      "fixture",
			Status:        ProviderReadinessPassed,
			StartupStatus: "healthy",
			AuthStatus:    "env",
		}},
		Model: SystemModelCapabilities{Name: "gpt-test"},
		Summary: SystemCapabilitySummary{
			ToolCount:        1,
			EnabledToolCount: 1,
			SkillCount:       1,
		},
		Features: SystemFeatureCapabilities{InterruptResume: true, SessionHistory: true},
		Tools: []SystemToolCapability{{
			Name:        "run_command",
			Source:      "builtin",
			Kind:        "function",
			Category:    "workspace",
			Enabled:     true,
			HealthState: "ok",
			Risk:        "high",
		}},
	}
	workspaceRoot := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Tools.Workspace.RootDir = workspaceRoot
	cfg.Tools.RunCommand.WorkDir = workspaceRoot
	cfg.Providers[0].Model = "gpt-test"
	cfg.Providers[0].ReasoningEffort = "high"
	cfg.Providers[0].APIKey = "redacted-test-key"
	cfg.Web.ListenAddr = "127.0.0.1:9999"
	cfg.MCP.Providers = []config.MCPProviderConfig{{
		Enabled:               true,
		Name:                  "fixture",
		Transport:             "stdio",
		Command:               "fixture-mcp",
		ToolNames:             []string{"fixture_tool"},
		StartupTimeoutSeconds: 10,
		ToolSafety:            "serial",
	}}
	memory := &clientMemoryStub{
		facts: []mem.Record{{
			Ref:        "facts/workspaces/acorn/repo.md#repo-root",
			Kind:       mem.KindFact,
			RelPath:    "facts/workspaces/acorn/repo.md",
			Title:      "Repo root",
			Status:     mem.StatusVerified,
			Scope:      "workspace:acorn",
			Tags:       []string{"repo"},
			Body:       "repo root is /repo",
			Created:    "2026-05-02T10:00:00Z",
			Updated:    "2026-05-02T10:00:00Z",
			SourceRun:  "run_1",
			SourceRefs: []string{"history/thread_1.md#summary"},
		}},
		skills: []mem.Record{{
			Ref:         "skills/learned/release-closeout.md#release-closeout",
			Kind:        mem.KindSkill,
			RelPath:     "skills/learned/release-closeout.md",
			Title:       "Release closeout",
			Status:      mem.StatusUnverified,
			Origin:      "agent_draft",
			TaskPattern: "release closeout",
			Tags:        []string{"release", "closeout"},
			Body:        "先验证再提交",
			Created:     "2026-05-02T10:00:00Z",
			Updated:     "2026-05-02T10:00:00Z",
			SourceRun:   "run_1",
			SourceRefs:  []string{"facts/workspaces/acorn/repo.md#repo-root"},
		}},
		history: []mem.Record{{
			Ref:       "history/thread_1.md",
			Kind:      mem.KindHistory,
			RelPath:   "history/thread_1.md",
			Title:     "thread_1",
			Status:    mem.StatusVerified,
			Body:      "history hit from previous run",
			Created:   "2026-05-02T10:00:00Z",
			Updated:   "2026-05-02T10:00:00Z",
			SourceRun: "run_1",
		}},
		search: []mem.SearchItem{{
			Ref:       "history/thread_1.md",
			Kind:      string(mem.KindHistory),
			Title:     "thread_1",
			Status:    string(mem.StatusVerified),
			Scope:     "workspace:acorn",
			Tags:      []string{"history"},
			Path:      "history/thread_1.md",
			Snippet:   "history hit from previous run",
			Score:     1,
			Created:   "2026-05-02T10:00:00Z",
			Updated:   "2026-05-02T10:00:00Z",
			SourceRun: "run_1",
		}},
	}
	server := newClientHotPathServer(service)
	server.runResume = newRunResumeTestService(&RunResult{RunID: "run_1", Status: "interrupted"}, nil)
	server.capabilities = NewCapabilitiesService(
		cfg,
		func(context.Context) (*skills.Snapshot, error) {
			return &skills.Snapshot{Skills: []skills.View{{
				Spec: skills.Spec{
					ID:      "skill.inspect",
					Name:    "Inspect",
					Version: "1.0.0",
					Source:  "local",
				},
				Eligible: true,
			}}}, nil
		},
		func(context.Context, []mcpprovider.ProviderConfig) []mcpprovider.ProviderStatus {
			return []mcpprovider.ProviderStatus{{
				Name:                "fixture",
				Configured:          true,
				Enabled:             true,
				Transport:           "stdio",
				StartupStatus:       "healthy",
				Command:             "fixture-mcp",
				ConfiguredToolNames: []string{"fixture_tool"},
				DiscoveredToolNames: []string{"fixture_tool"},
				ToolCount:           1,
				AuthStatus:          "env",
			}}
		},
		nil,
	)
	pendingActionStub := &pendingActionHandlerStub{
		summaries: []PendingActionSummary{{
			ActionID: "action_1",
			RunID:    "run_1",
			ThreadID: "thread_1",
			Kind:     "elicitation",
			Status:   "pending",
			Title:    "Approval required",
			Body:     "Allow Acorn to continue?",
			Options: []PendingActionOption{
				{ID: "accept", Label: "Accept"},
				{ID: "decline", Label: "Decline"},
			},
			CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		}},
		detail: &PendingActionDetail{
			PendingActionSummary: PendingActionSummary{
				ActionID: "action_1",
				RunID:    "run_1",
				ThreadID: "thread_1",
				Kind:     "elicitation",
				Status:   "pending",
				Title:    "Approval required",
				Body:     "Allow Acorn to continue?",
				Options: []PendingActionOption{
					{ID: "accept", Label: "Accept"},
					{ID: "decline", Label: "Decline"},
				},
				CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			},
			Payload: map[string]any{"message": "Allow Acorn to continue?"},
		},
	}
	server.pendingAction = newPendingActionTestService(pendingActionStub)
	inboxStub := &inboxHandlerStub{item: &MobileInbox{
		PendingActions: []PendingActionSummary{{
			ActionID: "action_1",
			RunID:    "run_1",
			ThreadID: "thread_1",
			Kind:     "elicitation",
			Status:   "pending",
			Title:    "Approval required",
			Body:     "Allow Acorn to continue?",
			Options: []PendingActionOption{
				{ID: "accept", Label: "Accept"},
				{ID: "decline", Label: "Decline"},
			},
			CreatedAt: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
		}},
		ActiveRuns: []RunSummary{{
			RunID:          "run_1",
			ThreadID:       "thread_1",
			ThreadTitle:    "Deploy Acorn",
			Status:         "running",
			Preview:        "Run the release workflow",
			LastEventLabel: "Run is running",
			AttentionLevel: "running",
			DurationMS:     60000,
			CreatedAt:      time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 5, 15, 10, 1, 0, 0, time.UTC),
		}},
		RecentTerminalRuns: []RunSummary{{
			RunID:          "run_terminal",
			ThreadID:       "thread_1",
			ThreadTitle:    "Release Acorn",
			Status:         "completed",
			Preview:        "Release completed",
			LastEventLabel: "Run completed",
			AttentionLevel: "normal",
			DurationMS:     300000,
			CreatedAt:      time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
			UpdatedAt:      time.Date(2026, 5, 15, 9, 5, 0, 0, time.UTC),
		}},
		System: systemSnapshot,
	}}
	inboxStore := &inboxServiceStore{
		runByID: map[string]core.RunRecord{
			"run_1":        inboxRunRecordFromSummary(inboxStub.item.ActiveRuns[0]),
			"run_terminal": inboxRunRecordFromSummary(inboxStub.item.RecentTerminalRuns[0]),
		},
		sessionByID: map[string]core.SessionRecord{
			"thread_1": sessionRecordFromRunSummary(inboxStub.item.ActiveRuns[0]),
		},
	}
	for _, item := range inboxStub.item.PendingActions {
		inboxStore.pendingActions = append(inboxStore.pendingActions, pendingActionRecordFromSummary(item))
	}
	for _, item := range inboxStub.item.ActiveRuns {
		inboxStore.activeRuns = append(inboxStore.activeRuns, inboxRunRecordFromSummary(item))
	}
	for _, item := range inboxStub.item.RecentTerminalRuns {
		inboxStore.recentTerminalRuns = append(inboxStore.recentTerminalRuns, inboxRunRecordFromSummary(item))
	}
	server.inbox = newInboxTestService(inboxStore)
	server.skills = newTestSkillService(t, testSkillFixture{
		id:          "skill.inspect",
		name:        "Inspect",
		summary:     "Inspect the repo.",
		instruction: "Use repo inspection.",
	})
	server.memory = memory
	server.deviceAuth = newDeviceAuthTestService(&deviceAuthHandlerStub{})
	server.cfg = cfg
	server.logger = slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	router := chi.NewRouter()
	server.registerRoutes(router)

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		want       string
	}{
		{name: "interrupt", method: http.MethodPost, path: "/v1/runs/run_1:interrupt", wantStatus: http.StatusAccepted, want: "interrupt_requested"},
		{name: "resume", method: http.MethodPost, path: "/v1/runs/run_1:resume", body: `{}`, wantStatus: http.StatusOK, want: `"run_id":"run_1"`},
		{name: "detail", method: http.MethodGet, path: "/v1/runs/run_1/detail", wantStatus: http.StatusOK, want: `"artifacts"`},
		{name: "inbox", method: http.MethodGet, path: "/v1/inbox", wantStatus: http.StatusOK, want: `"pending_actions":[{"action_id":"action_1"`},
		{name: "pending actions", method: http.MethodGet, path: "/v1/pending-actions", wantStatus: http.StatusOK, want: `"items":[{"action_id":"action_1"`},
		{name: "system status", method: http.MethodGet, path: "/v1/system/status?probe_mcp=1", wantStatus: http.StatusOK, want: `"runtime_readiness":{"status":"ready"}`},
		{name: "tools", method: http.MethodGet, path: "/v1/tools", wantStatus: http.StatusOK, want: "run_command"},
		{name: "skills", method: http.MethodGet, path: "/v1/skills", wantStatus: http.StatusOK, want: "skill.inspect"},
		{name: "skill create removed", method: http.MethodPost, path: "/v1/skills", body: `{"id":"skill.inspect","name":"Inspect","instruction":"Use repo inspection."}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "skill detail", method: http.MethodGet, path: "/v1/skills/skill.inspect", wantStatus: http.StatusOK, want: "Inspect"},
		{name: "skill patch removed", method: http.MethodPatch, path: "/v1/skills/skill.inspect", body: `{"content":"extra instruction"}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "skill delete removed", method: http.MethodDelete, path: "/v1/skills/skill.inspect", wantStatus: http.StatusMethodNotAllowed},
		{name: "skill files", method: http.MethodGet, path: "/v1/skills/skill.inspect/files", wantStatus: http.StatusOK, want: "SKILL.md"},
		{name: "core memory removed", method: http.MethodGet, path: "/v1/memory/core", wantStatus: http.StatusNotFound},
		{name: "core memory update removed", method: http.MethodPatch, path: "/v1/memory/core/core.about_you", body: `{"body":"updated core"}`, wantStatus: http.StatusNotFound},
		{name: "profile blocks deleted", method: http.MethodGet, path: "/v1/memory/profile-blocks", wantStatus: http.StatusNotFound},
		{name: "memory facts", method: http.MethodGet, path: "/v1/memory/facts?limit=5&include_inactive=true", wantStatus: http.StatusOK, want: `"title":"Repo root"`},
		{name: "memory skills", method: http.MethodGet, path: "/v1/memory/skills?limit=5&include_retired=true", wantStatus: http.StatusOK, want: `"origin":"agent_draft"`},
		{name: "memory history", method: http.MethodGet, path: "/v1/memory/history?limit=5", wantStatus: http.StatusOK, want: "history hit"},
		{name: "memory search", method: http.MethodGet, path: "/v1/memory/search?query=repo&kind=history&scope=workspace:acorn&include_inactive=true&include_retired=true", wantStatus: http.StatusOK, want: `"snippet":"history hit from previous run"`},
		{name: "memory invalid include flag", method: http.MethodGet, path: "/v1/memory/facts?include_inactive=yes", wantStatus: http.StatusBadRequest, want: "include_inactive must be true or false"},
		{name: "add memory fact removed", method: http.MethodPost, path: "/v1/memory/facts", body: `{"content":"repo root is /repo","labels":["repo"]}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "delete memory fact removed", method: http.MethodDelete, path: "/v1/memory/facts/42", wantStatus: http.StatusNotFound},
		{name: "episodic memory removed", method: http.MethodGet, path: "/v1/memory/episodes", wantStatus: http.StatusNotFound},
		{name: "memory candidates removed", method: http.MethodGet, path: "/v1/memory/candidates?status=pending", wantStatus: http.StatusNotFound},
		{name: "memory candidate update removed", method: http.MethodPatch, path: "/v1/memory/candidates/memcand_1", body: `{"payload_json":"{\"content\":\"edited\"}","reason":"edited reason","scope":{"type":"workspace","key":"acorn"}}`, wantStatus: http.StatusNotFound},
		{name: "memory candidate delete removed", method: http.MethodDelete, path: "/v1/memory/candidates/memcand_1", wantStatus: http.StatusNotFound},
		{name: "history search removed", method: http.MethodGet, path: "/v1/history/search?query=repo", wantStatus: http.StatusNotFound},
		{name: "codeintel status removed", method: http.MethodGet, path: "/v1/codeintel/status", wantStatus: http.StatusNotFound},
		{name: "codeintel repo map removed", method: http.MethodGet, path: "/v1/codeintel/repo-map?query=routes", wantStatus: http.StatusNotFound},
		{name: "codeintel symbols removed", method: http.MethodGet, path: "/v1/codeintel/symbols?query=registerRoutes", wantStatus: http.StatusNotFound},
		{name: "codeintel file symbols removed", method: http.MethodGet, path: "/v1/codeintel/file-symbols?path=internal/web/routes.go", wantStatus: http.StatusNotFound},
		{name: "codeintel references removed", method: http.MethodGet, path: "/v1/codeintel/references?symbol=registerRoutes", wantStatus: http.StatusNotFound},
		{name: "reflection list removed", method: http.MethodGet, path: "/v1/reflections", wantStatus: http.StatusNotFound},
		{name: "reflection findings removed", method: http.MethodGet, path: "/v1/reflections/findings", wantStatus: http.StatusNotFound},
		{name: "reflection approve removed", method: http.MethodPost, path: "/v1/reflections/7:approve", body: `{}`, wantStatus: http.StatusNotFound},
		{name: "reflection reject removed", method: http.MethodPost, path: "/v1/reflections/7:reject", body: `{}`, wantStatus: http.StatusNotFound},
		{name: "reflection rollback removed", method: http.MethodPost, path: "/v1/reflections/7:rollback", wantStatus: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := performClientRequest(router, tc.method, tc.path, tc.body)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if tc.want != "" && !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body missing %q: %s", tc.want, rec.Body.String())
			}
		})
	}

	detailRec := performClientRequest(router, http.MethodGet, "/v1/runs/run_1/detail", "")
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", detailRec.Code, detailRec.Body.String())
	}
	if strings.Contains(detailRec.Body.String(), `"raw"`) {
		t.Fatalf("run detail should not expose raw diagnostic events: %s", detailRec.Body.String())
	}
	if strings.Contains(detailRec.Body.String(), `"workbench"`) {
		t.Fatalf("run detail should not expose runtime workbench: %s", detailRec.Body.String())
	}
	if !strings.Contains(detailRec.Body.String(), `"artifacts":[{"artifact_id":"artifact_report"`) {
		t.Fatalf("run detail should expose top-level artifacts: %s", detailRec.Body.String())
	}

	systemStatusRec := performClientRequest(router, http.MethodGet, "/v1/system/status?probe_mcp=1", "")
	if systemStatusRec.Code != http.StatusOK {
		t.Fatalf("system status code = %d body=%s", systemStatusRec.Code, systemStatusRec.Body.String())
	}
	if !strings.Contains(systemStatusRec.Body.String(), `"provider_readiness":[{"scope":"mcp","provider":"fixture","status":"passed"`) {
		t.Fatalf("system status should include provider readiness, got %s", systemStatusRec.Body.String())
	}
	if !memory.factSelection.IncludeInactive || memory.factSelection.IncludeRetired {
		t.Fatalf("fact selection = %#v, want include_inactive only", memory.factSelection)
	}
	if memory.skillSelection.IncludeInactive || !memory.skillSelection.IncludeRetired {
		t.Fatalf("skill selection = %#v, want include_retired only", memory.skillSelection)
	}
	if memory.historySelection.IncludeInactive || memory.historySelection.IncludeRetired {
		t.Fatalf("history selection = %#v, want active only", memory.historySelection)
	}
	if memory.searchReq.Query != "repo" || memory.searchReq.Scope != "workspace:acorn" || !memory.searchReq.IncludeInactive || !memory.searchReq.IncludeRetired {
		t.Fatalf("search request = %#v, want repo scoped retired-inclusive search", memory.searchReq)
	}
	if len(memory.searchReq.Kinds) != 1 || memory.searchReq.Kinds[0] != mem.KindHistory {
		t.Fatalf("search kinds = %#v, want history", memory.searchReq.Kinds)
	}
}
