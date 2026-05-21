package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"

	"github.com/ycvk/acorn/internal/artifacts"
	"github.com/ycvk/acorn/internal/browser"
	"github.com/ycvk/acorn/internal/events"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/terminalsession"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/webaccess"
	workspacepkg "github.com/ycvk/acorn/internal/workspace"
)

func TestBuildCatalogIncludesReadOnlySuiteAndOptionalTools(t *testing.T) {
	ws := testWorkspace(t, t.TempDir())
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   false,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	if got, want := len(catalog.Tools), 7; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}
}

func TestBuildCatalogAllowsEmptyCatalog(t *testing.T) {
	catalog, err := BuildCatalog(CatalogConfig{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("build empty catalog: %v", err)
	}
	if len(catalog.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(catalog.Tools))
	}
}

func TestBuildCatalogAppendsExtraTools(t *testing.T) {
	extra, err := toolutils.InferTool("extra_tool", "extra tool", func(ctx context.Context, input map[string]any) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("build extra tool: %v", err)
	}

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace: testWorkspace(t, t.TempDir()),
	}, []einotool.BaseTool{extra}, nil, nil)
	if err != nil {
		t.Fatalf("build catalog with extra tools: %v", err)
	}
	if got, want := len(catalog.Tools), 7; got != want {
		t.Fatalf("expected %d tools, got %d", want, got)
	}
}

func TestReadFileReturnsStructuredLineRange(t *testing.T) {
	root := t.TempDir()
	body := "line 1\nline 2\nline 3\n"
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "read_file")

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
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "create_file")

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

func TestRollbackWorkspaceCheckpointRestoresMutationToolCheckpoint(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	createTool := mustToolByName(t, catalog.Tools, "create_file")
	rollbackTool := mustToolByName(t, catalog.Tools, "rollback_workspace_checkpoint")

	output, err := createTool.InvokableRun(context.Background(), `{"path":"notes.txt","content":"hello from acorn"}`)
	if err != nil {
		t.Fatalf("create_file: %v", err)
	}
	var created CreateFileOutput
	if err := json.Unmarshal([]byte(output), &created); err != nil {
		t.Fatalf("json.Unmarshal(create_file output): %v\noutput=%s", err, output)
	}

	rollbackOutput, err := rollbackTool.InvokableRun(context.Background(), `{"checkpoint_id":"`+created.CheckpointID+`"}`)
	if err != nil {
		t.Fatalf("rollback_workspace_checkpoint: %v", err)
	}
	var rolledBack RollbackWorkspaceCheckpointOutput
	if err := json.Unmarshal([]byte(rollbackOutput), &rolledBack); err != nil {
		t.Fatalf("json.Unmarshal(rollback output): %v\noutput=%s", err, rollbackOutput)
	}
	if rolledBack.Status != "succeeded" || strings.Join(rolledBack.RestoredPaths, ",") != "notes.txt" {
		t.Fatalf("unexpected rollback output: %+v", rolledBack)
	}
	if _, err := os.Stat(filepath.Join(root, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("notes.txt still exists or stat failed: %v", err)
	}
}

func TestMultiEditWritesMultipleFilesWithOneCheckpoint(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	runGitCommandForTest(t, root, "add", "a.txt", "b.txt")
	runGitCommandForTest(t, root, "commit", "-m", "fixtures")
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "multi_edit")

	output, err := tool.InvokableRun(context.Background(), `{"edits":[
		{"path":"a.txt","start_line":2,"end_line":2,"replacement":"TWO\n"},
		{"path":"b.txt","start_line":1,"end_line":2,"replacement":"ALPHA-BETA\n"}
	]}`)
	if err != nil {
		t.Fatalf("multi_edit: %v", err)
	}

	var decoded MultiEditOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(multi_edit output): %v\noutput=%s", err, output)
	}
	if decoded.CheckpointID == "" {
		t.Fatal("CheckpointID is required")
	}
	if strings.Join(decoded.CheckpointPaths, ",") != "a.txt,b.txt" {
		t.Fatalf("CheckpointPaths = %+v", decoded.CheckpointPaths)
	}
	if !strings.Contains(decoded.VerifiedDiffStat, "a.txt") || !strings.Contains(decoded.VerifiedDiffStat, "b.txt") {
		t.Fatalf("VerifiedDiffStat = %q, want both paths", decoded.VerifiedDiffStat)
	}
	bodyA, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	bodyB, err := os.ReadFile(filepath.Join(root, "b.txt"))
	if err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	if string(bodyA) != "one\nTWO\nthree\n" {
		t.Fatalf("a.txt = %q", string(bodyA))
	}
	if string(bodyB) != "ALPHA-BETA\ngamma\n" {
		t.Fatalf("b.txt = %q", string(bodyB))
	}
}

