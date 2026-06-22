package runtime

import (
	"encoding/json"
	"testing"
)

func TestExtractSemanticFact_FileRead(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "/tmp/config.yaml"})
	output := "server:\n  port: 8080\n  host: localhost\n"

	fact := ExtractSemanticFact("read_file", string(args), output)
	if fact == "" {
		t.Fatal("expected non-empty fact")
	}
	if !contains(fact, "/tmp/config.yaml") {
		t.Errorf("fact should mention file path, got: %s", fact)
	}
	if !contains(fact, "exists") {
		t.Errorf("fact should say file exists, got: %s", fact)
	}
	if !contains(fact, "server:") {
		t.Errorf("fact should include first line of output, got: %s", fact)
	}
}

func TestExtractSemanticFact_FileReadEmpty(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "/tmp/empty.txt"})
	fact := ExtractSemanticFact("read_file", string(args), "")
	if !contains(fact, "empty") {
		t.Errorf("fact should mention empty for zero-length output, got: %s", fact)
	}
}

func TestExtractSemanticFact_FileReadBadArgs(t *testing.T) {
	fact := ExtractSemanticFact("read_file", "not-json", "some output")
	if !contains(fact, "read_file result has invalid arguments") {
		t.Errorf("invalid argument summary expected, got: %s", fact)
	}
}

func TestExtractSemanticFact_FileWrite(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "/tmp/out.txt", "content": "hello world"})
	output, _ := json.Marshal(map[string]any{"bytes": 11, "path": "/tmp/out.txt"})

	fact := ExtractSemanticFact("create_file", string(args), string(output))
	if !contains(fact, "/tmp/out.txt") {
		t.Errorf("fact should mention file path, got: %s", fact)
	}
	if !contains(fact, "created") {
		t.Errorf("fact should say created, got: %s", fact)
	}
	if !contains(fact, "11") {
		t.Errorf("fact should include byte count from output, got: %s", fact)
	}
}

func TestExtractSemanticFact_FileWriteBadOutput(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"path": "/tmp/out.txt", "content": "hello"})
	fact := ExtractSemanticFact("create_file", string(args), "not-json-output")
	if !contains(fact, "/tmp/out.txt") {
		t.Errorf("fact should mention file path, got: %s", fact)
	}
	if !contains(fact, "5 bytes") {
		t.Errorf("fact should fall back to content length, got: %s", fact)
	}
}

func TestExtractSemanticFact_ShellExec(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"command": []string{"go", "test", "./..."}})
	output, _ := json.Marshal(map[string]any{"exit_code": 0, "stdout": "ok"})

	fact := ExtractSemanticFact("run_command", string(args), string(output))
	if !contains(fact, "go test ./...") {
		t.Errorf("fact should include command, got: %s", fact)
	}
	if !contains(fact, "exited 0") {
		t.Errorf("fact should include exit code, got: %s", fact)
	}
}

func TestExtractSemanticFact_ShellExecNonZero(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"command": []string{"ls", "/nonexistent"}})
	output, _ := json.Marshal(map[string]any{"exit_code": 1, "stderr": "no such file"})

	fact := ExtractSemanticFact("run_command", string(args), string(output))
	if !contains(fact, "exited 1") {
		t.Errorf("fact should include non-zero exit code, got: %s", fact)
	}
}

func TestExtractSemanticFact_ShellExecBadArgs(t *testing.T) {
	fact := ExtractSemanticFact("run_command", "bad-json", "output")
	if !contains(fact, "run_command result has invalid arguments") {
		t.Errorf("invalid argument summary expected, got: %s", fact)
	}
}

func TestExtractSemanticFact_UnknownTool(t *testing.T) {
	fact := ExtractSemanticFact("mcp_some_tool", `{"query":"x"}`, "result")
	if !contains(fact, "tool mcp_some_tool produced an unclassified result") {
		t.Errorf("unclassified result summary expected for unknown tool, got: %s", fact)
	}
}

func TestExtractSemanticFact_Truncation(t *testing.T) {
	longPath := ""
	for i := 0; i < 300; i++ {
		longPath += "x"
	}
	args, _ := json.Marshal(map[string]string{"path": longPath})
	fact := ExtractSemanticFact("read_file", string(args), "some content here")
	if len(fact) > maxFactLen {
		t.Errorf("fact should be truncated to max %d chars, got %d: %s", maxFactLen, len(fact), fact)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
