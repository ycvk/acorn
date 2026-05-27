package crystallization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/ycvk/acorn/internal/memorymodule"
)

type mockMemorySvc struct {
	procedures    []memorymodule.Record
	nextRef       string
	lastCreateReq memorymodule.CreateProcedureRequest
}

func (m *mockMemorySvc) Root() string { return "" }
func (m *mockMemorySvc) ListFacts(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return nil, nil
}
func (m *mockMemorySvc) ListSkills(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return m.procedures, nil
}
func (m *mockMemorySvc) ListHistory(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return nil, nil
}
func (m *mockMemorySvc) Prepare(ctx context.Context, req memorymodule.PrepareRequest) (*memorymodule.PrepareResult, error) {
	return nil, nil
}
func (m *mockMemorySvc) Search(ctx context.Context, req memorymodule.SearchRequest) (*memorymodule.SearchResult, error) {
	return nil, nil
}
func (m *mockMemorySvc) RebuildSemanticIndex(ctx context.Context, opts memorymodule.SemanticRebuildOptions) (*memorymodule.SemanticRebuildResult, error) {
	return nil, nil
}
func (m *mockMemorySvc) AppendHistory(ctx context.Context, event memorymodule.HistoryEvent) error {
	return nil
}
func (m *mockMemorySvc) PlanMemoryMutation(ctx context.Context, req memorymodule.PlanMemoryMutationRequest) (*memorymodule.MemoryMutationPlan, error) {
	return &memorymodule.MemoryMutationPlan{Action: memorymodule.MemoryMutationCreate, Path: req.Path}, nil
}
func (m *mockMemorySvc) ApplyMemoryMutation(ctx context.Context, req memorymodule.PlanMemoryMutationRequest) (*memorymodule.MemoryMutationResult, error) {
	plan := &memorymodule.MemoryMutationPlan{Action: memorymodule.MemoryMutationCreate, Path: req.Path}
	return &memorymodule.MemoryMutationResult{Message: "ok", MutationPlan: plan, Path: req.Path}, nil
}
func (m *mockMemorySvc) CreateProcedure(ctx context.Context, req memorymodule.CreateProcedureRequest) (*memorymodule.ProcedureRecord, error) {
	m.lastCreateReq = req
	ref := m.nextRef
	if ref == "" {
		ref = fmt.Sprintf("proc-%d", time.Now().UnixNano())
	}
	m.procedures = append(m.procedures, memorymodule.Record{
		Ref:          ref,
		Title:        req.Title,
		Body:         req.Body,
		TaskPattern:  req.TaskPattern,
		EvidenceRefs: append([]string(nil), req.EvidenceRefs...),
		Origin:       string(memorymodule.ProcedureOriginAgentDraft),
		Created:      time.Now().UTC().Format(time.RFC3339),
	})
	return &memorymodule.ProcedureRecord{Ref: ref}, nil
}
func (m *mockMemorySvc) BuildMemoryInstruction(ctx context.Context, workspaceSlug string) (string, error) {
	return "", nil
}

type stubIndexStore struct {
	queryErr  error
	upsertErr error
	entries   []IndexEntry
}

func (s *stubIndexStore) Upsert(ctx context.Context, entry *IndexEntry) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	if entry != nil {
		s.entries = append(s.entries, *entry)
	}
	return nil
}

func (s *stubIndexStore) Query(ctx context.Context, input string, limit int) ([]IndexEntry, error) {
	if s.queryErr != nil {
		return nil, s.queryErr
	}
	return append([]IndexEntry(nil), s.entries...), nil
}

func (s *stubIndexStore) Delete(ctx context.Context, skillID string) error {
	return nil
}

func TestCrystallizeRejectsLowValueRuns(t *testing.T) {
	svc := NewDefaultService(&mockMemorySvc{}, &stubIndexStore{})

	for _, req := range []CrystallizationRequest{
		{RunID: "no-tools", Input: "hello"},
		{RunID: "read-only", Input: "list files", ToolNames: []string{"file_read", "list_dir"}},
		{RunID: "no-evidence", Input: "deploy app", ToolNames: []string{"deploy_tool"}},
	} {
		res, err := svc.Crystallize(context.Background(), req)
		if err != nil {
			t.Fatalf("Crystallize(%s): %v", req.RunID, err)
		}
		if res.Verdict != VerdictInsufficientValue {
			t.Fatalf("Crystallize(%s) verdict = %s, want %s", req.RunID, res.Verdict, VerdictInsufficientValue)
		}
	}
}