func TestMultiEditRejectsOverlappingSpansBeforeWriting(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	original := "one\ntwo\nthree\n"
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte(original), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       ws,
		MutationEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "multi_edit")

	_, err = tool.InvokableRun(context.Background(), `{"edits":[
		{"path":"a.txt","start_line":1,"end_line":2,"replacement":"first\n"},
		{"path":"a.txt","start_line":2,"end_line":3,"replacement":"second\n"}
	]}`)
	if err == nil {
		t.Fatal("multi_edit should reject overlapping spans")
	}
	body, readErr := os.ReadFile(filepath.Join(root, "a.txt"))
	if readErr != nil {
		t.Fatalf("read a.txt: %v", readErr)
	}
	if string(body) != original {
		t.Fatalf("a.txt mutated after rejected multi_edit: %q", string(body))
	}
}

func TestSearchTextReturnsStructuredMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("beta\ngamma\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "search_text")

	output, err := tool.InvokableRun(context.Background(), `{"query":"beta","limit":10}`)
	if err != nil {
		t.Fatalf("search_text: %v", err)
	}
	var decoded SearchTextOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(search_text output): %v\noutput=%s", err, output)
	}
	if len(decoded.Matches) != 2 {
		t.Fatalf("match count = %d, want 2", len(decoded.Matches))
	}
}

func TestSearchTextEmitsMatchProgress(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustProgressToolByName(t, catalog.Tools, "search_text")

	var chunks []string
	_, err = tool.InvokableRunWithProgress(context.Background(), `{"query":"beta","limit":10}`, func(_ context.Context, event tooling.ToolProgressEvent) error {
		chunks = append(chunks, event.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("search_text: %v", err)
	}
	if got := strings.Join(chunks, "\n"); !strings.Contains(got, "a.txt:2:1 beta") {
		t.Fatalf("progress chunks = %#v, want match location", chunks)
	}
}

func TestNativeWorkspaceToolsExposeProgressInterface(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), newToolArtifactStore())
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		MutationEnabled:   true,
		RunCommandEnabled: true,
		ArtifactService:   artifactService,
		ArtifactContext:   fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_1"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	for _, name := range []string{
		"read_file",
		"list_files",
		"search_text",
		"git_summary",
		"create_file",
		"replace_span",
		"apply_unified_patch",
		"multi_edit",
		"rollback_workspace_checkpoint",
		"run_command",
		"run_verification",
	} {
		mustProgressToolByName(t, catalog.Tools, name)
	}
}

