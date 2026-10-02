package api

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/ycvk/acorn/internal/core"
)

func TestOpenAPIContractMatchesClientSurface(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "openapi.yaml")
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("validate openapi: %v", err)
	}
	if got, want := doc.Info.Title, "Acorn Remote Client API"; got != want {
		t.Fatalf("openapi title = %q, want %q", got, want)
	}
	if !strings.Contains(doc.Info.Description, "Remote client API for Acorn's single-user self-hosted agent backend.") {
		t.Fatalf("openapi description should describe the remote client API, got %q", doc.Info.Description)
	}
	if len(doc.Security) != 1 {
		t.Fatalf("openapi must require bearer auth for remote client routes")
	}
	if doc.Components.SecuritySchemes["bearerAuth"] == nil {
		t.Fatalf("openapi must define bearerAuth security scheme")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read openapi file: %v", err)
	}
	text := string(body)

	legacyPrefix := "/" + "api"
	for pathName := range doc.Paths.Map() {
		if strings.HasPrefix(pathName, legacyPrefix) {
			t.Fatalf("openapi contract should not contain legacy path %q", pathName)
		}
	}

	for _, want := range []string{
		"Acorn Remote Client API",
		"Remote client API for Acorn's single-user self-hosted agent backend.",
		"bearerAuth",
		"/v1/devices:pair",
		"/v1/devices",
		"/v1/devices/{device_id}",
		"/v1/devices/self/push-token",
		"/v1/threads",
		"/v1/threads/{thread_id}",
		"/v1/threads/{thread_id}/messages",
		"/v1/threads/{thread_id}/runs",
		"/v1/runs/{run_id}",
		"/v1/runs/{run_id}/events",
		"/v1/runs/{run_id}/detail",
		"/v1/runs/{run_id}:interrupt",
		"/v1/inbox",
		"/v1/pending-actions",
		"/v1/pending-actions/{action_id}",
		"/v1/pending-actions/{action_id}:decide",
		"/v1/system/status",
		"/v1/tools",
		"/v1/skills",
		"/v1/skills/{id}",
		"/v1/skills/{id}/files",
		"Client `/v1` run event endpoints stream the mobile live `RunEvent`",
		"client",
		"thread_not_found",
		"run_not_found",
		"invalid_after_seq",
		"run_event_projection_failed",
		"event: <RunEvent.type>",
		"Mobile clients persist",
		"backlog events first",
		"InboxResponse",
		"RunSummary",
		"PendingActionSummary",
		"PendingActionOption",
		"PendingActionListResponse",
		"PendingActionDetail",
		"DecidePendingActionRequest",
		"PendingActionDecision",
		"OperatorQuestionData",
		"RunArtifact",
		"unauthenticated",
		"device_revoked",
		"invalid_pairing_code",
		"device_not_found",
		"workspace_root",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("openapi contract should contain %q", want)
		}
	}

	for _, stale := range []string{
		"/v1/memory",
		"Acorn Web API",
		"Browser-facing",
		"local-first runtime",
		"standard client",
		"Standard client",
		"BearerAuth",
		"OwnerProfile",
		"CreatePairingCodeRequest",
		"/v1/settings",
		"ClientSettings",
		"ClientProviderSettings",
		"ClientRuntimeSettings",
		"ClientWebSettings",
		"settings_write_unsupported",
		"reasoning_effort",
		"enum: [direct_response, plan_execute]",
		"enum: [direct, agent, plan_execute]",
		legacyPrefix + "/system/capabilities",
		legacyPrefix + "/sessions/last-pending",
		legacyPrefix + "/sessions",
		legacyPrefix + "/sessions/{sessionId}",
		legacyPrefix + "/sessions/{sessionId}/workbench",
		legacyPrefix + "/sessions/{sessionId}/messages",
		legacyPrefix + "/sessions/{sessionId}/plan",
		legacyPrefix + "/runs/{runId}/trace",
		legacyPrefix + "/runs/{runId}/resume",
		legacyPrefix + "/runs/{runId}/interrupt",
		legacyPrefix + "/runs/{runId}/plan",
		legacyPrefix + "/codeintel/status",
		legacyPrefix + "/codeintel/repo-map",
		legacyPrefix + "/codeintel/symbols",
		legacyPrefix + "/codeintel/file-symbols",
		legacyPrefix + "/codeintel/references",
		legacyPrefix + "/memory/profile-blocks",
		legacyPrefix + "/memory/profile-blocks/{name}",
		"/v1/memory/profile-blocks",
		"/v1/memory/profile-blocks/{name}",
		legacyPrefix + "/memory/facts",
		legacyPrefix + "/memory/facts/{id}",
		legacyPrefix + "/history/search",
		legacyPrefix + "/reflections",
		legacyPrefix + "/reflections/findings",
		legacyPrefix + "/reflections/{id}/approve",
		legacyPrefix + "/reflections/{id}/reject",
		legacyPrefix + "/reflections/{id}/rollback",
		"/v1/memory/core",
		"/v1/memory/core/{id}",
		"/v1/memory/facts/{id}",
		"/v1/memory/facts/recent",
		"/v1/memory/episodes",
		"/v1/memory/candidates",
		"/v1/memory/candidates/{candidate_id}",
		"/v1/memory/candidates/{candidate_id}:approve",
		"/v1/memory/candidates/{candidate_id}:reject",
		"/v1/history/search",
		"/v1/reflections",
		"/v1/reflections/findings",
		"/v1/reflections/{id}:approve",
		"/v1/reflections/{id}:reject",
		"/v1/reflections/{id}:rollback",
		"/v1/codeintel/status",
		"/v1/codeintel/repo-map",
		"/v1/codeintel/symbols",
		"/v1/codeintel/file-symbols",
		"/v1/codeintel/references",
		legacyPrefix + "/skills",
		legacyPrefix + "/skills/{id}",
		legacyPrefix + "/skills/{id}/files",
		legacyPrefix + "/sessions/{sessionId}/working-checkpoint",
		legacyPrefix + "/memory/blocks",
		legacyPrefix + "/memory/conversations",
		legacyPrefix + "/memory/facts/evict",
		legacyPrefix + "/skills/{id}/verify",
		legacyPrefix + "/skills/{id}/reject",
		legacyPrefix + "/sessions/{sessionId}/messages/stream",
		legacyPrefix + "/runs/{runId}/resume/stream",
		"/api/recipes",
		"getCapabilities",
		"getLastPendingSession",
		"createSession",
		"listSessions",
		"getSessionWorkbench",
		"listSessionMessages",
		"sendMessage",
		"getRunTrace",
		"getSessionPlan",
		"getRunPlan",
		"SessionEnvelope",
		"SessionListEnvelope",
		"SessionDetailEnvelope",
		"SessionMessageListResponse",
		"SessionMessageSendRequest",
		"LastPendingSessionDTO",
		"RuntimeWorkbenchEnvelope",
		"RuntimeWorkbench",
		"WorkspaceGitStatus",
		"SubagentRun",
		"MutationCheckpoint",
		"RollbackResult",
		"PlanDTO",
		"PlanStepDTO",
		"PlanEvidenceDTO",
		"ChatSendResponse",
		"CapabilitiesResponse",
		"RunRecord",
		"StreamItem:",
		"StreamMessage:",
		"StreamAssistantDelta:",
		"StreamPlannedToolCall:",
		"StreamWarningScope:",
		"StreamWarningFact:",
		"StreamInterruptContext:",
		"StreamInterrupt:",
		"sendMessageStream",
		"resumeRunStream",
		"event: <StreamItem.kind>",
		"sse_heartbeat_interval_seconds",
		"sse_stale_timeout_seconds",
		"Recipe",
		"SkillStatus",
		"required_tools",
		"ConversationHit",
		"BlockUpdateRequest",
		"EvictFactsResponse",
		"RunDetailRaw",
		"UnsupportedRunEvent",
		"unsupported_events",
		"UpdateMemoryCandidateRequest",
		"memory_candidate_not_pending",
		"memory_candidate_committed",
		"CoreMemoryBlock",
		"CoreMemoryBlockListResponse",
		"KnowledgeFact",
		"KnowledgeFactListResponse",
		"ReflectionProposal",
		"ReflectionApprovalEnvelope",
		"ReflectionProposalEnvelope",
		"V1StreamItem",
		"RunEventStreamItem",
		"ProfileBlock",
		"ProfileBlockListResponse",
		"CreateSkillRequest",
		"PatchSkillRequest",
		"clientCreateSkill",
		"clientPatchSkill",
		"clientDeleteSkill",
		"CodeintelStatusEnvelope",
		"RepoMapResponse",
		"SymbolSearchResponse",
		"FileSymbolsResponse",
		"ReferenceSearchResponse",
		"TraceWarningSummary",
		"TraceSummary",
		"warning_summary",
		"trace_summary",
		"trace_debug",
	} {
		if strings.Contains(text, stale) {
			t.Fatalf("openapi contract should not contain stale entry %q", stale)
		}
	}

	for _, futurePath := range []string{
		"/v1/pairing-codes",
	} {
		if doc.Paths.Find(futurePath) != nil {
			t.Fatalf("openapi contract should not contain future path %q before implementation", futurePath)
		}
	}

	for _, schemaName := range []string{
		"Device",
		"DeviceListResponse",
		"PairDeviceRequest",
		"PairDeviceResponse",
		"SkillEnvelope",
		"SkillFileResponse",
		"Thread",
		"ThreadListResponse",
		"CreateThreadRequest",
		"UpdateThreadRequest",
		"MessageContent",
		"Message",
		"MessageListResponse",
		"CreateMessageRequest",
		"CreateRunRequest",
		"Run",
		"RunEvent",
		"RunDetail",
		"InboxResponse",
		"RunSummary",
		"PendingActionSummary",
		"PendingActionOption",
		"PendingActionListResponse",
		"PendingActionDetail",
		"DecidePendingActionRequest",
		"PendingActionDecision",
		"OperatorQuestionData",
		"SystemStatus",
		"ToolListResponse",
		"InterruptRunResponse",
		"RunArtifact",
	} {
		if doc.Components.Schemas[schemaName] == nil {
			t.Fatalf("missing schema %s", schemaName)
		}
	}

	if doc.Components.Parameters["ThreadID"] == nil {
		t.Fatalf("missing ThreadID parameter")
	}
	if doc.Components.Parameters["RunID"] == nil {
		t.Fatalf("missing RunID parameter")
	}
	if doc.Components.Parameters["DeviceID"] == nil {
		t.Fatalf("missing DeviceID parameter")
	}
}

