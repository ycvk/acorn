package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/core"
	corestore "github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/webaccess"
	workspacepkg "github.com/ycvk/acorn/internal/workspace"
)

func TestBuildWorkspaceToolsBuildsFileTools(t *testing.T) {
	fileTools, err := BuildWorkspaceTools(testWorkspace(t, t.TempDir()))
	if err != nil {
		t.Fatalf("BuildWorkspaceTools: %v", err)
	}
	names := make([]string, 0, len(fileTools))
	for _, tool := range fileTools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool info: %v", err)
		}
		names = append(names, info.Name)
	}
	if got, want := strings.Join(names, ","), "read_file,list_files,create_file,replace_span"; got != want {
		t.Fatalf("tools = %s, want %s", got, want)
	}
}

func TestBuildWorkspaceToolsRequiresWorkspace(t *testing.T) {
	_, err := BuildWorkspaceTools(nil)
	if err == nil || !strings.Contains(err.Error(), "workspace is required") {
		t.Fatalf("BuildWorkspaceTools(nil) error = %v, want workspace is required", err)
	}
}

func TestReadFileReturnsStructuredLineRange(t *testing.T) {
	root := t.TempDir()
	body := "line 1\nline 2\nline 3\n"
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	fileTools, err := BuildWorkspaceTools(ws)
	if err != nil {
		t.Fatalf("BuildWorkspaceTools: %v", err)
	}
	tool := mustToolByName(t, fileTools, "read_file")

	output, err := tool.InvokableRun(context.Background(), `{"path":"notes.txt","start_line":2,"end_line":3}`)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}

	var decoded ReadFileOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(read_file output): %v\noutput=%s", err, output)
	}
	if decoded.StartLine != 2 || decoded.EndLine != 3 {
		t.Fatalf("range = %d-%d, want 2-3", decoded.StartLine, decoded.EndLine)
	}
	if decoded.Content != "line 2\nline 3\n" {
		t.Fatalf("content = %q", decoded.Content)
	}
}

func TestCreateFileReturnsVerificationPreview(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(t, root)
	fileTools, err := BuildWorkspaceTools(ws)
	if err != nil {
		t.Fatalf("BuildWorkspaceTools: %v", err)
	}
	tool := mustToolByName(t, fileTools, "create_file")

	output, err := tool.InvokableRun(context.Background(), `{"path":"notes.txt","content":"hello from acorn"}`)
	if err != nil {
		t.Fatalf("create_file: %v", err)
	}

	var decoded CreateFileOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(create_file output): %v\noutput=%s", err, output)
	}
	if decoded.Path != filepath.Join(root, "notes.txt") {
		t.Fatalf("Path = %q, want %q", decoded.Path, filepath.Join(root, "notes.txt"))
	}
	if decoded.VerifiedBytes != len("hello from acorn") {
		t.Fatalf("VerifiedBytes = %d, want %d", decoded.VerifiedBytes, len("hello from acorn"))
	}
	if decoded.VerifiedContent != "hello from acorn" {
		t.Fatalf("VerifiedContent = %q", decoded.VerifiedContent)
	}
	if decoded.VerificationTruncated {
		t.Fatal("VerificationTruncated should be false for short content")
	}
	if decoded.CheckpointID == "" {
		t.Fatal("CheckpointID is required")
	}
	if strings.Join(decoded.CheckpointPaths, ",") != "notes.txt" {
		t.Fatalf("CheckpointPaths = %+v", decoded.CheckpointPaths)
	}
}

func TestNativeWorkspaceToolsExposeProgressInterface(t *testing.T) {
	fileTools, err := BuildWorkspaceTools(testWorkspace(t, t.TempDir()))
	if err != nil {
		t.Fatalf("BuildWorkspaceTools: %v", err)
	}
	for _, name := range []string{
		"read_file",
		"list_files",
		"create_file",
		"replace_span",
	} {
		mustProgressToolByName(t, fileTools, name)
	}
}