func TestArtifactToolsWriteReadAndList(t *testing.T) {
	store := newToolArtifactStore()
	service, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	catalog, err := BuildCatalog(CatalogConfig{
		ArtifactService: service,
		ArtifactContext: fixedArtifactContext{
			runID:     "run_1",
			sessionID: "session_1",
			callID:    "call_1",
		},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	if got, want := len(catalog.Tools), 3; got != want {
		t.Fatalf("artifact tool count = %d, want %d", got, want)
	}

	writeTool := mustToolByName(t, catalog.Tools, "artifact_write")
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

	readTool := mustToolByName(t, catalog.Tools, "artifact_read")
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

	listTool := mustToolByName(t, catalog.Tools, "artifact_list")
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
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
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
	catalog, err := BuildCatalog(CatalogConfig{
		ArtifactService: artifactService,
		ArtifactContext: fixedArtifactContext{runID: "run_web", sessionID: "session_web", callID: "call_web"},
		WebFetchService: fetchService,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "web_fetch")
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
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
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
	catalog, err := BuildCatalog(CatalogConfig{
		ArtifactService:  artifactService,
		ArtifactContext:  fixedArtifactContext{runID: "run_search", sessionID: "session_search", callID: "call_search"},
		WebSearchService: searchService,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "web_search")
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
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	browserService, err := browser.NewService(browser.Config{
		Timeout: time.Second,
		Policy:  webaccess.URLPolicy{},
	})
	if err != nil {
		t.Fatalf("browser.NewService: %v", err)
	}
	catalog, err := BuildCatalog(CatalogConfig{
		ArtifactService: artifactService,
		ArtifactContext: fixedArtifactContext{runID: "run_browser", sessionID: "session_browser", callID: "call_browser"},
		BrowserService:  browserService,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "browser")
	_, err = tool.InvokableRun(context.Background(), `{"action":"open","url":"http://93.184.216.34/"}`)
	if err == nil || !strings.Contains(err.Error(), "browser executable_path is not configured") {
		t.Fatalf("browser error = %v, want missing executable_path", err)
	}
}

func TestAskOperatorCreatesPendingActionAndInterrupts(t *testing.T) {
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.CreateRun(context.Background(), "run_ask_operator", "choose path", "run_ask_operator"); err != nil {
		t.Fatalf("create run: %v", err)
	}
	catalog, err := BuildCatalog(CatalogConfig{
		OperatorStore:   store,
		OperatorContext: fixedArtifactContext{runID: "run_ask_operator", sessionID: "session_ask_operator", callID: "call_question"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	tool := mustToolByName(t, catalog.Tools, "ask_operator")
	_, err = tool.InvokableRun(context.Background(), `{
		"title":"Choose path",
		"question":"Which path should Acorn take?",
		"options":[{"id":"fast","label":"Fast path"}],
		"allow_freeform":true
	}`)
	if err == nil {
		t.Fatal("ask_operator should interrupt")
	}
	var signal *adk.InterruptSignal
	if !errors.As(err, &signal) || signal == nil {
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
	if action.Kind != events.PendingActionKindOperatorQuestion || action.ActionID != "operator_question:run_ask_operator:call_question" {
		t.Fatalf("pending action = %#v", action)
	}
	var payload events.OperatorQuestionPayload
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

func TestTerminalSessionToolsStartReadAndList(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required")
	}
	root := t.TempDir()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), store)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	terminalService, err := terminalsession.NewService(store, artifactService)
	if err != nil {
		t.Fatalf("terminalsession.NewService: %v", err)
	}
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         testWorkspace(t, root),
		TerminalService:   terminalService,
		TerminalContext:   fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_start"},
		ArtifactService:   artifactService,
		ArtifactContext:   fixedArtifactContext{runID: "run_1", sessionID: "session_1", callID: "call_artifact"},
		MutationEnabled:   false,
		RunCommandEnabled: false,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}

	startTool := mustToolByName(t, catalog.Tools, "terminal_session_start")
	startOutput, err := startTool.InvokableRun(context.Background(), `{"command":["sh","-c","printf terminal"],"label":"fixture"}`)
	if err != nil {
		t.Fatalf("terminal_session_start: %v", err)
	}
	var started TerminalSessionStartOutput
	if err := json.Unmarshal([]byte(startOutput), &started); err != nil {
		t.Fatalf("json.Unmarshal(start output): %v\noutput=%s", err, startOutput)
	}
	if started.TerminalSessionID == "" || started.Status != "running" {
		t.Fatalf("unexpected start output: %+v", started)
	}

	statusTool := mustToolByName(t, catalog.Tools, "process_status")
	final := waitTerminalToolStatus(t, statusTool, started.TerminalSessionID)
	if final.Status != "exited" || final.ExitCode == nil || *final.ExitCode != 0 {
		t.Fatalf("unexpected final status: %+v", final)
	}

	readTool := mustToolByName(t, catalog.Tools, "terminal_session_read")
	readOutput, err := readTool.InvokableRun(context.Background(), `{"terminal_session_id":"`+started.TerminalSessionID+`","stream":"stdout","offset":0,"limit":64}`)
	if err != nil {
		t.Fatalf("terminal_session_read: %v", err)
	}
	var read TerminalSessionReadOutput
	if err := json.Unmarshal([]byte(readOutput), &read); err != nil {
		t.Fatalf("json.Unmarshal(read output): %v\noutput=%s", err, readOutput)
	}
	if read.Content != "terminal" || read.ArtifactID == "" {
		t.Fatalf("unexpected read output: %+v", read)
	}

	listTool := mustToolByName(t, catalog.Tools, "terminal_session_list")
	listOutput, err := listTool.InvokableRun(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("terminal_session_list: %v", err)
	}
	var list TerminalSessionListOutput
	if err := json.Unmarshal([]byte(listOutput), &list); err != nil {
		t.Fatalf("json.Unmarshal(list output): %v\noutput=%s", err, listOutput)
	}
	if len(list.Items) != 1 || list.Items[0].TerminalSessionID != started.TerminalSessionID {
		t.Fatalf("unexpected list output: %+v", list)
	}
}

func TestInspectGitStatusReturnsStructuredOutput(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	runGitCommandForTest(t, root, "init")
	runGitCommandForTest(t, root, "config", "user.name", "Acorn Test")
	runGitCommandForTest(t, root, "config", "user.email", "acorn@example.com")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitCommandForTest(t, root, "add", "tracked.txt")
	runGitCommandForTest(t, root, "commit", "-m", "seed")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "new.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{Workspace: ws}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "inspect_git_status")

	output, err := tool.InvokableRun(context.Background(), `{"path":"nested/new.txt"}`)
	if err != nil {
		t.Fatalf("inspect_git_status: %v", err)
	}

	var decoded InspectGitStatusOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(inspect_git_status output): %v\noutput=%s", err, output)
	}
	if decoded.RootPath != root {
		t.Fatalf("RootPath = %q, want %q", decoded.RootPath, root)
	}
	if decoded.Clean {
		t.Fatalf("Clean = true, want false")
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(decoded.Entries))
	}
	if decoded.Entries[0].Path != "nested/new.txt" {
		t.Fatalf("entry path = %q, want nested/new.txt", decoded.Entries[0].Path)
	}
}

func TestGitSummaryReturnsStatusDiffStatAndDiffArtifact(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitCommandForTest(t, root, "add", "tracked.txt")
	runGitCommandForTest(t, root, "commit", "-m", "tracked")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("seed\nchanged\n"), 0o644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}
	artifactStore := newToolArtifactStore()
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), artifactStore)
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:       testWorkspace(t, root),
		ArtifactService: artifactService,
		ArtifactContext: fixedArtifactContext{runID: "run_git", sessionID: "session_git", callID: "call_git"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "git_summary")

	output, err := tool.InvokableRun(context.Background(), `{"include_diff":true,"context_lines":1}`)
	if err != nil {
		t.Fatalf("git_summary: %v", err)
	}
	var decoded GitSummaryOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(git_summary output): %v\noutput=%s", err, output)
	}
	if decoded.Clean {
		t.Fatal("Clean = true, want false")
	}
	if strings.Join(decoded.ChangedPaths, ",") != "tracked.txt" {
		t.Fatalf("ChangedPaths = %+v", decoded.ChangedPaths)
	}
	if !strings.Contains(decoded.DiffStat, "tracked.txt") {
		t.Fatalf("DiffStat = %q, want tracked.txt", decoded.DiffStat)
	}
	if decoded.DiffArtifactID == "" || decoded.DiffArtifact == nil {
		t.Fatalf("diff artifact missing: %+v", decoded)
	}
	read, err := artifactService.ReadRange(context.Background(), artifacts.ReadRangeRequest{
		ArtifactID: decoded.DiffArtifactID,
		Limit:      4096,
	})
	if err != nil {
		t.Fatalf("read diff artifact: %v", err)
	}
	if !strings.Contains(string(read.Content), "+changed") {
		t.Fatalf("diff artifact content = %q", string(read.Content))
	}
}

