package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallerConfigTemplateParses guards against drift between the installer's
// embedded config heredoc (scripts/install-release.sh write_config_template) and
// the config schema. The installer writes that YAML verbatim; if a struct field is
// renamed/removed the strict-decode Load here fails loudly in CI instead of
// shipping an installer that writes an unloadable config. (The embedded acorn.init
// template has its own TestInitTemplateIsValidAndExecutionReady guard.)
func TestInstallerConfigTemplateParses(t *testing.T) {
	body := extractInstallerConfigHeredoc(t, filepath.Join("..", "..", "scripts", "install-release.sh"))
	dir := t.TempDir()
	path := filepath.Join(dir, "acorn.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write extracted config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("installer config heredoc must load against the current schema (drift?): %v", err)
	}
}

// extractInstallerConfigHeredoc returns the body of the single-quoted heredoc
// (`<<'EOF'`) that write_config_template emits — distinct from the systemd unit's
// unquoted `<<EOF`.
func extractInstallerConfigHeredoc(t *testing.T, script string) string {
	t.Helper()
	f, err := os.Open(script)
	if err != nil {
		t.Fatalf("open installer script: %v", err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	inBlock := false
	found := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !inBlock {
			if strings.Contains(line, "<<'EOF'") {
				inBlock = true
				found = true
			}
			continue
		}
		if strings.TrimSpace(line) == "EOF" {
			break
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan installer script: %v", err)
	}
	if !found || len(lines) == 0 {
		t.Fatal("no <<'EOF' config heredoc found in installer script")
	}
	return strings.Join(lines, "\n") + "\n"
}