func TestArtifactToolsWriteReadAndList(t *testing.T) {
	store := newToolArtifactStore()
	service, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	bridge := fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_1"}
	writeBase, err := buildArtifactWriteTool(service, bridge)
	if err != nil {
		t.Fatalf("buildArtifactWriteTool: %v", err)
	}
	readBase, err := buildArtifactReadTool(service)
	if err != nil {
		t.Fatalf("buildArtifactReadTool: %v", err)
	}
	listBase, err := buildArtifactListTool(service, bridge)
	if err != nil {
		t.Fatalf("buildArtifactListTool: %v", err)
	}
	artifactTools := []einotool.BaseTool{writeBase, readBase, listBase}

	writeTool := mustToolByName(t, artifactTools, "artifact_write")

	writeOutput, err := writeTool.InvokableRun(context.Background(), `{"kind":"markdown","title":"Report","mime_type":"text/markdown","content":"hello artifact"}`)
	if err != nil {
		t.Fatalf("artifact_write: %v", err)
	}
	var written ArtifactWriteOutput
	if err := json.Unmarshal([]byte(writeOutput), &written); err != nil {
		t.Fatalf("json.Unmarshal(artifact_write output): %v\noutput=%s", err, writeOutput)
	}
	if written.RunID != "run_1" || written.SessionID != "session_1" || written.SourceToolResultRef != "tool_result:run_1:call_1" {
		t.Fatalf("unexpected artifact write output: %+v", written)
	}
	if written.ArtifactID == "" || written.SizeBytes != int64(len("hello artifact")) {
		t.Fatalf("unexpected artifact identity/size: %+v", written)
	}

	readTool := mustToolByName(t, artifactTools, "artifact_read")
	readOutput, err := readTool.InvokableRun(context.Background(), `{"artifact_id":"`+written.ArtifactID+`","offset":6,"limit":20}`)
	if err != nil {
		t.Fatalf("artifact_read: %v", err)
	}
	var read ArtifactReadOutput
	if err := json.Unmarshal([]byte(readOutput), &read); err != nil {
		t.Fatalf("json.Unmarshal(artifact_read output): %v\noutput=%s", err, readOutput)
	}
	if read.Content != "artifact" || !read.EOF || read.Bytes != len("artifact") {
		t.Fatalf("unexpected artifact read output: %+v", read)
	}

	listTool := mustToolByName(t, artifactTools, "artifact_list")
	listOutput, err := listTool.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("artifact_list: %v", err)
	}
	var list ArtifactListOutput
	if err := json.Unmarshal([]byte(listOutput), &list); err != nil {
		t.Fatalf("json.Unmarshal(artifact_list output): %v\noutput=%s", err, listOutput)
	}
	if list.RunID != "run_1" || len(list.Items) != 1 || list.Items[0].ArtifactID != written.ArtifactID {
		t.Fatalf("unexpected artifact list output: %+v", list)
	}
}

func TestWebFetchToolPersistsRawAndMarkdownArtifacts(t *testing.T) {
	store := newToolArtifactStore()
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	fetchService, err := webaccess.NewFetchService(webaccess.FetchConfig{
		UserAgent:        "Acorn test",
		Timeout:          time.Second,
		MaxResponseBytes: 1024 * 1024,
		Policy: webaccess.URLPolicy{Resolver: toolWebFetchResolver{
			"example.com": {"93.184.216.34"},
		}},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body: io.NopCloser(strings.NewReader(`<!doctype html>
<html><head><title>Fetched Page</title></head>
<body><main><h1>Fetched Page</h1><p>Persist this page.</p></main></body></html>`)),
				Request: req,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("webaccess.NewFetchService: %v", err)
	}
	fetchTool, err := buildWebFetchTool(fetchService, artifactService, fixedArtifactContext{runID: "run_web", sessionID: "session_web", callID: "call_web"})
	if err != nil {
		t.Fatalf("buildWebFetchTool: %v", err)
	}
	tool := mustToolByName(t, []einotool.BaseTool{fetchTool}, "web_fetch")
	output, err := tool.InvokableRun(context.Background(), `{"url":"https://example.com/page","extract_mode":"full_page_markdown"}`)
	if err != nil {
		t.Fatalf("web_fetch: %v", err)
	}
	var decoded WebFetchOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode web_fetch output: %v", err)
	}
	if decoded.RawArtifactID == "" || decoded.MarkdownArtifactID == "" {
		t.Fatalf("missing artifact ids: %+v", decoded)
	}
	if !strings.Contains(decoded.MarkdownPreview, "Persist this page") {
		t.Fatalf("markdown preview = %q", decoded.MarkdownPreview)
	}
	if len(store.records) != 2 {
		t.Fatalf("stored artifacts = %d, want 2", len(store.records))
	}
}