func TestRunVerificationWritesArtifactsAndKeepsFailureAsResult(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is required")
	}
	root := t.TempDir()
	artifactService, err := artifacts.NewService(filepath.Join(t.TempDir(), "artifacts"), newToolArtifactStore())
	if err != nil {
		t.Fatalf("artifacts.NewService: %v", err)
	}
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         testWorkspace(t, root),
		RunCommandEnabled: true,
		ArtifactService:   artifactService,
		ArtifactContext:   fixedArtifactContext{runID: "run_verify", sessionID: "session_verify", callID: "call_verify"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_verification")

	output, err := tool.InvokableRun(context.Background(), `{"kind":"test","command":["sh","-lc","printf out; printf err 1>&2; exit 7"]}`)
	if err != nil {
		t.Fatalf("run_verification: %v", err)
	}
	var decoded RunVerificationOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_verification output): %v\noutput=%s", err, output)
	}
	if decoded.Status != verificationStatusFailed || decoded.ExitCode != 7 {
		t.Fatalf("status/exit = %s/%d, want failed/7", decoded.Status, decoded.ExitCode)
	}
	stdout, err := artifactService.ReadRange(context.Background(), artifacts.ReadRangeRequest{
		ArtifactID: decoded.StdoutArtifactID,
		Limit:      32,
	})
	if err != nil {
		t.Fatalf("read stdout artifact: %v", err)
	}
	stderr, err := artifactService.ReadRange(context.Background(), artifacts.ReadRangeRequest{
		ArtifactID: decoded.StderrArtifactID,
		Limit:      32,
	})
	if err != nil {
		t.Fatalf("read stderr artifact: %v", err)
	}
	if string(stdout.Content) != "out" || string(stderr.Content) != "err" {
		t.Fatalf("artifact content stdout=%q stderr=%q", string(stdout.Content), string(stderr.Content))
	}
}