func TestOpenAPIRunStatusEnumsUseClientProjection(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "openapi.yaml")
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}
	want := []string{"completed", "failed", "interrupted", "running"}
	for _, schemaName := range []string{"Run"} {
		schemaRef := doc.Components.Schemas[schemaName]
		if schemaRef == nil || schemaRef.Value == nil {
			t.Fatalf("missing %s schema", schemaName)
		}
		statusRef := schemaRef.Value.Properties["status"]
		if statusRef == nil || statusRef.Value == nil {
			t.Fatalf("%s missing status property", schemaName)
		}
		got := sortedStrings(enumStrings(statusRef.Value.Enum))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s.status enum = %v, want %v", schemaName, got, want)
		}
	}
}

func enumStrings(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func sortedStrings(items []string) []string {
	out := append([]string(nil), items...)
	sort.Strings(out)
	return out
}

// Every live run event kind must be in the RunEvent union, and every pending
// action kind must reach the client as <kind>.pending and <kind>.decided.
func TestOpenAPIRunEventUnionMatchesLiveEventKinds(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "openapi.yaml")
	doc, err := openapi3.NewLoader().LoadFromFile(path)
	if err != nil {
		t.Fatalf("load openapi: %v", err)
	}
	union := doc.Components.Schemas["RunEvent"]
	if union == nil || union.Value == nil || union.Value.Discriminator == nil {
		t.Fatal("RunEvent schema has no discriminator")
	}
	var mapped []string
	for kind := range union.Value.Discriminator.Mapping {
		mapped = append(mapped, kind)
	}
	if got, want := sortedStrings(mapped), sortedStrings(liveRunEventKinds); !reflect.DeepEqual(got, want) {
		t.Fatalf("RunEvent discriminator = %v, live kinds = %v", got, want)
	}
	for _, kind := range []core.PendingActionKind{core.PendingActionKindElicitation, core.PendingActionKindOperatorQuestion, core.PendingActionKindToolApproval} {
		for _, suffix := range []string{".pending", ".decided"} {
			if !IsLiveRunEventKind(string(kind) + suffix) {
				t.Fatalf("pending action kind %q has no live %s event", kind, suffix)
			}
		}
	}
}

func TestProjectToolApprovalEvents(t *testing.T) {
	pending, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 3, Kind: "tool_approval.pending", Payload: map[string]any{
		"action_id": "action_00000000000000a1", "tool_name": "browser", "arguments": `{"url":"https://example.com"}`,
	}})
	if err != nil {
		t.Fatalf("project pending: %v", err)
	}
	if got := pending.Data.(core.ToolApprovalData); got.ActionID != "action_00000000000000a1" || got.ToolName != "browser" || got.Arguments != `{"url":"https://example.com"}` {
		t.Fatalf("pending data = %+v", got)
	}
	decided, err := ProjectRunEvent(core.EventRecord{RunID: "run_1", Sequence: 4, Kind: "tool_approval.decided", Payload: map[string]any{
		"action_id": "action_00000000000000a1", "decision": "decline",
	}})
	if err != nil {
		t.Fatalf("project decided: %v", err)
	}
	if got := decided.Data.(core.ToolApprovalData); got.ActionID != "action_00000000000000a1" || got.Decision != "decline" {
		t.Fatalf("decided data = %+v", got)
	}
}