func TestWebSearchToolPersistsRawProviderArtifact(t *testing.T) {
	store := newToolArtifactStore()
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	searchService, err := webaccess.NewSearchService(webaccess.SearchConfig{
		APIKey:           "tvly-test",
		Timeout:          time.Second,
		MaxResults:       10,
		MaxResponseBytes: 1024 * 1024,
		Policy: webaccess.URLPolicy{Resolver: toolWebFetchResolver{
			"example.com": {"93.184.216.34"},
		}},
		SearchURL: "https://tavily.test/search",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
  "query": "acorn",
  "response_time": 0.1,
  "results": [
    {"title":"Acorn Docs","url":"https://example.com/docs","content":"Official docs","score":0.9},
    {"title":"Private","url":"http://10.0.0.1/admin","content":"Private","score":0.1}
  ]
}`)),
				Request: req,
			}, nil
		})},
	})
	if err != nil {
		t.Fatalf("webaccess.NewSearchService: %v", err)
	}
	searchTool, err := buildWebSearchTool(searchService, artifactService, fixedArtifactContext{runID: "run_search", sessionID: "session_search", callID: "call_search"})
	if err != nil {
		t.Fatalf("buildWebSearchTool: %v", err)
	}
	tool := mustToolByName(t, []einotool.BaseTool{searchTool}, "web_search")
	output, err := tool.InvokableRun(context.Background(), `{"query":"acorn","max_results":5}`)
	if err != nil {
		t.Fatalf("web_search: %v", err)
	}
	var decoded WebSearchOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode web_search output: %v", err)
	}
	if decoded.RawArtifactID == "" {
		t.Fatalf("missing raw artifact id: %+v", decoded)
	}
	if len(decoded.Results) != 1 || decoded.Results[0].URL != "https://example.com/docs" {
		t.Fatalf("results = %+v", decoded.Results)
	}
	if len(decoded.FilteredResults) != 1 || decoded.FilteredResults[0].Reason != "private_network" {
		t.Fatalf("filtered results = %+v", decoded.FilteredResults)
	}
	if len(store.records) != 1 {
		t.Fatalf("stored artifacts = %d, want 1", len(store.records))
	}
}

func TestBrowserToolFailsLoudlyWhenExecutableIsMissing(t *testing.T) {
	store := newToolArtifactStore()
	artifactService, err := corestore.NewArtifactService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("corestore.NewArtifactService: %v", err)
	}
	browserService, err := NewService(Config{
		Timeout: time.Second,
		Policy:  webaccess.URLPolicy{},
	})
	if err != nil {
		t.Fatalf("browser.NewService: %v", err)
	}
	browserTool, err := buildBrowserTool(browserService, artifactService, fixedArtifactContext{runID: "run_browser", sessionID: "session_browser", callID: "call_browser"})
	if err != nil {
		t.Fatalf("buildBrowserTool: %v", err)
	}
	tool := mustToolByName(t, []einotool.BaseTool{browserTool}, "browser")
	_, err = tool.InvokableRun(context.Background(), `{"action":"open","url":"http://93.184.216.34/"}`)
	if err == nil || !strings.Contains(err.Error(), "browser.executable_path is not configured") || !strings.Contains(err.Error(), "install Chrome/Chromium") {
		t.Fatalf("browser error = %v, want actionable missing executable_path", err)
	}
}

func TestAskOperatorCreatesPendingActionAndInterrupts(t *testing.T) {
	store, err := corestore.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), core.RunCreateParams{RunID: "run_ask_operator", Input: "choose path"}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	operatorTool, err := buildAskOperatorTool(store, fixedArtifactContext{runID: "run_ask_operator", sessionID: "session_ask_operator", callID: "call_question"})
	if err != nil {
		t.Fatalf("buildAskOperatorTool: %v", err)
	}

	tool := mustToolByName(t, []einotool.BaseTool{operatorTool}, "ask_operator")
	_, err = tool.InvokableRun(context.Background(), `{
		"title":"Choose path",
		"question":"Which path should Acorn take?",
		"options":[{"id":"fast","label":"Fast path"}],
		"allow_freeform":true
	}`)
	if err == nil {
		t.Fatal("ask_operator should interrupt")
	}
	if signal, ok := errors.AsType[*adk.InterruptSignal](err); !ok || signal == nil {
		t.Fatalf("expected interrupt info, got %v", err)
	}
	actions, err := store.ListPendingActions(context.Background(), 10)
	if err != nil {
		t.Fatalf("list pending actions: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("pending actions = %#v, want one", actions)
	}
	action := actions[0]
	if action.Kind != core.PendingActionKindOperatorQuestion || !strings.HasPrefix(action.ActionID, "action_") {
		t.Fatalf("pending action = %#v", action)
	}
	var payload core.OperatorQuestionPayload
	if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Question != "Which path should Acorn take?" || len(payload.Options) != 1 || payload.Options[0].ID != "fast" {
		t.Fatalf("payload = %#v", payload)
	}
	records, err := store.LoadEvents(context.Background(), "run_ask_operator")
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(records) != 1 || records[0].Kind != "operator_question.pending" {
		t.Fatalf("events = %#v", records)
	}
}

func mustToolByName(t *testing.T, tools []einotool.BaseTool, name string) einotool.InvokableTool {
	t.Helper()
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool.Info(%q): %v", name, err)
		}
		if info != nil && info.Name == name {
			invokable, ok := tool.(einotool.InvokableTool)
			if !ok {
				t.Fatalf("%s tool is not invokable", name)
			}
			return invokable
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func mustProgressToolByName(t *testing.T, baseTools []einotool.BaseTool, name string) ProgressTool {
	t.Helper()
	for _, tool := range baseTools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool.Info(%q): %v", name, err)
		}
		if info != nil && info.Name == name {
			progress, ok := tool.(ProgressTool)
			if !ok {
				t.Fatalf("%s tool is not progress-capable", name)
			}
			return progress
		}
	}
	t.Fatalf("tool %q not found", name)
	return nil
}

func testWorkspace(t *testing.T, root string) *workspacepkg.Workspace {
	t.Helper()
	return testWorkspaceWithConfig(t, workspacepkg.Config{
		RootDir:    root,
		StorageDir: t.TempDir(),
	})
}

func testWorkspaceWithConfig(t *testing.T, cfg workspacepkg.Config) *workspacepkg.Workspace {
	t.Helper()
	ws, err := workspacepkg.New(cfg)
	if err != nil {
		t.Fatalf("workspace.New: %v", err)
	}
	return ws
}

type fixedArtifactContext struct {
	runID     string
	sessionID string
	callID    string
}

func (c fixedArtifactContext) CurrentRunID(context.Context) string {
	return c.runID
}

func (c fixedArtifactContext) CurrentSessionID(context.Context) string {
	return c.sessionID
}

func (c fixedArtifactContext) CurrentToolCallID(context.Context) string {
	return c.callID
}

type toolArtifactStore struct {
	records map[string]core.ArtifactRecord
}

func newToolArtifactStore() *toolArtifactStore {
	return &toolArtifactStore{records: make(map[string]core.ArtifactRecord)}
}

func (s *toolArtifactStore) SaveArtifact(_ context.Context, record core.ArtifactRecord) (core.ArtifactRecord, error) {
	normalized, err := corestore.NormalizeArtifactRecord(record)
	if err != nil {
		return core.ArtifactRecord{}, err
	}
	s.records[normalized.ArtifactID] = normalized
	return normalized, nil
}

func (s *toolArtifactStore) LoadArtifact(_ context.Context, artifactID string) (core.ArtifactRecord, error) {
	record, ok := s.records[strings.TrimSpace(artifactID)]
	if !ok {
		return core.ArtifactRecord{}, core.ErrArtifactNotFound
	}
	return record, nil
}

func (s *toolArtifactStore) ListByRun(_ context.Context, runID string) ([]core.ArtifactRecord, error) {
	var items []core.ArtifactRecord
	for _, record := range s.records {
		if record.RunID == strings.TrimSpace(runID) {
			items = append(items, record)
		}
	}
	return items, nil
}

func (s *toolArtifactStore) ListBySession(_ context.Context, sessionID string) ([]core.ArtifactRecord, error) {
	var items []core.ArtifactRecord
	for _, record := range s.records {
		if record.SessionID == strings.TrimSpace(sessionID) {
			items = append(items, record)
		}
	}
	return items, nil
}

type toolWebFetchResolver map[string][]string

func (r toolWebFetchResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	values, ok := r[host]
	if !ok {
		return nil, errors.New("not found")
	}
	out := make([]net.IPAddr, 0, len(values))
	for _, value := range values {
		out = append(out, net.IPAddr{IP: net.ParseIP(value)})
	}
	return out, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