func TestRunCommandReturnsExactFailureTruth(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["sh","-lc","printf hi && printf err 1>&2; exit 7"]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", decoded.ExitCode)
	}
	if decoded.Stdout != "hi" {
		t.Fatalf("Stdout = %q, want hi", decoded.Stdout)
	}
	if strings.TrimSpace(decoded.Stderr) != "err" {
		t.Fatalf("Stderr = %q, want err", decoded.Stderr)
	}
	if decoded.Cwd != root {
		t.Fatalf("Cwd = %q, want %q", decoded.Cwd, root)
	}
}

func TestRunCommandDoesNotRequireCommandNameList(t *testing.T) {
	root := t.TempDir()
	initGitRepoForToolsTest(t, root)
	ws := testWorkspaceWithConfig(t, workspacepkg.Config{
		RootDir:                  root,
		StorageDir:               t.TempDir(),
		RunCommandDefaultTimeout: 5,
	})

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["git","status","--short"]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", decoded.ExitCode)
	}
	if decoded.Cwd != root {
		t.Fatalf("Cwd = %q, want %q", decoded.Cwd, root)
	}
}

func TestRunCommandEmitsProgressChunks(t *testing.T) {
	root := t.TempDir()
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustProgressToolByName(t, catalog.Tools, "run_command")

	var mu sync.Mutex
	var chunks []string
	output, err := tool.InvokableRunWithProgress(context.Background(), `{"command":["sh","-lc","printf out; printf err 1>&2"]}`, func(_ context.Context, event tooling.ToolProgressEvent) error {
		mu.Lock()
		defer mu.Unlock()
		chunks = append(chunks, event.Delta)
		return nil
	})
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	mu.Lock()
	progressText := strings.Join(chunks, "")
	mu.Unlock()
	if !strings.Contains(progressText, "out") || !strings.Contains(progressText, "err") {
		t.Fatalf("progress chunks = %#v, want stdout and stderr", chunks)
	}
	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.Stdout != "out" || decoded.Stderr != "err" {
		t.Fatalf("output = stdout:%q stderr:%q, want out/err", decoded.Stdout, decoded.Stderr)
	}
}

func TestRunCommandCancellationKillsProcessGroup(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("process-group cancellation test only runs on darwin/linux")
	}

	root := t.TempDir()
	ws := testWorkspace(t, root)
	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")
	pidFile := filepath.Join(root, "child.pid")

	_, err = tool.InvokableRun(context.Background(), `{"command":["sh","-lc","sleep 30 & child=$!; echo $child > child.pid; wait $child"],"timeout_seconds":1}`)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("timeout error = %v, want deadline exceeded", err)
	}

	body, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		probeErr := syscall.Kill(childPID, 0)
		if errors.Is(probeErr, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d is still running after run_command cancellation", childPID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRunCommandUsesWhitelistedEnvOnly(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ACORN_ALLOWED", "visible")
	t.Setenv("ACORN_BLOCKED", "hidden")
	ws := testWorkspaceWithConfig(t, workspacepkg.Config{
		RootDir:                  root,
		StorageDir:               t.TempDir(),
		RunCommandDefaultTimeout: 5,
		RunCommandEnvWhitelist:   []string{"ACORN_ALLOWED"},
	})

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["sh","-lc","printf \"%s|%s\" \"$ACORN_ALLOWED\" \"$ACORN_BLOCKED\""]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.Stdout != "visible|" {
		t.Fatalf("Stdout = %q, want visible|", decoded.Stdout)
	}
}

