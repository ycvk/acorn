package skilllifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/skills"
)

func TestBuildAgentToolsIncludesSkillAssessOnly(t *testing.T) {
	loader := newSkillLifecycleTestLoader(t)
	tools, err := BuildAgentTools(ToolOptions{
		Loader: loader,
		Store:  &toolEventStore{},
		Bridge: fixedRunBridge{runID: "run_parent", sessionID: "session_parent"},
	})
	if err != nil {
		t.Fatalf("BuildAgentTools: %v", err)
	}
	names := toolNamesFromSet(t, tools)
	if !names["skill_assess"] {
		t.Fatal("missing skill_assess")
	}
	if names[legacySkillToolName("eval")] || names[legacySkillToolName("curate")] {
		t.Fatalf("unexpected legacy lifecycle tools: %#v", names)
	}
}

func TestSkillAssessUpdatesMutableSkillWithEvidence(t *testing.T) {
	root := t.TempDir()
	cfg := skillLifecycleTestConfig(root)
	loader := skills.NewLoader(cfg)
	ctx := context.Background()

	created, err := loader.CreateSkill(ctx, skills.CreateInput{
		ID:          "skill.generated",
		Name:        "Generated",
		Instruction: "Use generated workflow.",
	})
	if err != nil {
		t.Fatalf("CreateSkill: %v", err)
	}
	if created.LifecycleStatus != skills.LifecycleDraft {
		t.Fatalf("created lifecycle = %q", created.LifecycleStatus)
	}

	store := &toolEventStore{}
	tools, err := BuildAgentTools(ToolOptions{
		Loader: loader,
		Store:  store,
		Bridge: fixedRunBridge{runID: "run_parent", sessionID: "session_parent"},
	})
	if err != nil {
		t.Fatalf("BuildAgentTools: %v", err)
	}
	assessTool := mustInvokableLifecycleTool(t, tools, "skill_assess")
	body, err := json.Marshal(AssessToolInput{
		ID:              "skill.generated",
		Verdict:         AssessmentVerified,
		Reason:          "Dogfood run passed against durable evidence.",
		EvidenceRefs:    []string{"tool_result:run_parent:skill_create"},
		ChangesRequired: []string{"none"},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	outBody, err := assessTool.InvokableRun(ctx, string(body))
	if err != nil {
		t.Fatalf("skill_assess: %v", err)
	}
	var output AssessToolOutput
	if err := json.Unmarshal([]byte(outBody), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output.Assessment.SkillID != "skill.generated" {
		t.Fatalf("assessment skill id = %q", output.Assessment.SkillID)
	}
	if output.Assessment.Verdict != AssessmentVerified || !output.Assessment.Applied {
		t.Fatalf("assessment = %#v", output.Assessment)
	}
	if output.Assessment.SourceRunID != "run_parent" {
		t.Fatalf("source run id = %q", output.Assessment.SourceRunID)
	}
	if output.Updated == nil || output.Updated.LifecycleStatus != skills.LifecycleVerified {
		t.Fatalf("updated = %#v", output.Updated)
	}
	if !reflect.DeepEqual(output.Updated.EvidenceRefs, []string{"tool_result:run_parent:skill_create"}) {
		t.Fatalf("updated evidence refs = %#v", output.Updated.EvidenceRefs)
	}
	if len(store.records) != 1 {
		t.Fatalf("events = %d, want 1", len(store.records))
	}
	if store.records[0].Kind != "skill.lifecycle" || store.records[0].RunID != "run_parent" {
		t.Fatalf("event = %#v", store.records[0])
	}
	eventPayload, err := json.Marshal(store.records[0].Payload)
	if err != nil {
		t.Fatalf("marshal event payload: %v", err)
	}
	if !strings.Contains(string(eventPayload), `"action":"assessed"`) || !strings.Contains(string(eventPayload), `"verdict":"verified"`) {
		t.Fatalf("payload = %s", eventPayload)
	}
}

func TestSkillAssessBuiltinSkillIsVisibleButNotApplied(t *testing.T) {
	loader := &builtinSkillLifecycleLoader{
		skill: skills.Spec{
			ID:              "skill.creator",
			Name:            "Skill Creator",
			Source:          skills.BuiltinScope,
			Origin:          skills.OriginHuman,
			LifecycleStatus: skills.LifecycleVerified,
			EvidenceRefs:    []string{"builtin:acorn-native-skill-seed-pack"},
		},
	}
	store := &toolEventStore{}
	tools, err := BuildAgentTools(ToolOptions{
		Loader: loader,
		Store:  store,
		Bridge: fixedRunBridge{runID: "run_parent", sessionID: "session_parent"},
	})
	if err != nil {
		t.Fatalf("BuildAgentTools: %v", err)
	}
	assessTool := mustInvokableLifecycleTool(t, tools, "skill_assess")
	body, err := json.Marshal(AssessToolInput{
		ID:           "skill.creator",
		Verdict:      AssessmentRetired,
		Reason:       "No longer selected by the LLM.",
		EvidenceRefs: []string{"builtin:acorn-native-skill-seed-pack"},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	outBody, err := assessTool.InvokableRun(context.Background(), string(body))
	if err != nil {
		t.Fatalf("skill_assess: %v", err)
	}
	var output AssessToolOutput
	if err := json.Unmarshal([]byte(outBody), &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output.Assessment.Applied {
		t.Fatalf("assessment = %#v, want unapplied", output.Assessment)
	}
	if output.Updated != nil {
		t.Fatalf("updated = %#v, want nil", output.Updated)
	}
	if len(store.records) != 1 {
		t.Fatalf("events = %d, want 1", len(store.records))
	}
	if !strings.Contains(mustJSON(t, store.records[0].Payload), `"applied":false`) {
		t.Fatalf("event payload = %#v", store.records[0].Payload)
	}
	if loader.updateCalled {
		t.Fatal("builtin skill should not be updated")
	}
}

func TestSkillAssessRejectsVerifiedWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	cfg := skillLifecycleTestConfig(root)
	loader := skills.NewLoader(cfg)
	store := &toolEventStore{}
	tools, err := BuildAgentTools(ToolOptions{
		Loader: loader,
		Store:  store,
		Bridge: fixedRunBridge{runID: "run_parent", sessionID: "session_parent"},
	})
	if err != nil {
		t.Fatalf("BuildAgentTools: %v", err)
	}
	assessTool := mustInvokableLifecycleTool(t, tools, "skill_assess")
	body, err := json.Marshal(AssessToolInput{
		ID:      "skill.missing",
		Verdict: AssessmentVerified,
		Reason:  "Missing evidence should fail.",
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	_, err = assessTool.InvokableRun(context.Background(), string(body))
	if err == nil || !strings.Contains(err.Error(), "requires evidence_refs") {
		t.Fatalf("error = %v, want evidence failure", err)
	}
	if len(store.records) != 0 {
		t.Fatalf("events = %d, want 0", len(store.records))
	}
}

func toolNamesFromSet(t *testing.T, tools []einotool.BaseTool) map[string]bool {
	t.Helper()

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		names[info.Name] = true
	}
	return names
}

func newSkillLifecycleTestLoader(t *testing.T) *skills.Loader {
	t.Helper()
	cfg := skillLifecycleTestConfig(t.TempDir())
	return skills.NewLoader(cfg)
}

func skillLifecycleTestConfig(root string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Tools.Workspace.RootDir = root
	cfg.Runtime.StorageDir = filepath.Join(root, ".acorn")
	return cfg
}

func mustInvokableLifecycleTool(t *testing.T, items []einotool.BaseTool, name string) einotool.InvokableTool {
	t.Helper()
	for _, item := range items {
		info, err := item.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		if info.Name != name {
			continue
		}
		invokable, ok := item.(einotool.InvokableTool)
		if !ok {
			t.Fatalf("%s is not invokable", name)
		}
		return invokable
	}
	t.Fatalf("missing tool %s", name)
	return nil
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(body)
}

type builtinSkillLifecycleLoader struct {
	skill        skills.Spec
	updateCalled bool
}

func (l *builtinSkillLifecycleLoader) ScanSkills(context.Context) (*skills.ScanResult, error) {
	return &skills.ScanResult{Skills: []skills.Spec{l.skill}}, nil
}

func (l *builtinSkillLifecycleLoader) UpdateSkillLifecycle(context.Context, string, skills.LifecycleUpdate) (*skills.Spec, error) {
	l.updateCalled = true
	return nil, errors.New("builtin skill should not be updated")
}

func legacySkillToolName(kind string) string {
	return "skill_" + kind
}

type toolEventStore struct {
	records []events.EventRecord
}

func (s *toolEventStore) AppendEventContext(_ context.Context, runID, kind string, payload any) (events.EventRecord, error) {
	record := events.EventRecord{
		RunID:    runID,
		Kind:     kind,
		Sequence: int64(len(s.records) + 1),
		Payload:  payload,
	}
	s.records = append(s.records, record)
	return record, nil
}

type fixedRunBridge struct {
	runID     string
	sessionID string
}

func (b fixedRunBridge) CurrentRunID(context.Context) string {
	return b.runID
}

func (b fixedRunBridge) CurrentSessionID(context.Context) string {
	return b.sessionID
}
