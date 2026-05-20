package crystallization

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

func TestCrystallize_NoTools(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	svc := NewDefaultService(&mockMemorySvc{}, store)
	req := CrystallizationRequest{RunID: "r1", Input: "hello", ToolNames: []string{}}
	res, err := svc.Crystallize(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictInsufficientValue {
		t.Fatalf("expected insufficient_value, got %s", res.Verdict)
	}
}

func TestCrystallize_OnlyReadTools(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	svc := NewDefaultService(&mockMemorySvc{}, store)
	req := CrystallizationRequest{RunID: "r2", Input: "list files", ToolNames: []string{"file_read", "list_dir"}}
	res, err := svc.Crystallize(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictInsufficientValue {
		t.Fatalf("expected insufficient_value, got %s", res.Verdict)
	}
}

func TestCrystallize_NoEvidenceRefs(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	svc := NewDefaultService(&mockMemorySvc{}, store)
	req := CrystallizationRequest{RunID: "r2", Input: "deploy app", ToolNames: []string{"deploy_tool"}}
	res, err := svc.Crystallize(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictInsufficientValue {
		t.Fatalf("expected insufficient_value, got %s", res.Verdict)
	}
	if res.Reason != "no verified tool evidence" {
		t.Fatalf("reason = %q, want no verified tool evidence", res.Reason)
	}
}

func TestCrystallize_ReturnsIndexQueryError(t *testing.T) {
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

func TestCrystallize_ReturnsBuildIndexError(t *testing.T) {
	mem := &mockMemorySvc{}
	svc := NewDefaultService(mem, &stubIndexStore{upsertErr: errors.New("index write failed")})

	_, err := svc.Crystallize(context.Background(), CrystallizationRequest{
		RunID:        "r_index_build",
		Input:        "deploy app",
		ToolNames:    []string{"deploy_tool"},
		EvidenceRefs: []string{"tool_result:r_index_build:call_deploy"},
	})
	if err == nil || !strings.Contains(err.Error(), "build insight index entry") {
		t.Fatalf("error = %v, want build index error", err)
	}
	if len(mem.procedures) != 1 {
		t.Fatalf("procedures = %#v, want procedure created before index build failure", mem.procedures)
	}
}

func TestCrystallize_TooSimilar(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	mem := &mockMemorySvc{}
	svc := NewDefaultService(mem, store)

	if _, err := mem.CreateProcedure(context.Background(), memorymodule.CreateProcedureRequest{
		Title:       "Deploy App",
		Body:        "Deploy the application to production",
		TaskPattern: "When asked to deploy the application to production server",
	}); err != nil {
		t.Fatalf("CreateProcedure: %v", err)
	}
	if _, err := svc.BuildIndexEntry(context.Background(), mem.procedures[0].Ref); err != nil {
		t.Fatalf("BuildIndexEntry: %v", err)
	}

	req := CrystallizationRequest{
		RunID:        "r3",
		Input:        "deploy the application to production server",
		ToolNames:    []string{"deploy_tool"},
		EvidenceRefs: []string{"tool_result:r3:call_deploy"},
	}
	res, err := svc.Crystallize(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictTooSimilar {
		t.Fatalf("expected too_similar, got %s (reason: %s)", res.Verdict, res.Reason)
	}
}

func TestCrystallize_Success(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	mem := &mockMemorySvc{}
	svc := NewDefaultService(mem, store)

	req := CrystallizationRequest{
		RunID:        "r4",
		Input:        "create a new user in the database",
		Output:       "User created successfully",
		ToolNames:    []string{"db_create", "db_verify"},
		EvidenceRefs: []string{"tool_result:r4:call_create", "tool_result:r4:call_verify"},
		Messages:     []adk.Message{{Role: "user", Content: "create user"}},
	}
	res, err := svc.Crystallize(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictCrystallized {
		t.Fatalf("expected crystallized, got %s (reason: %s)", res.Verdict, res.Reason)
	}
	if res.SkillID == "" {
		t.Fatal("expected SkillID to be set")
	}
	if got, want := strings.Join(mem.lastCreateReq.EvidenceRefs, ","), "tool_result:r4:call_create,tool_result:r4:call_verify"; got != want {
		t.Fatalf("evidence refs = %q, want %q", got, want)
	}
}

func TestSimilarityChecker(t *testing.T) {
	checker := NewSimilarityChecker(0.85)
	existing := []IndexEntry{
		{SkillID: "s1", TaskPattern: "deploy application to production", Keywords: []string{"deploy", "application", "production"}},
		{SkillID: "s2", TaskPattern: "run tests in ci", Keywords: []string{"test", "ci"}},
	}

	id, score := checker.FindMostSimilar("deploy application to production", existing)
	if id != "s1" {
		t.Fatalf("expected s1, got %s", id)
	}
	if score <= 0 || score > 1 {
		t.Fatalf("score out of range: %f", score)
	}
	if score < checker.Threshold() {
		t.Fatalf("expected score %.2f >= threshold %.2f", score, checker.Threshold())
	}

	id, score = checker.FindMostSimilar("completely unrelated task about cooking", existing)
	if id != "" {
		t.Fatalf("expected no match, got %s with score %.2f", id, score)
	}
}

func TestQualityScorer(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	if err := store.Upsert(context.Background(), &IndexEntry{
		SkillID:      "s-old",
		TaskPattern:  "old task",
		Keywords:     []string{"a", "b", "c", "d", "e", "f"},
		QualityScore: 0,
		CreatedAt:    time.Now().UTC().Add(-100 * 24 * time.Hour),
		UpdatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	scorer := NewQualityScorer(store)
	score, err := scorer.Score(context.Background(), "s-old")
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if score < 70 {
		t.Fatalf("expected score >= 70 for old skill with many keywords, got %d", score)
	}
	if score > 100 {
		t.Fatalf("score should be capped at 100, got %d", score)
	}
}

func TestSummarizer(t *testing.T) {
	sum := NewSummarizer()
	result := sum.Summarize("Deploy App", "Steps to deploy the app to production server with zero downtime", "When asked to deploy", []string{"deploy", "verify"})
	if result == "" {
		t.Fatal("expected non-empty summary")
	}
	if len(result) > 200 {
		t.Fatalf("summary exceeded 200 chars: %d", len(result))
	}
	if !strings.Contains(result, "Pattern:") {
		t.Fatal("expected summary to contain pattern")
	}
}

func TestExtractKeywords(t *testing.T) {
	kw := extractKeywords("deploy app to server", "Deploy Application", "Use docker to build and push image")
	seen := make(map[string]bool)
	for _, k := range kw {
		seen[k] = true
	}
	if !seen["deploy"] || !seen["app"] || !seen["server"] {
		t.Fatalf("expected deploy/app/server in keywords, got %v", kw)
	}
	if len(kw) > 20 {
		t.Fatalf("keywords should be capped at 20, got %d", len(kw))
	}
}

func TestOnlyReadTools(t *testing.T) {
	if !onlyReadTools([]string{"file_read", "list_dir"}) {
		t.Fatal("expected onlyReadTools true for read/list")
	}
	if onlyReadTools([]string{"file_write", "deploy"}) {
		t.Fatal("expected onlyReadTools false for write/deploy")
	}
	if onlyReadTools([]string{}) {
		t.Fatal("expected onlyReadTools false for empty")
	}
}

func TestIndexStore_UpsertQueryDelete(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	ctx := context.Background()
	entry := &IndexEntry{
		SkillID:      "test-skill",
		SkillName:    "Test Skill",
		Summary:      "A test skill",
		Keywords:     []string{"test", "skill"},
		TaskPattern:  "test pattern",
		QualityScore: 75,
		Source:       "test",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if err := store.Upsert(ctx, entry); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	results, err := store.Query(ctx, "test pattern", 10)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected query to return results")
	}

	if err := store.Delete(ctx, "test-skill"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	results, err = store.Query(ctx, "test pattern", 10)
	if err != nil {
		t.Fatalf("query after delete failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results after delete, got %d", len(results))
	}
}

func TestIndexStoreRejectsInvalidStoredTime(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	if _, err := store.db.ExecContext(context.Background(), `
		INSERT INTO insight_index (skill_id, skill_name, summary, keywords, task_pattern, quality_score, source, created_at, updated_at)
		VALUES ('bad-time', 'Bad Time', 'summary', 'bad,time', 'bad time', 50, 'test', 'not-a-time', '2026-05-10T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert bad index row: %v", err)
	}
	_, err := store.Query(context.Background(), "bad time", 10)
	if err == nil || !strings.Contains(err.Error(), "parse insight index created_at") {
		t.Fatalf("Query error = %v, want created_at parse error", err)
	}
}

func TestBuildIndexEntryRejectsInvalidSkillCreatedTime(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	mem := &mockMemorySvc{procedures: []memorymodule.Record{{
		Ref:         "skill-bad-time",
		Title:       "Bad Time Skill",
		Body:        "Body",
		TaskPattern: "bad time",
		Origin:      string(memorymodule.ProcedureOriginActionVerified),
		Created:     "not-a-time",
	}}}
	svc := NewDefaultService(mem, store)
	_, err := svc.BuildIndexEntry(context.Background(), "skill-bad-time")
	if err == nil || !strings.Contains(err.Error(), "parse skill created timestamp") {
		t.Fatalf("BuildIndexEntry error = %v, want skill created parse error", err)
	}
}

func newTestStore(t *testing.T) (*SQLiteIndexStore, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := OpenIndexStore(dbPath)
	if err != nil {
		t.Fatalf("open index store: %v", err)
	}
	cleanup := func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close index store: %v", err)
		}
	}
	return store, cleanup
}