func TestRunCommandKeepsInheritedEnvWhenWhitelistEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ACORN_VISIBLE_WITHOUT_FILTER", "present")
	ws := testWorkspace(t, root)

	catalog, err := BuildCatalog(CatalogConfig{
		Workspace:         ws,
		RunCommandEnabled: true,
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("BuildCatalog: %v", err)
	}
	tool := mustToolByName(t, catalog.Tools, "run_command")

	output, err := tool.InvokableRun(context.Background(), `{"command":["sh","-lc","printf \"%s\" \"$ACORN_VISIBLE_WITHOUT_FILTER\""]}`)
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}

	var decoded RunCommandOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("json.Unmarshal(run_command output): %v\noutput=%s", err, output)
	}
	if decoded.Stdout != "present" {
		t.Fatalf("Stdout = %q, want present", decoded.Stdout)
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

func mustProgressToolByName(t *testing.T, tools []einotool.BaseTool, name string) tooling.ProgressTool {
	t.Helper()
	for _, tool := range tools {
		info, err := tool.Info(context.Background())
		if err != nil {
			t.Fatalf("tool.Info(%q): %v", name, err)
		}
		if info != nil && info.Name == name {
			progress, ok := tool.(tooling.ProgressTool)
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
		RootDir:                  root,
		StorageDir:               t.TempDir(),
		RunCommandDefaultTimeout: 5,
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

func initGitRepoForToolsTest(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	runGitCommandForTest(t, root, "init")
	runGitCommandForTest(t, root, "config", "user.name", "Acorn Test")
	runGitCommandForTest(t, root, "config", "user.email", "acorn@example.com")
	if err := os.WriteFile(filepath.Join(root, ".gitkeep"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write .gitkeep: %v", err)
	}
	runGitCommandForTest(t, root, "add", ".gitkeep")
	runGitCommandForTest(t, root, "commit", "-m", "seed")
}

func runGitCommandForTest(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(output))
	}
}

func waitTerminalToolStatus(t *testing.T, statusTool einotool.InvokableTool, terminalSessionID string) ProcessStatusOutput {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		output, err := statusTool.InvokableRun(context.Background(), `{"terminal_session_id":"`+terminalSessionID+`"}`)
		if err != nil {
			t.Fatalf("process_status: %v", err)
		}
		var decoded ProcessStatusOutput
		if err := json.Unmarshal([]byte(output), &decoded); err != nil {
			t.Fatalf("json.Unmarshal(process_status output): %v\noutput=%s", err, output)
		}
		switch decoded.Status {
		case "exited", "signaled", "failed", "closed":
			return decoded
		}
		if time.Now().After(deadline) {
			t.Fatalf("terminal session %s still %s", terminalSessionID, decoded.Status)
		}
		time.Sleep(25 * time.Millisecond)
	}
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
	records map[string]artifacts.Record
}

func newToolArtifactStore() *toolArtifactStore {
	return &toolArtifactStore{records: make(map[string]artifacts.Record)}
}

func (s *toolArtifactStore) SaveArtifact(_ context.Context, record artifacts.Record) (artifacts.Record, error) {
	normalized, err := artifacts.NormalizeRecord(record)
	if err != nil {
		return artifacts.Record{}, err
	}
	s.records[normalized.ArtifactID] = normalized
	return normalized, nil
}

func (s *toolArtifactStore) LoadArtifact(_ context.Context, artifactID string) (artifacts.Record, error) {
	record, ok := s.records[strings.TrimSpace(artifactID)]
	if !ok {
		return artifacts.Record{}, artifacts.ErrArtifactNotFound
	}
	return record, nil
}

func (s *toolArtifactStore) ListArtifactsByRun(_ context.Context, runID string) ([]artifacts.Record, error) {
	var items []artifacts.Record
	for _, record := range s.records {
		if record.RunID == strings.TrimSpace(runID) {
			items = append(items, record)
		}
	}
	return items, nil
}

func (s *toolArtifactStore) ListArtifactsBySession(_ context.Context, sessionID string) ([]artifacts.Record, error) {
	var items []artifacts.Record
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