func TestCrystallizeReturnsIndexQueryError(t *testing.T) {
	mem := &mockMemorySvc{}
	svc := NewDefaultService(mem, &stubIndexStore{queryErr: errors.New("index offline")})

	_, err := svc.Crystallize(context.Background(), CrystallizationRequest{
		RunID:        "r_index_query",
		Input:        "deploy app",
		ToolNames:    []string{"deploy_tool"},
		EvidenceRefs: []string{"tool_result:r_index_query:call_deploy"},
	})
	if err == nil || !strings.Contains(err.Error(), "query insight index for similarity") {
		t.Fatalf("error = %v, want similarity index query error", err)
	}
	if len(mem.procedures) != 0 {
		t.Fatalf("procedures = %#v, want none before similarity check succeeds", mem.procedures)
	}
}

func TestCrystallizeDetectsSimilarProcedure(t *testing.T) {
	store := &stubIndexStore{entries: []IndexEntry{{
		SkillID:     "existing",
		TaskPattern: "When asked to deploy the application to production server",
		Keywords:    []string{"deploy", "application", "production", "server"},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}}}
	svc := NewDefaultService(&mockMemorySvc{}, store)

	res, err := svc.Crystallize(context.Background(), CrystallizationRequest{
		RunID:        "r_similar",
		Input:        "deploy the application to production server",
		ToolNames:    []string{"deploy_tool"},
		EvidenceRefs: []string{"tool_result:r_similar:call_deploy"},
	})
	if err != nil {
		t.Fatalf("Crystallize: %v", err)
	}
	if res.Verdict != VerdictTooSimilar || res.SimilarTo != "existing" {
		t.Fatalf("result = %#v, want too_similar to existing", res)
	}
}

func TestCrystallizeCreatesProcedureWithEvidence(t *testing.T) {
	store := &stubIndexStore{}
	mem := &mockMemorySvc{}
	svc := NewDefaultService(mem, store)

	res, err := svc.Crystallize(context.Background(), CrystallizationRequest{
		RunID:        "r_success",
		Input:        "create a new user in the database",
		Output:       "User created successfully",
		ToolNames:    []string{"db_create", "db_verify"},
		EvidenceRefs: []string{"tool_result:r_success:call_create", "tool_result:r_success:call_verify"},
		Messages:     []adk.Message{{Role: "user", Content: "create user"}},
	})
	if err != nil {
		t.Fatalf("Crystallize: %v", err)
	}
	if res.Verdict != VerdictCrystallized {
		t.Fatalf("verdict = %s, want %s", res.Verdict, VerdictCrystallized)
	}
	if res.SkillID == "" {
		t.Fatal("expected SkillID to be set")
	}
	if got, want := strings.Join(mem.lastCreateReq.EvidenceRefs, ","), "tool_result:r_success:call_create,tool_result:r_success:call_verify"; got != want {
		t.Fatalf("evidence refs = %q, want %q", got, want)
	}
	if len(store.entries) == 0 {
		t.Fatal("expected index entry to be upserted")
	}
}

func TestBuildIndexEntryRejectsInvalidSkillCreatedTime(t *testing.T) {
	mem := &mockMemorySvc{procedures: []memorymodule.Record{{
		Ref:         "skill-bad-time",
		Title:       "Bad Time Skill",
		Body:        "Body",
		TaskPattern: "bad time",
		Origin:      string(memorymodule.ProcedureOriginActionVerified),
		Created:     "not-a-time",
	}}}
	svc := NewDefaultService(mem, &stubIndexStore{})
	_, err := svc.BuildIndexEntry(context.Background(), "skill-bad-time")
	if err == nil || !strings.Contains(err.Error(), "parse skill created timestamp") {
		t.Fatalf("BuildIndexEntry error = %v, want skill created parse error", err)
	}
}
